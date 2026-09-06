#!/usr/bin/env bash
set -euo pipefail

MODE="${1:-run}"
BASE_URL="${SYNFACTORY_SOAK_BASE_URL:-http://127.0.0.1:8080}"
SAMPLES="${SYNFACTORY_SOAK_SAMPLES:-120}"
INTERVAL="${SYNFACTORY_SOAK_INTERVAL_SECONDS:-30}"
EVIDENCE_DIR="${SYNFACTORY_SOAK_EVIDENCE_DIR:-./data/autonomy-soak}"
FAULT_EVERY="${SYNFACTORY_SOAK_FAULT_EVERY:-0}"
FAULT_SEQUENCE="${SYNFACTORY_SOAK_FAULT_SEQUENCE:-api,scheduler,worker}"
COMPOSE_ARGS_STRING="${SYNFACTORY_SOAK_COMPOSE_ARGS:--f compose.yaml}"

mkdir -p "$EVIDENCE_DIR"
RUN_ID="${SYNFACTORY_SOAK_RUN_ID:-$(date -u +%Y%m%dT%H%M%SZ)}"
EVIDENCE_FILE="$EVIDENCE_DIR/$RUN_ID.jsonl"
FAULT_FILE="$EVIDENCE_DIR/$RUN_ID.faults.log"

read -r -a COMPOSE_ARGS <<< "$COMPOSE_ARGS_STRING"
IFS=',' read -r -a FAULT_SERVICES <<< "$FAULT_SEQUENCE"

require_positive_integer() {
  local name="$1" value="$2"
  if ! [[ "$value" =~ ^[0-9]+$ ]] || (( value < 1 )); then
    echo "$name must be a positive integer" >&2
    exit 2
  fi
}

sample_health() {
  local observed_at tmp status body
  observed_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  tmp="$(mktemp)"
  status="$(curl -sS --connect-timeout 5 --max-time 15 -o "$tmp" -w '%{http_code}' "$BASE_URL/ops" || true)"
  body="$(tr -d '\r\n' < "$tmp")"
  rm -f "$tmp"

  if [[ "$status" == "200" ]] && [[ "$body" == \{* ]]; then
    printf '{"observed_at":"%s","http_status":200,"stats":%s}\n' "$observed_at" "$body" | tee -a "$EVIDENCE_FILE" >/dev/null
    return 0
  fi

  printf '{"observed_at":"%s","http_status":%s,"stats":null}\n' "$observed_at" "${status:-0}" | tee -a "$EVIDENCE_FILE" >/dev/null
  return 1
}

restart_service() {
  local service="$1" observed_at
  case "$service" in
    api|scheduler|worker) ;;
    *) echo "unsupported fault service: $service" >&2; exit 2 ;;
  esac
  observed_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf '%s restart %s\n' "$observed_at" "$service" | tee -a "$FAULT_FILE"
  docker compose "${COMPOSE_ARGS[@]}" restart "$service"
}

case "$MODE" in
  sample)
    sample_health
    ;;
  restart)
    restart_service "${2:-}"
    ;;
  run)
    require_positive_integer SYNFACTORY_SOAK_SAMPLES "$SAMPLES"
    require_positive_integer SYNFACTORY_SOAK_INTERVAL_SECONDS "$INTERVAL"
    if ! [[ "$FAULT_EVERY" =~ ^[0-9]+$ ]]; then
      echo "SYNFACTORY_SOAK_FAULT_EVERY must be a non-negative integer" >&2
      exit 2
    fi
    failures=0
    fault_index=0
    for ((i = 1; i <= SAMPLES; i++)); do
      if ! sample_health; then
        failures=$((failures + 1))
      fi
      if (( FAULT_EVERY > 0 && i < SAMPLES && i % FAULT_EVERY == 0 )); then
        service="${FAULT_SERVICES[$((fault_index % ${#FAULT_SERVICES[@]}))]}"
        restart_service "$service"
        fault_index=$((fault_index + 1))
      fi
      if (( i < SAMPLES )); then sleep "$INTERVAL"; fi
    done
    printf 'soak run %s complete: samples=%d transport_failures=%d evidence=%s\n' "$RUN_ID" "$SAMPLES" "$failures" "$EVIDENCE_FILE"
    ;;
  *)
    echo "usage: $0 [run|sample|restart <api|scheduler|worker>]" >&2
    exit 2
    ;;
esac
