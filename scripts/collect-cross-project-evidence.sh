#!/usr/bin/env bash
# collect-cross-project-evidence.sh
# 辅助收集跨项目 demo 的外围证据（gateway health / metrics / trace）
# 用法: ./scripts/collect-cross-project-evidence.sh <workflow_id> [output_dir]

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

WORKFLOW_ID="${1:-}"
OUTPUT_DIR="${2:-$ROOT_DIR/evidence/cross-project}"

AEE_URL="${AEE_URL:-http://127.0.0.1:8080}"
GATEWAY_URL="${GATEWAY_URL:-http://127.0.0.1:8081}"
PROMETHEUS_URL="${PROMETHEUS_URL:-http://127.0.0.1:9090}"
JAEGER_URL="${JAEGER_URL:-http://127.0.0.1:16686}"

BLUE='\033[0;34m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; NC='\033[0m'
info()  { echo -e "${BLUE}[INFO]${NC}  $*"; }
ok()    { echo -e "${GREEN}[OK]${NC}    $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}   $*"; }
err()   { echo -e "${RED}[ERR]${NC}    $*" >&2; }

usage() {
  cat <<EOF
Usage: $(basename "$0") <workflow_id> [output_dir]

Collect peripheral evidence for a cross-project workflow run:
  - Gateway health status
  - Prometheus metrics (agent_exec + gateway)
  - Jaeger trace summary
  - AEE run metadata

Environment:
  AEE_URL        Agent Execution Engine base URL (default: http://127.0.0.1:8080)
  GATEWAY_URL    AI Infra Gateway base URL (default: http://127.0.0.1:8081)
  PROMETHEUS_URL Prometheus URL (default: http://127.0.0.1:9090)
  JAEGER_URL     Jaeger UI URL (default: http://127.0.0.1:16686)

Example:
  $(basename "$0") 7c907514-1730-4bb1-bde8-982050800a6a ./evidence/cross-project/run-001
EOF
}

if [ -z "$WORKFLOW_ID" ] || [ "$WORKFLOW_ID" = "--help" ] || [ "$WORKFLOW_ID" = "-h" ]; then
  usage
  exit 1
fi

info "收集跨项目证据: workflow_id=$WORKFLOW_ID"
info "输出目录: $OUTPUT_DIR"
mkdir -p "$OUTPUT_DIR"

# ── 1. AEE Run Metadata ─────────────────────────────────────────────────────
info "1/5  收集 AEE workflow 元数据"
if curl -fsS "$AEE_URL/api/v1/workflows/$WORKFLOW_ID" > "$OUTPUT_DIR/workflow-run.json" 2>/dev/null; then
  ok "workflow-run.json"
else
  warn "无法获取 workflow 详情，AEE 可能未运行"
fi

if curl -fsS "$AEE_URL/api/v1/workflows/$WORKFLOW_ID/steps" > "$OUTPUT_DIR/workflow-steps.json" 2>/dev/null; then
  ok "workflow-steps.json"
else
  warn "无法获取 workflow steps"
fi

# ── 2. Gateway Health ───────────────────────────────────────────────────────
info "2/5  收集 Gateway 健康状态"
if curl -fsS "$GATEWAY_URL/gateway/health" > "$OUTPUT_DIR/gateway-health.json" 2>/dev/null; then
  ok "gateway-health.json"
else
  warn "无法获取 gateway health，gateway 可能未运行"
fi

# ── 3. Prometheus Metrics ───────────────────────────────────────────────────
info "3/5  收集 Prometheus 指标"

# Agent 核心指标
AGENT_METRICS_QUERY='agent_exec_workflows_total|agent_exec_workflow_duration_seconds|agent_exec_step_duration_seconds|agent_exec_checkpoint_saved_total'
if curl -fsS "$PROMETHEUS_URL/api/v1/query?query=$AGENT_METRICS_QUERY" > "$OUTPUT_DIR/agent-metrics.json" 2>/dev/null; then
  ok "agent-metrics.json"
else
  warn "无法获取 agent metrics，Prometheus 可能未运行"
fi

# Gateway 指标
GATEWAY_METRICS_QUERY='gateway_backend_unhealthy_total|gateway_requests_total|gateway_request_duration_seconds'
if curl -fsS "$PROMETHEUS_URL/api/v1/query?query=$GATEWAY_METRICS_QUERY" > "$OUTPUT_DIR/gateway-metrics.json" 2>/dev/null; then
  ok "gateway-metrics.json"
else
  warn "无法获取 gateway metrics"
fi

# ── 4. Jaeger Trace ─────────────────────────────────────────────────────────
info "4/5  收集 Jaeger trace 摘要"

# 尝试从 workflow-run.json 提取 trace_id
TRACE_ID=""
if [ -f "$OUTPUT_DIR/workflow-run.json" ]; then
  TRACE_ID="$(python3 -c 'import json,sys; data=json.load(open("'"$OUTPUT_DIR/workflow-run.json"'")); print(data.get("trace_id","") or data.get("run",{}).get("trace_id",""))' 2>/dev/null || true)"
fi

if [ -n "$TRACE_ID" ]; then
  if curl -fsS "$JAEGER_URL/api/traces/$TRACE_ID" > "$OUTPUT_DIR/jaeger-trace.json" 2>/dev/null; then
    ok "jaeger-trace.json (trace_id=$TRACE_ID)"
  else
    warn "无法获取 Jaeger trace，可能 trace 尚未导出"
  fi
  echo "$TRACE_ID" > "$OUTPUT_DIR/trace-id.txt"
else
  warn "无法从 workflow 中提取 trace_id，跳过 trace 收集"
fi

# ── 5. AEE Metrics Raw ──────────────────────────────────────────────────────
info "5/5  收集 AEE /metrics 原始输出"
if curl -fsS "$AEE_URL/metrics" > "$OUTPUT_DIR/aee-metrics-raw.txt" 2>/dev/null; then
  ok "aee-metrics-raw.txt"
else
  warn "无法获取 AEE /metrics"
fi

# ── Summary ─────────────────────────────────────────────────────────────────
ok "证据收集完成: $OUTPUT_DIR"
cat <<EOF

收集到的证据文件:
  $(ls -1 "$OUTPUT_DIR" | sed 's/^/  /')

查看方式:
  Workflow:  $AEE_URL/api/v1/workflows/$WORKFLOW_ID
  Trace:     $JAEGER_URL/trace/$TRACE_ID
  Metrics:   $PROMETHEUS_URL
EOF
