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
	"net"
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
	"github.com/shanq/tardi/internal/models"
	"github.com/shanq/tardi/internal/sshexec"
)

const (
	desktopTicketTTL          = 45 * time.Second
	desktopSessionMaxDuration = 2 * time.Hour
	desktopMaxPerUser         = 2
	desktopReadDeadline       = 60 * time.Second
	desktopPingInterval       = 20 * time.Second
	desktopWriteRateBytes     = 10 * 1024 * 1024 // 10 MB/min browser -> VPS cap
	desktopWriteRateWindow    = time.Minute
	desktopVNCAddress         = "127.0.0.1:5901"
)

var desktopSessions sync.Map

func incrementDesktopSession(userID uuid.UUID) (int32, func()) {
	v, _ := desktopSessions.LoadOrStore(userID, new(int32))
	counter := v.(*int32)
	n := atomic.AddInt32(counter, 1)
	return n, func() {
		atomic.AddInt32(counter, -1)
	}
}

type desktopSessionRequest struct {
	LaunchTradingView bool   `json:"launch_tradingview"`
	Symbol            string `json:"symbol"`
}

// DesktopSessionHandler prepares the private VPS desktop and returns a
// short-lived ticket for the noVNC WebSocket. VNC itself remains bound to
// localhost on the VPS; browser traffic reaches it only through this backend
// ticket + SSH tunnel.
func DesktopSessionHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
			return
		}

		if len(deps.Config.TerminalTicketSecret) == 0 {
			WriteError(w, http.StatusServiceUnavailable, "not_configured", "remote desktop is not enabled")
			return
		}

		instanceID, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "bad_request", "invalid instance id")
			return
		}

		var req desktopSessionRequest
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&req)
		}

		inst, err := getDesktopInstance(r.Context(), deps, instanceID, user.ID)
		if err != nil {
			writeDesktopInstanceError(w, err, "desktop session")
			return
		}

		if err := ensureDesktopReady(r.Context(), deps, inst, req.LaunchTradingView, req.Symbol); err != nil {
			slog.Error("desktop session: prepare failed", "error", err, "instance_id", instanceID)
			WriteError(w, http.StatusBadGateway, "desktop_prepare_failed", "failed to prepare desktop")
			return
		}

		ticket := signScopedTicket(deps.Config.TerminalTicketSecret, "desktop", user.ID, instanceID, time.Now().Add(desktopTicketTTL))
		WriteJSON(w, http.StatusOK, map[string]any{
			"ticket":   ticket,
			"display":  ":1",
			"geometry": "1440x900",
		})
	}
}

// DesktopOpenHandler launches TradingView into the existing private desktop.
func DesktopOpenHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
			return
		}

		instanceID, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "bad_request", "invalid instance id")
			return
		}

		var req desktopSessionRequest
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&req)
		}

		inst, err := getDesktopInstance(r.Context(), deps, instanceID, user.ID)
		if err != nil {
			writeDesktopInstanceError(w, err, "desktop open")
			return
		}

		if err := ensureDesktopReady(r.Context(), deps, inst, true, req.Symbol); err != nil {
			slog.Error("desktop open: launch failed", "error", err, "instance_id", instanceID)
			WriteError(w, http.StatusBadGateway, "desktop_launch_failed", "failed to launch TradingView")
			return
		}

		WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// DesktopWebSocketHandler bridges noVNC frames to the VPS-local VNC server.
func DesktopWebSocketHandler(deps Dependencies) http.HandlerFunc {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  32 * 1024,
		WriteBufferSize: 32 * 1024,
		Subprotocols:    []string{"binary"},
		CheckOrigin:     terminalOriginCheck(deps.Config.AllowedOrigins),
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if len(deps.Config.TerminalTicketSecret) == 0 {
			WriteError(w, http.StatusServiceUnavailable, "not_configured", "remote desktop is not enabled")
			return
		}

		instanceID, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "bad_request", "invalid instance id")
			return
		}

		userID, signedInstance, err := verifyScopedTicket(deps.Config.TerminalTicketSecret, "desktop", r.URL.Query().Get("t"))
		if err != nil {
			WriteError(w, http.StatusUnauthorized, "invalid_ticket", "ticket is invalid or expired")
			return
		}
		if signedInstance != instanceID {
			WriteError(w, http.StatusUnauthorized, "invalid_ticket", "ticket does not match instance")
			return
		}

		inst, err := getDesktopInstance(r.Context(), deps, instanceID, userID)
		if err != nil {
			writeDesktopInstanceError(w, err, "desktop ws")
			return
		}

		count, decrement := incrementDesktopSession(userID)
		defer decrement()
		if count > desktopMaxPerUser {
			WriteError(w, http.StatusTooManyRequests, "too_many_sessions", "too many concurrent desktop sessions")
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Warn("desktop ws: upgrade failed", "error", err)
			return
		}
		defer conn.Close()

		var rootPassword string
		if inst.RootPassword != nil {
			rootPassword = *inst.RootPassword
		}

		vncConn, err := sshexec.DialTCP(*inst.IPv4, deps.Config.SSHPrivateKey, rootPassword, desktopVNCAddress)
		if err != nil {
			slog.Warn("desktop ws: open vnc tunnel", "error", err, "instance_id", instanceID)
			_ = conn.WriteJSON(map[string]any{"type": "error", "message": "failed to reach desktop"})
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "vnc tunnel failed"), time.Now().Add(2*time.Second))
			return
		}
		defer vncConn.Close()

		runDesktopProxy(r.Context(), conn, vncConn, userID, instanceID)
	}
}

func getDesktopInstance(ctx context.Context, deps Dependencies, instanceID, userID uuid.UUID) (*models.VpsInstance, error) {
	inst, err := db.GetInstanceByID(ctx, deps.Pool, instanceID, userID)
	if err != nil {
		return nil, err
	}
	if inst.Status != models.VpsStatusActive {
		return nil, errDesktopNotReady
	}
	if inst.IPv4 == nil || *inst.IPv4 == "" {
		return nil, errDesktopNoIP
	}
	return inst, nil
}

var (
	errDesktopNotReady = errors.New("desktop instance not ready")
	errDesktopNoIP     = errors.New("desktop instance has no ip")
)

func writeDesktopInstanceError(w http.ResponseWriter, err error, logPrefix string) {
	switch {
	case errors.Is(err, db.ErrNotFound):
		WriteError(w, http.StatusNotFound, "not_found", "instance not found")
	case errors.Is(err, errDesktopNotReady):
		WriteError(w, http.StatusConflict, "instance_not_ready", "agent is not active")
	case errors.Is(err, errDesktopNoIP):
		WriteError(w, http.StatusConflict, "instance_not_ready", "agent has no IP address")
	default:
		slog.Error(logPrefix+": get instance", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get instance")
	}
}

func ensureDesktopReady(ctx context.Context, deps Dependencies, inst *models.VpsInstance, launchTradingView bool, symbol string) error {
	var rootPassword string
	if inst.RootPassword != nil {
		rootPassword = *inst.RootPassword
	}

	runner := deps.SSHRunner
	if runner == nil {
		runner = sshexec.DefaultRunner{}
	}

	cmd := buildDesktopPrepareCommand(launchTradingView, symbol)
	_, err := runner.RunCommand(*inst.IPv4, deps.Config.SSHPrivateKey, rootPassword, cmd, 12*time.Minute)
	return err
}

func buildDesktopPrepareCommand(launchTradingView bool, symbol string) string {
	var b strings.Builder
	b.WriteString(`#!/bin/bash
set -euo pipefail

find_runtime_dir() {
    if [ -f /opt/openclaw/.env ]; then
        echo /opt/openclaw
    elif [ -f /opt/hermes/.env ]; then
        echo /opt/hermes
    else
        echo ""
    fi
}

install_host_admin() {
    RUNTIME_DIR=$(find_runtime_dir)
    if [ -z "$RUNTIME_DIR" ]; then
        echo "no tardi runtime env found" >&2
        return 1
    fi
    AGENT_TOKEN=$(grep '^AGENT_TOKEN=' "$RUNTIME_DIR/.env" 2>/dev/null | cut -d= -f2-)
    API_URL=$(grep '^API_URL=' "$RUNTIME_DIR/.env" 2>/dev/null | cut -d= -f2-)
    if [ -z "$AGENT_TOKEN" ] || [ -z "$API_URL" ]; then
        echo "agent token or api url missing" >&2
        return 1
    fi
    curl -sf -H "Authorization: Bearer ${AGENT_TOKEN}" "${API_URL}/api/agent/host-admin-script" -o "$RUNTIME_DIR/install-host-admin.sh"
    chmod +x "$RUNTIME_DIR/install-host-admin.sh"
    "$RUNTIME_DIR/install-host-admin.sh"
}

find_client() {
    for path in \
        /opt/openclaw/host-admin/bin/tardi-host-admin \
        /opt/hermes/host-admin/bin/tardi-host-admin \
        /usr/local/bin/tardi-host-admin; do
        if [ -x "$path" ]; then
            echo "$path"
            return 0
        fi
    done
    return 1
}

if ! CLIENT=$(find_client); then
    install_host_admin
    CLIENT=$(find_client)
fi

if [ ! -S /run/tardi-host-admin/admin.sock ]; then
    systemctl restart tardi-host-admin.service 2>/dev/null || install_host_admin
    sleep 2
fi
if [ ! -S /run/tardi-host-admin/admin.sock ]; then
    install_host_admin
fi
CLIENT=$(find_client)

NEEDS_INSTALL=false
if ! command -v vncserver >/dev/null 2>&1; then
    NEEDS_INSTALL=true
elif [ ! -f /etc/systemd/system/tardi-desktop.service ]; then
    NEEDS_INSTALL=true
elif ! grep -q -- '-SecurityTypes None' /etc/systemd/system/tardi-desktop.service 2>/dev/null; then
    NEEDS_INSTALL=true
fi
if [ "$NEEDS_INSTALL" = true ]; then
    "$CLIENT" desktop.install >/tmp/tardi-desktop-install.log
fi
`)
	if launchTradingView {
		if strings.TrimSpace(symbol) == "" {
			symbol = "BINANCE:BTCUSDT"
		}
		b.WriteString("\"$CLIENT\" desktop.open " + shellQuote(symbol) + " >/tmp/tardi-desktop-open.log\n")
	} else {
		b.WriteString("\"$CLIENT\" desktop.start >/tmp/tardi-desktop-start.log\n")
	}
	b.WriteString(`for i in $(seq 1 30); do
    if bash -lc '>/dev/tcp/127.0.0.1/5901' 2>/dev/null; then
        "$CLIENT" desktop.status
        exit 0
    fi
    sleep 1
done
echo "desktop VNC port did not become ready" >&2
exit 1
`)
	return b.String()
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func runDesktopProxy(parent context.Context, conn *websocket.Conn, vncConn net.Conn, userID, instanceID uuid.UUID) {
	ctx, cancel := context.WithTimeout(parent, desktopSessionMaxDuration)
	defer cancel()

	startedAt := time.Now()
	var bytesIn, bytesOut int64

	slog.Info("desktop session", "event", "open", "user_id", userID, "instance_id", instanceID)
	defer func() {
		slog.Info("desktop session", "event", "close",
			"user_id", userID,
			"instance_id", instanceID,
			"duration_ms", time.Since(startedAt).Milliseconds(),
			"bytes_in", bytesIn,
			"bytes_out", bytesOut,
		)
	}()

	_ = conn.SetReadDeadline(time.Now().Add(desktopReadDeadline))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(desktopReadDeadline))
	})

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
	closeDone := func() {
		once.Do(func() {
			close(done)
			_ = vncConn.Close()
			_ = conn.Close()
		})
	}

	go func() {
		defer closeDone()
		buf := make([]byte, 32*1024)
		for {
			n, err := vncConn.Read(buf)
			if n > 0 {
				if werr := writeBinary(buf[:n]); werr != nil {
					return
				}
				atomic.AddInt64(&bytesOut, int64(n))
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					slog.Debug("desktop: vnc read", "error", err, "instance_id", instanceID)
				}
				return
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(desktopPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := writePing(); err != nil {
					closeDone()
					return
				}
			case <-done:
				return
			case <-ctx.Done():
				closeDone()
				return
			}
		}
	}()

	rateBucket := newRateBucket(desktopWriteRateBytes, desktopWriteRateWindow)
	for {
		if ctx.Err() != nil {
			closeDone()
			return
		}
		mt, data, err := conn.ReadMessage()
		if err != nil {
			closeDone()
			return
		}
		if mt != websocket.BinaryMessage && mt != websocket.TextMessage {
			continue
		}
		if !rateBucket.allow(len(data)) {
			continue
		}
		if _, err := vncConn.Write(data); err != nil {
			closeDone()
			return
		}
		atomic.AddInt64(&bytesIn, int64(len(data)))
	}
}

func signScopedTicket(secret []byte, scope string, userID, instanceID uuid.UUID, exp time.Time) string {
	payload := fmt.Sprintf("%s.%s.%s.%d", scope, userID.String(), instanceID.String(), exp.Unix())
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + sig
}

func verifyScopedTicket(secret []byte, expectedScope, ticket string) (uuid.UUID, uuid.UUID, error) {
	parts := strings.Split(ticket, ".")
	if len(parts) != 5 {
		return uuid.Nil, uuid.Nil, errors.New("malformed ticket")
	}
	if parts[0] != expectedScope {
		return uuid.Nil, uuid.Nil, errors.New("bad scope")
	}
	userID, err := uuid.Parse(parts[1])
	if err != nil {
		return uuid.Nil, uuid.Nil, errors.New("bad user id")
	}
	instanceID, err := uuid.Parse(parts[2])
	if err != nil {
		return uuid.Nil, uuid.Nil, errors.New("bad instance id")
	}
	expUnix, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return uuid.Nil, uuid.Nil, errors.New("bad expiry")
	}
	if time.Now().Unix() > expUnix {
		return uuid.Nil, uuid.Nil, errors.New("expired")
	}

	payload := strings.Join(parts[:4], ".")
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[4])) {
		return uuid.Nil, uuid.Nil, errors.New("bad signature")
	}
	return userID, instanceID, nil
}
