package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/shanq/tardi/internal/api/middleware"
	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/models"
	"github.com/shanq/tardi/internal/sshexec"
)

// DashboardTokenHandler reads the current gateway token from the VPS.
// This is a read-only operation — it does NOT modify openclaw.json.
// trustedProxies and other config fixes are handled by the heartbeat
// drift guard (every 5 min) and the provisioner at initial setup.
func DashboardTokenHandler(deps Dependencies) http.HandlerFunc {
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

		inst, err := db.GetInstanceByID(r.Context(), deps.Pool, instanceID, user.ID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				WriteError(w, http.StatusNotFound, "not_found", "instance not found")
				return
			}
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get instance")
			return
		}

		if inst.IPv4 == nil || *inst.IPv4 == "" || inst.RootPassword == nil || *inst.RootPassword == "" {
			WriteError(w, http.StatusConflict, "conflict", "instance not reachable")
			return
		}

		// Try DB-cached token first — avoids SSH entirely when available.
		// For OpenClaw this is the gateway token; for Hermes it's the API_SERVER_KEY.
		if inst.OpenClawAuthToken != nil && *inst.OpenClawAuthToken != "" {
			WriteJSON(w, http.StatusOK, map[string]string{
				"token": *inst.OpenClawAuthToken,
			})
			return
		}

		// Fallback: read token from VPS via SSH.
		// Both scripts end with the same JSON output logic.
		scriptTail := "\n" + `if [ -z "$GW_TOKEN" ]; then` + "\n" +
			`    echo '{"error":"no gateway token"}'` + "\n" +
			"    exit 0\nfi\n" +
			`echo "{\"token\":\"$GW_TOKEN\"}"` + "\n"

		var script string
		if inst.Framework == models.FrameworkHermes {
			script = "#!/bin/bash\n" +
				"GW_TOKEN=$(grep '^API_SERVER_KEY=' /opt/hermes/.env 2>/dev/null | cut -d= -f2- || true)\n" +
				scriptTail
		} else {
			script = "#!/bin/bash\n" +
				"OC_CFG=\"/opt/openclaw/data/openclaw/openclaw.json\"\n" +
				"GW_TOKEN=$(docker exec openclaw-gateway printenv OPENCLAW_GATEWAY_TOKEN 2>/dev/null || true)\n" +
				"if [ -z \"$GW_TOKEN\" ]; then\n" +
				"    GW_TOKEN=$(grep '^OPENCLAW_GATEWAY_TOKEN=' /opt/openclaw/.env 2>/dev/null | cut -d= -f2- || true)\n" +
				"fi\n" +
				"if [ -z \"$GW_TOKEN\" ]; then\n" +
				"    GW_TOKEN=$(cat \"$OC_CFG\" 2>/dev/null | jq -r '.gateway.auth.token // empty' 2>/dev/null || true)\n" +
				"fi\n" + scriptTail
		}
		out, err := sshexec.RunCommand(*inst.IPv4, deps.Config.SSHPrivateKey, *inst.RootPassword, script, 30*time.Second)
		if err != nil {
			slog.Error("dashboard-token: ssh failed", "error", err, "instance_id", instanceID)
			WriteError(w, http.StatusBadGateway, "gateway_error", "failed to generate dashboard token")
			return
		}

		var result struct {
			Token string `json:"token"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &result); err != nil {
			slog.Error("dashboard-token: invalid response", "error", err, "raw", out, "instance_id", instanceID)
			WriteError(w, http.StatusBadGateway, "gateway_error", "invalid response from agent")
			return
		}

		if result.Error != "" {
			slog.Error("dashboard-token: agent error", "error", result.Error, "instance_id", instanceID)
			WriteError(w, http.StatusBadGateway, "gateway_error", result.Error)
			return
		}

		if result.Token == "" {
			WriteError(w, http.StatusBadGateway, "gateway_error", "empty token returned")
			return
		}

		// Sync token to DB so backend RPC calls use the correct token
		if err := db.UpdateInstanceOpenClawAuthToken(r.Context(), deps.Pool, instanceID, result.Token); err != nil {
			slog.Warn("dashboard-token: failed to sync token to DB", "error", err, "instance_id", instanceID)
		}

		WriteJSON(w, http.StatusOK, map[string]string{
			"token": result.Token,
		})
	}
}
