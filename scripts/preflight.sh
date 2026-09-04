#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${1:-${SYNFACTORY_ENV_FILE:-$ROOT/.env}}"
ERRORS=0

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  ERRORS=$((ERRORS + 1))
}

info() {
  printf 'OK: %s\n' "$*"
}

placeholder() {
  local value="${1:-}"
  [[ -z "$value" || "$value" == *replace-with* || "$value" == *changeme* || "$value" == *factory.example.com* ]]
}

if [[ ! -f "$ENV_FILE" ]]; then
  echo "ERROR: environment file not found: $ENV_FILE" >&2
  exit 1
fi
set -a
# shellcheck disable=SC1090
source "$ENV_FILE"
set +a

command -v docker >/dev/null 2>&1 || fail "docker is required"
if command -v docker >/dev/null 2>&1; then
  docker compose version >/dev/null 2>&1 || fail "docker compose plugin is required"
fi
command -v python3 >/dev/null 2>&1 || fail "python3 is required to validate runtime configuration"

for pair in \
  "SYNFACTORY_DATABASE_URL:${SYNFACTORY_DATABASE_URL:-}" \
  "SYNFACTORY_OPERATOR_TOKEN:${SYNFACTORY_OPERATOR_TOKEN:-}" \
  "SYNFACTORY_GITHUB_WEBHOOK_SECRET:${SYNFACTORY_GITHUB_WEBHOOK_SECRET:-}"; do
  key="${pair%%:*}"
  value="${pair#*:}"
  if placeholder "$value"; then
    fail "$key is missing or still contains an example placeholder"
  fi
done

case "${SYNFACTORY_GITHUB_AUTH_MODE:-pat}" in
  pat)
    if placeholder "${SYNFACTORY_GITHUB_TOKEN:-}"; then
      fail "SYNFACTORY_GITHUB_TOKEN is required and must not be a placeholder in pat mode"
    fi
    ;;
  app)
    if [[ ! "${SYNFACTORY_GITHUB_APP_ID:-}" =~ ^[1-9][0-9]*$ ]]; then
      fail "SYNFACTORY_GITHUB_APP_ID must be a positive integer in app mode"
    fi
    key_file="${SYNFACTORY_GITHUB_APP_PRIVATE_KEY_FILE:-}"
    if [[ -z "$key_file" || ! -r "$key_file" ]]; then
      fail "GitHub App private key file is not readable: ${key_file:-<unset>}"
    fi
    ;;
  *)
    fail "SYNFACTORY_GITHUB_AUTH_MODE must be pat or app"
    ;;
esac

if placeholder "${SYNFACTORY_DOMAIN:-}"; then
  fail "SYNFACTORY_DOMAIN is missing or still contains the example domain"
fi

for key in SYNFACTORY_REPOSITORY_ROOT SYNFACTORY_WORKSPACE_ROOT; do
  value="${!key:-}"
  if [[ "$value" != /* ]]; then
    fail "$key must be an absolute host path"
  elif [[ ! -d "$value" ]]; then
    fail "$key directory does not exist: $value (create it before launch)"
  elif [[ ! -w "$value" ]]; then
    fail "$key directory is not writable: $value"
  fi
done

RUNTIME_HOST="${SYNFACTORY_RUNTIME_CONFIG_HOST:-$ROOT/config/runtimes.local.json}"
if [[ ! -r "$RUNTIME_HOST" ]]; then
  fail "runtime config is not readable: $RUNTIME_HOST"
elif command -v python3 >/dev/null 2>&1; then
  if ! python3 - "$RUNTIME_HOST" <<'PY'
import json, sys
path = sys.argv[1]
try:
    data = json.load(open(path, encoding="utf-8"))
except Exception as exc:
    print(f"runtime config parse failed: {exc}", file=sys.stderr)
    raise SystemExit(1)
runtimes = data.get("runtimes") or {}
roles = data.get("roles") or {}
if not runtimes or not roles:
    print("runtime config requires non-empty runtimes and roles", file=sys.stderr)
    raise SystemExit(1)
referenced = []
for role in roles.values():
    for item in role.get("chain", []):
        name = item.get("runtime")
        if name:
            referenced.append(name)
viable = False
for name in referenced:
    runtime = runtimes.get(name) or {}
    kind = runtime.get("kind", "")
    if runtime.get("binary"):
        viable = True
        break
    if kind == "openai_compatible":
        base = str(runtime.get("base_url", ""))
        model = str(runtime.get("model", ""))
        if base.startswith(("http://", "https://")) and model and "replace-with" not in model:
            viable = True
            break
if not viable:
    print("no structurally viable runtime is referenced by any role chain", file=sys.stderr)
    raise SystemExit(1)
PY
  then
    fail "runtime configuration has no viable role route"
  fi
fi

if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
  if ! docker compose --env-file "$ENV_FILE" -f "$ROOT/compose.yaml" --profile local-db config >/dev/null; then
    fail "docker compose configuration is invalid"
  fi
fi

if (( ERRORS > 0 )); then
  printf 'Preflight failed with %d error(s).\n' "$ERRORS" >&2
  exit 1
fi

info "environment, GitHub auth, storage roots, runtime config, and Compose configuration are launch-ready"
