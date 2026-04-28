# Observability Case Study｜LLM 5xx Failover 排障

这份案例不是单纯罗列有哪些 metrics 和 trace，而是回答一个更实际的问题：当 Agent workflow 里的 `llm_call` 遇到下游推理后端 5xx 时，怎么判断问题在 agent 编排层还是在 AI Infra gateway / backend 层，以及系统最终为什么还能成功。

## 场景

- 入口请求：`POST /api/v1/workflows`
- 关键步骤：`plan`（`llm_call`）→ `exec`（`tool_call`）
- 下游路径：`agent-exec-engine/internal/llm/client.go` → AI Infra gateway `/v1/chat/completions`
- 异常：gateway 首选高权重 backend 返回 5xx
- 预期：gateway 自动 failover，workflow 最终仍成功完成

这个案例的证据来自两部分：

1. `agent-exec-engine` 已归档的运行态 evidence，证明 workflow、trace、Prometheus 指标都落地了。
2. `ai-job-orchestrator` 的 gateway failover 测试与文档，证明 5xx 切换、健康探针摘除和恢复行为是可复验的。

## 先看什么信号

| 观测面 | 信号 | 说明 |
|---|---|---|
| workflow API | `run.status=completed` | 说明用户请求最终成功，不是整条链路硬失败 |
| agent trace | `workflow.execute` + `step.execute(plan)` + `step.execute(exec)` | 说明 DAG 调度和 step 执行链路完整 |
| agent metrics | `agent_exec_workflows_total{status="completed"} 1` | 说明 workflow 结果被 Prometheus 正常记录 |
| agent metrics | `agent_exec_step_duration_seconds{step_type="llm_call",status="success"}` | 说明 LLM 步骤最终成功，而不是在 agent 内部失败 |
| gateway log | `[gateway] backend bad returned 500, failover` | 说明 5xx 发生在 infra backend，而不是 agent 本身 |
| gateway health | `healthy_backends: 2 -> 1 -> 2` | 说明故障 backend 被探针摘除后又恢复 |

## 执行流

### 1. 用户入口

`agent-exec-engine/internal/api/handler.go` 的 `CreateWorkflow` 接收 `POST /api/v1/workflows`，然后调用 `WorkflowManager.CreateAndRun()`。

`agent-exec-engine/internal/api/manager.go` 创建 `dag.Scheduler`，异步执行 workflow。

### 2. Agent 侧 trace / metrics

`agent-exec-engine/internal/dag/scheduler_run.go` 在 workflow 开始时创建 `workflow.execute` span，在每个 step 开始时创建 `step.execute` span，并记录：

- workflow 级指标：`agent_exec_workflows_total`、`agent_exec_workflow_duration_seconds`
- step 级指标：`agent_exec_step_duration_seconds`
- checkpoint 指标：`agent_exec_checkpoint_saved_total`

这些指标来自 `agent-exec-engine/internal/observability/metrics.go`，trace span 定义来自 `agent-exec-engine/internal/observability/tracer.go`。

### 3. LLM 请求出站

`agent-exec-engine/internal/llm/client.go` 负责向 OpenAI-compatible gateway 发起 `/chat/completions` 请求。它对可重试错误做最多 3 次尝试：

- 网络错误
- 5xx HTTP 响应

因此，当下游 gateway 或 backend 短暂返回 5xx 时，agent 侧不会立即把 workflow 判死。

### 4. Infra gateway 故障转移

AI Infra 的 `internal/gateway/router.go` 会：

1. 先从健康 backend 中选择候选。
2. 若首个 backend 返回 5xx，则在同一次请求里切到下一个 backend。
3. 背景健康探针 `internal/gateway/health.go` 会把异常 backend 从 healthy 集合中摘除。
4. 当 `/health` 恢复 200 后，再把 backend 加回 healthy 集合。

## 这次案例里能直接对上的证据

### Agent 侧运行态 evidence

- workflow run：`agent-exec-engine/evidence/runtime/workflow-run.json`
  - `run.id = 7c907514-1730-4bb1-bde8-982050800a6a`
  - `status = completed`
  - `plan` 和 `exec` 两个步骤都为 `success`
- trace evidence：`agent-exec-engine/evidence/runtime/jaeger-traces.json`
  - trace id：`b376f00835489947038881031ddb7ec5`
  - span 树：`workflow.execute` → `step.execute(plan)` → `step.execute(exec)`
- metrics evidence：`agent-exec-engine/evidence/runtime/metrics.txt`
  - `agent_exec_workflows_total{status="completed"} 1`
  - `agent_exec_workflow_duration_seconds_count{workflow_name="obs-metrics"} 1`
  - `agent_exec_step_duration_seconds_count{step_type="llm_call",status="success"} 1`
  - `agent_exec_step_duration_seconds_count{step_type="tool_call",status="success"} 1`
  - `agent_exec_checkpoint_saved_total 2`

### Infra 侧 failover evidence

- gateway 文档：`ai-job-orchestrator/docs/gateway-failover-evidence.md`
- gateway 测试：`ai-job-orchestrator/internal/gateway/failover_evidence_test.go`
- 已验证行为：
  - 首个高权重 backend 返回 500 时，同一次请求自动 failover 到下一个 backend
  - `/gateway/health` 的 `healthy_backends` 从 2 降到 1
  - backend `/health` 恢复 200 后，`healthy_backends` 从 1 回到 2

## 复现实验

### A. 验证 agent 侧对 5xx 的容忍

```bash
cd /Users/xioshark/Desktop/career/滕彦翕/项目/agent-exec-engine
go test ./internal/llm -run 'TestClientChat_RetriesServerErrors' -v
```

预期：前两次返回 5xx，第三次成功；测试断言 `attempts = 3`。

### B. 验证 infra gateway 的 failover 与恢复

```bash
cd /Users/xioshark/Desktop/career/滕彦翕/项目/ai-job-orchestrator
go test ./internal/gateway -run 'TestGateway_(Failover|ProbeRemovesAndRecoversBackend)$' -v
```

预期：

- 日志出现 `backend bad returned 500, failover`
- 测试验证 `healthy_backends` 发生 `2 -> 1 -> 2`
- 恢复后高权重 backend 重新接收流量

### C. 验证 agent 运行态信号

```bash
cd /Users/xioshark/Desktop/career/滕彦翕/项目/agent-exec-engine
docker compose -f deployments/docker-compose.yaml up -d
make run

curl -X POST http://localhost:8080/api/v1/workflows \
  -H 'Content-Type: application/json' \
  -d '{"name":"obs-metrics","steps":[{"id":"plan","type":"llm_call"},{"id":"exec","type":"tool_call","depends_on":["plan"]}]}'
```

之后检查：

- `GET /healthz`
- `GET /metrics`
- Jaeger 中的 `workflow.execute` trace
- `evidence/runtime/workflow-run.json`、`metrics.txt`、`jaeger-traces.json`

## 归因过程

1. 如果 workflow 已完成，说明问题不是 DAG 调度器整体挂死。
2. 如果 trace 里同时存在 `workflow.execute` 和 `step.execute(plan)`，说明 agent 执行链已走通。
3. 如果 `llm_call` 的 step metric 是 success，但 gateway 出现 5xx failover 日志，说明异常发生在 infra backend，而不是 agent step executor 本身。
4. 如果 `/gateway/health` 里的 `healthy_backends` 下降，再恢复，说明健康探针已经完成隔离和回收。

结论：这条问题路径的根因是下游推理 backend 临时 5xx，不是 agent-exec-engine 的 DAG、checkpoint 或 step 编排故障。系统之所以还能成功，是因为 agent 侧有有限重试，infra 侧有 gateway failover 和健康探针摘除/恢复。

## 修复 / 缓解动作

- 立即缓解：依赖 gateway 自动 failover，把流量切到健康 backend。
- 自动隔离：健康探针把异常 backend 从 healthy 集合移除。
- 自动恢复：backend `/health` 恢复 200 后重新接流量。
- 如果仍失败：检查是否所有 backend 都不健康；这种情况下 agent 侧的 3 次重试会全部耗尽，`llm_call` 才会真正失败。

## 这份案例的价值

它把两边最容易被分开讲的材料连起来了：

- `agent-exec-engine` 负责证明“workflow 没坏，trace 和 metrics 能告诉你哪一步成功”。
- `ai-job-orchestrator` 负责证明“5xx 的真实根因和自动切换发生在 gateway / backend 层”。

因此，这不是“有埋点”的展示，而是一条可以直接拿来讲排障方法的案例。

## Request-ID / Trace-ID 串联说明

**已实现（A6）**：`RequestIDMiddleware` 将入站 `X-Request-ID` 注入到 context（`llm.RequestIDKey{}`），LLM client、`rag_search` 和 `knowledge_qa` 的出站 HTTP 请求自动携带该 header。

| 链路层 | 可用 ID | 透传状态 | 定位方式 |
|--------|---------|---------|---------|
| Agent workflow | `trace_id` + `run_id` | 内部全链路 | Jaeger span 树 |
| Agent → Gateway | `X-Request-ID` | ✅ 自动透传 | gateway 日志直接按 request_id 检索 |
| Agent → RAG QA | `X-Request-ID` | ✅ 自动透传 | RAG 服务日志按 request_id 检索 |
| Gateway → Backend | `X-Request-ID`（gateway 生成） | N/A | Gateway 自身日志 |

**排障路径**：
1. 从 Jaeger 找到失败的 `step.execute(llm_call)` span，提取 `trace_id` 和 `X-Request-ID`
2. 用 `X-Request-ID` 直接在 Gateway 日志中搜索对应请求
3. 对比 `/gateway/health` 的 `healthy_backends` 变化确认 backend 状态
4. 如涉及 RAG 调用，用同一 `X-Request-ID` 在 RAG 服务日志中检索

---

## 端到端排障案例：Agent Workflow + Gateway Failover + Health 恢复

> 对应 `EXECUTION-LIST.md` 的 R3。

### 场景描述

用户提交一个跨项目 Agent workflow（6 步：ReAct 规划 → RAG 检索 → Gateway 摘要 → 沙箱执行 → 人工确认 → 最终报告），其中第 3 步 `gateway-summary`（`llm_call`）遇到 gateway 首选 backend 5xx，gateway 自动 failover，workflow 最终成功完成。

### 观测信号串联

| 时间线 | Agent 侧信号 | Gateway 侧信号 | 说明 |
|--------|-------------|----------------|------|
| T0 | `POST /api/v1/workflows` → `run_id=run-abc` | — | workflow 创建 |
| T1 | `step.execute(react-plan)` span completed | — | ReAct 规划完成 |
| T2 | `step.execute(retrieve-evidence)` span completed | — | rag_search 完成 |
| T3 | `step.execute(gateway-summary)` span started | — | LLM call 开始 |
| T4 | — | `[gateway] backend bad returned 500, failover` | gateway 检测到 5xx |
| T5 | — | `healthy_backends: 2 → 1` | 探针摘除故障 backend |
| T6 | — | failover 成功，返回 200 | 切到健康 backend |
| T7 | `step.execute(gateway-summary)` span completed | — | LLM call 成功 |
| T8 | `step.execute(sandbox-artifact)` span completed | — | 沙箱执行完成 |
| T9 | `workflow.execute` span completed | — | workflow 完成 |

### 排障路径（3 分钟定位法）

**Step 1：确认 workflow 最终成功**

```bash
curl http://localhost:8080/api/v1/workflows/{workflow_id}
# → run.status=completed
```

**Step 2：定位失败发生在哪一层**

```bash
# Jaeger 查询 run_id=run-abc
# → step.execute(gateway-summary) span 有 retry 事件
# → 但最终 status=success
```

**Step 3：查看 gateway 是否发生了 failover**

```bash
# Gateway 日志搜索
grep "failover" /var/log/ai-infra-gateway.log | tail -5
# → [gateway] backend bad returned 500, failover

# 检查健康状态变化
curl http://localhost:8081/gateway/health
# → healthy_backends 从 2 恢复到 2（说明已自动恢复）
```

**Step 4：确认 X-Request-ID 串联**

```bash
# 从 Agent 日志获取 request_id
grep "request_id" /var/log/agent-exec-engine.log | grep "gateway-summary"
# → request_id=req-xyz

# 用同一 request_id 在 gateway 日志检索
grep "req-xyz" /var/log/ai-infra-gateway.log
# → 找到对应的 5xx + failover 事件
```

### 结论

| 层 | 状态 | 证据 |
|----|------|------|
| Agent 编排层 | ✅ workflow 最终完成 | `run.status=completed` |
| LLM 调用层 | ✅ 重试 + failover 成功 | Jaeger span: retry event → success |
| Gateway 层 | ✅ 自动 failover | 日志: `backend bad returned 500, failover` |
| Backend 健康 | ✅ 自动恢复 | `/gateway/health`: 2→1→2 |
| 可观测性 | ✅ X-Request-ID 全链路 | Agent + Gateway + RAG 日志可按同一 ID 检索 |
