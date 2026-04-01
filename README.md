# Agent Workflow Execution Engine

分布式 Agent 工作流执行引擎 — 为 LLM Agent 提供生产级的多步任务编排、安全工具执行与全链路追踪。

## 架构概览

```
                              ┌────────────────────────────┐
                              │     API Server (gin)       │
                              │   POST /workflows          │
                              │   GET  /workflows/:id      │
                              │   POST /tools/register     │
                              └─────────────┬──────────────┘
                                            │
                    ┌───────────────────────┼───────────────────────┐
                    │                       │                       │
          ┌─────────▼──────────┐  ┌────────▼─────────┐  ┌─────────▼──────────┐
          │   DAG Scheduler    │  │  MCP Tool Server  │  │   Observability    │
          │                    │  │                    │  │                    │
          │  · 拓扑排序执行     │  │  · JSON-RPC 2.0   │  │  · OTLP Trace      │
          │  · 条件分支/循环    │  │  · 工具注册/发现   │  │  · Prometheus      │
          │  · Checkpoint 持久 │  │  · 权限校验        │  │  · 结构化日志       │
          │  · 断点恢复        │  │  · 频率限制        │  │  · Grafana 大盘     │
          └─────────┬──────────┘  └────────┬─────────┘  └──────────────────────┘
                    │                       │
                    │              ┌────────▼─────────┐
                    │              │   Sandbox        │
                    │              │                  │
                    │              │  · Docker 短容器  │
                    │              │  · cgroup 资源限  │
                    │              │  · 网络隔离       │
                    │              │  · 超时 hardkill  │
                    └──────────────┴──────────────────┘
                                   │
                          ┌────────▼─────────┐
                          │   Redis / PG     │
                          │   状态持久化      │
                          └──────────────────┘
```

## 项目结构

```
agent-exec-engine/
├── cmd/
│   └── server/             # 主入口
│       └── main.go
├── internal/
│   ├── dag/                # DAG 工作流引擎
│   │   ├── graph.go        # DAG 定义与拓扑排序
│   │   ├── scheduler.go    # 步骤调度器
│   │   ├── step.go         # 步骤状态机
│   │   ├── checkpoint.go   # Checkpoint 持久化与恢复
│   │   └── scheduler_test.go
│   ├── sandbox/            # 安全沙箱执行器
│   │   ├── executor.go     # Docker 容器生命周期管理
│   │   ├── policy.go       # 资源限制与网络策略
│   │   ├── collector.go    # 执行产出收集
│   │   └── executor_test.go
│   ├── mcp/                # MCP Tool Server
│   │   ├── server.go       # JSON-RPC 2.0 服务端
│   │   ├── registry.go     # 工具注册中心
│   │   ├── ratelimit.go    # per-tool 频率限制
│   │   ├── tools/          # 内置工具实现
│   │   │   ├── code_exec.go
│   │   │   ├── web_search.go
│   │   │   ├── file_reader.go
│   │   │   └── sql_query.go
│   │   └── server_test.go
│   ├── observability/      # 可观测性
│   │   ├── tracer.go       # OpenTelemetry Trace
│   │   ├── metrics.go      # Prometheus Metrics
│   │   └── logger.go       # 结构化日志
│   ├── api/                # HTTP API 层
│   │   ├── handler.go      # 请求处理
│   │   ├── middleware.go   # 中间件
│   │   └── router.go       # 路由注册
│   └── store/              # 持久化层
│       ├── redis.go        # Redis 操作封装
│       └── interface.go    # 存储接口抽象
├── pkg/
│   └── types/              # 公共类型定义
│       ├── workflow.go     # 工作流/步骤类型
│       ├── tool.go         # 工具类型
│       └── event.go        # 事件类型
├── configs/
│   ├── config.yaml         # 默认配置
│   └── grafana/            # Grafana Dashboard JSON
│       └── agent-exec.json
├── deployments/
│   ├── Dockerfile
│   ├── docker-compose.yaml
│   └── k8s/
│       ├── deployment.yaml
│       └── service.yaml
├── docs/
│   ├── architecture.md     # 架构设计文档
│   └── api.md              # API 文档
├── Makefile
├── go.mod
├── go.sum
└── README.md
```

## 核心模块

### 1. DAG 工作流引擎 (`internal/dag/`)

Agent 的多步推理（Plan → Tool Call → Observe → Reflect）建模为 DAG：

- **拓扑排序调度**：自动解析步骤依赖，按顺序执行
- **步骤状态机**：`Pending → Running → Success/Failed/Timeout`
- **条件分支**：基于前序步骤输出的动态路由
- **Checkpoint 持久化**：每步完成后写 Redis，支持断点恢复
- **超时/重试/熔断**：单步超时自动 cancel，可配置重试策略

### 2. 安全沙箱 (`internal/sandbox/`)

Agent 工具调用的隔离执行环境：

- **Docker 短生命周期容器**：每次工具调用 = 一个容器
- **cgroup 资源硬限**：CPU / 内存 / 磁盘 / PID 数量
- **文件系统隔离**：只读挂载 + tmpfs 可写层
- **网络策略**：白名单出站规则，禁止访问云元数据
- **hardkill 超时**：超时后强制销毁容器
- **产出收集**：stdout/stderr/文件 → 结构化返回

### 3. MCP Tool Server (`internal/mcp/`)

实现 Model Context Protocol 服务端：

- **JSON-RPC 2.0 协议**：标准 MCP 通信
- **工具注册中心**：动态注册/发现/热更新
- **内置工具**：代码执行、搜索、文件读取、SQL 查询
- **权限控制**：per-tool 调用频率限制与权限校验

### 4. 可观测性 (`internal/observability/`)

全链路追踪，复用 OTLP 网关经验：

- **OpenTelemetry Trace**：每个 Agent Step 生成 Span
- **Prometheus Metrics**：step_duration、tool_calls、token_usage、sandbox_oom
- **结构化日志**：zap JSON 日志，关联 trace_id

## 快速开始

```bash
# 依赖
make deps

# 本地开发（需要 Docker）
docker compose -f deployments/docker-compose.yaml up -d
make run

# 测试
make test

# 构建
make build
```

## 真实运行证据

2026-03-31 在本机完成了一轮可复验的运行态验收，并在云服务器 A100 上补齐了 P4.4.4 的真实 vLLM 集成测试。证据分别沉淀在 [`evidence/runtime/`](evidence/runtime/)、[`evidence/screenshots/`](evidence/screenshots/) 和 [`evidence/a100/`](evidence/a100/)：

- `docker compose -f deployments/docker-compose.yaml up -d` 成功拉起 `agent-exec-engine + Redis + Prometheus + Grafana + Jaeger`
- `GET /healthz` 返回 `{"status":"ok","version":"0.1.0"}`
- Prometheus 抓到 `agent_exec_workflows_total`、`agent_exec_workflow_duration_seconds`、`agent_exec_step_duration_seconds`
- Jaeger 中可查询到同一条 trace 里的 `workflow.execute -> step.execute`
- MCP Inspector 通过 `initialize`、`tools/list`、`tools/call` 手测
- A100 `vLLM` 服务成功加载 `/infra/data/models/models/Qwen2.5-7B-Instruct`
- `GET /v1/models`、`POST /v1/chat/completions` 与 `go test ./internal/llm -v -tags=vllm -count=1 -timeout=120s` 全部通过

### Benchmark

`BenchmarkWorkflowCreate` 已补齐，最近一次基准结果如下：

```text
BenchmarkWorkflowCreate-10    	  260830	     11194 ns/op	   15620 B/op	     125 allocs/op
```

原始输出见 [`evidence/runtime/benchmark.txt`](evidence/runtime/benchmark.txt)。

### A100 / vLLM

2026-03-31 在 `NVIDIA A100-SXM4-80GB` 云服务器上完成了 `PLAN.md` 要求的真实 `-tags=vllm` 验收：

```bash
export PATH=/usr/local/go/bin:$PATH
export LLM_BASE_URL=http://127.0.0.1:8000/v1
export LLM_API_KEY=dummy
go test ./internal/llm -v -tags=vllm -count=1 -timeout=120s
```

最近一次结果：

```text
ok  	github.com/Xio-Shark/agent-exec-engine/internal/llm	0.310s
```

原始输出见：

- [`evidence/a100/nvidia-smi.txt`](evidence/a100/nvidia-smi.txt)
- [`evidence/a100/go-version.txt`](evidence/a100/go-version.txt)
- [`evidence/a100/python-packages.txt`](evidence/a100/python-packages.txt)
- [`evidence/a100/vllm-models.json`](evidence/a100/vllm-models.json)
- [`evidence/a100/vllm-chat-smoke.json`](evidence/a100/vllm-chat-smoke.json)
- [`evidence/a100/vllm-test.txt`](evidence/a100/vllm-test.txt)

### 截图

- Grafana Dashboard: [`evidence/screenshots/grafana-dashboard.png`](evidence/screenshots/grafana-dashboard.png)
- Jaeger Trace: [`evidence/screenshots/jaeger-trace.png`](evidence/screenshots/jaeger-trace.png)
- API 响应: [`evidence/screenshots/api-response.png`](evidence/screenshots/api-response.png)
- Benchmark 输出: [`evidence/screenshots/benchmark.png`](evidence/screenshots/benchmark.png)
- 全量测试通过: [`evidence/screenshots/go-test.png`](evidence/screenshots/go-test.png)

更完整的验收命令、输出与 A100 真机记录见 [`docs/runtime-evidence.md`](docs/runtime-evidence.md)。

## API

REST API 基于 [gin](https://github.com/gin-gonic/gin) 框架实现。完整文档见 [docs/api.md](docs/api.md)。

### Endpoints

| Method | Path | 说明 |
|--------|------|------|
| `GET` | `/healthz` | 健康检查 |
| `GET` | `/metrics` | Prometheus metrics |
| `POST` | `/api/v1/workflows` | 创建并启动工作流 |
| `GET` | `/api/v1/workflows/:id` | 查询工作流状态 |
| `POST` | `/api/v1/workflows/:id/resume` | 恢复暂停的工作流 |
| `DELETE` | `/api/v1/workflows/:id` | 取消工作流 |
| `GET` | `/api/v1/workflows/:id/steps` | 列出步骤状态 |
| `GET` | `/api/v1/workflows/:id/steps/:step_id` | 查询单步详情 |
| `POST` | `/api/v1/tools` | 注册声明式动态工具（当前支持 `template` handler） |
| `GET` | `/api/v1/tools` | 列出所有工具 |
| `DELETE` | `/api/v1/tools/:name` | 注销工具 |

### 示例

```bash
# 创建工作流
curl -X POST http://localhost:8080/api/v1/workflows \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "code-review-agent",
    "steps": [
      {"id": "plan", "type": "llm_call", "config": {"system_prompt": "分析代码变更"}},
      {"id": "search", "type": "tool_call", "tool": "code_exec", "depends_on": ["plan"]},
      {"id": "review", "type": "llm_call", "depends_on": ["search"]}
    ]
  }'

# 查询状态
curl http://localhost:8080/api/v1/workflows/{id}

# 查询步骤
curl http://localhost:8080/api/v1/workflows/{id}/steps

# 列出工具
curl http://localhost:8080/api/v1/tools | jq .

# 健康检查
curl http://localhost:8080/healthz
```

`POST /api/v1/tools` 现已支持声明式动态注册：请求体必须包含 `handler`，当前仅开放 `type=template` 模式。服务端使用 `text/template` 渲染输入参数并执行 `missingkey=error` 校验，避免“注册成功但运行期才崩”的假能力。

## 设计决策

| 决策 | 选择 | 原因 |
|------|------|------|
| 语言 | Go | 高并发、Docker SDK 生态、与现有 AI Infra 平台统一 |
| 状态存储 | Redis | Checkpoint 读写频繁，需要低延迟；与现有平台复用 |
| 沙箱 | Docker | 成熟稳定，cgroup v2 资源隔离，生态最好 |
| 协议 | MCP JSON-RPC | 行业标准化趋势，最大化工具互操作性 |
| API 框架 | gin | Go 生态最流行 HTTP 框架，性能优异，中间件丰富 |
| 追踪 | OTLP | 直接复用已有 OTLP 网关，无需新增基础设施 |

## 与已有项目的关系

本项目是 [AI Infra Platform](https://github.com/Xio-Shark/ai-infra-platform) 的上层扩展，两者通过 HTTP API 松耦合：

```
Agent Execution Engine  ← 本项目（Agent 工作流编排层）
        ↓ HTTP 调用
AI Infra Platform       ← github.com/Xio-Shark/ai-infra-platform（推理网关 + GPU 调度）
        ↓ 调用
推理引擎                ← vLLM / SGLang + CUDA 算子
```

| 项目 | 职责 | 仓库 |
|------|------|------|
| **Agent Exec Engine** | 多步 Agent 工作流编排、DAG 调度、安全沙箱、MCP 工具 | [Xio-Shark/Agent-Exec-Engine](https://github.com/Xio-Shark/Agent-Exec-Engine) |
| **AI Infra Platform** | GPU 资源感知调度、推理网关、并发压测引擎 | [Xio-Shark/ai-infra-platform](https://github.com/Xio-Shark/ai-infra-platform) |

## 文档

| 文档 | 说明 |
|------|------|
| [docs/architecture.md](docs/architecture.md) | 架构设计、模块职责、数据流 |
| [docs/api.md](docs/api.md) | REST API 详细文档，含每个 endpoint 的请求/响应示例 |
| [docs/deployment.md](docs/deployment.md) | Docker / K8s 部署指南、环境变量、生产检查清单 |

## P5 集成约定

- `infra.gateway_url` 指向 AI Infra 推理网关，`llm.base_url` 默认派生为 `{gateway_url}/v1`
- `infra.scheduler_url` 指向 AI Infra API Server，用于 GPU 调度预约
- `ReleaseGPU` 通过 AI Infra 现有的 `POST /jobs/{id}/cancel` 语义释放预约作业，避免悬挂占卡
- `deployments/docker-compose.yaml` 通过外部网络 `ai-infra-platform-push_default` 接入 AI Infra 服务

## P6 可观测性约定

- Prometheus 抓取配置位于 `deployments/prometheus.yml`
- Grafana dashboard JSON 位于 `configs/grafana/agent-exec.json`
- `docker compose -f deployments/docker-compose.yaml up -d` 同时拉起 Jaeger（`http://localhost:16686`）承接 OTLP Trace
- 运行 `docker compose -f deployments/docker-compose.yaml up -d` 后，Grafana 自动加载 Dashboard

## License

MIT
