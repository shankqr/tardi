package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/shanq/tardi/internal/api/middleware"
	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/sshexec"
)

const (
	terminalTicketTTL          = 30 * time.Second
	terminalSessionMaxDuration = 60 * time.Minute
	terminalMaxPerUser         = 2
	terminalReadDeadline       = 60 * time.Second
	terminalPingInterval       = 20 * time.Second
	terminalWriteRateBytes     = 5 * 1024 * 1024 // 5 MB/min browser → VPS cap
	terminalWriteRateWindow    = time.Minute
)

// terminalSessions counts concurrent terminal sessions per user UUID.
// Map value is *int32 manipulated with atomic ops.
var terminalSessions sync.Map

func incrementTerminalSession(userID uuid.UUID) (int32, func()) {
	v, _ := terminalSessions.LoadOrStore(userID, new(int32))
	counter := v.(*int32)
	n := atomic.AddInt32(counter, 1)
	return n, func() {
		atomic.AddInt32(counter, -1)
	}
}

// TerminalTicketHandler returns POST /api/instances/{id}/terminal/ticket.
// It mints a short-lived HMAC-signed ticket the browser uses to open the
// WebSocket terminal connection. The ticket binds the user, instance, and
// expiry so the unauthed WS endpoint can verify both ownership and freshness
// without holding any per-request server state.
func TerminalTicketHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
			return
		}

		if len(deps.Config.TerminalTicketSecret) == 0 {
			WriteError(w, http.StatusServiceUnavailable, "not_configured", "web terminal is not enabled")
			return
		}

		instanceID, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "bad_request", "invalid instance id")
			return
		}

		inst, err := db.GetInstanceByID(r.Context(), deps.Pool, instanceID, user.ID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				WriteError(w, http.StatusNotFound, "not_found", "instance not found")
				return
			}
			slog.Error("terminal ticket: get instance", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get instance")
			return
		}

		if inst.Status != "active" {
			WriteError(w, http.StatusConflict, "instance_not_ready", "agent is not active")
			return
		}
		if inst.IPv4 == nil || *inst.IPv4 == "" {
			WriteError(w, http.StatusConflict, "instance_not_ready", "agent has no IP address")
			return
		}

		ticket := signTerminalTicket(deps.Config.TerminalTicketSecret, user.ID, instanceID, time.Now().Add(terminalTicketTTL))
		WriteJSON(w, http.StatusOK, map[string]any{"ticket": ticket})
	}
}

// TerminalWebSocketHandler returns GET /api/instances/{id}/terminal/ws.
// Browsers cannot set custom headers on the WebSocket constructor, so we
// authenticate via a short-lived HMAC ticket passed as ?t=. After verifying
// the ticket we re-check ownership against the DB (state may have changed
// between mint and connect), upgrade to a WebSocket, and bridge it to a PTY
// shell over SSH on the user's VPS.
func TerminalWebSocketHandler(deps Dependencies) http.HandlerFunc {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin:     terminalOriginCheck(deps.Config.AllowedOrigins),
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if len(deps.Config.TerminalTicketSecret) == 0 {
			WriteError(w, http.StatusServiceUnavailable, "not_configured", "web terminal is not enabled")
			return
		}

		instanceID, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "bad_request", "invalid instance id")
			return
		}

		ticket := r.URL.Query().Get("t")
		userID, signedInstance, err := verifyTerminalTicket(deps.Config.TerminalTicketSecret, ticket)
		if err != nil {
			WriteError(w, http.StatusUnauthorized, "invalid_ticket", "ticket is invalid or expired")
			return
		}
		if signedInstance != instanceID {
			WriteError(w, http.StatusUnauthorized, "invalid_ticket", "ticket does not match instance")
			return
		}

		inst, err := db.GetInstanceByID(r.Context(), deps.Pool, instanceID, userID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				WriteError(w, http.StatusNotFound, "not_found", "instance not found")
				return
			}
			slog.Error("terminal ws: get instance", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get instance")
			return
		}
		if inst.Status != "active" || inst.IPv4 == nil || *inst.IPv4 == "" {
			WriteError(w, http.StatusConflict, "instance_not_ready", "agent is not ready")
			return
		}

		// Per-user concurrency cap. Increment first, decrement on exit.
		count, decrement := incrementTerminalSession(userID)
		defer decrement()
		if count > terminalMaxPerUser {
			WriteError(w, http.StatusTooManyRequests, "too_many_sessions", "too many concurrent terminal sessions")
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Warn("terminal ws: upgrade failed", "error", err)
			return
		}
		defer conn.Close()

		var rootPassword string
		if inst.RootPassword != nil {
			rootPassword = *inst.RootPassword
		}

		shell, err := sshexec.OpenShell(*inst.IPv4, deps.Config.SSHPrivateKey, rootPassword, 80, 24)
		if err != nil {
			slog.Warn("terminal ws: open shell", "error", err, "instance_id", instanceID)
			_ = conn.WriteJSON(map[string]any{"type": "error", "message": "failed to open shell: " + err.Error()})
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "ssh dial failed"), time.Now().Add(2*time.Second))
			return
		}
		defer shell.Close()

		runTerminalSession(r.Context(), conn, shell, userID, instanceID)
	}
}

// runTerminalSession bridges a WebSocket connection to an SSH PTY. It runs
// until the client disconnects, the SSH session ends, or the hard cap fires.
func runTerminalSession(parent context.Context, conn *websocket.Conn, shell *sshexec.Shell, userID, instanceID uuid.UUID) {
	ctx, cancel := context.WithTimeout(parent, terminalSessionMaxDuration)
	defer cancel()

	startedAt := time.Now()
	var bytesIn, bytesOut int64

	slog.Info("terminal session", "event", "open", "user_id", userID, "instance_id", instanceID)
	defer func() {
		slog.Info("terminal session", "event", "close",
			"user_id", userID,
			"instance_id", instanceID,
			"duration_ms", time.Since(startedAt).Milliseconds(),
			"bytes_in", bytesIn,
			"bytes_out", bytesOut,
		)
	}()

	// Pong handler refreshes the read deadline.
	_ = conn.SetReadDeadline(time.Now().Add(terminalReadDeadline))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(terminalReadDeadline))
	})

	// Writer mutex — both the read pump and the ping ticker write to the conn.
	var writeMu sync.Mutex
	writeBinary := func(data []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteMessage(websocket.BinaryMessage, data)
	}
	writePing := func() error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
	}

	done := make(chan struct{})
	var once sync.Once
	closeDone := func() { once.Do(func() { close(done) }) }

	// SSH → WebSocket pump.
	go func() {
		defer closeDone()
		buf := make([]byte, 4096)
		for {
			n, err := shell.Stdout.Read(buf)
			if n > 0 {
				if werr := writeBinary(buf[:n]); werr != nil {
					return
				}
				atomic.AddInt64(&bytesOut, int64(n))
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					slog.Debug("terminal: shell read", "error", err, "instance_id", instanceID)
				}
				return
			}
		}
	}()

	// Ping ticker.
	go func() {
		ticker := time.NewTicker(terminalPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := writePing(); err != nil {
					return
				}
			case <-done:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	// WebSocket → SSH pump (runs in caller goroutine).
	rateBucket := newRateBucket(terminalWriteRateBytes, terminalWriteRateWindow)
	for {
		if ctx.Err() != nil {
			return
		}
		mt, data, err := conn.ReadMessage()
		if err != nil {
			closeDone()
			return
		}
		switch mt {
		case websocket.BinaryMessage:
			if !rateBucket.allow(len(data)) {
				_ = writeBinary([]byte("\r\n[tardi] write rate limit exceeded; dropping input\r\n"))
				continue
			}
			if _, werr := shell.Stdin.Write(data); werr != nil {
				closeDone()
				return
			}
			atomic.AddInt64(&bytesIn, int64(len(data)))
		case websocket.TextMessage:
			handleTerminalControl(shell, data)
		}
	}
}

func handleTerminalControl(shell *sshexec.Shell, data []byte) {
	var msg struct {
		Type string `json:"type"`
		Cols int    `json:"cols"`
		Rows int    `json:"rows"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}
	if msg.Type == "resize" && msg.Cols > 0 && msg.Rows > 0 {
		_ = shell.Resize(msg.Cols, msg.Rows)
	}
}

// terminalOriginCheck returns a CheckOrigin function that accepts only the
// configured allowed origins. An empty allow-list rejects all origins.
func terminalOriginCheck(allowed []string) func(*http.Request) bool {
	allowSet := make(map[string]struct{}, len(allowed))
	for _, o := range allowed {
		allowSet[strings.TrimSpace(o)] = struct{}{}
	}
	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return false
		}
		_, ok := allowSet[origin]
		return ok
	}
}

// signTerminalTicket returns a ticket of the form "<userID>.<instanceID>.<expUnix>.<sig>"
// where sig is base64url(HMAC-SHA256(secret, payload)).
func signTerminalTicket(secret []byte, userID, instanceID uuid.UUID, exp time.Time) string {
	payload := fmt.Sprintf("%s.%s.%d", userID.String(), instanceID.String(), exp.Unix())
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + sig
}

func verifyTerminalTicket(secret []byte, ticket string) (uuid.UUID, uuid.UUID, error) {
	parts := strings.Split(ticket, ".")
	if len(parts) != 4 {
		return uuid.Nil, uuid.Nil, errors.New("malformed ticket")
	}
	userID, err := uuid.Parse(parts[0])
	if err != nil {
		return uuid.Nil, uuid.Nil, errors.New("bad user id")
	}
	instanceID, err := uuid.Parse(parts[1])
	if err != nil {
		return uuid.Nil, uuid.Nil, errors.New("bad instance id")
	}
	expUnix, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return uuid.Nil, uuid.Nil, errors.New("bad expiry")
	}
	if time.Now().Unix() > expUnix {
		return uuid.Nil, uuid.Nil, errors.New("expired")
	}

	payload := parts[0] + "." + parts[1] + "." + parts[2]
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[3])) {
		return uuid.Nil, uuid.Nil, errors.New("bad signature")
	}
	return userID, instanceID, nil
}

// rateBucket is a simple token-bucket-style counter used to cap browser→VPS
// write bandwidth and prevent a runaway paste from blowing through Cloud Run
// egress. It is not thread-safe and is owned by a single goroutine.
type rateBucket struct {
	limit    int
	window   time.Duration
	used     int
	resetAt  time.Time
}

func newRateBucket(limit int, window time.Duration) *rateBucket {
	return &rateBucket{limit: limit, window: window, resetAt: time.Now().Add(window)}
}

func (b *rateBucket) allow(n int) bool {
	now := time.Now()
	if now.After(b.resetAt) {
		b.used = 0
		b.resetAt = now.Add(b.window)
	}
	if b.used+n > b.limit {
		return false
	}
	b.used += n
	return true
}

