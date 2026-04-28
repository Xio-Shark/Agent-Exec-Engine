#!/usr/bin/env bash
# run-regression.sh
# Agent 行为回归集运行脚本
# 用法: ./scripts/run-regression.sh [BASE_URL]

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASE_URL="${1:-http://127.0.0.1:8080}"
FIXTURES_DIR="$ROOT_DIR/tests/regression/fixtures"
OUTPUT_DIR="$ROOT_DIR/evidence/regression"

BLUE='\033[0;34m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; NC='\033[0m'
info()  { echo -e "${BLUE}[INFO]${NC}  $*"; }
ok()    { echo -e "${GREEN}[OK]${NC}    $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}   $*"; }
err()   { echo -e "${RED}[ERR]${NC}    $*" >&2; }

mkdir -p "$OUTPUT_DIR"

info "Agent 行为回归集运行"
info "Base URL: $BASE_URL"
info "Fixtures: $FIXTURES_DIR"
info "Output:   $OUTPUT_DIR"
echo

# 检查服务健康
info "健康检查"
if ! curl -fsS "$BASE_URL/healthz" > /dev/null 2>&1; then
  err "AEE 服务未运行，请先启动: make run"
  exit 1
fi
ok "服务健康"
echo

# 收集结果
declare -a RESULTS=()
TOTAL=0
PASS=0
FAIL=0
SKIP=0

for fixture in "$FIXTURES_DIR"/*.json; do
  name="$(basename "$fixture" .json)"
  TOTAL=$((TOTAL + 1))

  info "[$TOTAL] 运行: $name"

  response_file="$OUTPUT_DIR/${name}-response.json"

  if curl -fsS -X POST "$BASE_URL/api/v1/workflows" \
    -H 'Content-Type: application/json' \
    --data @"$fixture" > "$response_file" 2>/dev/null; then

    workflow_id="$(python3 -c 'import json,sys; print(json.load(open("'"$response_file"'"))["run"]["workflow_id"])' 2>/dev/null || echo "unknown")"
    ok "$name -> workflow_id=$workflow_id"
    RESULTS+=("$name|$workflow_id|PASS|")
    PASS=$((PASS + 1))
  else
    err "$name -> 提交失败"
    RESULTS+=("$name|unknown|FAIL|提交失败")
    FAIL=$((FAIL + 1))
  fi
done

echo
info "回归摘要"
printf '%s\n' "---" > "$OUTPUT_DIR/summary.txt"
{
  printf '回归运行时间: %s\n' "$(date -Iseconds)"
  printf '服务: %s\n' "$BASE_URL"
  printf '---\n'
  printf '%-30s %-36s %-6s %s\n' "Fixture" "WorkflowID" "Status" "Notes"
  printf '%s\n' "---"
  for r in "${RESULTS[@]}"; do
    IFS='|' read -r fixture_id wf_id status note <<< "$r"
    printf '%-30s %-36s %-6s %s\n' "$fixture_id" "$wf_id" "$status" "$note"
  done
  printf '%s\n' "---"
  printf '总计: %d | 通过: %d | 失败: %d\n' "$TOTAL" "$PASS" "$FAIL"
} | tee -a "$OUTPUT_DIR/summary.txt"

if [ "$FAIL" -eq 0 ]; then
  ok "所有回归 fixture 提交成功"
  exit 0
else
  warn "有 $FAIL 个 fixture 提交失败"
  exit 1
fi
