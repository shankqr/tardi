package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/shanq/tardi/internal/api/middleware"
	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/models"
)

const (
	codexVerificationURL     = "https://auth.openai.com/codex/device"
	codexLoginLogPath        = "/tmp/codex-login.log"
	codexAuthHostPath        = "/opt/openclaw/data/codex/auth.json"
	codexLinkStartMinGap     = 30 * time.Second
	codexRestartMaxDuration  = 90 * time.Second
	codexHealthPollInterval  = 5 * time.Second
	codexRestartGoroutineCap = 120 * time.Second
)

// Regexes are exported through package-level helpers to keep them testable.
// ansiEscapeRE covers SGR and other CSI sequences the codex CLI colourises
// output with. codexCodeRE matches the "XXXX-YYYYY" device code shape (codex
// today emits 4-5 uppercase alphanumerics with a dash; we accept 4-4/4-5/4-6
// to be forgiving).
var (
	ansiEscapeRE   = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	codexCodeRE    = regexp.MustCompile(`[A-Z0-9]{4}-[A-Z0-9]{4,6}`)
	codexLinkedRE  = regexp.MustCompile(`\bLINKED\b`)
	codexPendingRE = regexp.MustCompile(`\bPENDING\b`)
)

// CodexLinkState holds in-process per-instance state that the codex link
// handlers use to rate limit the start endpoint and avoid redundant gateway
// restarts while a link is being finalised. Scope is a single Cloud Run
// instance — cross-instance coordination isn't necessary because both
// behaviours are best-effort optimisations.
type CodexLinkState struct {
	lastStart sync.Map // uuid.UUID -> time.Time
	restartAt sync.Map // uuid.UUID -> time.Time
}

func NewCodexLinkState() *CodexLinkState { return &CodexLinkState{} }

func (s *CodexLinkState) recordStart(id uuid.UUID) {
	s.lastStart.Store(id, time.Now())
}

func (s *CodexLinkState) recentStart(id uuid.UUID) (time.Duration, bool) {
	v, ok := s.lastStart.Load(id)
	if !ok {
		return 0, false
	}
	return time.Since(v.(time.Time)), true
}

func (s *CodexLinkState) markRestart(id uuid.UUID) bool {
	now := time.Now()
	existing, loaded := s.restartAt.LoadOrStore(id, now)
	if !loaded {
		return true
	}
	// Stale marker — recycle.
	if time.Since(existing.(time.Time)) > codexRestartMaxDuration {
		s.restartAt.Store(id, now)
		return true
	}
	return false
}

func (s *CodexLinkState) clearRestart(id uuid.UUID) {
	s.restartAt.Delete(id)
}

func (s *CodexLinkState) restartInFlight(id uuid.UUID) bool {
	v, ok := s.restartAt.Load(id)
	if !ok {
		return false
	}
	if time.Since(v.(time.Time)) > codexRestartMaxDuration {
		s.restartAt.Delete(id)
		return false
	}
	return true
}

// extractDeviceCode returns the device code in the given codex CLI output,
// or empty if none is present. Strips ANSI escapes before matching.
func extractDeviceCode(out string) string {
	return codexCodeRE.FindString(ansiEscapeRE.ReplaceAllString(out, ""))
}

// loadActiveOCInstance parses the instance id from the path, ensures it
// belongs to the user, is active, has an IP, and runs OpenClaw. Writes an
// error response and returns nil if any check fails.
func loadActiveOCInstance(w http.ResponseWriter, r *http.Request, deps Dependencies) *models.VpsInstance {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
		return nil
	}
	instanceID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", "invalid instance id")
		return nil
	}
	inst, err := db.GetInstanceByID(r.Context(), deps.Pool, instanceID, user.ID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "not_found", "instance not found")
			return nil
		}
		slog.Error("codex: get instance", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get instance")
		return nil
	}
	if inst.Framework == models.FrameworkHermes {
		WriteError(w, http.StatusConflict, "not_supported", "Codex linking is only supported on OpenClaw agents")
		return nil
	}
	if inst.Status != "active" {
		WriteError(w, http.StatusConflict, "conflict", "instance is not active")
		return nil
	}
	if inst.IPv4 == nil || *inst.IPv4 == "" {
		WriteError(w, http.StatusConflict, "conflict", "instance has no IP address")
		return nil
	}
	return inst
}

func sshCreds(inst *models.VpsInstance, cfg *dependenciesConfigAccessor) (string, []byte, string) {
	host := *inst.IPv4
	pw := ""
	if inst.RootPassword != nil {
		pw = *inst.RootPassword
	}
	return host, cfg.sshKey(), pw
}

// dependenciesConfigAccessor is a tiny adapter so we can pass just the bits
// of Dependencies a handler needs without leaking the whole struct into
// helpers. Kept local to this file.
type dependenciesConfigAccessor struct {
	deps Dependencies
}

func (d *dependenciesConfigAccessor) sshKey() []byte { return d.deps.Config.SSHPrivateKey }

// CodexLinkStartHandler launches `codex login --device-auth` inside the
// container and returns the device code + verification URL. The login
// subprocess keeps running inside the container until the user completes
// auth at openai.com/codex/device or the 15-min window expires.
func CodexLinkStartHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		inst := loadActiveOCInstance(w, r, deps)
		if inst == nil {
			return
		}

		// Per-instance rate limit: reject if the last start was <30s ago.
		// Prevents button-mashing from churning codex login processes.
		if since, ok := deps.CodexState.recentStart(inst.ID); ok && since < codexLinkStartMinGap {
			retryIn := int((codexLinkStartMinGap - since).Seconds()) + 1
			WriteError(w, http.StatusTooManyRequests, "rate_limited",
				fmt.Sprintf("please wait %ds before starting another link", retryIn))
			return
		}

		cfg := &dependenciesConfigAccessor{deps: deps}
		host, key, pw := sshCreds(inst, cfg)

		script := `set -e
docker exec openclaw-gateway pkill -f 'codex login' 2>/dev/null || true
if ! docker exec openclaw-gateway sh -c 'command -v codex' >/dev/null 2>&1; then
    docker exec -u 0 openclaw-gateway npm install -g @openai/codex >/dev/null 2>&1 || true
fi
docker exec -d openclaw-gateway sh -c 'rm -f ` + codexLoginLogPath + `; codex login --device-auth > ` + codexLoginLogPath + ` 2>&1'
for i in $(seq 1 40); do
    sleep 0.5
    if docker exec openclaw-gateway grep -qE 'codex/device' ` + codexLoginLogPath + ` 2>/dev/null; then
        break
    fi
done
docker exec openclaw-gateway cat ` + codexLoginLogPath + ` 2>/dev/null || true`

		out, err := deps.SSHRunner.RunCommand(host, key, pw, script, 45*time.Second)
		if err != nil {
			slog.Error("codex link start: ssh failed", "instance_id", inst.ID, "error", err)
			WriteError(w, http.StatusBadGateway, "ssh_failed", "could not reach your agent")
			return
		}

		code := extractDeviceCode(out)
		if code == "" {
			slog.Warn("codex link start: no code in output", "instance_id", inst.ID, "output", out)
			WriteError(w, http.StatusBadGateway, "no_code", "codex did not return a device code — please try again in a moment")
			return
		}

		deps.CodexState.recordStart(inst.ID)
		slog.Info("codex link start: device code issued", "instance_id", inst.ID)
		WriteJSON(w, http.StatusOK, map[string]any{
			"code":             code,
			"verification_url": codexVerificationURL,
			"expires_in":       900,
		})
	}
}

// CodexLinkStatusHandler reports one of four states to the client:
//   - linked: DB already records the link (fast path, no SSH).
//   - restarting: auth.json is present but we're still confirming the
//     codex app-server picked it up.
//   - pending: login process still running in the container.
//   - absent: no auth.json, no login running.
//
// On the first observation of a new auth.json, we fire a background
// goroutine that restarts the gateway, waits for health, then writes the
// link state and an audit-log entry.
func CodexLinkStatusHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		inst := loadActiveOCInstance(w, r, deps)
		if inst == nil {
			return
		}

		// Fast path: DB says linked. No SSH needed.
		if inst.CodexLinkedAt != nil {
			WriteJSON(w, http.StatusOK, map[string]any{"status": "linked"})
			return
		}

		cfg := &dependenciesConfigAccessor{deps: deps}
		host, key, pw := sshCreds(inst, cfg)

		// Probe: auth.json present? login process running?
		probe := fmt.Sprintf(`if [ -f %s ]; then
    echo LINKED
elif docker exec openclaw-gateway pgrep -f 'codex login' >/dev/null 2>&1; then
    echo PENDING
else
    echo ABSENT
fi`, codexAuthHostPath)

		out, err := deps.SSHRunner.RunCommand(host, key, pw, probe, 15*time.Second)
		if err != nil {
			slog.Error("codex link status: ssh failed", "instance_id", inst.ID, "error", err)
			WriteError(w, http.StatusBadGateway, "ssh_failed", "could not reach your agent")
			return
		}

		switch {
		case codexLinkedRE.MatchString(out):
			// auth.json present but DB not updated yet. Either a restart is
			// already in flight, or we need to kick one off.
			if deps.CodexState.restartInFlight(inst.ID) {
				WriteJSON(w, http.StatusOK, map[string]any{"status": "restarting"})
				return
			}
			if deps.CodexState.markRestart(inst.ID) {
				go finaliseCodexLink(deps, inst.ID, host, key, pw)
			}
			WriteJSON(w, http.StatusOK, map[string]any{"status": "restarting"})
		case codexPendingRE.MatchString(out):
			WriteJSON(w, http.StatusOK, map[string]any{"status": "pending"})
		default:
			WriteJSON(w, http.StatusOK, map[string]any{"status": "absent"})
		}
	}
}

// finaliseCodexLink restarts the gateway, waits for /health to come back,
// persists the link in the DB, and writes an audit-log entry. Runs off
// the request goroutine.
func finaliseCodexLink(deps Dependencies, instanceID uuid.UUID, host string, key []byte, pw string) {
	defer deps.CodexState.clearRestart(instanceID)

	ctx, cancel := context.WithTimeout(context.Background(), codexRestartGoroutineCap)
	defer cancel()

	if _, err := deps.SSHRunner.RunCommand(host, key, pw, "docker restart openclaw-gateway", 30*time.Second); err != nil {
		slog.Error("codex finalise: gateway restart failed", "instance_id", instanceID, "error", err)
		return
	}

	// Poll /health until ready or 90s elapsed.
	healthy := false
	deadline := time.Now().Add(codexRestartMaxDuration)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			slog.Warn("codex finalise: context done before health confirmed", "instance_id", instanceID)
			return
		case <-time.After(codexHealthPollInterval):
		}
		out, err := deps.SSHRunner.RunCommand(host, key, pw,
			"curl -sf -o /dev/null -w %{http_code} http://localhost:18789/health", 10*time.Second)
		if err == nil && out == "200" {
			healthy = true
			break
		}
	}
	if !healthy {
		slog.Warn("codex finalise: gateway did not become healthy within timeout", "instance_id", instanceID)
		return
	}

	linkedAt := time.Now().UTC()
	if err := db.SetCodexLinkState(ctx, deps.Pool, instanceID, &linkedAt); err != nil {
		slog.Error("codex finalise: persist link state", "instance_id", instanceID, "error", err)
		return
	}

	writeCodexAuditLog(ctx, deps, instanceID, "codex_link", nil)
	slog.Info("codex finalise: linked", "instance_id", instanceID)
}

// CodexUnlinkHandler removes the persisted auth.json, clears the DB link
// state, writes an audit-log entry, and restarts the gateway.
func CodexUnlinkHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		inst := loadActiveOCInstance(w, r, deps)
		if inst == nil {
			return
		}

		cfg := &dependenciesConfigAccessor{deps: deps}
		host, key, pw := sshCreds(inst, cfg)

		script := fmt.Sprintf(`docker exec openclaw-gateway pkill -f 'codex login' 2>/dev/null || true
docker exec openclaw-gateway codex logout 2>/dev/null || true
rm -f %s
docker restart openclaw-gateway >/dev/null 2>&1`, codexAuthHostPath)

		if _, err := deps.SSHRunner.RunCommand(host, key, pw, script, 60*time.Second); err != nil {
			slog.Error("codex unlink: ssh failed", "instance_id", inst.ID, "error", err)
			WriteError(w, http.StatusBadGateway, "ssh_failed", "could not reach your agent")
			return
		}

		if err := db.SetCodexLinkState(r.Context(), deps.Pool, inst.ID, nil); err != nil {
			slog.Error("codex unlink: clear DB state", "instance_id", inst.ID, "error", err)
			// SSH already succeeded; don't fail the user-facing request.
		}

		writeCodexAuditLog(r.Context(), deps, inst.ID, "codex_unlink", nil)
		slog.Info("codex unlink: ok", "instance_id", inst.ID)
		WriteJSON(w, http.StatusOK, map[string]any{"status": "unlinked"})
	}
}

func writeCodexAuditLog(ctx context.Context, deps Dependencies, instanceID uuid.UUID, action string, email *string) {
	user := middleware.UserFromContext(ctx)
	var userID uuid.UUID
	if user != nil {
		userID = user.ID
	}
	var metadata map[string]any
	if email != nil && *email != "" {
		metadata = map[string]any{"email": *email}
	}
	entry := &models.AuditLogEntry{
		ID:           uuid.New(),
		UserID:       userID,
		Action:       action,
		ResourceType: "instance",
		ResourceID:   &instanceID,
		Metadata:     metadata,
	}
	if err := db.InsertAuditLog(ctx, deps.Pool, entry); err != nil {
		slog.Warn("codex audit log insert failed", "instance_id", instanceID, "action", action, "error", err)
	}
}
