#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

export SYNFACTORY_TEST_DATABASE_URL="${SYNFACTORY_TEST_DATABASE_URL:-postgres://postgres:postgres@localhost:5432/synfactory_test?sslmode=disable}"

echo "==> Start local PostgreSQL"
docker compose --profile local-db up -d --wait postgres

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
