#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
WORKFLOW_FILE="${WORKFLOW_FILE:-$ROOT_DIR/examples/cross-project-workflow.json}"
MAX_ATTEMPTS="${MAX_ATTEMPTS:-30}"
POLL_SECONDS="${POLL_SECONDS:-1}"
OUTPUT_DIR="${OUTPUT_DIR:-}"

BLUE='\033[0;34m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'

info()  { echo -e "${BLUE}[INFO]${NC}  $*"; }
ok()    { echo -e "${GREEN}[OK]${NC}    $*"; }
warn()  { echo -e "${YELLOW}[WAIT]${NC}  $*"; }

if [ ! -f "$WORKFLOW_FILE" ]; then
  echo "workflow file not found: $WORKFLOW_FILE" >&2
  exit 1
fi

info "1/7  健康检查"
curl -fsS "$BASE_URL/healthz" | python3 -m json.tool
echo

info "2/7  校验关键工具已注册（rag_search / code_exec）"
tools_json="$(curl -fsS "$BASE_URL/api/v1/tools")"
printf '%s\n' "$tools_json" | python3 -m json.tool
printf '%s' "$tools_json" | python3 -c 'import json, sys; payload=json.load(sys.stdin); names={tool["name"] for tool in payload.get("tools", [])}; required={"rag_search", "code_exec"}; missing=sorted(required-names); assert not missing, f"missing tools: {missing}"'
ok "关键工具已可用"
echo

info "3/7  提交跨项目 workflow"
create_response="$(curl -fsS -X POST "$BASE_URL/api/v1/workflows" \
  -H 'Content-Type: application/json' \
  --data @"$WORKFLOW_FILE")"
printf '%s\n' "$create_response" | python3 -m json.tool

workflow_id="$(printf '%s' "$create_response" | python3 -c 'import json,sys; print(json.load(sys.stdin)["run"]["workflow_id"])')"
run_id="$(printf '%s' "$create_response" | python3 -c 'import json,sys; print(json.load(sys.stdin)["run"]["id"])')"
ok "workflow=$workflow_id run=$run_id"
echo

info "4/7  轮询状态，等待 human-review 暂停"
status="pending"
attempt=1
while [ "$attempt" -le "$MAX_ATTEMPTS" ]; do
  run_response="$(curl -fsS "$BASE_URL/api/v1/workflows/$workflow_id")"
  status="$(printf '%s' "$run_response" | python3 -c 'import json,sys; print(json.load(sys.stdin)["run"]["status"])')"
  echo "attempt $attempt/$MAX_ATTEMPTS status=$status"

  if [ "$status" = "paused" ] || [ "$status" = "completed" ] || [ "$status" = "failed" ]; then
    break
  fi

  sleep "$POLL_SECONDS"
  attempt=$((attempt + 1))
done
echo

if [ "$status" = "paused" ]; then
  warn "工作流已暂停，准备 resume"
  curl -fsS "$BASE_URL/api/v1/workflows/$workflow_id/steps" | python3 -m json.tool
  echo

  info "5/7  人工确认后 resume"
  curl -fsS -X POST "$BASE_URL/api/v1/workflows/$workflow_id/resume" \
    -H 'Content-Type: application/json' \
    -d '{"step_id": "human-review", "input": {"approved": true, "reviewer": "demo-script"}}' \
    | python3 -m json.tool

  attempt=1
  while [ "$attempt" -le "$MAX_ATTEMPTS" ]; do
    run_response="$(curl -fsS "$BASE_URL/api/v1/workflows/$workflow_id")"
    status="$(printf '%s' "$run_response" | python3 -c 'import json,sys; print(json.load(sys.stdin)["run"]["status"])')"
    echo "resume attempt $attempt/$MAX_ATTEMPTS status=$status"

    if [ "$status" = "completed" ] || [ "$status" = "failed" ]; then
      break
    fi

    sleep "$POLL_SECONDS"
    attempt=$((attempt + 1))
  done
else
  info "5/7  跳过 resume，当前状态为 $status"
fi
echo

info "6/7  打印最终 workflow 与步骤"
final_workflow="$(curl -fsS "$BASE_URL/api/v1/workflows/$workflow_id")"
final_steps="$(curl -fsS "$BASE_URL/api/v1/workflows/$workflow_id/steps")"
printf '%s\n' "$final_workflow" | python3 -m json.tool
echo
printf '%s\n' "$final_steps" | python3 -m json.tool
echo

if [ -n "$OUTPUT_DIR" ]; then
  mkdir -p "$OUTPUT_DIR"
  printf '%s\n' "$final_workflow" > "$OUTPUT_DIR/workflow-run.json"
  printf '%s\n' "$final_steps" > "$OUTPUT_DIR/workflow-steps.json"
  ok "已写出 demo 结果到 $OUTPUT_DIR"

  # 自动收集外围证据（gateway health / metrics / trace）
  COLLECTOR="$ROOT_DIR/scripts/collect-cross-project-evidence.sh"
  if [ -x "$COLLECTOR" ]; then
    info "调用辅助脚本收集外围证据..."
    "$COLLECTOR" "$workflow_id" "$OUTPUT_DIR" || warn "外围证据收集部分失败，但不影响主流程"
  fi
fi

info "7/7  观测入口"
echo "- Workflow API: $BASE_URL/api/v1/workflows/$workflow_id"
echo "- Workflow Steps: $BASE_URL/api/v1/workflows/$workflow_id/steps"
echo "- Prometheus: http://127.0.0.1:9090"
echo "- Jaeger: http://127.0.0.1:16686"
echo "- 提示：若未设置 QDRANT_URL，rag_search 会返回 stub 结果；该脚本仍可验证控制流、gateway llm_call、sandbox 和 pause/resume。"

if [ "$status" = "completed" ]; then
  ok "✅ 跨项目 demo 执行完成"
  exit 0
fi

echo "workflow ended with status: $status" >&2
exit 1