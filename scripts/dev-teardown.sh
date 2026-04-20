#!/usr/bin/env bash
# dev-teardown.sh — Destroy the Tardi dev GCP environment to near-zero idle cost.
#
# What it does:
#   1. Backs up all dev-* Secret Manager values to scripts/.dev-secrets-backup/ (gitignored)
#   2. Optionally dumps the Cloud SQL database via Cloud SQL Auth Proxy (--dump-db)
#   3. Removes the Artifact Registry from Terraform state so images survive the destroy
#   4. Runs `terraform destroy` on infra/environments/dev/
#
# Resume with: scripts/dev-bringup.sh
#
# Usage: scripts/dev-teardown.sh [--dump-db] [--yes]

set -euo pipefail

PROJECT="tardi-dev-488420"
REGION="us-central1"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKUP_DIR="$REPO_ROOT/scripts/.dev-secrets-backup"
TF_DIR="$REPO_ROOT/infra/environments/dev"
REGISTRY_ADDR="module.env.google_artifact_registry_repository.tardi"

DUMP_DB=0
ASSUME_YES=0
for arg in "$@"; do
  case "$arg" in
    --dump-db) DUMP_DB=1 ;;
    --yes|-y)  ASSUME_YES=1 ;;
    *) echo "Unknown arg: $arg" >&2; exit 2 ;;
  esac
done

echo "==> Tardi dev teardown"
echo "    Project:  $PROJECT"
echo "    TF dir:   $TF_DIR"
echo "    Backup:   $BACKUP_DIR"
echo

# --- Sanity checks ------------------------------------------------------------

command -v gcloud     >/dev/null || { echo "ERROR: gcloud not found" >&2; exit 1; }
command -v terraform  >/dev/null || { echo "ERROR: terraform not found" >&2; exit 1; }

ACTIVE_ACCT=$(gcloud auth list --filter=status:ACTIVE --format="value(account)" 2>/dev/null || true)
if [[ -z "$ACTIVE_ACCT" ]]; then
  echo "ERROR: no active gcloud account. Run: gcloud auth login && gcloud auth application-default login" >&2
  exit 1
fi
echo "    gcloud:   $ACTIVE_ACCT"

if ! gcloud projects describe "$PROJECT" >/dev/null 2>&1; then
  echo "ERROR: cannot access project $PROJECT with this account" >&2
  exit 1
fi

# --- Confirm ------------------------------------------------------------------

if [[ "$ASSUME_YES" -ne 1 ]]; then
  echo
  echo "This will DESTROY all dev infra: Cloud SQL (incl. database contents),"
  echo "Cloud Run, VPC, IAM, 14 Secret Manager entries, logging config."
  echo "Artifact Registry images are preserved. Terraform state bucket is preserved."
  read -r -p "Type 'destroy dev' to continue: " CONFIRM
  if [[ "$CONFIRM" != "destroy dev" ]]; then
    echo "Aborted."
    exit 1
  fi
fi

# --- Step 1: backup secrets ---------------------------------------------------

echo
echo "==> Backing up Secret Manager values to $BACKUP_DIR"
mkdir -p "$BACKUP_DIR"
chmod 700 "$BACKUP_DIR"

SECRET_NAMES=$(gcloud secrets list --project="$PROJECT" \
  --filter="name~projects/.*/secrets/dev-" \
  --format="value(name)" 2>/dev/null || true)

if [[ -z "$SECRET_NAMES" ]]; then
  echo "    (no dev-* secrets found — nothing to back up)"
else
  while IFS= read -r secret; do
    [[ -z "$secret" ]] && continue
    # dev-database-url is regenerated from Cloud SQL on apply — don't cache it
    if [[ "$secret" == "dev-database-url" ]]; then
      echo "    skip  $secret (regenerated on apply)"
      continue
    fi
    OUT="$BACKUP_DIR/$secret.txt"
    if gcloud secrets versions access latest --secret="$secret" --project="$PROJECT" > "$OUT" 2>/dev/null; then
      chmod 600 "$OUT"
      VAL=$(head -c 12 "$OUT")
      if [[ "$VAL" == "PLACEHOLDER" ]]; then
        echo "    WARN  $secret still at PLACEHOLDER — bring-up will restore the same placeholder"
      else
        echo "    ok    $secret ($(wc -c < "$OUT" | tr -d ' ') bytes)"
      fi
    else
      echo "    FAIL  $secret (no accessible version)" >&2
      rm -f "$OUT"
    fi
  done <<< "$SECRET_NAMES"
fi

# --- Step 2: optional DB dump -------------------------------------------------

if [[ "$DUMP_DB" -eq 1 ]]; then
  echo
  echo "==> Dumping Cloud SQL database"
  command -v cloud-sql-proxy >/dev/null || { echo "ERROR: cloud-sql-proxy not found" >&2; exit 1; }
  PGDUMP="/opt/homebrew/opt/libpq/bin/pg_dump"
  [[ -x "$PGDUMP" ]] || PGDUMP="pg_dump"
  command -v "$PGDUMP" >/dev/null || { echo "ERROR: pg_dump not found (brew install libpq)" >&2; exit 1; }

  INSTANCE="$PROJECT:$REGION:tardi-db-dev"
  PORT=5433
  RAW_URL=$(gcloud secrets versions access latest --secret="dev-database-url" --project="$PROJECT")
  DB_URL=$(echo "$RAW_URL" | sed "s|@/tardi?host=/cloudsql/.*|@localhost:${PORT}/tardi?sslmode=disable|")

  cloud-sql-proxy "$INSTANCE" --port "$PORT" --quiet &
  PROXY_PID=$!
  trap 'kill "$PROXY_PID" 2>/dev/null || true; wait "$PROXY_PID" 2>/dev/null || true' EXIT

  for _ in $(seq 1 20); do
    if /opt/homebrew/opt/libpq/bin/pg_isready -h localhost -p "$PORT" -q 2>/dev/null; then break; fi
    sleep 0.5
  done

  DUMP_FILE="$BACKUP_DIR/db-$(date +%Y%m%d-%H%M%S).sql"
  "$PGDUMP" "$DB_URL" --no-owner --no-acl > "$DUMP_FILE"
  chmod 600 "$DUMP_FILE"
  echo "    wrote $DUMP_FILE ($(wc -c < "$DUMP_FILE" | tr -d ' ') bytes)"

  kill "$PROXY_PID" 2>/dev/null || true
  wait "$PROXY_PID" 2>/dev/null || true
  trap - EXIT
fi

# --- Step 3: preserve Artifact Registry --------------------------------------

echo
echo "==> Removing Artifact Registry from Terraform state (preserves images)"
cd "$TF_DIR"
terraform init -input=false >/dev/null

if terraform state list | grep -qx "$REGISTRY_ADDR"; then
  terraform state rm "$REGISTRY_ADDR"
  echo "    removed $REGISTRY_ADDR from state"
else
  echo "    $REGISTRY_ADDR not in state (already removed or never created)"
fi

# --- Step 4: destroy ----------------------------------------------------------

echo
echo "==> terraform destroy"
terraform destroy -auto-approve

# --- Summary ------------------------------------------------------------------

cat <<EOF

==> Teardown complete.

Preserved:
  - Artifact Registry repo 'tardi' (images)
  - Terraform state bucket: tardi-dev-488420-terraform-state
  - Local secrets backup:   $BACKUP_DIR

Residual idle cost: ~\$0.07/mo (state bucket + registry).

Do NOT push to the 'dev' branch while dev is torn down — deploy-backend
will fail (Cloud Run service no longer exists and migrations can't run).

To bring dev back up:
  $REPO_ROOT/scripts/dev-bringup.sh
EOF
