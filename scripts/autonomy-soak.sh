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
FAULT_EVIDENCE_FILE="$EVIDENCE_DIR/$RUN_ID.fault-validation.jsonl"

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

run_fault_scenario() {
  local name="$1" package="$2" pattern="$3" started_at finished_at tmp status
  started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  tmp="$(mktemp)"
  status="passed"
  if ! go test "$package" -run "$pattern" -count=1 >"$tmp" 2>&1; then
    status="failed"
  fi
  finished_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf '{"scenario":"%s","started_at":"%s","finished_at":"%s","status":"%s"}\n' \
    "$name" "$started_at" "$finished_at" "$status" | tee -a "$FAULT_EVIDENCE_FILE" >/dev/null
  if [[ "$status" != "passed" ]]; then
    cat "$tmp" >&2
    rm -f "$tmp"
    return 1
  fi
  rm -f "$tmp"
}

validate_faults() {
  local failures=0
  : > "$FAULT_EVIDENCE_FILE"

  run_fault_scenario \
    "provider_outage_falls_back_without_duplicate_execution" \
    "./internal/runtime" \
    '^TestRegistryFallsBackOnUnavailable$' || failures=$((failures + 1))

  run_fault_scenario \
    "webhook_loss_reconcile_repairs_truth_and_dedupes" \
    "./internal/github" \
    '^TestReconcilerEmitsCanonicalEventsWithoutDuplicatingSweep$' || failures=$((failures + 1))

  run_fault_scenario \
    "bounded_repair_and_independent_capacity" \
    "./internal/workflow" \
    '^(TestAutonomyFaultMatrixPreservesBoundedProgress|TestAutonomySelectionKeepsIndependentRoleCapacityUseful)$' || failures=$((failures + 1))

  printf 'fault validation %s complete: scenarios=3 failures=%d evidence=%s\n' \
    "$RUN_ID" "$failures" "$FAULT_EVIDENCE_FILE"
  (( failures == 0 ))
}

case "$MODE" in
  sample)
    sample_health
    ;;
  restart)
    restart_service "${2:-}"
    ;;
  validate-faults)
    validate_faults
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
    echo "usage: $0 [run|sample|restart <api|scheduler|worker>|validate-faults]" >&2
    exit 2
    ;;
esac
