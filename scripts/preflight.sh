#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${SYNFACTORY_ENV_FILE:-.env}"
PROFILE="${SYNFACTORY_COMPOSE_PROFILE:-local-db}"
errors=0
warnings=0

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  errors=$((errors + 1))
}

warn() {
  printf 'WARN: %s\n' "$*" >&2
  warnings=$((warnings + 1))
}

info() {
  printf 'OK: %s\n' "$*"
}

read_env() {
  local key="$1"
  [ -f "$ENV_FILE" ] || return 0
  awk -F= -v key="$key" '$1 == key {sub(/^[^=]*=/, ""); value=$0} END {print value}' "$ENV_FILE" | tr -d '\r'
}

require_secret() {
  local key="$1" value
  value="$(read_env "$key")"
  if [ -z "$value" ]; then
    fail "$key is empty in $ENV_FILE"
    return
  fi
  case "$value" in
    *replace-with-*|*example.com*) fail "$key still contains an example/placeholder value" ;;
    *) info "$key is configured" ;;
  esac
}

require_absolute_dir() {
  local key="$1" value
  value="$(read_env "$key")"
  if [ -z "$value" ]; then
    fail "$key is empty in $ENV_FILE"
    return
  fi
  case "$value" in
    /*) ;;
    *) fail "$key must be an absolute host path (got: $value)"; return ;;
  esac
  if [ ! -d "$value" ]; then
    fail "$key directory does not exist: $value (create it and make it writable by the worker host user)"
  elif [ ! -w "$value" ]; then
    fail "$key directory is not writable: $value"
  else
    info "$key is an existing writable absolute directory"
  fi
}

if ! command -v docker >/dev/null 2>&1; then
  fail "docker is not installed or not on PATH"
elif ! docker compose version >/dev/null 2>&1; then
  fail "Docker Compose v2 is unavailable (expected: docker compose ...)"
else
  info "Docker and Docker Compose are available"
fi

if [ ! -f "$ENV_FILE" ]; then
  fail "$ENV_FILE is missing (copy .env.example to .env and configure it)"
else
  info "$ENV_FILE exists"
  require_secret SYNFACTORY_DATABASE_URL
  require_secret SYNFACTORY_GITHUB_TOKEN
  require_secret SYNFACTORY_GITHUB_WEBHOOK_SECRET
  require_secret SYNFACTORY_OPERATOR_TOKEN
  require_absolute_dir SYNFACTORY_REPOSITORY_ROOT
  require_absolute_dir SYNFACTORY_WORKSPACE_ROOT
fi

runtime_config="$(read_env SYNFACTORY_RUNTIME_CONFIG_HOST)"
runtime_config="${runtime_config:-./config/runtimes.local.json}"
if [ ! -f "$runtime_config" ]; then
  fail "runtime config is missing: $runtime_config (copy config/runtimes.example.json first)"
else
  if ! grep -q '"kind"[[:space:]]*:' "$runtime_config"; then
    fail "runtime config has no runtime entries: $runtime_config"
  else
    info "runtime config contains at least one runtime entry"
  fi
  if grep -q 'replace-with-router-model' "$runtime_config"; then
    warn "router-fallback still uses replace-with-router-model; set a real model before relying on that fallback"
  fi
  if grep -q '127\.0\.0\.1:20128' "$runtime_config"; then
    fail "runtime config points to 127.0.0.1:20128; a Compose worker must use host.docker.internal for a host-local 9router"
  fi
fi

agent_bin="$(read_env SYNFACTORY_AGENT_BIN)"
agent_bin="${agent_bin:-./data/agent-bin}"
if [ ! -d "$agent_bin" ]; then
  warn "agent binary directory does not exist yet: $agent_bin; create/mount it if using standalone CLI binaries"
fi

if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1 && [ -f "$ENV_FILE" ]; then
  if docker compose --profile "$PROFILE" config --quiet >/dev/null 2>&1; then
    info "docker compose --profile $PROFILE config is valid"
  else
    fail "docker compose --profile $PROFILE config failed; run it without --quiet for details"
  fi
fi

if [ "$errors" -gt 0 ]; then
  printf '\nPreflight failed with %d error(s) and %d warning(s).\n' "$errors" "$warnings" >&2
  exit 1
fi

printf '\nPreflight passed with %d warning(s). External provider authentication is intentionally verified at runtime/probe time.\n' "$warnings"
