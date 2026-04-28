# Backend Resume Evidence

本页服务于 `滕彦翕_后端开发实习` 简历，不是新的功能设计。它把后端主简历中的 `Workflow Execution Engine` bullet 映射到项目内可验证证据，便于投递前自查和面试追问时快速定位。

## 简历定位

`agent-exec-engine` 在后端主简历中的角色不是“只会套 Agent 框架”，而是一个通用工作流执行后端：

- 控制面：DAG 调度、状态机、并发执行、条件分支、人工暂停/恢复。
- 执行面：MCP 工具调用、Docker 沙箱、输入校验、限流、超时与资源隔离。
- 可靠性：Redis checkpoint、失败恢复、cancel 传播、可回查运行状态。
- 可观测：workflow / step / tool 维度的 trace、metrics、结构化日志。

## Claim Mapping

| 简历 claim | 项目证据 | 验证方式 |
| --- | --- | --- |
| DAG 调度主链路使用拓扑排序、并行执行和条件分支 | `internal/dag/graph.go`、`internal/dag/scheduler_run.go`、`internal/dag/step.go` | `go test ./internal/dag -count=1` |
| Human-in-the-loop 暂停/恢复和长任务 checkpoint | `internal/dag/checkpoint.go`、`internal/api/manager.go` | `go test ./internal/dag ./internal/api -run '(Checkpoint|Resume|Human)' -count=1` |
| MCP JSON-RPC 2.0 工具层，覆盖输入校验和限流 | `internal/mcp/server.go`、`internal/mcp/registry.go`、`internal/mcp/validator.go`、`internal/mcp/ratelimit.go` | `go test ./internal/mcp -count=1` |
| Docker 沙箱隔离工具执行 | `internal/sandbox/executor.go`、`internal/sandbox/pool.go`、`internal/sandbox/images.go` | `go test ./internal/sandbox -count=1` |
| Agent 场景作为 workflow step 接入，而非替代后端控制面 | `internal/llm/executor.go`、`internal/llm/react_executor.go`、`pkg/types/workflow.go` | `go test ./internal/llm -count=1` |
| OpenTelemetry、Prometheus 和结构化日志贯穿主链路 | `internal/observability/`、`configs/grafana/agent-exec.json`、`evidence/runtime/metrics.txt`、`evidence/runtime/jaeger-traces.json` | `docker compose -f deployments/docker-compose.yaml up -d && make demo-workflow` |

## 跨项目边界证据

| 简历 claim | 项目证据 | 验证方式 |
| --- | --- | --- |
| Agent 后端主服务与外部 Infra / RAG / Eval 的边界清晰 | [`system-map.md`](system-map.md)、[`contract-table.md`](contract-table.md) | 阅读文档并核对代码路径 |
| LLM 推理通过 Gateway 承接，含故障切换语义 | `internal/llm/client.go`、`ai-job-orchestrator/docs/gateway-failover-evidence.md` | `go test ./internal/llm -run 'TestClientChat_RetriesServerErrors' -v` |
| RAG 检索作为 Agent 工具边界接入 | `internal/mcp/tools/rag_search.go` | `go test ./internal/mcp -run 'TestRAGSearch' -v` |

## Reviewer Path

1. 先读 [`reviewer-quickstart.md`](reviewer-quickstart.md)，确认 demo 路线。
2. 再读 [`system-map.md`](system-map.md)，确认"主服务是谁、外部依赖是谁"。
3. 再读 [`contract-table.md`](contract-table.md)，确认跨项目接口契约。
4. 再读 [`runtime-evidence.md`](runtime-evidence.md)，确认运行证据。
5. 最后按上表打开对应代码和测试。

## 表述边界

- 当前简历把 RAG 写成“外部检索工具接入边界”，不宣称已内置完整 RAG adapter。
- MCP 是本项目需要的薄协议面，完整边界见 [`mcp-design-decision.md`](mcp-design-decision.md)。
- 本项目主线是后端执行框架，LLM / ReAct 是 step executor 场景之一。
