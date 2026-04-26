#!/usr/bin/env bash
# Usage: ./scripts/db-query.sh <env> <query|file>
# Examples:
#   ./scripts/db-query.sh prod "SELECT count(*) FROM users"
#   ./scripts/db-query.sh dev "SELECT * FROM instances LIMIT 5"
#   ./scripts/db-query.sh prod --file fix.sql

set -euo pipefail

PSQL="/opt/homebrew/opt/libpq/bin/psql"
ENV="${1:?Usage: db-query.sh <dev|prod> <query|--file path>}"
shift

case "$ENV" in
  dev)
    PROJECT="tardi-dev-488420"
    SECRET="dev-database-url"
    INSTANCE="tardi-dev-488420:us-central1:tardi-db-dev"
    PORT=5433
    ;;
  prod)
    PROJECT="tardi-prod-2026"
    SECRET="prod-database-url"
    INSTANCE="tardi-prod-2026:us-central1:tardi-db-prod"
    PORT=5434
    ;;
  *)
    echo "Error: env must be 'dev' or 'prod'" >&2
    exit 1
    ;;
esac

# Fetch DB URL from Secret Manager and rewrite for TCP connection
RAW_URL=$(gcloud secrets versions access latest --secret="$SECRET" --project="$PROJECT" 2>/dev/null)
# Replace unix socket path with localhost TCP
DB_URL=$(echo "$RAW_URL" | sed "s|@/tardi?host=/cloudsql/.*|@localhost:${PORT}/tardi?sslmode=disable|")

# Start Cloud SQL Auth Proxy in background
cloud-sql-proxy "$INSTANCE" --port "$PORT" --quiet &
PROXY_PID=$!

cleanup() {
  kill "$PROXY_PID" 2>/dev/null
  wait "$PROXY_PID" 2>/dev/null || true
}
trap cleanup EXIT

# Wait for proxy to be ready
for i in $(seq 1 10); do
  if /opt/homebrew/opt/libpq/bin/pg_isready -h localhost -p "$PORT" -q 2>/dev/null; then
    break
  fi
  sleep 0.5
done

# Run query
if [ "${1:-}" = "--file" ]; then
  FILE="${2:?Missing SQL file path}"
  "$PSQL" "$DB_URL" -f "$FILE"
else
  QUERY="$*"
  "$PSQL" "$DB_URL" -c "$QUERY"
fi
