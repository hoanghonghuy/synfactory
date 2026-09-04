#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${1:-${SYNFACTORY_ENV_FILE:-$ROOT/.env}}"
ERRORS=0
GITHUB_AUTH_MODE="pat"

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

resolve_host_path() {
  local value="$1"
  if [[ "$value" == /* ]]; then
    printf '%s\n' "$value"
  else
    printf '%s/%s\n' "$ROOT" "${value#./}"
  fi
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

GITHUB_AUTH_MODE="${SYNFACTORY_GITHUB_AUTH_MODE:-pat}"
case "$GITHUB_AUTH_MODE" in
  pat)
    if placeholder "${SYNFACTORY_GITHUB_TOKEN:-}"; then
      fail "SYNFACTORY_GITHUB_TOKEN is required and must not be a placeholder in pat mode"
    fi
    ;;
  app)
    if [[ ! "${SYNFACTORY_GITHUB_APP_ID:-}" =~ ^[1-9][0-9]*$ ]]; then
      fail "SYNFACTORY_GITHUB_APP_ID must be a positive integer in app mode"
    fi
    key_host="$(resolve_host_path "${SYNFACTORY_GITHUB_APP_PRIVATE_KEY_HOST:-}")"
    if [[ -z "${SYNFACTORY_GITHUB_APP_PRIVATE_KEY_HOST:-}" || ! -r "$key_host" ]]; then
      fail "GitHub App host private key is not readable: ${SYNFACTORY_GITHUB_APP_PRIVATE_KEY_HOST:-<unset>}"
    fi
    if [[ "${SYNFACTORY_GITHUB_APP_PRIVATE_KEY_FILE:-}" != "/run/secrets/synfactory-github-app.pem" ]]; then
      fail "stock Compose App mode requires SYNFACTORY_GITHUB_APP_PRIVATE_KEY_FILE=/run/secrets/synfactory-github-app.pem"
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

RUNTIME_HOST="$(resolve_host_path "${SYNFACTORY_RUNTIME_CONFIG_HOST:-config/runtimes.local.json}")"
AGENT_BIN_HOST="$(resolve_host_path "${SYNFACTORY_AGENT_BIN:-data/agent-bin}")"
AGENT_HOME_HOST="$(resolve_host_path "${SYNFACTORY_AGENT_HOME:-data/agent-home}")"
if [[ ! -r "$RUNTIME_HOST" ]]; then
  fail "runtime config is not readable: $RUNTIME_HOST"
elif command -v python3 >/dev/null 2>&1; then
  if ! python3 - "$RUNTIME_HOST" "$AGENT_BIN_HOST" "$AGENT_HOME_HOST" <<'PY'
import json
import os
import pathlib
import shutil
import sys

path, agent_bin, agent_home = sys.argv[1:]
try:
    with open(path, encoding="utf-8") as handle:
        data = json.load(handle)
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
        if name and name not in referenced:
            referenced.append(name)

search_dirs = [pathlib.Path(agent_bin), pathlib.Path(agent_home) / ".local" / "bin"]

def binary_available(binary):
    if not binary:
        return False
    candidate = pathlib.Path(binary)
    if candidate.is_absolute():
        return candidate.is_file() and os.access(candidate, os.X_OK)
    if "/" in binary:
        return False
    if shutil.which(binary):
        return True
    return any((directory / binary).is_file() and os.access(directory / binary, os.X_OK) for directory in search_dirs)

def secret_available(name):
    value = os.environ.get(name, "").strip()
    return bool(value) and "replace-with" not in value and "changeme" not in value

for name in referenced:
    runtime = runtimes.get(name) or {}
    if binary_available(str(runtime.get("binary", ""))):
        print(f"runtime route available: {name}")
        raise SystemExit(0)
    if runtime.get("kind") == "openai_compatible":
        base = str(runtime.get("base_url", ""))
        model = str(runtime.get("model", ""))
        key_env = str(runtime.get("api_key_env", ""))
        if (
            base.startswith(("http://", "https://"))
            and model
            and "replace-with" not in model
            and key_env
            and secret_available(key_env)
        ):
            print(f"runtime route available: {name}")
            raise SystemExit(0)

print(
    "no usable runtime route: install at least one referenced CLI in the mounted agent bin/home, "
    "or configure a referenced OpenAI-compatible endpoint with model and API key",
    file=sys.stderr,
)
raise SystemExit(1)
PY
  then
    fail "runtime configuration has no usable role route"
  fi
fi

if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
  compose_args=(--env-file "$ENV_FILE" -f "$ROOT/compose.yaml")
  if [[ "$GITHUB_AUTH_MODE" == "app" ]]; then
    compose_args+=(-f "$ROOT/compose.github-app.yaml")
  fi
  if ! docker compose "${compose_args[@]}" --profile local-db config >/dev/null; then
    fail "docker compose configuration is invalid for GitHub auth mode $GITHUB_AUTH_MODE"
  fi
fi

if (( ERRORS > 0 )); then
  printf 'Preflight failed with %d error(s).\n' "$ERRORS" >&2
  exit 1
fi

info "environment, GitHub auth, storage roots, runtime route, and Compose configuration are launch-ready"
