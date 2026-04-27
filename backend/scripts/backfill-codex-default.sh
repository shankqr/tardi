#!/usr/bin/env bash
# Usage: ./scripts/backfill-codex-default.sh <dev|prod> [--apply]
#
# Switches all existing OpenClaw instances to codex/gpt-5.5 as primary:
#   1. UPDATEs agent_configs rows so model/provider match the new default
#      (so future re-syncs don't revert).
#   2. SSHes into every active OpenClaw VPS and pushes the change live via
#      `openclaw config set agents.defaults.model.primary`.
#
# Without --apply, runs in dry-run mode (lists what would change, no writes).

set -euo pipefail

ENV="${1:?Usage: backfill-codex-default.sh <dev|prod> [--apply]}"
APPLY="${2:-}"

case "$ENV" in
  dev)
    SSH_KEY="$HOME/.ssh/tardi-backend"
    ;;
  prod)
    SSH_KEY="$HOME/.ssh/tardi-backend-prod"
    ;;
  *)
    echo "Error: env must be 'dev' or 'prod'" >&2
    exit 1
    ;;
esac

if [ ! -f "$SSH_KEY" ]; then
  echo "Error: SSH key not found at $SSH_KEY" >&2
  echo "Re-fetch from Secret Manager (see reference_vps_ssh_access.md)." >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DB_QUERY="$SCRIPT_DIR/db-query.sh"

DRY_RUN=true
if [ "$APPLY" = "--apply" ]; then
  DRY_RUN=false
fi

echo "==> Environment: $ENV  (dry-run: $DRY_RUN)"
echo

# --- Step 1: report (and optionally apply) DB changes ----------------------

echo "==> agent_configs rows that will change:"
"$DB_QUERY" "$ENV" "SELECT vps_instance_id,
       config->>'provider' AS old_provider,
       config->>'model'    AS old_model
  FROM agent_configs
 WHERE COALESCE(config->>'model','') <> 'codex/gpt-5.5'
    OR COALESCE(config->>'provider','') <> 'codex';"

if [ "$DRY_RUN" = false ]; then
  echo "==> Applying agent_configs UPDATE..."
  "$DB_QUERY" "$ENV" "UPDATE agent_configs
       SET config = jsonb_set(
                      jsonb_set(COALESCE(config, '{}'::jsonb), '{model}', '\"codex/gpt-5.5\"'),
                      '{provider}', '\"codex\"');"
fi
echo

# --- Step 2: list active OpenClaw VPSes ------------------------------------

echo "==> Active OpenClaw VPSes:"
# Tab-separated, headerless rows of "<id>\t<ipv4>"
ROWS=$("$DB_QUERY" "$ENV" "\\pset format unaligned
\\pset tuples_only on
\\pset fieldsep '|'
SELECT id, ipv4
  FROM vps_instances
 WHERE status = 'active'
   AND framework = 'openclaw'
   AND ipv4 IS NOT NULL;")

if [ -z "$ROWS" ]; then
  echo "  (none)"
  exit 0
fi
echo "$ROWS"
echo

if [ "$DRY_RUN" = true ]; then
  echo "Dry-run complete. Re-run with --apply to push changes."
  exit 0
fi

# --- Step 3: push live config to each VPS ----------------------------------

OK_COUNT=0
FAIL_COUNT=0
FAILED_IPS=()

while IFS='|' read -r INST_ID IP; do
  [ -z "$IP" ] && continue
  echo "==> $INST_ID ($IP)"
  if ssh -i "$SSH_KEY" -o StrictHostKeyChecking=no -o ConnectTimeout=10 "root@$IP" \
       "docker exec openclaw-gateway openclaw models set 'codex/gpt-5.5' >/dev/null 2>&1 && \
        docker exec openclaw-gateway openclaw config set agents.defaults.model.primary 'codex/gpt-5.5'" \
       2>&1 | sed 's/^/    /'; then
    OK_COUNT=$((OK_COUNT + 1))
  else
    FAIL_COUNT=$((FAIL_COUNT + 1))
    FAILED_IPS+=("$IP")
  fi
done <<< "$ROWS"

echo
echo "==> Done. ok=$OK_COUNT  failed=$FAIL_COUNT"
if [ "$FAIL_COUNT" -gt 0 ]; then
  echo "Failed VPSes:"
  printf '  %s\n' "${FAILED_IPS[@]}"
  exit 1
fi
