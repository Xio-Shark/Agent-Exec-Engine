#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
WORKFLOW_FILE="${WORKFLOW_FILE:-$ROOT_DIR/examples/obs-metrics-workflow.json}"
MAX_ATTEMPTS="${MAX_ATTEMPTS:-15}"
POLL_SECONDS="${POLL_SECONDS:-1}"

if [ ! -f "$WORKFLOW_FILE" ]; then
  echo "workflow file not found: $WORKFLOW_FILE" >&2
  exit 1
fi

echo "[1/3] health check: $BASE_URL/healthz"
curl -fsS "$BASE_URL/healthz" | python -m json.tool

echo "[2/3] create workflow from $WORKFLOW_FILE"
create_response="$(curl -fsS -X POST "$BASE_URL/api/v1/workflows" \
  -H 'Content-Type: application/json' \
  --data @"$WORKFLOW_FILE")"
printf '%s\n' "$create_response" | python -m json.tool

workflow_id="$(printf '%s' "$create_response" | python -c 'import json,sys; print(json.load(sys.stdin)["run"]["workflow_id"])')"
echo "[3/3] poll workflow status: $workflow_id"

attempt=1
while [ "$attempt" -le "$MAX_ATTEMPTS" ]; do
  run_response="$(curl -fsS "$BASE_URL/api/v1/workflows/$workflow_id")"
  status="$(printf '%s' "$run_response" | python -c 'import json,sys; print(json.load(sys.stdin)["run"]["status"])')"
  echo "attempt $attempt/$MAX_ATTEMPTS status=$status"

  if [ "$status" = "completed" ]; then
    printf '%s\n' "$run_response" | python -m json.tool
    exit 0
  fi

  if [ "$status" = "failed" ] || [ "$status" = "paused" ]; then
    printf '%s\n' "$run_response" | python -m json.tool
    echo "workflow ended with non-success status: $status" >&2
    exit 1
  fi

  sleep "$POLL_SECONDS"
  attempt=$((attempt + 1))
done

echo "timed out waiting for workflow completion after $MAX_ATTEMPTS attempts" >&2
echo "expected output shape reference: $ROOT_DIR/evidence/runtime/workflow-run.json" >&2
exit 1
