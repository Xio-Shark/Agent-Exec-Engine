# Agent Execution Engine — 可执行实施计划

> **用途**：本文档是交付给下一个 agent 的完整执行工单。每个任务包含：要改哪个文件、用什么依赖（精确版本）、执行什么命令、验收标准是什么。
>
> **项目根目录**：`/Users/xioshark/Desktop/career/agent-exec-engine`
>
> **约束**：做扎实（6-8 周），真实 Docker SDK，对接 vLLM/SGLang，与 AI Infra Platform 真实集成
>
> 最后更新：2026-03-31

---

## 环境信息

| 项目 | 值 |
|------|-----|
| Go 版本 | `go1.26.0 darwin/arm64`（go.mod 声明 `go 1.22` 兼容） |
| Go 路径 | `/Users/xioshark/.local/opt/go/current/bin/go` |
| Docker | `Docker 29.2.1` / `Docker Compose v5.0.2` |
| OS | macOS Darwin 25.2.0 (arm64) |
| 项目 module | `github.com/Xio-Shark/agent-exec-engine` |
| AI Infra Platform 路径 | `/Users/xioshark/Desktop/career/滕彦翕/项目/ai-infra-platform-push/` |
| AI Infra Platform module | `ai-infra-platform`（go 1.22，服务：api-server/scheduler/worker/notifier/benchctl） |

## 已有文件清单（骨架已完成）

```
cmd/server/main.go                       # 主入口，已串联 config+redis+mcp+sandbox
internal/config/config.go                 # viper 配置加载，4 个测试 PASS
internal/config/config_test.go
internal/dag/graph.go                     # DAG 拓扑排序，Kahn 算法
internal/dag/scheduler.go                 # 调度器骨架，goroutine 并行
internal/dag/scheduler_test.go            # 4 个测试 PASS
internal/dag/step.go                      # 步骤状态机 FSM
internal/dag/checkpoint.go                # Checkpoint 骨架（in-memory）
internal/mcp/server.go                    # MCP JSON-RPC 2.0 骨架
internal/mcp/registry.go                  # 工具注册中心骨架
internal/mcp/ratelimit.go                 # 令牌桶限流骨架
internal/mcp/tools/tools.go              # 4 个内置工具 stub
internal/observability/logger.go          # zap 日志骨架
internal/observability/metrics.go         # 20+ Prometheus metrics 定义
internal/observability/tracer.go          # OTEL tracer 骨架
internal/sandbox/executor.go              # 沙箱骨架（stub，非真实 Docker）
internal/store/interface.go               # Store 接口定义
internal/store/redis.go                   # Redis 真实客户端
pkg/types/workflow.go                     # 工作流/步骤类型定义
pkg/types/tool.go                        # 工具类型定义
pkg/types/event.go                       # 事件类型定义
configs/config.yaml                      # 默认配置文件
deployments/Dockerfile                   # 多阶段构建
deployments/docker-compose.yaml          # Redis + Prometheus + Grafana
.github/workflows/ci.yml                 # GitHub Actions CI
.golangci.yml                            # Lint 配置
Makefile                                 # build/run/test/lint/docker
```

## 已安装依赖（go.mod 已锁定）

| 依赖 | 版本 | 用途 |
|------|------|------|
| `github.com/google/uuid` | v1.6.0 | 工作流运行 ID 生成 |
| `github.com/prometheus/client_golang` | v1.20.0 | Prometheus metrics |
| `github.com/redis/go-redis/v9` | v9.7.0 | Redis 客户端 |
| `github.com/spf13/viper` | v1.19.0 | 配置文件加载 |
| `go.opentelemetry.io/otel` | v1.32.0 | OpenTelemetry API |
| `go.opentelemetry.io/otel/trace` | v1.32.0 | Trace API |
| `go.uber.org/zap` | v1.27.0 | 结构化日志 |

## 当前测试状态

```
go test ./... -v -count=1
# internal/config: 4 PASS
# internal/dag:    4 PASS
# 其余包无测试文件
```

---

## 阶段总览

| 阶段 | 周期 | 核心产出 | 状态 |
|------|------|---------|------|
| P0 基础设施 | W1 | go mod tidy / CI / 配置加载 / Redis 连接 | **DONE** |
| P1 DAG 引擎 | W1-W2 | 拓扑调度 + 并行执行 + Checkpoint + 断点恢复 | TODO |
| P2 Docker 沙箱 | W2-W3 | 真实容器管理 + 资源隔离 + 产出收集 | TODO |
| P3 MCP Server | W3-W4 | JSON-RPC 2.0 + 工具注册 + 4 个内置工具 | TODO |
| P4 vLLM 对接 | W4-W5 | OpenAI-compatible 调用 + Agent Step 执行器 | TODO |
| P5 AI Infra 集成 | W5-W6 | 推理网关调用 + GPU 调度联动 | TODO |
| P6 可观测性 | W6-W7 | OTLP Trace + Prometheus + Grafana 大盘 | TODO |
| P7 API + 文档 | W7-W8 | REST API + 集成测试 + README + 架构文档 | **DONE** |

---

## P0: 基础设施 — DONE

所有子任务已完成，详见 git log。遗留：
- P0.3 Redis 单元测试需要 Redis 实例（CI 的 docker-compose services 已配置）
- P0.5 GitHub 仓库未创建（手动操作）

---

## P1: DAG 工作流引擎

### P1.1 Graph 条件分支

**目标**：让 DAG 支持条件分支（if-else 路由）和并行 fan-out/fan-in。

**新增依赖**：
```bash
cd /Users/xioshark/Desktop/career/agent-exec-engine
go get github.com/google/cel-go@v0.21.0
# 用于条件表达式求值（例如 step.output.status == "approved"）
```

**修改文件**：`internal/dag/graph.go`

**具体任务**：
- [ ] **P1.1.1** 在 `Graph` 中添加 `EvaluateCondition(stepID string, env map[string]any) (bool, error)` 方法
  - 使用 `cel-go` 编译并求值 `Step.Condition` 字段
  - `env` 来自前序步骤的 `StepState.Output`（JSON 反序列化为 `map[string]any`）
  - 条件为 false 时，该步骤标记为 `StepSkipped`，并对下游执行 `MarkComplete`（跳过不阻塞）
- [ ] **P1.1.2** 在 `Graph` 中添加 `SkipStep(stepID string) []string` 方法
  - 与 `MarkComplete` 类似，但将步骤状态设为 `StepSkipped`
  - 返回新 ready 的下游步骤
- [ ] **P1.1.3** 补充测试（`internal/dag/graph_test.go`）：
  - `TestGraph_ConditionalBranch`：A → (condition=true)B / (condition=false)C → D，验证只走一个分支
  - `TestGraph_SkipPropagation`：跳过 B 后 D 仍可执行（因为 D 只依赖 A 和 B，B 跳过等同完成）
  - `TestGraph_EmptyGraph`：空步骤列表返回空 ready
  - `TestGraph_SingleNode`：无依赖的单节点直接 ready
  - `TestGraph_IsolatedNode`：有孤立节点（无入边无出边）正常处理

**验收命令**：
```bash
go test ./internal/dag/ -v -run "TestGraph" -count=1
# 期望：所有新增测试 PASS
```

---

### P1.2 步骤状态机增强

**修改文件**：`internal/dag/step.go`

**具体任务**：
- [ ] **P1.2.1** 在 `CanTransition` 中添加 `StepSkipped` 作为终态（不允许从 Skipped 转其他状态）
- [ ] **P1.2.2** 添加 `TransitionWithLog(from, to StepStatus, logger *zap.Logger)` 函数
  - 每次状态转换记录一条 `Info` 日志：`step_id`, `from`, `to`, `timestamp`
  - 非法转换记录 `Warn` 日志
- [ ] **P1.2.3** 补充测试（`internal/dag/step_test.go`，新建文件）：
  - `TestCanTransition_AllValid`：遍历 `ValidTransitions` 所有合法路径
  - `TestCanTransition_Invalid`：`Success → Running` 返回 false
  - `TestShouldRetry_NoPolicy`：Retry 为 nil 时返回 false
  - `TestShouldRetry_ExhaustedRetries`：RetryCount >= MaxRetries 返回 false
  - `TestStepState_JSONRoundTrip`：序列化 → 反序列化 → 字段一致

**验收命令**：
```bash
go test ./internal/dag/ -v -run "TestCanTransition|TestShouldRetry|TestStepState" -count=1
```

---

### P1.3 Scheduler 生产化

**新增依赖**：
```bash
go get golang.org/x/sync@v0.10.0
# 提供 errgroup 用于并行步骤执行
```

**修改文件**：`internal/dag/scheduler.go`

**具体任务**：
- [ ] **P1.3.1** 用 `errgroup.Group` + `SetLimit(maxParallel)` 替换当前的裸 `sync.WaitGroup`
  - `maxParallel` 从 `Config.DAG.MaxParallelSteps` 读取
  - 错误时 `errgroup` 自动 cancel context，传播到所有并行步骤
- [ ] **P1.3.2** 实现步骤间数据传递
  - 在 `Scheduler` 中添加 `stepOutputs map[string]string`（stepID → output JSON）
  - `executeStep` 完成后将 `StepState.Output` 写入 `stepOutputs`
  - 执行步骤时将所有 `DependsOn` 步骤的 output 合并为 `input map[string]any` 传入 `StepExecutor.Execute`
- [ ] **P1.3.3** 实现条件分支求值
  - 在调度循环中，对 `Step.Type == StepTypeBranch` 的步骤调用 `Graph.EvaluateCondition`
  - 条件为 false → `Graph.SkipStep` → 收集新 ready 步骤
- [ ] **P1.3.4** 实现 Human-in-the-loop
  - 对 `Step.Type == StepTypeHuman` 的步骤，将 `WorkflowRun.Status` 设为 `WorkflowPaused`
  - 添加 `Resume(ctx context.Context, stepID string, input map[string]any) error` 方法
  - 调用 `Resume` 时将输入写入 `stepOutputs` 并恢复调度循环
  - 暂停时保存 Checkpoint
- [ ] **P1.3.5** 实现 cancel 传播
  - `Run()` 方法接收的 `ctx` cancel 时，所有进行中步骤的 `stepCtx` 也 cancel
  - 添加 `Cancel()` 方法，主动 cancel 整个工作流
  - cancel 后所有 `Running` 步骤标记为 `Cancelled`

**验收命令**：
```bash
go test ./internal/dag/ -v -count=1 -race
# 期望：所有测试 PASS，-race 无竞争检测
```

---

### P1.4 Checkpoint 生产化

**修改文件**：`internal/dag/checkpoint.go`

**具体任务**：
- [ ] **P1.4.1** 删除 `RedisCheckpointer` 中的 `store map[string][]byte`，替换为真实 Redis
  - 构造函数改为 `NewRedisCheckpointer(store store.Store) *RedisCheckpointer`
  - `Save`：`store.Set(ctx, "checkpoint:{runID}", jsonBytes, 86400)` — TTL 24 小时
  - `Load`：`store.Get(ctx, "checkpoint:{runID}")` → 反序列化
  - 乐观锁：`Save` 前先 `Load` 当前版本，如果 `version` 不匹配返回 `ErrCheckpointConflict`
- [ ] **P1.4.2** 实现 `RestoreScheduler` 函数
  - 签名：`RestoreScheduler(ctx context.Context, wf *types.Workflow, cp *types.Checkpoint, executors map[types.StepType]StepExecutor, opts ...SchedulerOption) (*Scheduler, error)`
  - 从 Checkpoint 重建 Graph（已完成的步骤直接 `MarkComplete`）
  - 将 `StepStates` 从 Checkpoint 恢复到 `WorkflowRun`
  - 恢复后继续调度剩余步骤
- [ ] **P1.4.3** 补充测试（`internal/dag/checkpoint_test.go`，新建文件）：
  - `TestCheckpoint_SaveLoad`：保存 → 加载 → 字段一致（使用内存 Store mock）
  - `TestCheckpoint_OptimisticLock`：并发 Save 同一个 runID，第二个返回 conflict 错误
  - `TestCheckpoint_TTL`：验证 TTL 参数传递正确
  - `TestRestoreScheduler`：3 步工作流执行到第 2 步 → checkpoint → 新建 Scheduler → 从第 3 步继续

**内存 Store Mock**（用于不依赖 Redis 的单元测试）：

新建文件 `internal/store/memory.go`：
```go
// MemoryStore 实现 Store 接口，用于测试
type MemoryStore struct {
    mu   sync.RWMutex
    data map[string][]byte
}
```

**验收命令**：
```bash
go test ./internal/dag/ -v -run "TestCheckpoint|TestRestore" -count=1
go test ./internal/store/ -v -count=1
```

---

### P1.5 集成测试

**新建文件**：`internal/dag/integration_test.go`

以 `//go:build integration` 标记，需要 Redis 才能跑。

**具体任务**：
- [ ] **P1.5.1** `TestIntegration_LinearWorkflow`
  - 创建 3 步线性 DAG：A → B → C
  - 每步的 mockExecutor 返回 `"step-{id}-output"`
  - 验证：所有步骤 `StepSuccess`，WorkflowRun `WorkflowCompleted`，Output 传递正确
- [ ] **P1.5.2** `TestIntegration_DiamondDAG`
  - 创建菱形 DAG：A → B, A → C, B+C → D
  - 验证：B 和 C 并行执行（通过记录执行时间戳验证），D 在两者都完成后执行
- [ ] **P1.5.3** `TestIntegration_RetrySuccess`
  - 创建 2 步 DAG，第 2 步前 2 次返回 error，第 3 次返回 success
  - 配置 `RetryPolicy{MaxRetries: 3, Backoff: 10ms}`
  - 验证：`RetryCount == 2`，最终 `StepSuccess`
- [ ] **P1.5.4** `TestIntegration_Timeout`
  - 创建 1 步 DAG，executor 中 `time.Sleep(5*time.Second)`
  - 配置 `Step.Timeout = 100ms`
  - 验证：`StepTimeout`，`WorkflowFailed`
- [ ] **P1.5.5** `TestIntegration_CheckpointRestore`
  - 创建 3 步 DAG，执行到第 2 步完成后手动调用 `saveCheckpoint`
  - 新建 Scheduler 用 `RestoreScheduler` 恢复
  - 验证：只执行第 3 步，前 2 步不重复执行

**验收命令**：
```bash
# 需要启动 Redis
docker run -d --name test-redis -p 6379:6379 redis:7-alpine
go test ./internal/dag/ -v -tags=integration -count=1
docker rm -f test-redis
```

**P1 总验收**：
```bash
go build ./...
go test ./... -v -race -count=1
# 期望：全部 PASS，0 race condition
# 期望测试数量：≥ 20 个（8 现有 + 5 graph + 5 step + 5 integration - 部分重叠）
```

---

## P2: Docker 沙箱

### P2.1 Docker SDK 集成

**新增依赖**：
```bash
go get github.com/docker/docker@v27.0.0+incompatible
go get github.com/docker/go-connections@v0.5.0
go get github.com/opencontainers/image-spec@v1.1.0
# Docker SDK 需要这三个包
```

**修改文件**：`internal/sandbox/executor.go`

**具体任务**：
- [ ] **P2.1.1** 在 `Executor` struct 中添加 `dockerCli *client.Client` 字段
  - `NewExecutor()` 中：
    ```go
    import "github.com/docker/docker/client"
    cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
    ```
  - 在 `NewExecutor` 中调用 `cli.Ping(ctx)` 验证 Docker 可用
- [ ] **P2.1.2** 实现 `Execute` 真实逻辑（替换当前 stub）：
  1. **创建容器**：
     ```go
     resp, err := e.dockerCli.ContainerCreate(ctx, &container.Config{
         Image: req.Image,
         Cmd:   req.Command,
         Env:   envSlice,
         Labels: map[string]string{"agent-exec-sandbox": "true"},
     }, &container.HostConfig{
         Resources: container.Resources{
             CPUQuota:  req.Policy.CPUQuota,
             Memory:    req.Policy.MemoryLimit,
             PidsLimit: &req.Policy.PidsLimit,
         },
         ReadonlyRootfs: req.Policy.ReadOnlyFS,
         Tmpfs:          map[string]string{"/tmp": fmt.Sprintf("size=%d", req.Policy.DiskLimit)},
         NetworkMode:    container.NetworkMode(req.Policy.NetworkMode),
     }, nil, nil, "")
     ```
  2. **注入文件**（如果 `req.Files` 不为空）：
     - 创建 tar archive → `cli.CopyToContainer(ctx, containerID, "/workspace", tarReader, ...)`
  3. **启动容器**：`cli.ContainerStart(ctx, containerID, ...)`
  4. **等待完成**：`cli.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)`
     - 使用 `context.WithTimeout(ctx, req.Policy.Timeout)` 确保超时
  5. **收集日志**：`cli.ContainerLogs(ctx, containerID, ...)` → 读取 stdout/stderr
  6. **检查 OOM**：`cli.ContainerInspect(ctx, containerID)` → `State.OOMKilled`
  7. **收集输出文件**（如果需要）：`cli.CopyFromContainer(ctx, containerID, "/output")`
  8. **删除容器**：`defer cli.ContainerRemove(ctx, containerID, ...)` — 在函数开头用 defer 确保 always remove
- [ ] **P2.1.3** 实现 `Cleanup`：
  ```go
  containers, _ := cli.ContainerList(ctx, container.ListOptions{
      Filters: filters.NewArgs(filters.Arg("label", "agent-exec-sandbox=true")),
  })
  for _, c := range containers {
      cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true})
  }
  ```

---

### P2.2 并发控制

**新建文件**：`internal/sandbox/pool.go`

- [ ] **P2.2.1** 实现信号量控制并发沙箱数量：
  ```go
  type Pool struct {
      sem chan struct{} // buffered channel 作为信号量
      executor *Executor
  }
  func NewPool(maxConcurrent int, executor *Executor) *Pool
  func (p *Pool) Execute(ctx context.Context, req ExecutionRequest) (*ExecutionResult, error)
  // Acquire → Execute → Release
  ```

---

### P2.3 镜像预拉取

**新建文件**：`internal/sandbox/images.go`

- [ ] **P2.3.1** 实现启动时预拉取常用镜像：
  ```go
  func (e *Executor) PrePullImages(ctx context.Context, images []string) error
  // 默认镜像列表：["python:3.12-slim", "alpine:3.19", "node:20-slim"]
  // 使用 cli.ImagePull → io.Copy(io.Discard, reader) 等待完成
  ```

---

### P2.4 测试

**新建文件**：`internal/sandbox/executor_test.go`

以 `//go:build docker` 标记，需要 Docker daemon 才能跑。

- [ ] **P2.4.1** `TestExecute_PythonHelloWorld`：执行 `python -c "print('hello')"` → stdout 包含 "hello"
- [ ] **P2.4.2** `TestExecute_BashCommand`：执行 `echo "test" && ls /` → 正常输出
- [ ] **P2.4.3** `TestExecute_Timeout`：执行 `sleep 30`，Timeout=1s → `TimedOut == true`
- [ ] **P2.4.4** `TestExecute_OOM`：执行 Python 分配大数组（`[0]*10**9`），MemoryLimit=32MB → `OOMKilled == true`
- [ ] **P2.4.5** `TestExecute_NetworkNone`：执行 `curl google.com`，NetworkMode=none → 失败
- [ ] **P2.4.6** `TestExecute_ReadOnlyFS`：执行 `touch /test` → 失败（只读文件系统）
- [ ] **P2.4.7** `TestExecute_FileInjectAndCollect`：注入 `/workspace/input.txt` → 读取 → 写入 `/output/result.txt` → 收集
- [ ] **P2.4.8** `TestCleanup`：创建 3 个容器 → `Cleanup()` → 0 个残留

**验收命令**：
```bash
go test ./internal/sandbox/ -v -tags=docker -count=1 -timeout=120s
# 期望：8 个测试 PASS
# 注意：首次运行需要拉取镜像，可能较慢
```

---

## P3: MCP Tool Server

### P3.1 JSON-RPC 2.0 协议完善

**修改文件**：`internal/mcp/server.go`

- [ ] **P3.1.1** 完善错误码常量：
  ```go
  const (
      ErrParse          = -32700 // JSON 解析错误
      ErrInvalidRequest = -32600 // 无效的 JSON-RPC 请求
      ErrMethodNotFound = -32601 // 方法不存在
      ErrInvalidParams  = -32602 // 参数无效
      ErrInternal       = -32603 // 内部错误
      ErrToolNotFound   = -32000 // 工具不存在（自定义）
      ErrRateLimited    = -32001 // 限流（自定义）
      ErrPermission     = -32002 // 权限不足（自定义）
  )
  ```
- [ ] **P3.1.2** 支持 batch request：
  - 检测请求 body 是 `[` 开头 → 解析为 `[]JSONRPCRequest`
  - 并行处理每个请求 → 返回 `[]JSONRPCResponse`
- [ ] **P3.1.3** 支持 stdio transport（新建 `internal/mcp/stdio.go`）：
  - 从 stdin 逐行读 JSON → 处理 → 写 JSON 到 stdout
  - 用 `bufio.Scanner` + `json.Decoder`
  - 在 `cmd/server/main.go` 中根据 `--stdio` flag 选择 HTTP 或 stdio 模式
- [ ] **P3.1.4** MCP Inspector 兼容性测试：
  ```bash
  # 安装 MCP Inspector
  npx @anthropics/mcp-inspector go run ./cmd/server --stdio
  # 手动验证 tools/list 和 tools/call 返回格式正确
  ```

### P3.2 工具输入校验

**新建文件**：`internal/mcp/validator.go`

- [ ] **P3.2.1** 实现 `ValidateInput(schema types.ToolSchema, input map[string]any) error`
  - 检查 `Required` 字段是否都存在
  - 检查每个字段的 `Type` 是否匹配（string/integer/boolean/array/object）
  - 检查 `Enum` 约束
  - 在 `Registry.Call` 中调用，校验失败返回 `ErrInvalidParams`

### P3.3 内置工具生产化

**修改文件**：`internal/mcp/tools/tools.go`

- [ ] **P3.3.1** `code_exec`：构造函数接收 `*sandbox.Pool`，Execute 时调用 `pool.Execute`
- [ ] **P3.3.2** `file_reader`：
  - 接收 `basePath string`（workspace 根目录）
  - 路径校验：`filepath.Clean` → `filepath.Rel` → 确保不逃逸出 basePath
  - 文件大小限制：超过 1MB 返回错误
  - 读取文件内容返回字符串
- [ ] **P3.3.3** `web_search`：
  - 使用 Tavily API（`go get github.com/nichenqin/tavily-go@latest`，或直接 HTTP 调用）
  - API Key 从 `config.yaml` 的 `tools.web_search.api_key` 读取
  - 如果无 API Key，返回 stub 结果 + 警告日志
- [ ] **P3.3.4** `sql_query`：
  - 使用 `database/sql` + `github.com/lib/pq@v1.10.9`（PostgreSQL 驱动）
  - 只允许 `SELECT` 开头的 SQL（大小写不敏感）
  - 事务隔离级别 `ReadOnly`
  - 查询超时 10s
  - DSN 从 `config.yaml` 的 `tools.sql_query.dsn` 读取

### P3.4 测试

**新建文件**：`internal/mcp/server_test.go`, `internal/mcp/validator_test.go`

- [ ] **P3.4.1** `TestServer_Initialize`：发送 `initialize` 请求 → 返回 server info + capabilities
- [ ] **P3.4.2** `TestServer_ListTools`：注册 2 个工具 → `tools/list` → 返回 2 个
- [ ] **P3.4.3** `TestServer_CallTool_Success`：调用已注册工具 → 返回 content
- [ ] **P3.4.4** `TestServer_CallTool_NotFound`：调用未注册工具 → 返回 -32000 错误
- [ ] **P3.4.5** `TestServer_CallTool_RateLimited`：注册限流工具（1/min）→ 连续调用 2 次 → 第 2 次返回 -32001
- [ ] **P3.4.6** `TestServer_BatchRequest`：发送 2 个请求的 batch → 返回 2 个响应
- [ ] **P3.4.7** `TestValidator_RequiredFields`：缺少必填字段 → 返回错误
- [ ] **P3.4.8** `TestValidator_TypeMismatch`：字段类型不匹配 → 返回错误

**验收命令**：
```bash
go test ./internal/mcp/... -v -count=1
# 期望：≥ 8 个测试 PASS
```

---

## P4: vLLM/SGLang 对接

### P4.1 LLM Client

**新建目录 + 文件**：`internal/llm/client.go`

```bash
mkdir -p /Users/xioshark/Desktop/career/agent-exec-engine/internal/llm/prompts
```

**不需要新增依赖**——使用 `net/http` + `encoding/json` 直接调用 OpenAI-compatible API。

**具体任务**：
- [ ] **P4.1.1** 定义 LLM 请求/响应类型（`internal/llm/types.go`）：
  ```go
  type ChatRequest struct {
      Model    string    `json:"model"`
      Messages []Message `json:"messages"`
      Tools    []Tool    `json:"tools,omitempty"`
      MaxTokens int     `json:"max_tokens,omitempty"`
      Stream    bool     `json:"stream,omitempty"`
  }
  type Message struct { Role string; Content string; ToolCalls []ToolCall }
  type ToolCall struct { ID string; Type string; Function FunctionCall }
  type FunctionCall struct { Name string; Arguments string }
  type ChatResponse struct { Choices []Choice; Usage Usage }
  type Choice struct { Message Message; FinishReason string }
  type Usage struct { PromptTokens int; CompletionTokens int; TotalTokens int }
  ```
- [ ] **P4.1.2** 实现 `Client` struct（`internal/llm/client.go`）：
  - 构造函数：`NewClient(baseURL, model, apiKey string, timeout time.Duration) *Client`
  - `Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)` 方法
  - 内部用 `http.Client` + `json.Marshal` → POST `{baseURL}/chat/completions`
  - 设置 `Authorization: Bearer {apiKey}` header
  - 超时用 `context.WithTimeout`
  - 重试：3 次，指数退避 `100ms → 200ms → 400ms`，只重试 5xx 和网络错误
- [ ] **P4.1.3** 实现流式响应（`internal/llm/stream.go`）：
  - `ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error)`
  - 解析 SSE 格式：`data: {...}\n\n`
  - 遇到 `data: [DONE]` 关闭 channel

### P4.2 LLM Step Executor

**新建文件**：`internal/llm/executor.go`

实现 `dag.StepExecutor` 接口。

- [ ] **P4.2.1** 实现 `LLMStepExecutor`：
  ```go
  type LLMStepExecutor struct {
      client   *Client
      registry *mcp.Registry  // 用于处理 tool_use
      tracer   *observability.Tracer
      metrics  *observability.Metrics
  }
  ```
- [ ] **P4.2.2** `Execute` 方法核心逻辑：
  1. 从 `step.Config["system_prompt"]` 读取系统提示词
  2. 从 `input` 中获取前序步骤输出，拼接为 user message
  3. 将 `registry.List()` 转换为 OpenAI tools 格式传入
  4. 调用 `client.Chat()`
  5. 如果 `FinishReason == "tool_calls"`：
     - 遍历 `ToolCalls` → 调用 `registry.Call()` → 收集 `ToolResult`
     - 将 tool_result 追加到 messages → 再次调用 `client.Chat()`
     - 循环直到 `FinishReason != "tool_calls"` 或达到 10 轮上限
  6. 返回最终 assistant message 的 content
  7. 上报 `metrics.TokensUsed` 和 `metrics.ToolCallsTotal`
- [ ] **P4.2.3** 实现 `ToolStepExecutor`（用于 DAG 中纯工具调用步骤）：
  ```go
  func (e *ToolStepExecutor) Execute(ctx context.Context, step *types.Step, input map[string]any) (string, error) {
      toolName := step.ToolName
      result := e.registry.Call(ctx, types.ToolCall{ID: uuid.New().String(), ToolName: toolName, Input: input})
      if result.IsError { return "", fmt.Errorf(result.Content) }
      return result.Content, nil
  }
  ```

### P4.3 Prompt 管理

**新建文件**：`internal/llm/prompts/templates.go`

- [ ] **P4.3.1** 定义 3 个角色模板：
  ```go
  const PlannerPrompt = `You are a planning agent. Analyze the task and break it into steps...`
  const CoderPrompt = `You are a coding agent. Use the provided tools to implement the plan...`
  const ReviewerPrompt = `You are a code reviewer. Analyze the changes and provide feedback...`
  ```
- [ ] **P4.3.2** 实现模板渲染：`RenderPrompt(template string, data map[string]any) string`
  - 使用 `text/template` 标准库
  - 支持注入：`{{.WorkflowName}}`, `{{.StepID}}`, `{{.PreviousOutput}}`

### P4.4 测试

- [ ] **P4.4.1** `TestLLMClient_Chat_Mock`（`internal/llm/client_test.go`）：
  - 启动 `httptest.NewServer` 模拟 vLLM 响应
  - 验证请求格式正确，响应解析正确
- [ ] **P4.4.2** `TestLLMStepExecutor_ToolUseLoop`：
  - Mock LLM 返回 tool_calls → mock tool 返回结果 → LLM 返回 final answer
  - 验证 tool 被调用，最终输出正确
- [ ] **P4.4.3** `TestLLMStepExecutor_MaxRoundsLimit`：
  - Mock LLM 永远返回 tool_calls → 验证在 10 轮后停止
- [ ] **P4.4.4** 集成测试（需要真实 vLLM，标记 `//go:build vllm`）：
  ```bash
  # 在 A100 服务器上：
  # 1. 启动 vLLM
  python -m vllm.entrypoints.openai.api_server --model Qwen/Qwen2.5-7B-Instruct --port 8000
  # 2. 设置环境变量
  export LLM_BASE_URL=http://localhost:8000/v1
  export LLM_API_KEY=dummy
  # 3. 运行测试
  go test ./internal/llm/ -v -tags=vllm -count=1 -timeout=120s
  ```

**验收命令**：
```bash
go test ./internal/llm/... -v -count=1  # mock 测试
# 期望：≥ 4 个测试 PASS
```

---

## P5: AI Infra Platform 集成

### P5.1 推理网关对接

**前置条件**：AI Infra Platform 项目（`/Users/xioshark/Desktop/career/滕彦翕/项目/ai-infra-platform-push/`）的推理网关在本地或远程可达。

**修改文件**：`configs/config.yaml`, `internal/llm/client.go`

- [ ] **P5.1.1** 在 `config.yaml` 中添加：
  ```yaml
  infra:
    gateway_url: "http://localhost:8081"  # AI Infra Platform 推理网关地址
    scheduler_url: "http://localhost:8082"  # AI Infra Platform scheduler 地址
  ```
- [ ] **P5.1.2** 在 `Config` struct 中添加 `Infra InfraConfig`
- [ ] **P5.1.3** LLM Client 的 `baseURL` 默认使用 `config.Infra.GatewayURL + "/v1"`
  - 推理网关已实现 OpenAI-compatible 接口，无需额外适配
- [ ] **P5.1.4** 利用网关已有的 per-model 限流和健康探针
  - Agent Exec Engine 侧不再重复做 LLM 限流

### P5.2 GPU 调度联动

**新建文件**：`internal/infra/scheduler_client.go`

- [ ] **P5.2.1** 实现 HTTP 客户端调用 AI Infra Platform 的 Scheduler API：
  - `RequestGPU(ctx, taskID string, gpuCount int, memoryMin int64) error`
  - `ReleaseGPU(ctx, taskID string) error`
  - 对应 AI Infra Platform 的 `/api/v1/tasks` 接口
- [ ] **P5.2.2** 在 `LLMStepExecutor.Execute` 开始前调用 `RequestGPU`，结束后调用 `ReleaseGPU`

### P5.3 部署集成

**修改文件**：`deployments/docker-compose.yaml`

- [ ] **P5.3.1** 将 agent-exec-engine 加入 AI Infra Platform 的 docker-compose 网络
- [ ] **P5.3.2** 共享 Redis 实例（使用同一个 Redis service）
- [ ] **P5.3.3** K8s manifest（`deployments/k8s/`）更新：
  - `deployment.yaml`：agent-exec-engine Deployment，1 副本
  - `service.yaml`：ClusterIP Service，端口 8080
  - 与 AI Infra Platform 同 namespace

**验收**：
```bash
# 本地验证
docker compose -f deployments/docker-compose.yaml up -d
curl http://localhost:8080/healthz
# 期望：{"status":"ok"}

# 通过推理网关验证 LLM 调用
curl http://localhost:8080/api/v1/workflows \
  -H 'Content-Type: application/json' \
  -d '{"name":"test","steps":[{"id":"a","type":"llm_call","config":{"system_prompt":"Say hello"}}]}'
```

---

## P6: 可观测性

### P6.1 OTLP Trace 接入

**新增依赖**：
```bash
go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc@v1.32.0
go get go.opentelemetry.io/otel/sdk@v1.32.0
go get google.golang.org/grpc@v1.68.0
```

**修改文件**：`internal/observability/tracer.go`, `cmd/server/main.go`

- [ ] **P6.1.1** 在 `NewTracer` 中配置真实 OTLP gRPC Exporter：
  ```go
  exporter, _ := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(otlpEndpoint), otlptracegrpc.WithInsecure())
  tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter), sdktrace.WithResource(resource))
  otel.SetTracerProvider(tp)
  ```
- [ ] **P6.1.2** 在 `main.go` 中初始化 TracerProvider，defer `tp.Shutdown(ctx)`
- [ ] **P6.1.3** 在 Scheduler、MCP Server、Sandbox Executor 中埋入 Span 调用
  - Scheduler.Run → `StartWorkflowSpan`
  - Scheduler.executeStep → `StartStepSpan`
  - Registry.Call → `StartToolSpan`
  - Executor.Execute → `StartSandboxSpan`

### P6.2 Metrics 埋点

**修改文件**：`internal/dag/scheduler.go`, `internal/mcp/server.go`, `internal/sandbox/executor.go`

- [ ] **P6.2.1** Scheduler 埋点：
  - `executeStep` 开始/结束时记录 `StepDuration`
  - 步骤重试时增加 `StepRetries`
  - 工作流完成/失败时增加 `WorkflowsTotal`
- [ ] **P6.2.2** MCP Server 埋点：
  - `handleCallTool` 记录 `ToolCallDuration` 和 `ToolCallsTotal`
  - 限流拒绝时增加 `RateLimitRejected`
- [ ] **P6.2.3** Sandbox 埋点：
  - 容器创建时增加 `SandboxCreated`
  - OOM 时增加 `SandboxOOM`
  - 超时时增加 `SandboxTimeout`
  - 执行时间记录 `SandboxDuration`

### P6.3 Grafana Dashboard

**新建文件**：`configs/grafana/agent-exec.json`

- [ ] **P6.3.1** 使用 Grafana Dashboard JSON 格式，包含 6 个面板：
  - 工作流概览（`agent_exec_workflows_total`, `agent_exec_workflow_duration_seconds`）
  - 步骤明细（`agent_exec_step_duration_seconds`）
  - 工具调用（`agent_exec_tool_calls_total`, `agent_exec_tool_call_duration_seconds`）
  - 沙箱（`agent_exec_sandbox_*`）
  - Token 消耗（`agent_exec_tokens_used_total`）
  - Checkpoint（`agent_exec_checkpoint_*`）
- [ ] **P6.3.2** 在 `deployments/docker-compose.yaml` 中挂载 Dashboard JSON 到 Grafana：
  ```yaml
  grafana:
    volumes:
      - ../configs/grafana:/var/lib/grafana/dashboards
  ```

### P6.4 日志关联 Trace

**修改文件**：`internal/observability/logger.go`

- [ ] **P6.4.1** 实现 `WithContext` 方法的真实逻辑：
  ```go
  spanCtx := trace.SpanFromContext(ctx).SpanContext()
  return &Logger{Logger: l.With(
      zap.String("trace_id", spanCtx.TraceID().String()),
      zap.String("span_id", spanCtx.SpanID().String()),
  )}
  ```

**验收命令**：
```bash
# 启动全套服务
docker compose -f deployments/docker-compose.yaml up -d

# 触发一个工作流
curl -X POST http://localhost:8080/api/v1/workflows ...

# 验证 Prometheus
curl http://localhost:8080/metrics | grep agent_exec
# 期望：看到 agent_exec_* 指标

# 验证 Grafana
# 浏览器打开 http://localhost:3000 → 导入 Dashboard → 验证面板有数据

# 验证 Trace（如果有 Jaeger/Tempo）
# 浏览器打开 Jaeger UI → 搜索 service=agent-exec-engine → 验证 Span 嵌套
```

---

## P7: API + 文档 + 收尾

### P7.1 REST API

**新增依赖**：
```bash
go get github.com/gin-gonic/gin@v1.10.0
```

**新建/修改文件**：`internal/api/handler.go`, `internal/api/router.go`, `internal/api/middleware.go`

- [ ] **P7.1.1** 实现路由注册（`internal/api/router.go`）：
  ```go
  func SetupRouter(sched *dag.Scheduler, registry *mcp.Registry, metrics *observability.Metrics) *gin.Engine {
      r := gin.New()
      r.Use(gin.Recovery(), RequestIDMiddleware(), LoggerMiddleware())
      v1 := r.Group("/api/v1")
      {
          v1.POST("/workflows", CreateWorkflow)
          v1.GET("/workflows/:id", GetWorkflow)
          v1.POST("/workflows/:id/resume", ResumeWorkflow)
          v1.DELETE("/workflows/:id", CancelWorkflow)
          v1.GET("/workflows/:id/steps", ListSteps)
          v1.GET("/workflows/:id/steps/:step_id", GetStep)
          v1.POST("/tools", RegisterTool)
          v1.GET("/tools", ListTools)
          v1.DELETE("/tools/:name", UnregisterTool)
      }
      return r
  }
  ```
- [ ] **P7.1.2** 实现每个 handler 的请求/响应结构体 + 逻辑
- [ ] **P7.1.3** 统一错误响应格式：
  ```go
  type ErrorResponse struct {
      Code    int    `json:"code"`
      Message string `json:"message"`
      Details string `json:"details,omitempty"`
  }
  ```
- [ ] **P7.1.4** 更新 `cmd/server/main.go`：使用 gin Router 替换 `http.NewServeMux`

### P7.2 集成测试

**新建文件**：`internal/api/handler_test.go`

- [ ] **P7.2.1** 使用 `httptest.NewServer` + gin 的 `TestMode`
- [ ] **P7.2.2** 端到端测试：创建 → 执行 → 查询 → 验证
- [ ] **P7.2.3** 负载测试：`go test -bench=BenchmarkWorkflowCreate -benchtime=10s`

### P7.3 文档

- [ ] **P7.3.1** `docs/architecture.md`：含 ASCII 架构图 + 模块职责 + 数据流
- [ ] **P7.3.2** `docs/api.md`：每个 endpoint 的请求/响应示例 + curl 命令
- [ ] **P7.3.3** `docs/deployment.md`：Docker + K8s 部署步骤
- [ ] **P7.3.4** 更新 `README.md`：真实运行截图 + 性能数据

### P7.4 简历同步

- [ ] **P7.4.1** 在 `/Users/xioshark/Desktop/career/滕彦翕/简历/yaml/` 下新建或更新 Agent Infra 方向简历 YAML
- [ ] **P7.4.2** 更新 `/Users/xioshark/Desktop/career/外延资料/项目导航与作证/导航文档/简历与仓库证据导航.md`
- [ ] **P7.4.3** 截图证据：Grafana Dashboard、API 响应、测试通过截图

**P7 总验收**：
```bash
go build ./...
go test ./... -v -race -count=1
# 期望：全部 PASS，≥ 50 个测试

# API 验证
curl http://localhost:8080/api/v1/tools | jq .
curl http://localhost:8080/healthz | jq .
curl http://localhost:8080/metrics | head -20
```

---

## 依赖关系

```
P0 (DONE) ──→ P1 ──→ P2 ──→ P3 ──→ P4 ──→ P5
                │      │      │      │      │
                │      │      │      │      └──→ P6 ──→ P7
                │      │      │      │
                │      │      │      └──────┴─── P3 和 P4 可部分并行
                │      │      │
                │      └──────┴─── P1 和 P2 的后半可部分并行
                │
                └─── P1 必须先完成（DAG 是所有模块的基础）
```

**并行执行建议**：
- P1.1-P1.4 顺序执行，P1.5 可与 P2.1 并行
- P3.1-P3.3 可与 P2.3-P2.4 并行（MCP 不依赖 Docker）
- P4 和 P3 可并行（LLM Client 不依赖 MCP Server 代码，只共享 Registry 接口）
- P6 可在 P4 完成后立即开始（不依赖 P5）

## 验收标准

| 阶段 | 验收命令 | 期望结果 |
|------|---------|---------|
| P1 | `go test ./internal/dag/ -v -race -count=1` | ≥ 20 tests PASS, 0 race |
| P2 | `go test ./internal/sandbox/ -v -tags=docker -count=1` | 8 tests PASS (需 Docker) |
| P3 | `go test ./internal/mcp/... -v -count=1` | ≥ 8 tests PASS |
| P4 | `go test ./internal/llm/... -v -count=1` | ≥ 4 tests PASS (mock) |
| P5 | `curl http://localhost:8080/healthz` | `{"status":"ok"}` |
| P6 | `curl http://localhost:8080/metrics \| grep agent_exec` | 看到 metrics |
| P7 | `go test ./... -v -race -count=1` | ≥ 50 tests PASS |

## 新增依赖清单（按阶段）

| 阶段 | 包 | 版本 | 命令 |
|------|-----|------|------|
| P1 | `github.com/google/cel-go` | v0.21.0 | `go get github.com/google/cel-go@v0.21.0` |
| P1 | `golang.org/x/sync` | v0.10.0 | `go get golang.org/x/sync@v0.10.0` |
| P2 | `github.com/docker/docker` | v27.0.0 | `go get github.com/docker/docker@v27.0.0+incompatible` |
| P2 | `github.com/docker/go-connections` | v0.5.0 | `go get github.com/docker/go-connections@v0.5.0` |
| P2 | `github.com/opencontainers/image-spec` | v1.1.0 | `go get github.com/opencontainers/image-spec@v1.1.0` |
| P3 | `github.com/lib/pq` | v1.10.9 | `go get github.com/lib/pq@v1.10.9` |
| P6 | `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc` | v1.32.0 | `go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc@v1.32.0` |
| P6 | `go.opentelemetry.io/otel/sdk` | v1.32.0 | `go get go.opentelemetry.io/otel/sdk@v1.32.0` |
| P6 | `google.golang.org/grpc` | v1.68.0 | `go get google.golang.org/grpc@v1.68.0` |
| P7 | `github.com/gin-gonic/gin` | v1.10.0 | `go get github.com/gin-gonic/gin@v1.10.0` |

## 风险清单

| 风险 | 影响 | 缓解措施 |
|------|------|---------|
| 本地无 GPU 跑不了 vLLM | P4 集成测试阻塞 | mock 测试先行，GPU 测试在 A100 服务器执行 |
| Docker SDK macOS 权限 | P2 阻塞 | 确认 Docker Desktop 运行，`docker ps` 可用 |
| AI Infra Platform API 变化 | P5 阻塞 | 先用 HTTP API 松耦合，封装 client 层 |
| MCP 协议版本更新 | P3 需改 | 锁定 `2024-11-05` 版本 |
| cel-go 编译慢 | P1 构建时间增加 | 可接受，cel-go 是成熟库 |
| PostgreSQL 依赖 | P3.3 sql_query 需要 PG | 可选实现，无 PG 时返回 stub |
