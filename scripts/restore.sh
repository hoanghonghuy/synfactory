#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 /path/to/synfactory-backup.tar.gz" >&2
  exit 2
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${SYNFACTORY_ENV_FILE:-$ROOT/.env}"
ARCHIVE="$(realpath "$1")"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

if [[ -f "$ENV_FILE" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$ENV_FILE"
  set +a
fi

: "${SYNFACTORY_DATABASE_URL:?SYNFACTORY_DATABASE_URL is required}"

tar -xzf "$ARCHIVE" -C "$WORK"
(
  cd "$WORK"
  sha256sum -c manifest.sha256
)

restore_database() {
  if command -v docker >/dev/null 2>&1 \
    && docker compose -f "$ROOT/compose.yaml" ps -q postgres 2>/dev/null | grep -q .; then
    : "${POSTGRES_USER:=synfactory}"
    : "${POSTGRES_DB:=synfactory}"
    docker compose -f "$ROOT/compose.yaml" exec -T postgres \
      pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
      --clean --if-exists --no-owner --no-privileges --single-transaction < "$WORK/database.dump"
    return
  fi
  command -v pg_restore >/dev/null 2>&1 || {
    echo "pg_restore is required when the Compose postgres service is not running" >&2
    exit 1
  }
  pg_restore --dbname="$SYNFACTORY_DATABASE_URL" \
    --clean --if-exists --no-owner --no-privileges --single-transaction "$WORK/database.dump"
}

restore_database

if [[ -f "$WORK/runtimes.json" ]]; then
  RESTORED_RUNTIME="$ROOT/config/runtimes.restored.json"
  cp "$WORK/runtimes.json" "$RESTORED_RUNTIME"
  chmod 600 "$RESTORED_RUNTIME"
  echo "runtime config restored to $RESTORED_RUNTIME; review it before replacing production config"
fi

echo "database restore completed; restore .env and CLI credentials separately, then run: docker compose up -d"
