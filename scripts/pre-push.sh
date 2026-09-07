#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

cleanup_db=0
container_name="synfactory-prepush-postgres-$$"

if [ -z "${SYNFACTORY_TEST_DATABASE_URL:-}" ]; then
  echo "==> Start isolated PostgreSQL on an ephemeral localhost port"
  docker run --rm -d \
    --name "$container_name" \
    -e POSTGRES_USER=postgres \
    -e POSTGRES_PASSWORD=postgres \
    -e POSTGRES_DB=synfactory_test \
    -p 127.0.0.1::5432 \
    postgres:16 >/dev/null
  cleanup_db=1
  trap 'if [ "$cleanup_db" = "1" ]; then docker rm -f "$container_name" >/dev/null 2>&1 || true; fi' EXIT

  port_mapping="$(docker port "$container_name" 5432/tcp)"
  db_port="${port_mapping##*:}"
  if ! [[ "$db_port" =~ ^[0-9]+$ ]]; then
    echo "Could not resolve temporary PostgreSQL port from: $port_mapping" >&2
    exit 1
  fi
  export SYNFACTORY_TEST_DATABASE_URL="postgres://postgres:postgres@127.0.0.1:${db_port}/synfactory_test?sslmode=disable"

  ready=0
  for _ in $(seq 1 30); do
    if docker exec "$container_name" pg_isready -U postgres -d synfactory_test >/dev/null 2>&1; then
      ready=1
      break
    fi
    sleep 1
  done
  if [ "$ready" != "1" ]; then
    echo "PostgreSQL did not become ready" >&2
    exit 1
  fi
else
  echo "==> Use provided SYNFACTORY_TEST_DATABASE_URL"
fi

echo "==> Verify module metadata"
go mod tidy
git diff --exit-code -- go.mod go.sum

echo "==> Go checks"
unformatted="$(gofmt -l .)"
if [ -n "$unformatted" ]; then
  echo "Unformatted Go files:" >&2
  echo "$unformatted" >&2
  exit 1
fi
go vet ./...
go test ./...

echo "==> Operations script syntax"
bash -n scripts/backup.sh scripts/restore.sh scripts/preflight.sh

echo "==> Frontend checks"
cd web
npm ci --prefer-offline --ignore-scripts --no-audit --no-fund
npm run build

echo "Pre-push core verification passed."
