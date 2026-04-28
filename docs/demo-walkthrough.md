# Demo Walkthrough

5 分钟快速体验 Agent Workflow Engine 的核心能力。

## 前置条件

```bash
# 1. 启动 Redis（checkpoint 持久化）
docker run -d --name redis -p 6379:6379 redis:7-alpine

# 2. 启动引擎（默认 :8080）
go run ./cmd/server
```

## 一键运行

```bash
bash scripts/demo.sh
```

## Demo 做了什么

`scripts/demo.sh` 提交一个 5 步条件分支 DAG（`examples/conditional-dag.json`），演示：

```
analyze (tool_call: 代码静态分析)
    │
    ▼
branch-severity (CEL 条件分支)
    ├── severity=high → human-review (暂停等待人工审批)
    └── severity=low  → auto-fix (自动修复)
    │
    ▼
generate-report (LLM 总结)
```

### 关键步骤

| 步骤 | 你会看到 | 演示的能力 |
|------|---------|-----------|
| `3/6 提交工作流` | 返回 workflow_id + run_id | DAG 解析 & 拓扑排序 |
| `4/6 等待暂停` | status → "paused" | Human-in-the-loop 暂停 |
| `5/6 Resume` | 继续执行后续步骤 | 断点恢复 |
| `6/6 最终状态` | 各步骤 output + tokens_used | 可观测性 |

## 手动逐步执行

```bash
# 健康检查
curl http://localhost:8080/healthz

# 提交工作流
curl -X POST http://localhost:8080/api/v1/workflows \
  -H 'Content-Type: application/json' \
  -d @examples/conditional-dag.json

# 查询状态
curl http://localhost:8080/api/v1/workflows/{id}

# 查看步骤详情
curl http://localhost:8080/api/v1/workflows/{id}/steps

# 人工审批（Resume）
curl -X POST http://localhost:8080/api/v1/workflows/{id}/resume \
  -H 'Content-Type: application/json' \
  -d '{"step_id": "human-review", "input": {"approved": true}}'
```

## Prometheus 指标

启动后访问 `http://localhost:8080/metrics`，关键指标：

- `workflow_step_duration_seconds` — 各步骤执行耗时
- `workflow_step_total` — 步骤执行计数（按 status 分组）
- `mcp_tool_call_total` — 工具调用次数
- `llm_token_usage_total` — LLM token 消耗
