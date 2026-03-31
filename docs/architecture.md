# Architecture

## High-Level Overview

Agent Execution Engine 是面向 LLM Agent 的生产级多步任务编排引擎。核心 idea：把 Agent 的推理过程（Plan → Tool Call → Observe → Reflect）建模为 DAG，用工业级基础设施保障可靠执行。

```
                              ┌────────────────────────────┐
                              │     API Server (gin)       │
                              │   POST /api/v1/workflows   │
                              │   GET  /api/v1/workflows   │
                              │   POST /api/v1/tools       │
                              └─────────────┬──────────────┘
                                            │
                    ┌───────────────────────┼───────────────────────┐
                    │                       │                       │
          ┌─────────▼──────────┐  ┌────────▼─────────┐  ┌─────────▼──────────┐
          │   DAG Scheduler    │  │  MCP Tool Server  │  │   Observability    │
          │                    │  │                    │  │                    │
          │  · Kahn 拓扑排序   │  │  · JSON-RPC 2.0   │  │  · OTLP Trace      │
          │  · CEL 条件分支    │  │  · 工具注册/发现   │  │  · Prometheus      │
          │  · errgroup 并行   │  │  · 输入校验        │  │  · zap 结构化日志  │
          │  · Checkpoint 持久 │  │  · 令牌桶限流      │  │  · Grafana 大盘    │
          │  · 断点恢复        │  │  · stdio/HTTP 传输 │  │                    │
          └─────────┬──────────┘  └────────┬─────────┘  └──────────────────────┘
                    │                       │
                    │              ┌────────▼─────────┐
                    │              │   Docker Sandbox  │
                    │              │                   │
                    │              │  · 短生命周期容器  │
                    │              │  · cgroup 资源限制 │
                    │              │  · 网络隔离       │
                    │              │  · OOM 检测       │
                    └──────────────┴───────────────────┘
                                   │
                          ┌────────▼─────────┐
                          │   Redis / PG     │
                          │   状态持久化      │
                          └──────────────────┘
                                   │
                          ┌────────▼─────────┐
                          │  AI Infra Platform│
                          │  推理网关 + GPU   │
                          └──────────────────┘
```

## Module Responsibilities

### `cmd/server/main.go`
应用入口。负责配置加载、依赖注入、服务启动和优雅关停。

### `internal/api/`
HTTP API 层。包含：
- **router.go** — gin 路由注册，挂载 health、metrics、REST v1 端点
- **handler.go** — 9 个 REST handler（workflow CRUD + tool CRUD + steps）
- **middleware.go** — RequestID 注入 + zap 访问日志
- **errors.go** — 统一错误响应格式
- **manager.go** — WorkflowManager，管理多 workflow 定义和运行实例的生命周期

### `internal/dag/`
DAG 工作流引擎核心。包含：
- **graph.go** — DAG 定义，Kahn 拓扑排序，CEL 条件分支求值
- **scheduler.go** — 调度器构造和配置选项
- **scheduler_run.go** — 核心执行循环（runLoop），errgroup 并行，tool_use 循环
- **scheduler_state.go** — 步骤状态管理、checkpoint 保存、事件发布
- **step.go** — 步骤状态机（FSM），合法转换表
- **checkpoint.go** — Redis-backed checkpoint 持久化 + 断点恢复

### `internal/sandbox/`
Docker 沙箱执行器。包含：
- **executor.go** — 容器创建、启动、等待、日志收集、OOM 检测、cleanup
- **pool.go** — 信号量并发控制
- **images.go** — 启动时预拉取常用镜像

### `internal/mcp/`
MCP Tool Server。包含：
- **server.go** — JSON-RPC 2.0 协议处理（HTTP + batch request）
- **registry.go** — 工具注册中心（Register/Unregister/List/Call）
- **validator.go** — 工具输入 schema 校验
- **ratelimit.go** — 令牌桶限流
- **stdio.go** — stdin/stdout 传输
- **tools/** — 4 个内置工具（code_exec, web_search, file_reader, sql_query）

### `internal/llm/`
LLM 客户端层。包含：
- **client.go** — OpenAI-compatible HTTP 客户端，支持重试和指数退避
- **stream.go** — SSE 流式响应解析
- **executor.go** — LLMStepExecutor（tool_use 多轮循环）+ ToolStepExecutor
- **prompts/templates.go** — 角色模板（Planner/Coder/Reviewer）

### `internal/observability/`
可观测性。包含：
- **tracer.go** — OpenTelemetry OTLP gRPC exporter + Span 埋点
- **metrics.go** — 20+ Prometheus metrics 定义
- **logger.go** — zap 结构化日志，trace_id/span_id 关联

### `internal/store/`
持久化抽象。包含：
- **interface.go** — Store 接口（Set/Get/Delete/Ping/Close）
- **redis.go** — Redis 实现
- **memory.go** — 内存实现（测试用）

### `pkg/types/`
公共类型。包含：
- **workflow.go** — Workflow, Step, StepState, WorkflowRun, Checkpoint
- **tool.go** — ToolDefinition, ToolCall, ToolResult, ToolSchema
- **event.go** — Event, EventType

## Data Flow

### Workflow Execution

```
HTTP POST /api/v1/workflows
    │
    ▼
WorkflowManager.CreateAndRun()
    │
    ├── validateWorkflow()
    ├── dag.NewScheduler()
    │       ├── NewGraph() → Kahn 拓扑排序
    │       └── initStepStates()
    │
    └── go scheduler.Run(ctx)
            │
            ├── runLoop()
            │   ├── graph.ReadySteps()
            │   ├── processReadySteps()
            │   │   ├── StepTypeBranch → EvaluateCondition(CEL)
            │   │   ├── StepTypeHuman  → pauseWorkflow()
            │   │   └── default        → async execute
            │   │
            │   └── executeBatch()
            │       ├── errgroup.SetLimit(maxParallel)
            │       ├── executor.Execute(ctx, step, input)
            │       │   ├── LLMStepExecutor → client.Chat() → tool_use loop
            │       │   └── ToolStepExecutor → registry.Call()
            │       ├── graph.MarkComplete(stepID)
            │       └── saveCheckpoint()
            │
            └── completeWorkflow() / failWorkflow()

```

### Tool Call

```
registry.Call(ctx, ToolCall)
    │
    ├── ValidateInput(schema, input)
    ├── rateLimiter.Allow()
    └── handler(ctx, input)
        │
        ├── code_exec → sandbox.Pool.Execute()
        │   ├── Acquire semaphore
        │   ├── ContainerCreate()
        │   ├── CopyToContainer() (file inject)
        │   ├── ContainerStart()
        │   ├── ContainerWait()
        │   ├── ContainerLogs()
        │   ├── ContainerInspect() (OOM check)
        │   └── ContainerRemove()
        │
        ├── web_search → Tavily API / stub
        ├── file_reader → os.ReadFile() (path-validated)
        └── sql_query → database/sql SELECT (read-only tx)
```

## Key Design Decisions

| 决策 | 选择 | 原因 |
|------|------|------|
| DAG 调度 | Kahn + errgroup | 拓扑排序保证顺序依赖，errgroup 控制并发度 |
| 条件分支 | CEL 表达式 | Google 官方表达式引擎，安全沙箱化求值 |
| 状态持久化 | Redis Checkpoint | 读写频繁需低延迟，乐观锁防并发冲突 |
| 沙箱 | Docker SDK | cgroup v2 资源隔离，成熟稳定 |
| 工具协议 | MCP JSON-RPC 2.0 | 行业标准化趋势，支持 stdio + HTTP 双传输 |
| API 框架 | gin | Go 生态最流行 HTTP 框架，性能优异 |
| 追踪 | OTLP gRPC | 直接复用已有 AI Infra Platform 的 OTLP 网关 |
| 日志关联 | zap + trace_id | 结构化日志关联分布式追踪，排障效率高 |
