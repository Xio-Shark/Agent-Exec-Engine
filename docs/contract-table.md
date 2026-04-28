# Cross-Project Contract Table

> 聚焦表：把 `agent-exec-engine` 与外部服务（AI Infra / RAG）之间的**核心调用路径**写成可验证的契约。
>
> 这不是接口文档全集，而是面试官或 reviewer 能在 3 分钟内看完并定位到代码的"最小可验证契约"。

---

## 契约总览

| # | 路径 | 调用方 → 接收方 | 传输 | 状态 | 关键目标 |
|---|------|----------------|------|------|---------|
| 1 | `llm_call` | AEE → AI Infra Gateway | HTTP JSON (OpenAI-compatible) | ✅ 已实现 | 推理请求 + 故障切换 |
| 2 | `tool_call` | AEE MCP → Sandbox / RAG / 外部 | JSON-RPC 2.0 / HTTP / Docker | ✅ 已实现 | 工具执行 + 安全隔离 |
| 3 | `workflow cancel / resume` | Client → AEE API | HTTP REST | ✅ 已实现 | 长任务控制面 |
| 4 | `gateway 5xx failover` | AI Infra Gateway → Backend | HTTP (内部) | ✅ 已实现 | 推理后端故障自动转移 |
| 5 | `RAG eval run` | Eval → RAG API | HTTP JSON | ✅ 已实现 | 回归评测 + bad case 沉淀 |

---

## 1. `llm_call` → AI Infra Gateway

| 维度 | 契约 |
|------|------|
| **Endpoint** | `POST {llm.base_url}/chat/completions`，默认由 `AI_INFRA_GATEWAY_URL + /v1` 派生 |
| **请求字段** | `model`、`messages`、可选 `tools`（当 registry 非空时带工具定义） |
| **成功响应** | OpenAI-compatible `choices[0]`；`finish_reason=tool_calls` 时进入工具回合 |
| **超时 / 取消** | `http.Client.Timeout`；可重试错误最多 3 次；上游 `context` 取消会中断请求 |
| **幂等性** | ❌ 非幂等；出现重试时依赖 workflow checkpoint 恢复，而非下游去重 |
| **错误语义** | `5xx` → 可重试；`4xx` / 解码失败 → 直接向上返回错误 |
| **降级语义** | 无静默降级；3 次重试耗尽后 `llm_call` 步骤失败，workflow 进入 `failed` 状态 |
| **观测** | 当前**未自动透传** `X-Request-ID`；跨服务排障依赖 trace_id + gateway 自身健康日志 |
| **证据** | `internal/llm/client.go`、[`observability-case-study.md`](observability-case-study.md)、`ai-job-orchestrator/docs/gateway-failover-evidence.md` |

**关键 tradeoff**：为什么不由 AEE 自己做 gateway failover？
- AEE 只做有限重试（最多 3 次），因为重试策略应与故障切换解耦
- Gateway 层有健康探针和 backend 池状态，比 AEE 更适合做实时 failover
- 证据：`ai-job-orchestrator/internal/gateway/router.go` 的 `tryNextBackend` 逻辑

---

## 2. `tool_call` → MCP Registry → 执行器

| 维度 | 契约 |
|------|------|
| **入口** | `registry.Call(ctx, ToolCall)` |
| **前置校验** | `ValidateInput(schema, input)` → schema 不匹配直接拒绝 |
| **限流** | `rateLimiter.Allow()` → 令牌桶拒绝时返回 `429 Too Many Requests` |
| **执行分发** | `code_exec` → `sandbox.Pool.Execute()`；`rag_search` → HTTP；`web_search` → Tavily API / stub；`file_reader` → `os.ReadFile`（路径校验后） |
| **超时 / 取消** | Sandbox 有独立 `timeout` + `hardkill`；HTTP 工具有 `http.Client.Timeout`；均沿用上游 `context` |
| **幂等性** | `rag_search` / `file_reader` ✅ 只读可重试；`code_exec` / `web_search` ❌ 非幂等 |
| **错误语义** | 校验失败 → `400`；限流 → `429`；沙箱 OOM/超时 → `500` + 明确错误类型；工具未注册 → `404` |
| **降级语义** | `QDRANT_URL` 未配置时 `rag_search` 返回显式 stub 文本；`web_search` 未配置 API key 时返回 stub |
| **观测** | 每个 tool call 产生 `step.execute(tool_call)` span 和 `agent_exec_step_duration_seconds{step_type="tool_call"}` metric |
| **证据** | `internal/mcp/registry.go`、`internal/mcp/validator.go`、`internal/mcp/ratelimit.go`、`internal/sandbox/executor.go` |

**关键 tradeoff**：为什么用 JSON-RPC 2.0 而不是 gRPC？
- MCP 协议本身定义 JSON-RPC 2.0；gRPC 需要额外 proto 定义和代码生成
- 当前只需要薄协议面（registry + validator + guardrail），不需要完整 SDK
- 证据：`docs/mcp-design-decision.md`

---

## 3. `workflow cancel / resume`

| 维度 | Cancel | Resume |
|------|--------|--------|
| **Endpoint** | `DELETE /api/v1/workflows/:id` | `POST /api/v1/workflows/:id/resume` |
| **请求字段** | path `id` | path `id` + body `step_id` / `input` |
| **成功响应** | `200` + `workflow cancelled` | `200` + `workflow resumed` |
| **状态转换** | `running` → `cancelled`（运行中步骤标记为 `cancelled`） | `paused` → `running` |
| **超时 / 取消** | 调用 `Scheduler.Cancel()`，传播到 errgroup | 恢复后重新进入 `runLoop` 生命周期 |
| **幂等性** | ❌ best-effort；重复取消不保证成功 | ❌ 非幂等；同一暂停点恢复后再次提交应视为错误 |
| **错误语义** | 缺失 workflow → `404`；无效状态 → 统一错误包 | 缺失 workflow → `404`；非法状态 → 统一错误包 |
| **观测** | 入站生成/回显 `X-Request-ID`，日志记录 `request_id` | 同左 |
| **证据** | `internal/api/manager.go`、`internal/dag/scheduler_run.go` | `internal/api/manager.go`、`internal/dag/checkpoint.go` |

**关键 tradeoff**：为什么 resume 不是幂等的？
- resume 会改变 workflow 状态并触发后续步骤执行，重复 resume 可能导致重复副作用
- 幂等应由调用方通过 `workflow_id` + 状态检查来保证，而不是让 resume 接口承担去重

---

## 4. `gateway 5xx failover`（AI Infra 内部）

| 维度 | 契约 |
|------|------|
| **触发条件** | Backend 返回 `5xx` 或健康探针标记为 unhealthy |
| **切换行为** | 同一次请求内自动切到下一个 healthy backend；`internal/gateway/router.go` 的 `tryNextBackend` |
| **健康探针** | 背景 goroutine 轮询 `/health`；失败 backend 从 healthy 集合摘除；恢复后重新加入 |
| **状态暴露** | `GET /gateway/health` 返回 `healthy_backends` 数量变化（如 `2 → 1 → 2`） |
| **超时 / 取消** | 单个 backend 请求有超时；整体请求沿用调用方 `context` |
| **幂等性** | N/A（这是网关内部行为，不是对外接口） |
| **错误语义** | 所有 backend 都不健康时返回 `503`；单个 backend 5xx 对调用方不可见（已 failover） |
| **降级语义** | 无静默降级；5xx 被显式记录并触发切换 |
| **观测** | Gateway 日志出现 `backend bad returned 500, failover`；metrics 记录 `gateway_backend_unhealthy_total` |
| **证据** | `ai-job-orchestrator/internal/gateway/router.go`、`ai-job-orchestrator/internal/gateway/health.go`、`ai-job-orchestrator/docs/gateway-failover-evidence.md` |

**关键 tradeoff**：为什么探针摘除和 failover 不在 AEE 层做？
- AEE 是 Agent 编排层，不应感知 backend 池的拓扑变化
- Gateway 有 backend 健康状态的实时视图，比 AEE 更适合做毫秒级切换
- 分离后 AEE 只需要"重试"，Gateway 只需要"切换"，职责清晰

---

## 5. `RAG eval run`

| 维度 | 契约 |
|------|------|
| **Endpoint** | `POST /v1/evals/run` |
| **请求字段** | `dataset_name`、`snapshot_name` |
| **成功响应** | `eval_run_id`、`summary`、`report_json_path`、`report_markdown_path`、`bad_cases` |
| **超时 / 取消** | 当前是同步 HTTP 执行；调用方需自行设置请求超时 |
| **幂等性** | ❌ 非幂等；每次调用创建新的 eval run |
| **错误语义** | dataset / snapshot 不存在 → `404`；feature flag 关闭 → 依赖层拒绝 |
| **降级语义** | 无静默降级；评测失败返回明确错误和空结果 |
| **观测** | `rag` 回显 `X-Request-ID`，记录 `eval.run.completed` completion log |
| **证据** | `rag/app/api/routes/evals.py`、`rag/app/core/observability.py`、`rag/docs/benchmark-report.md` |

**关键 tradeoff**：为什么 eval 是同步 HTTP 而不是异步 callback？
- RAG 评测数据集通常不大（百级到千级），同步执行在本地环境可控
- 异步会增加状态管理和回调复杂度，当前阶段收益不足
- 未来如需大规模回归，可升级为异步 + webhook

---

## 6. Resolved Gaps

| # | Path | Previous Status | Resolution |
|---|------|----------------|------------|
| 6 | `knowledge_qa` → RAG | 🔶 部分就绪 | ✅ **已实现**：新增 `internal/mcp/tools/knowledge_qa.go`，调用 RAG `/v1/qa/ask` 端点，返回 `audit_id` + `sources` + `confidence`；配置 `tools.knowledge_qa.rag_service_url` 或 `AGENT_EXEC_TOOLS_KNOWLEDGE_QA_RAG_SERVICE_URL` |
| 7 | `X-Request-ID` 透传 | 🔶 部分就绪 | ✅ **已实现**：`RequestIDMiddleware` 将 `X-Request-ID` 注入 context（`llm.RequestIDKey{}`），LLM client + rag_search + knowledge_qa 出站 HTTP 自动携带该 header |
| 8 | `workflow retry` 耗尽策略 | 🔶 部分就绪 | ✅ **已有测试**：`TestScheduler_RetryExhaustedMarksFailed` 验证重试耗尽后 step 状态为 `failed`、workflow 状态为 `failed`、checkpoint 持久化 `StepFailed`；`failWorkflow` 中 `_ = s.saveCheckpoint()` 保证失败态落盘 |

---

## 7. 验证命令速查

```bash
# 1. 验证 llm_call 重试
cd agent-exec-engine && go test ./internal/llm -run 'TestClientChat_RetriesServerErrors' -v

# 2. 验证 tool_call 限流 + 校验
cd agent-exec-engine && go test ./internal/mcp -run '(Validator|RateLimit)' -count=1 -v

# 3. 验证 cancel / resume
cd agent-exec-engine && go test ./internal/dag ./internal/api -run '(Cancel|Resume)' -count=1 -v

# 4. 验证 gateway failover
cd ai-job-orchestrator && go test ./internal/gateway -run 'TestGateway_(Failover|ProbeRemovesAndRecoversBackend)$' -v

# 5. 验证 RAG eval
cd rag && python -m pytest tests/ -k eval -v
```

---

## 8. 关联文档

| 文档 | 内容 |
|------|------|
| [`system-map.md`](system-map.md) | 系统边界总览 |
| [`cross-project-contracts.md`](cross-project-contracts.md) | 更详细的契约讨论（含下一批升级计划） |
| [`cross-project-demo.md`](cross-project-demo.md) | 黄金路径 demo |
| [`observability-case-study.md`](observability-case-study.md) | trace 串联案例 |
| [`backend-resume-evidence.md`](backend-resume-evidence.md) | 简历 claim 映射 |
