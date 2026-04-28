# Cross-Project Contracts

这份文档对应 `EXECUTION-LIST.md` 的 P3，用最小但可执行的契约矩阵把 `Agent -> Infra / RAG / Eval` 的边界写清楚，避免叙事停留在“HTTP 调一下另一个服务”。

## 契约矩阵

| 路径 | 状态 | 调用边界 | 传输 | 关键目标 |
| --- | --- | --- | --- | --- |
| `workflow resume / cancel` | 当前已实现 | 外部调用方 -> agent-exec-engine API | HTTP JSON | 控制工作流暂停恢复与取消 |
| `llm_call` | 当前已实现 | agent-exec-engine -> AI Infra gateway | OpenAI-compatible HTTP | 通过 gateway 承接推理请求与故障切换 |
| `GPU reserve / release` | 当前已实现 | agent-exec-engine -> AI Infra scheduler | HTTP JSON | 为 LLM step 申请 / 释放 GPU 预约 |
| `rag_search` | 当前已实现 | agent-exec-engine -> embedding API + Qdrant | HTTP JSON | 检索知识证据，服务于 tool_call / react |
| `eval regression run` | 当前已实现 | 调用方 / 后续 Agent 流程 -> RAG eval API | HTTP JSON | 运行回归评测并生成报告 |
| `knowledge_qa / audit trail` | 下一批升级 | agent-exec-engine -> RAG QA API | HTTP JSON | 产出 `audit_id` 并回看问答审计细节 |

## 当前约束

- `agent-exec-engine` 的 API 入口会生成或回显 `X-Request-ID`，`rag` 的 HTTP 层也会回显同名 header。
- 当前 `llm_call`、scheduler client 和 `rag_search` 的出站请求还没有统一自动透传 `X-Request-ID`；跨服务排障主要依赖 trace、run id 和下游服务自己的日志字段。
- 只有纯读取路径适合无害重试；`resume`、`reserve GPU`、`eval run` 都会产生新状态，不应视作幂等接口。

## 1. Workflow Resume / Cancel

| 维度 | Resume | Cancel |
| --- | --- | --- |
| Endpoint | `POST /api/v1/workflows/:id/resume` | `DELETE /api/v1/workflows/:id` |
| 请求字段 | path `id` + body `step_id` / `input` | path `id` |
| 成功响应 | `200`，消息为 `workflow resumed` | `200`，消息为 `workflow cancelled` |
| 错误语义 | 缺失 workflow 返回 `404`；非法状态按统一错误包返回 | 缺失 workflow 返回 `404`；无效状态按统一错误包返回 |
| 超时 / 取消 | 恢复后重新进入 `running` 生命周期 | 调用 `Scheduler.Cancel()`，运行中步骤标记为 `cancelled` |
| 幂等性 | 非幂等；同一暂停点恢复后再次提交应视为错误 | best-effort；重复取消不保证成功 |
| 观测 | 入站会生成 / 回显 `X-Request-ID`，日志记录 `request_id` | 同左 |
| 证据 | `docs/api.md`、`internal/api/middleware.go`、`internal/api/manager.go` | `docs/api.md`、`internal/dag/scheduler_run.go` |

## 2. `llm_call` -> AI Infra Gateway

| 维度 | 契约 |
| --- | --- |
| Endpoint | `POST {llm.base_url}/chat/completions`，默认由 `infra.gateway_url + /v1` 派生 |
| 请求字段 | `model`、`messages`、可选 `tools`；当 registry 存在时带工具定义 |
| 成功响应 | OpenAI-compatible `choices[0]`；`finish_reason=tool_calls` 时进入工具回合 |
| 超时 / 取消 | 使用 `http.Client.Timeout`；对可重试错误最多 3 次；上游 `context` 取消会中断请求 |
| 幂等性 | 非幂等；出现重试时依赖 workflow checkpoint，而不是要求下游去重 |
| 错误语义 | `5xx` 视为可重试；`4xx` 或解码失败直接向上返回错误 |
| 观测 | 当前未自动透传 `X-Request-ID`；跨服务主要依赖 trace 与 gateway 自身健康日志 |
| 证据 | `internal/llm/client.go`、`docs/observability-case-study.md`、`ai-job-orchestrator/docs/gateway-failover-evidence.md` |

## 3. GPU Reserve / Release

| 维度 | Reserve | Release |
| --- | --- | --- |
| Endpoint | `POST /jobs` 后接 `POST /jobs/{job_id}/schedule` | `POST /jobs/{job_id}/cancel` |
| 请求字段 | `name`、`job_type=inference`、`executor=shell`、`command=[true]`、`metadata.task_id`、`resource_spec.gpu`、`resource_spec.gpu_memory` | 只需要本地保存的 `job_id` |
| 成功语义 | reservation job 创建并进入调度 | reservation job 被取消，显式释放 GPU |
| 超时 / 取消 | 沿用调用方 `context` 与 scheduler client timeout | 在 `LLMStepExecutor.Execute` 的 `defer` 中触发，无论 step 成功还是失败都尝试释放 |
| 幂等性 | 非幂等；重复 reserve 会创建新 job | 非幂等；本地无 reservation 时直接报错 |
| 资源释放 | 由 `releaseGPU()` 显式兜底，不依赖下游隐式回收 | 当前真实释放语义是 AI Infra 的 `cancel` |
| 观测 | 当前未自动透传 `X-Request-ID`；定位主要依赖 `task_id` / `job_id` |
| 证据 | `internal/infra/scheduler_client.go`、`internal/llm/executor.go`、`internal/infra/scheduler_client_test.go` |

## 4. `rag_search` -> Embedding API + Qdrant

| 维度 | 契约 |
| --- | --- |
| Tool name | `rag_search` |
| 输入字段 | `query` 必填；`collection` 默认 `default`；`top_k` 默认 5，最大 50 |
| 成功响应 | 多行文本，每行包含 `score`、`text`、`source` |
| 超时 / 取消 | embedding 请求 10s；Qdrant 检索 10s；均沿用上游 `context` |
| 幂等性 | 只读检索，可安全重试 |
| 错误语义 | 空 query 直接报错；Qdrant / embedding 非 2xx 返回显式错误 |
| 降级语义 | 未配置 `QDRANT_URL` 时返回显式 stub 文本，不是静默成功 |
| 观测 | 当前没有 `audit_id`；也未自动透传 `X-Request-ID` |
| 证据 | `internal/mcp/tools/rag_search.go`、`internal/mcp/tools/rag_search_test.go`、`docs/runtime-evidence.md` |

> 这条路径是当前的检索契约，不等于 RAG QA API。它适合给 Agent 提供证据块，不适合直接承担问答审计。

## 5. Eval Regression Run -> RAG Eval API

| 维度 | 契约 |
| --- | --- |
| Endpoint | `POST /v1/evals/run` |
| 请求字段 | `dataset_name`、`snapshot_name` |
| 成功响应 | `eval_run_id`、`summary`、`report_json_path`、`report_markdown_path`、`bad_cases` |
| 超时 / 取消 | 当前是同步 HTTP 执行；调用方需要自己设置请求超时 |
| 幂等性 | 非幂等；每次调用都会创建新的 eval run |
| 错误语义 | dataset / snapshot 不存在返回 `404`；feature flag 关闭时由依赖层拒绝 |
| 观测 | `rag` 会回显 `X-Request-ID`，并记录 `eval.run.completed` completion log |
| 证据 | `rag/app/api/routes/evals.py`、`rag/app/core/observability.py`、`rag/README.md` |

> 如果后续把 `agent-eval/shared/tasks.json` 升级为主线回归输入，建议把它作为评测数据集来源，而不是再造一套新的 HTTP 协议。

## 6. 下一批升级：`knowledge_qa` / Audit Trail

| 维度 | 目标契约 |
| --- | --- |
| Endpoint | `POST /v1/qa/ask` + `GET /v1/qa/runs/{audit_id}` |
| 请求字段 | `query`、可选 `top_k` |
| 成功响应 | `answer`、`citations`、`confidence`、`refusal_reason`、`audit_id` |
| 价值 | 让 P2 的 cross-project demo 真正产出 `audit_id`，把 Agent 检索从证据块级别升级到问答审计级别 |
| 当前缺口 | `agent-exec-engine` 现在只有 `rag_search` tool，还没有对 `qa/ask` 的 HTTP adapter |
| 证据来源 | `rag/app/api/routes/qa.py`、`rag/app/services/qa.py`、`rag/README.md` |

## 落地建议

1. 继续保留当前 `rag_search` 契约，用于 ReAct / tool_call 的轻量证据检索。
2. 下一批新增 `knowledge_qa` 风格 adapter，把 `audit_id` 引入 cross-project demo。
3. 在 `llm_call`、scheduler client、RAG HTTP adapter 三条出站链路上统一补 `X-Request-ID` 透传，减少跨服务排障成本。