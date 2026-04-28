# Agent Backend System Map

> 这份文档回答三个问题：**主服务是谁**、**外部依赖是谁**、**接口在哪里**。
>
> 它不与 `architecture.md` 重复；`architecture.md` 讲内部模块，`system-map.md` 讲系统边界。

---

## 1. 系统边界总览

```text
┌─────────────────────────────────────────────────────────────────────────────┐
│                          Agent Backend 主服务                                │
│                     agent-exec-engine (Go 1.25)                              │
│                                                                             │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐        │
│  │ API Server  │  │DAG Scheduler│  │ MCP Registry│  │Observability│        │
│  │  (gin)      │  │ (Kahn+CEL)  │  │(JSON-RPC)   │  │(OTLP/Prom)  │        │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘        │
│         │                │                │                │               │
│  ┌──────┴──────┐  ┌──────┴──────┐  ┌──────┴──────┐  ┌──────┴──────┐        │
│  │ LLM Client  │  │  Sandbox    │  │   Store     │  │   Config    │        │
│  │ (OpenAI-compat)│ │(Docker+cgroup)│ │(Redis/PG)  │  │  (env/Viper)│        │
│  └─────────────┘  └─────────────┘  └─────────────┘  └─────────────┘        │
└─────────────────────────────────────────────────────────────────────────────┘
         │                │                │
         │   ┌────────────┘                │
         │   │                             │
         ▼   ▼                             ▼
┌─────────────────────┐          ┌─────────────────────┐
│  AI Infra Platform  │          │   RAG Knowledge     │
│ ai-job-orchestrator │          │       Base          │
│  (推理网关 + GPU调度)  │          │  (检索 + 问答 + 评测) │
└─────────────────────┘          └─────────────────────┘
         │                                  │
         │                                  │
         ▼                                  ▼
┌─────────────────────┐          ┌─────────────────────┐
│  Agent Eval Corpus  │          │   Vector Store      │
│   (回归样本来源)      │          │  (Qdrant / Milvus)  │
└─────────────────────┘          └─────────────────────┘
```

---

## 2. 主服务边界（Core Boundary）

`agent-exec-engine` 是唯一主服务。以下模块属于主服务内部，**不对外暴露直接接口**：

| 模块 | 职责 | 代码路径 |
|------|------|----------|
| DAG Scheduler | 拓扑排序、条件分支、并行执行、checkpoint | `internal/dag/` |
| MCP Registry | 工具注册/发现、输入校验、限流 | `internal/mcp/` |
| Sandbox | Docker 短容器、资源隔离、产物收集 | `internal/sandbox/` |
| LLM Client | OpenAI-compatible HTTP 客户端、重试、流式解析 | `internal/llm/` |
| Observability | OTLP trace、Prometheus metrics、zap 日志 | `internal/observability/` |
| Store | Redis/PG 状态持久化抽象 | `internal/store/` |

**对外暴露的接口**只有 HTTP REST API（`internal/api/`）和 Prometheus `/metrics` 端点。

---

## 3. 外部依赖边界（Dependency Boundary）

### 3.1 AI Infra Platform (`ai-job-orchestrator`)

| 能力 | 接口 | 方向 | 说明 |
|------|------|------|------|
| LLM 推理 | `POST /v1/chat/completions` | AEE → Gateway | `llm.base_url` 默认派生自 `AI_INFRA_GATEWAY_URL` |
| GPU 预约 | `POST /jobs` + `POST /jobs/{id}/schedule` | AEE → Scheduler | LLM step 前预约，step 后释放 |
| 健康状态 | `GET /gateway/health` | AEE → Gateway | 排障时查询 healthy_backends 数量 |

**关键约定**：
- Gateway failover 由 `ai-job-orchestrator` 负责，AEE 只负责有限重试（最多 3 次）
- GPU 预约在 `LLMStepExecutor.Execute` 的 `defer` 中释放，无论成功失败
- `X-Request-ID` 当前**未自动透传**，跨服务排障依赖 trace_id + run_id

**证据**：`internal/llm/client.go`、`internal/infra/scheduler_client.go`、`ai-job-orchestrator/docs/gateway-failover-evidence.md`

### 3.2 RAG Knowledge Base (`rag`)

| 能力 | 接口 | 方向 | 说明 |
|------|------|------|------|
| 轻量检索 | `rag_search` tool（内置） | AEE MCP → Embedding + Qdrant | 返回证据块列表，供 ReAct 使用 |
| 问答审计 | `POST /v1/qa/ask` + `GET /v1/qa/runs/{audit_id}` | AEE → RAG QA API | 产出 `audit_id`，支持审计回溯 |
| 评测回归 | `POST /v1/evals/run` | Eval → RAG | 运行回归评测并生成报告 |

**关键约定**：
- `rag_search` 是只读路径，可安全重试
- `knowledge_qa` 是非幂等路径，每次调用产生新 `audit_id`
- 未配置 `QDRANT_URL` 时，`rag_search` 返回显式 stub 文本（非静默成功）

**证据**：`internal/mcp/tools/rag_search.go`、`rag/app/api/routes/qa.py`、`rag/docs/benchmark-report.md`

### 3.3 Agent Eval Corpus (`agent-eval`)

| 能力 | 接口 | 方向 | 说明 |
|------|------|------|------|
| 行为样本 | `shared/tasks.json` | Eval → AEE | 回归 fixtures 的数据来源 |
| 评测执行 | 本地脚本 / CI | Eval 内部 | 不直接调用 AEE HTTP API |

**关键约定**：
- `agent-eval` 不暴露 HTTP 服务，它产出**结构化样本文件**
- AEE 的 regression fixtures 从 `agent-eval/shared/tasks.json` 抽取
- 回归集是「证据」而非「运行时依赖」

**证据**：`agent-eval/shared/tasks.json`、`agent-eval/docs/evidence-index.md`

---

## 4. 数据流边界（Data Flow Boundary）

### 4.1 正常执行流

```text
Client → AEE API → DAG Scheduler → Step Executor
                                      │
                    ┌─────────────────┼─────────────────┐
                    ▼                 ▼                 ▼
              LLM Step          Tool Step          Human Step
                    │                 │
                    ▼                 ▼
            AI Infra Gateway    MCP Registry
                    │                 │
                    ▼                 ▼
              LLM Backend      Docker Sandbox / RAG
```

### 4.2 可观测性流

```text
AEE internal → OTLP Exporter → Jaeger (trace)
                    │
                    ├──→ Prometheus (metrics)
                    │
                    └──→ zap (structured logs)
```

### 4.3 状态持久化流

```text
Scheduler checkpoint → Store interface → Redis (prod) / memory (test)
```

---

## 5. 边界上的未覆盖能力（Explicitly Out of Scope）

| 能力 | 为什么不在主服务 | 当前替代方案 |
|------|----------------|-------------|
| 模型训练 / 微调 | 属于 AI Infra，不是 Agent 编排 | `ai-job-orchestrator` 的 scheduler |
| 向量数据库管理 | 属于 RAG 服务 | `rag` 内部管理 Qdrant/Milvus |
| K8s GPU 调度器 | 属于云原生基础设施 | `ai-job-orchestrator` 的 scheduler-sim |
| 多机分布式推理 | 属于推理框架层 | vLLM / SGLang 在 gateway backend 上运行 |
| 完整 MCP SDK | 本项目只需要薄协议面 | 自研 `internal/mcp/`，覆盖 JSON-RPC + registry |

---

## 6. Reviewer 快速入口

想验证这份边界图是否真实：

1. **看接口存在性**：`internal/api/handler.go` 的 9 个 REST handler
2. **看外部调用代码**：`internal/llm/client.go`（gateway）、`internal/infra/scheduler_client.go`（scheduler）、`internal/mcp/tools/rag_search.go`（RAG）
3. **看可观测性串联**：`internal/observability/tracer.go` + `docs/observability-case-study.md`
4. **看跨项目 demo**：`docs/cross-project-demo.md` + `scripts/demo-cross-project.sh`

---

## 7. 关联文档

| 文档 | 内容 |
|------|------|
| [`architecture.md`](architecture.md) | 内部模块架构与数据流 |
| [`contract-table.md`](contract-table.md) | 跨项目接口契约聚焦表 |
| [`cross-project-demo.md`](cross-project-demo.md) | 黄金路径 demo |
| [`observability-case-study.md`](observability-case-study.md) | trace 串联案例 |
| [`backend-resume-evidence.md`](backend-resume-evidence.md) | 简历 claim 映射 |
