#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${SYNFACTORY_ENV_FILE:-$ROOT/.env}"
BACKUP_ROOT="${SYNFACTORY_BACKUP_DIR:-$ROOT/backups}"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
WORK="$BACKUP_ROOT/.tmp-$STAMP-$$"
ARCHIVE="$BACKUP_ROOT/synfactory-$STAMP.tar.gz"

mkdir -p "$BACKUP_ROOT" "$WORK"
trap 'rm -rf "$WORK"' EXIT

if [[ -f "$ENV_FILE" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$ENV_FILE"
  set +a
fi

: "${SYNFACTORY_DATABASE_URL:?SYNFACTORY_DATABASE_URL is required}"

backup_database() {
  if command -v docker >/dev/null 2>&1 \
    && docker compose -f "$ROOT/compose.yaml" ps -q postgres 2>/dev/null | grep -q .; then
    : "${POSTGRES_USER:=synfactory}"
    : "${POSTGRES_DB:=synfactory}"
    docker compose -f "$ROOT/compose.yaml" exec -T postgres \
      pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --format=custom > "$WORK/database.dump"
    return
  fi
  command -v pg_dump >/dev/null 2>&1 || {
    echo "pg_dump is required when the Compose postgres service is not running" >&2
    exit 1
  }
  pg_dump "$SYNFACTORY_DATABASE_URL" --format=custom --file="$WORK/database.dump"
}

backup_database

RUNTIME_HOST="${SYNFACTORY_RUNTIME_CONFIG_HOST:-$ROOT/config/runtimes.local.json}"
if [[ -f "$RUNTIME_HOST" ]]; then
  cp "$RUNTIME_HOST" "$WORK/runtimes.json"
fi

cat > "$WORK/README.txt" <<'EOF'
SynFactory backup bundle.
- database.dump is the durable PostgreSQL source of truth.
- runtimes.json is included only when a local runtime config existed.
- .env, API keys, CLI login credentials and webhook secrets are intentionally NOT backed up.
Restore those secrets from the deployment secret store.
EOF

(
  cd "$WORK"
  sha256sum database.dump README.txt > manifest.sha256
  if [[ -f runtimes.json ]]; then
    sha256sum runtimes.json >> manifest.sha256
  fi
)

tar -C "$WORK" -czf "$ARCHIVE" .
chmod 600 "$ARCHIVE"
echo "$ARCHIVE"
