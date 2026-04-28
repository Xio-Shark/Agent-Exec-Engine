#!/usr/bin/env bash
# demo.sh — agent-exec-engine 端到端演示
# 前置：启动服务 (go run ./cmd/server 或 docker compose up)
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
BLUE='\033[0;34m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'

info()  { echo -e "${BLUE}[INFO]${NC}  $*"; }
ok()    { echo -e "${GREEN}[OK]${NC}    $*"; }
warn()  { echo -e "${YELLOW}[WAIT]${NC}  $*"; }

# ── 1. 健康检查 ──────────────────────────────────────────
info "1/6  健康检查"
curl -sf "$BASE_URL/healthz" | python3 -m json.tool
echo

# ── 2. 查看已注册工具 ────────────────────────────────────
info "2/6  列出已注册 MCP 工具"
curl -sf "$BASE_URL/api/v1/tools" | python3 -m json.tool
echo

# ── 3. 提交条件分支 DAG ──────────────────────────────────
info "3/6  提交条件分支工作流 (conditional-dag.json)"
WORKFLOW_RESP=$(curl -sf -X POST "$BASE_URL/api/v1/workflows" \
  -H 'Content-Type: application/json' \
  -d @"$(dirname "$0")/../examples/conditional-dag.json")
echo "$WORKFLOW_RESP" | python3 -m json.tool

RUN_ID=$(echo "$WORKFLOW_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['run']['id'])")
WF_ID=$(echo "$WORKFLOW_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['run']['workflow_id'])")
ok "工作流已创建  workflow=$WF_ID  run=$RUN_ID"
echo

# ── 4. 轮询状态（等待 Human-in-the-loop 暂停） ─────────
info "4/6  等待执行到 human-review 步骤 (paused)..."
for i in $(seq 1 30); do
  STATUS=$(curl -sf "$BASE_URL/api/v1/workflows/$WF_ID" \
    | python3 -c "import sys,json; print(json.load(sys.stdin)['run']['status'])" 2>/dev/null || echo "pending")
  if [ "$STATUS" = "paused" ]; then
    warn "工作流已暂停 — 等待人工审批"
    curl -sf "$BASE_URL/api/v1/workflows/$WF_ID/steps" | python3 -m json.tool
    break
  elif [ "$STATUS" = "completed" ] || [ "$STATUS" = "failed" ]; then
    ok "工作流已结束: $STATUS"
    break
  fi
  sleep 1
done
echo

# ── 5. 模拟人工审批（Resume） ────────────────────────────
if [ "$STATUS" = "paused" ]; then
  info "5/6  发送 Resume 指令 (human-review → approved)"
  curl -sf -X POST "$BASE_URL/api/v1/workflows/$WF_ID/resume" \
    -H 'Content-Type: application/json' \
    -d '{"step_id": "human-review", "input": {"approved": true}}' \
    | python3 -m json.tool
  echo

  # 等待完成
  info "      等待后续步骤执行..."
  for i in $(seq 1 30); do
    STATUS=$(curl -sf "$BASE_URL/api/v1/workflows/$WF_ID" \
      | python3 -c "import sys,json; print(json.load(sys.stdin)['run']['status'])" 2>/dev/null || echo "running")
    if [ "$STATUS" = "completed" ] || [ "$STATUS" = "failed" ]; then
      break
    fi
    sleep 1
  done
else
  info "5/6  跳过 (工作流未暂停)"
fi
echo

# ── 6. 查看最终结果 ──────────────────────────────────────
info "6/6  最终工作流状态"
curl -sf "$BASE_URL/api/v1/workflows/$WF_ID" | python3 -m json.tool
echo

info "      各步骤详情"
curl -sf "$BASE_URL/api/v1/workflows/$WF_ID/steps" | python3 -m json.tool
echo

if [ "$STATUS" = "completed" ]; then
  ok "✅ Demo 完成 — 工作流成功执行全部步骤"
else
  warn "⚠️ Demo 结束 — 最终状态: $STATUS"
fi
