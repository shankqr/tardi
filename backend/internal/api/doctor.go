package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/shanq/tardi/internal/api/middleware"
	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/models"
	"github.com/shanq/tardi/internal/sshexec"
)

// healthCheckScript runs targeted checks for issues we've actually seen
// in production: config drift, container crash loops, and resource exhaustion.
const healthCheckScript = `#!/bin/bash
set -o pipefail

# Build a JSON array of check results using jq
echo '[]' > /tmp/tardi-health.json

add() {
    local tmp
    tmp=$(jq --arg n "$1" --arg s "$2" --arg m "$3" --arg d "$4" \
        '. + [{"name": $n, "status": $s, "message": $m, "detail": $d}]' /tmp/tardi-health.json)
    echo "$tmp" > /tmp/tardi-health.json
}

# ── 1. Container state ──────────────────────────────────────────────
CINSPECT=$(docker inspect openclaw-gateway --format='{{.State.Status}}|{{.State.Health.Status}}|{{.RestartCount}}|{{.State.StartedAt}}' 2>/dev/null)
if [ $? -ne 0 ]; then
    add "Container" "fail" "Container not found" "openclaw-gateway container does not exist or is not created"
else
    C_STATUS=$(echo "$CINSPECT" | cut -d'|' -f1)
    C_HEALTH=$(echo "$CINSPECT" | cut -d'|' -f2)
    C_RESTARTS=$(echo "$CINSPECT" | cut -d'|' -f3)
    C_STARTED=$(echo "$CINSPECT" | cut -d'|' -f4)
    if [ "$C_STATUS" = "running" ] && [ "$C_HEALTH" = "healthy" ]; then
        add "Container" "pass" "Running and healthy" "Restarts: ${C_RESTARTS}, Started: ${C_STARTED}"
    elif [ "$C_STATUS" = "running" ]; then
        add "Container" "warn" "Running but health: ${C_HEALTH}" "Restarts: ${C_RESTARTS}. Container may still be starting up."
    else
        add "Container" "fail" "Container is ${C_STATUS}" "Restarts: ${C_RESTARTS}"
    fi
    if [ "${C_RESTARTS:-0}" -gt 5 ]; then
        add "Stability" "warn" "High restart count: ${C_RESTARTS}" "Possible crash loop. Check logs for errors."
    fi
fi

# ── 2. API Keys (require non-empty values) ──────────────────────────
HAS_OR=$(grep -c '^OPENROUTER_API_KEY=.\+' /opt/openclaw/.env 2>/dev/null) || HAS_OR=0
HAS_AN=$(grep -c '^ANTHROPIC_API_KEY=.\+' /opt/openclaw/.env 2>/dev/null) || HAS_AN=0
HAS_OA=$(grep -c '^OPENAI_API_KEY=.\+' /opt/openclaw/.env 2>/dev/null) || HAS_OA=0
KEY_TOTAL=$((HAS_OR + HAS_AN + HAS_OA))
if [ "$KEY_TOTAL" -gt 0 ]; then
    PROVIDERS=""
    [ "$HAS_OR" -gt 0 ] && PROVIDERS="OpenRouter"
    [ "$HAS_AN" -gt 0 ] && PROVIDERS="${PROVIDERS:+$PROVIDERS, }Anthropic"
    [ "$HAS_OA" -gt 0 ] && PROVIDERS="${PROVIDERS:+$PROVIDERS, }OpenAI"
    add "API Keys" "pass" "${PROVIDERS}" ""
else
    add "API Keys" "fail" "No API keys configured" "Your agent cannot make AI calls without at least one API key. Set one in the AI Provider section."
fi

# ── 2b. Model Configuration ───────────────────────────────────────
if [ "$C_STATUS" = "running" ] 2>/dev/null; then
    MODEL_OUT=$(docker exec openclaw-gateway openclaw models list 2>&1 || echo "")
    # Check that output has at least 2 lines (header + model row) and no errors
    MODEL_LINES=$(echo "$MODEL_OUT" | wc -l | tr -d ' ')
    if [ -n "$MODEL_OUT" ] && [ "$MODEL_LINES" -gt 1 ] && ! echo "$MODEL_OUT" | grep -qi 'error\|not found\|no model'; then
        DEFAULT_MODEL=$(echo "$MODEL_OUT" | sed -n '2p')
        add "AI Model" "pass" "Model configured" "${DEFAULT_MODEL}"
    else
        # Fallback: check openclaw.json for model-related fields
        OC_CFG_FOR_MODEL=$(cat /opt/openclaw/data/openclaw/openclaw.json 2>/dev/null)
        MODEL_JSON=$(echo "$OC_CFG_FOR_MODEL" | jq -r '(.models // .defaultModel // .ai // empty)' 2>/dev/null)
        if [ -n "$MODEL_JSON" ] && [ "$MODEL_JSON" != "null" ]; then
            add "AI Model" "pass" "Model found in config" "${MODEL_JSON}"
        else
            add "AI Model" "warn" "No model detected" "The agent may not know which AI model to use. Try re-saving your AI Provider settings."
        fi
    fi
fi

# ── 3. Config Version Sync ──────────────────────────────────────────
LOCAL_VER=$(cat /opt/openclaw/.config_version 2>/dev/null || echo "0")
AGENT_TOKEN=$(grep '^AGENT_TOKEN=' /opt/openclaw/.env 2>/dev/null | cut -d= -f2-)
API_URL=$(grep '^API_URL=' /opt/openclaw/.env 2>/dev/null | cut -d= -f2-)
if [ -n "$AGENT_TOKEN" ] && [ -n "$API_URL" ]; then
    REMOTE_CFG=$(curl -sf --max-time 5 "${API_URL}/api/agent/config" -H "Authorization: Bearer ${AGENT_TOKEN}" 2>/dev/null)
    if [ -n "$REMOTE_CFG" ]; then
        REMOTE_VER=$(echo "$REMOTE_CFG" | jq -r '.version // 0')
        if [ "$LOCAL_VER" = "$REMOTE_VER" ]; then
            add "Config Sync" "pass" "In sync (version ${LOCAL_VER})" ""
        else
            add "Config Sync" "warn" "Out of sync — local v${LOCAL_VER}, remote v${REMOTE_VER}" "Config will sync on next heartbeat (within 5 minutes)"
        fi
    else
        add "Config Sync" "warn" "Could not reach Tardi API" "Config sync status unknown"
    fi
else
    add "Config Sync" "warn" "Agent token or API URL missing" "Heartbeat and config sync may not be working"
fi

# ── 4. Recent Errors ────────────────────────────────────────────────
if [ "$C_STATUS" = "running" ] 2>/dev/null; then
    RECENT_ERRORS=$(docker logs openclaw-gateway --tail 50 2>&1 | grep -i 'error\|fatal\|panic\|crash\|ECONNREFUSED' | grep -v 'node_modules' | tail -5)
    if [ -z "$RECENT_ERRORS" ]; then
        add "Recent Logs" "pass" "No errors in last 50 log lines" ""
    else
        add "Recent Logs" "warn" "Errors found in recent logs" "$RECENT_ERRORS"
    fi
fi

# ── 5. Disk Space ───────────────────────────────────────────────────
DISK_PCT=$(df / 2>/dev/null | awk 'NR==2{gsub(/%/,""); print $5}')
DISK_AVAIL=$(df -h / 2>/dev/null | awk 'NR==2{print $4}')
if [ -n "$DISK_PCT" ]; then
    if [ "$DISK_PCT" -lt 80 ]; then
        add "Disk Space" "pass" "${DISK_PCT}% used (${DISK_AVAIL} free)" ""
    elif [ "$DISK_PCT" -lt 95 ]; then
        add "Disk Space" "warn" "${DISK_PCT}% used (${DISK_AVAIL} free)" "Disk getting full — consider cleaning up old data"
    else
        add "Disk Space" "fail" "${DISK_PCT}% used (${DISK_AVAIL} free)" "Disk nearly full — agent may stop working"
    fi
fi

# ── 6. Memory ───────────────────────────────────────────────────────
MEM_TOTAL=$(free -m 2>/dev/null | awk '/Mem:/{print $2}')
MEM_AVAIL=$(free -m 2>/dev/null | awk '/Mem:/{print $7}')
if [ -n "$MEM_TOTAL" ] && [ -n "$MEM_AVAIL" ] && [ "$MEM_TOTAL" -gt 0 ]; then
    MEM_USED=$((MEM_TOTAL - MEM_AVAIL))
    MEM_PCT=$((MEM_USED * 100 / MEM_TOTAL))
    if [ "$MEM_PCT" -lt 80 ]; then
        add "Memory" "pass" "${MEM_USED}MB / ${MEM_TOTAL}MB (${MEM_PCT}%)" ""
    elif [ "$MEM_PCT" -lt 95 ]; then
        add "Memory" "warn" "${MEM_USED}MB / ${MEM_TOTAL}MB (${MEM_PCT}%)" "Memory pressure may cause slowdowns or OOM kills"
    else
        add "Memory" "fail" "${MEM_USED}MB / ${MEM_TOTAL}MB (${MEM_PCT}%)" "Memory critically low — agent may crash"
    fi
fi

# ── 7. iptables NAT ────────────────────────────────────────────────
# Cloudflare Proxy connects to origin on port 80. iptables NAT redirects
# port 80 → 18789 since OpenClaw runs as UID 1000 and can't bind port 80.
if iptables -t nat -C PREROUTING -p tcp --dport 80 -j REDIRECT --to-port 18789 2>/dev/null; then
    add "iptables NAT" "pass" "Port 80 → 18789 redirect active" ""
else
    add "iptables NAT" "fail" "Missing port 80 → 18789 redirect" "Run: iptables -t nat -A PREROUTING -p tcp --dport 80 -j REDIRECT --to-port 18789 && netfilter-persistent save"
fi

# ── 8. Host networking ────────────────────────────────────────────
# Verify OpenClaw is using host networking (not bridge)
OC_NET=$(docker inspect -f '{{.HostConfig.NetworkMode}}' openclaw-gateway 2>/dev/null)
if [ "$OC_NET" = "host" ]; then
    add "Network Mode" "pass" "Host networking" ""
else
    add "Network Mode" "warn" "Network mode: ${OC_NET}" "Expected host networking. Container may be using old bridge setup."
fi

cat /tmp/tardi-health.json
rm -f /tmp/tardi-health.json
`

// hermesHealthCheckScript runs the same categories of checks for Hermes instances.
const hermesHealthCheckScript = `#!/bin/bash
set -o pipefail

echo '[]' > /tmp/tardi-health.json

add() {
    local tmp
    tmp=$(jq --arg n "$1" --arg s "$2" --arg m "$3" --arg d "$4" \
        '. + [{"name": $n, "status": $s, "message": $m, "detail": $d}]' /tmp/tardi-health.json)
    echo "$tmp" > /tmp/tardi-health.json
}

# ── 1. Container state ──────────────────────────────────────────────
CINSPECT=$(docker inspect hermes-agent --format='{{.State.Status}}|{{.State.Health.Status}}|{{.RestartCount}}|{{.State.StartedAt}}' 2>/dev/null)
if [ $? -ne 0 ]; then
    add "Container" "fail" "Container not found" "hermes-agent container does not exist or is not created"
else
    C_STATUS=$(echo "$CINSPECT" | cut -d'|' -f1)
    C_HEALTH=$(echo "$CINSPECT" | cut -d'|' -f2)
    C_RESTARTS=$(echo "$CINSPECT" | cut -d'|' -f3)
    C_STARTED=$(echo "$CINSPECT" | cut -d'|' -f4)
    if [ "$C_STATUS" = "running" ] && [ "$C_HEALTH" = "healthy" ]; then
        add "Container" "pass" "Running and healthy" "Restarts: ${C_RESTARTS}, Started: ${C_STARTED}"
    elif [ "$C_STATUS" = "running" ]; then
        add "Container" "warn" "Running but health: ${C_HEALTH}" "Restarts: ${C_RESTARTS}. Container may still be starting up."
    else
        add "Container" "fail" "Container is ${C_STATUS}" "Restarts: ${C_RESTARTS}"
    fi
    if [ "${C_RESTARTS:-0}" -gt 5 ]; then
        add "Stability" "warn" "High restart count: ${C_RESTARTS}" "Possible crash loop. Check logs for errors."
    fi
fi

# ── 2. API Keys ────────────────────────────────────────────────────
HAS_OR=$(grep -c '^OPENROUTER_API_KEY=.\+' /opt/hermes/.env 2>/dev/null) || HAS_OR=0
HAS_AN=$(grep -c '^ANTHROPIC_API_KEY=.\+' /opt/hermes/.env 2>/dev/null) || HAS_AN=0
HAS_OA=$(grep -c '^OPENAI_API_KEY=.\+' /opt/hermes/.env 2>/dev/null) || HAS_OA=0
KEY_TOTAL=$((HAS_OR + HAS_AN + HAS_OA))
if [ "$KEY_TOTAL" -gt 0 ]; then
    PROVIDERS=""
    [ "$HAS_OR" -gt 0 ] && PROVIDERS="OpenRouter"
    [ "$HAS_AN" -gt 0 ] && PROVIDERS="${PROVIDERS:+$PROVIDERS, }Anthropic"
    [ "$HAS_OA" -gt 0 ] && PROVIDERS="${PROVIDERS:+$PROVIDERS, }OpenAI"
    add "API Keys" "pass" "${PROVIDERS}" ""
else
    add "API Keys" "fail" "No API keys configured" "Your agent cannot make AI calls without at least one API key."
fi

# ── 3. Config Version Sync ────────────────────────────────────────
LOCAL_VER=$(cat /opt/hermes/.config_version 2>/dev/null || echo "0")
AGENT_TOKEN=$(grep '^AGENT_TOKEN=' /opt/hermes/.env 2>/dev/null | cut -d= -f2-)
API_URL=$(grep '^API_URL=' /opt/hermes/.env 2>/dev/null | cut -d= -f2-)
if [ -n "$AGENT_TOKEN" ] && [ -n "$API_URL" ]; then
    REMOTE_CFG=$(curl -sf --max-time 5 "${API_URL}/api/agent/config" -H "Authorization: Bearer ${AGENT_TOKEN}" 2>/dev/null)
    if [ -n "$REMOTE_CFG" ]; then
        REMOTE_VER=$(echo "$REMOTE_CFG" | jq -r '.version // 0')
        if [ "$LOCAL_VER" = "$REMOTE_VER" ]; then
            add "Config Sync" "pass" "In sync (version ${LOCAL_VER})" ""
        else
            add "Config Sync" "warn" "Out of sync — local v${LOCAL_VER}, remote v${REMOTE_VER}" "Config will sync on next heartbeat (within 5 minutes)"
        fi
    else
        add "Config Sync" "warn" "Could not reach Tardi API" "Config sync status unknown"
    fi
fi

# ── 4. Recent Errors ──────────────────────────────────────────────
if [ "$C_STATUS" = "running" ] 2>/dev/null; then
    RECENT_ERRORS=$(docker logs hermes-agent --tail 50 2>&1 | grep -i 'error\|fatal\|panic\|crash\|traceback' | tail -5)
    if [ -z "$RECENT_ERRORS" ]; then
        add "Recent Logs" "pass" "No errors in last 50 log lines" ""
    else
        add "Recent Logs" "warn" "Errors found in recent logs" "$RECENT_ERRORS"
    fi
fi

# ── 5. Disk Space ─────────────────────────────────────────────────
DISK_PCT=$(df / 2>/dev/null | awk 'NR==2{gsub(/%/,""); print $5}')
DISK_AVAIL=$(df -h / 2>/dev/null | awk 'NR==2{print $4}')
if [ -n "$DISK_PCT" ]; then
    if [ "$DISK_PCT" -lt 80 ]; then
        add "Disk Space" "pass" "${DISK_PCT}% used (${DISK_AVAIL} free)" ""
    elif [ "$DISK_PCT" -lt 95 ]; then
        add "Disk Space" "warn" "${DISK_PCT}% used (${DISK_AVAIL} free)" "Disk getting full"
    else
        add "Disk Space" "fail" "${DISK_PCT}% used (${DISK_AVAIL} free)" "Disk nearly full"
    fi
fi

# ── 6. Memory ─────────────────────────────────────────────────────
MEM_TOTAL=$(free -m 2>/dev/null | awk '/Mem:/{print $2}')
MEM_AVAIL=$(free -m 2>/dev/null | awk '/Mem:/{print $7}')
if [ -n "$MEM_TOTAL" ] && [ -n "$MEM_AVAIL" ] && [ "$MEM_TOTAL" -gt 0 ]; then
    MEM_USED=$((MEM_TOTAL - MEM_AVAIL))
    MEM_PCT=$((MEM_USED * 100 / MEM_TOTAL))
    if [ "$MEM_PCT" -lt 80 ]; then
        add "Memory" "pass" "${MEM_USED}MB / ${MEM_TOTAL}MB (${MEM_PCT}%)" ""
    elif [ "$MEM_PCT" -lt 95 ]; then
        add "Memory" "warn" "${MEM_USED}MB / ${MEM_TOTAL}MB (${MEM_PCT}%)" "Memory pressure may cause slowdowns"
    else
        add "Memory" "fail" "${MEM_USED}MB / ${MEM_TOTAL}MB (${MEM_PCT}%)" "Memory critically low"
    fi
fi

# ── 7. Hermes API health ─────────────────────────────────────────
HEALTH_RESP=$(curl -sf http://localhost:8642/health 2>/dev/null)
if [ $? -eq 0 ]; then
    add "API Server" "pass" "Hermes API responding on port 8642" ""
else
    add "API Server" "fail" "Hermes API not responding" "curl http://localhost:8642/health failed"
fi

# ── 8. Host networking ────────────────────────────────────────────
HM_NET=$(docker inspect -f '{{.HostConfig.NetworkMode}}' hermes-agent 2>/dev/null)
if [ "$HM_NET" = "host" ]; then
    add "Network Mode" "pass" "Host networking" ""
else
    add "Network Mode" "warn" "Network mode: ${HM_NET}" "Expected host networking."
fi

# ── 9. Docker access from Hermes ─────────────────────────────────
if docker exec hermes-agent sh -lc 'docker version >/dev/null 2>&1 && docker compose version >/dev/null 2>&1'; then
    add "Docker Access" "pass" "Docker CLI and Compose available inside Hermes" ""
else
    DOCKER_DETAIL=$(docker exec hermes-agent sh -lc 'docker version 2>&1; docker compose version 2>&1' 2>&1 | tail -20)
    add "Docker Access" "fail" "Hermes cannot use Docker from chat" "$DOCKER_DETAIL"
fi

TERMINAL_BACKEND=$(grep -A3 '^terminal:' /opt/hermes/data/config.yaml 2>/dev/null | grep 'backend:' | awk '{print $2}' | tr -d '"' || true)
if [ "$TERMINAL_BACKEND" = "local" ]; then
    add "Terminal Backend" "pass" "Local terminal with host Docker access" ""
else
    add "Terminal Backend" "warn" "Terminal backend: ${TERMINAL_BACKEND:-unknown}" "Expected local so Docker commands run from the Hermes container against the VPS Docker daemon."
fi

cat /tmp/tardi-health.json
rm -f /tmp/tardi-health.json
`

// DoctorHandler runs a smart health check on the VPS that detects real
// production issues: config drift, crash loops, and resource exhaustion.
// POST /api/instances/{id}/doctor
func DoctorHandler(deps Dependencies) http.HandlerFunc {
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

		var script string
		if inst.Framework == models.FrameworkHermes {
			script = hermesHealthCheckScript
		} else {
			script = healthCheckScript
		}
		out, err := sshexec.RunCommand(*inst.IPv4, deps.Config.SSHPrivateKey, *inst.RootPassword, script, 45*time.Second)
		if err != nil {
			slog.Error("doctor: ssh failed", "error", err, "instance_id", instanceID)
			WriteJSON(w, http.StatusOK, map[string]any{
				"error":  "Could not connect to your agent",
				"detail": err.Error(),
			})
			return
		}

		// Parse the JSON array of checks from the script output
		var checks []map[string]string
		if jsonErr := json.Unmarshal([]byte(out), &checks); jsonErr != nil {
			slog.Warn("doctor: failed to parse health check output",
				"instance_id", instanceID, "error", jsonErr, "raw", out)
			// Fall back to raw output
			WriteJSON(w, http.StatusOK, map[string]any{
				"checks": nil,
				"raw":    out,
			})
			return
		}

		WriteJSON(w, http.StatusOK, map[string]any{
			"checks": checks,
		})
	}
}
