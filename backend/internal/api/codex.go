package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/google/uuid"

	"github.com/shanq/tardi/internal/api/middleware"
	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/models"
	"github.com/shanq/tardi/internal/sshexec"
)

const (
	codexVerificationURL = "https://auth.openai.com/codex/device"
	codexLoginLogPath    = "/tmp/codex-login.log"
	codexAuthHostPath    = "/opt/openclaw/data/codex/auth.json"
)

var (
	ansiEscapeRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	codexCodeRE  = regexp.MustCompile(`[A-Z0-9]{4}-[A-Z0-9]{4,6}`)
)

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
		WriteError(w, http.StatusConflict, "conflict", "Codex linking is only supported on OpenClaw agents")
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

		ip := *inst.IPv4
		pw := ""
		if inst.RootPassword != nil {
			pw = *inst.RootPassword
		}
		sshKey := deps.Config.SSHPrivateKey

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

		out, err := sshexec.RunCommand(ip, sshKey, pw, script, 30*time.Second)
		if err != nil {
			slog.Error("codex link start: ssh failed", "instance_id", inst.ID, "error", err)
			WriteError(w, http.StatusBadGateway, "ssh_failed", "could not reach your agent")
			return
		}

		cleaned := ansiEscapeRE.ReplaceAllString(out, "")
		match := codexCodeRE.FindString(cleaned)
		if match == "" {
			slog.Warn("codex link start: no code in output", "instance_id", inst.ID, "output", cleaned)
			WriteError(w, http.StatusBadGateway, "no_code", "codex did not return a device code — it may need a moment, try again")
			return
		}

		slog.Info("codex link start: device code issued", "instance_id", inst.ID)
		WriteJSON(w, http.StatusOK, map[string]any{
			"code":             match,
			"verification_url": codexVerificationURL,
			"expires_in":       900,
		})
	}
}

// CodexLinkStatusHandler polls the VPS to see whether the user has
// completed the device-code flow. When auth.json first appears, the
// gateway is restarted so the codex app-server subprocess picks up the
// fresh credentials.
func CodexLinkStatusHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		inst := loadActiveOCInstance(w, r, deps)
		if inst == nil {
			return
		}

		ip := *inst.IPv4
		pw := ""
		if inst.RootPassword != nil {
			pw = *inst.RootPassword
		}
		sshKey := deps.Config.SSHPrivateKey

		script := fmt.Sprintf(`if [ -f %s ]; then
    echo LINKED
elif docker exec openclaw-gateway pgrep -f 'codex login' >/dev/null 2>&1; then
    echo PENDING
else
    docker exec openclaw-gateway cat %s 2>/dev/null | tail -1
    echo ABSENT
fi`, codexAuthHostPath, codexLoginLogPath)

		out, err := sshexec.RunCommand(ip, sshKey, pw, script, 15*time.Second)
		if err != nil {
			slog.Error("codex link status: ssh failed", "instance_id", inst.ID, "error", err)
			WriteError(w, http.StatusBadGateway, "ssh_failed", "could not reach your agent")
			return
		}

		cleaned := ansiEscapeRE.ReplaceAllString(out, "")
		switch {
		case regexp.MustCompile(`\bLINKED\b`).MatchString(cleaned):
			// Restart gateway so the codex app-server picks up the new auth.
			// Fire-and-forget — status will reflect LINKED regardless.
			go func(ip, pw string, key []byte, id uuid.UUID) {
				if _, rErr := sshexec.RunCommand(ip, key, pw, "docker restart openclaw-gateway", 30*time.Second); rErr != nil {
					slog.Error("codex link status: gateway restart failed", "instance_id", id, "error", rErr)
				}
			}(ip, pw, sshKey, inst.ID)
			WriteJSON(w, http.StatusOK, map[string]any{"status": "linked"})
		case regexp.MustCompile(`\bPENDING\b`).MatchString(cleaned):
			WriteJSON(w, http.StatusOK, map[string]any{"status": "pending"})
		default:
			WriteJSON(w, http.StatusOK, map[string]any{"status": "absent"})
		}
	}
}

// CodexUnlinkHandler logs codex out, deletes the persisted auth.json, and
// restarts the gateway so any cached credentials in the running codex
// app-server are dropped.
func CodexUnlinkHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		inst := loadActiveOCInstance(w, r, deps)
		if inst == nil {
			return
		}

		ip := *inst.IPv4
		pw := ""
		if inst.RootPassword != nil {
			pw = *inst.RootPassword
		}
		sshKey := deps.Config.SSHPrivateKey

		script := fmt.Sprintf(`docker exec openclaw-gateway pkill -f 'codex login' 2>/dev/null || true
docker exec openclaw-gateway codex logout 2>/dev/null || true
rm -f %s
docker restart openclaw-gateway >/dev/null 2>&1`, codexAuthHostPath)

		_, err := sshexec.RunCommand(ip, sshKey, pw, script, 60*time.Second)
		if err != nil {
			slog.Error("codex unlink: ssh failed", "instance_id", inst.ID, "error", err)
			WriteError(w, http.StatusBadGateway, "ssh_failed", "could not reach your agent")
			return
		}

		slog.Info("codex unlink: ok", "instance_id", inst.ID)
		WriteJSON(w, http.StatusOK, map[string]any{"status": "unlinked"})
	}
}
