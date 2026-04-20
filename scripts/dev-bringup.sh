#!/usr/bin/env bash
# dev-bringup.sh — Rebuild the Tardi dev GCP environment from a clean state.
#
# Prereq: scripts/dev-teardown.sh was used to tear dev down. The Artifact Registry
# still exists in GCP with a usable image. Secret backups live in
# scripts/.dev-secrets-backup/.
#
# Flow:
#   1. Re-import the preserved Artifact Registry into Terraform state
#   2. terraform apply (creates APIs, VPC, Cloud SQL, secrets, Cloud Run, IAM)
#   3. Restore manual secret values from the local backup
#   4. Bounce Cloud Run so it picks up the real secret versions
#   5. Poll /readyz until the service is healthy
#
# Usage: scripts/dev-bringup.sh [--yes]

set -euo pipefail

PROJECT="tardi-dev-488420"
REGION="us-central1"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKUP_DIR="$REPO_ROOT/scripts/.dev-secrets-backup"
TF_DIR="$REPO_ROOT/infra/environments/dev"
REGISTRY_ADDR="module.env.google_artifact_registry_repository.tardi"
REGISTRY_ID="projects/$PROJECT/locations/$REGION/repositories/tardi"
SERVICE="tardi-api-dev"
READYZ_URL="https://tardi-api-dev-lckw22k4gq-uc.a.run.app/readyz"
DASHBOARD_URL="https://dev.tardi-467.pages.dev"

ASSUME_YES=0
for arg in "$@"; do
  case "$arg" in
    --yes|-y) ASSUME_YES=1 ;;
    *) echo "Unknown arg: $arg" >&2; exit 2 ;;
  esac
done

echo "==> Tardi dev bring-up"
echo "    Project:  $PROJECT"
echo "    TF dir:   $TF_DIR"
echo "    Backup:   $BACKUP_DIR"
echo

# --- Sanity checks ------------------------------------------------------------

command -v gcloud    >/dev/null || { echo "ERROR: gcloud not found" >&2; exit 1; }
command -v terraform >/dev/null || { echo "ERROR: terraform not found" >&2; exit 1; }
command -v curl      >/dev/null || { echo "ERROR: curl not found" >&2; exit 1; }

if [[ ! -d "$BACKUP_DIR" ]]; then
  echo "ERROR: no secrets backup at $BACKUP_DIR" >&2
  echo "       Either run dev-teardown.sh first, or seed this directory with the"
  echo "       13 manual secret values (one file per secret, named dev-<name>.txt)." >&2
  exit 1
fi

if ! gcloud projects describe "$PROJECT" >/dev/null 2>&1; then
  echo "ERROR: cannot access project $PROJECT" >&2
  exit 1
fi

# Confirm registry is still alive (if gone, we can't provision Cloud Run)
if ! gcloud artifacts repositories describe tardi \
     --location="$REGION" --project="$PROJECT" >/dev/null 2>&1; then
  cat <<EOF >&2
ERROR: Artifact Registry 'tardi' is missing. Cloud Run needs an image to start.

Recovery:
  1. Let terraform recreate it: remove the import step from this script and run
     \`terraform apply\`. This will fail at the Cloud Run step.
  2. Then push an image manually:
     gh workflow run deploy-backend.yml --ref dev
     (wait for it to push the image, even though its deploy step will fail)
  3. Re-run this script.
EOF
  exit 1
fi

if [[ "$ASSUME_YES" -ne 1 ]]; then
  read -r -p "Proceed with bring-up? [y/N] " CONFIRM
  [[ "$CONFIRM" =~ ^[Yy]$ ]] || { echo "Aborted."; exit 1; }
fi

# --- Step 1: re-import Artifact Registry -------------------------------------

echo
echo "==> Importing Artifact Registry back into Terraform state"
cd "$TF_DIR"
terraform init -input=false >/dev/null

if terraform state list | grep -qx "$REGISTRY_ADDR"; then
  echo "    already imported"
else
  terraform import "$REGISTRY_ADDR" "$REGISTRY_ID"
fi

# --- Step 2: apply ------------------------------------------------------------

echo
echo "==> terraform apply"
terraform apply -auto-approve

# --- Step 3: restore secret values -------------------------------------------

echo
echo "==> Restoring secret values from $BACKUP_DIR"
shopt -s nullglob
RESTORED=0
for file in "$BACKUP_DIR"/dev-*.txt; do
  secret=$(basename "$file" .txt)
  # dev-database-url is managed by Terraform (regenerated on apply), don't overwrite
  if [[ "$secret" == "dev-database-url" ]]; then
    continue
  fi
  if gcloud secrets versions add "$secret" \
       --data-file="$file" --project="$PROJECT" >/dev/null 2>&1; then
    echo "    ok    $secret"
    RESTORED=$((RESTORED + 1))
  else
    echo "    FAIL  $secret (does it exist in Secret Manager?)" >&2
  fi
done
shopt -u nullglob
echo "    restored $RESTORED secret(s)"

# --- Step 4: bounce Cloud Run -------------------------------------------------

echo
echo "==> Bouncing Cloud Run to pick up real secret values"
gcloud run services update "$SERVICE" \
  --region="$REGION" --project="$PROJECT" \
  --update-labels="wake-ts=$(date +%s)" >/dev/null
echo "    new revision deploying"

# --- Step 5: wait for health --------------------------------------------------

echo
echo "==> Waiting for $READYZ_URL"
DEADLINE=$(( $(date +%s) + 180 ))
while true; do
  CODE=$(curl -s -o /dev/null -w "%{http_code}" --max-time 10 "$READYZ_URL" || echo "000")
  if [[ "$CODE" == "200" ]]; then
    echo "    ready (HTTP 200)"
    break
  fi
  if [[ $(date +%s) -ge $DEADLINE ]]; then
    echo "    timeout waiting for readiness (last: $CODE). Check logs:" >&2
    echo "      gcloud run services logs read $SERVICE --region=$REGION --project=$PROJECT --limit=50" >&2
    exit 1
  fi
  sleep 5
done

cat <<EOF

==> Bring-up complete.

API:       $READYZ_URL
Dashboard: $DASHBOARD_URL

Next push to the 'dev' branch will rebuild the backend image via
deploy-backend.yml and roll out a fresh revision.

To wind down again: $REPO_ROOT/scripts/dev-teardown.sh
EOF
