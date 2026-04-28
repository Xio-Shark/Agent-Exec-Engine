# Reliability Evidence: Retry Exhaustion & Step Idempotency

> 对应 `EXECUTION-LIST.md` 的 A3。
> 生成时间：2026-04-24

---

## 1. 重试耗尽后状态落盘

### 测试用例

`internal/dag/scheduler_test.go` — `TestScheduler_RetryExhaustedMarksFailed`

### 验证项

| # | 检查点 | 预期 | 实际 |
|---|--------|------|------|
| 1 | Workflow 最终状态 | `failed` | ✅ `WorkflowFailed` |
| 2 | Step 最终状态 | `failed` | ✅ `StepFailed` |
| 3 | Step 重试次数 | 2（等于 `MaxRetries`） | ✅ `RetryCount == 2` |
| 4 | Checkpoint 持久化失败态 | checkpoint 中 step 为 `failed` | ✅ `cp.StepStates["fragile"].Status == StepFailed` |

### 代码路径

1. `scheduler_run.go:executeStep` — 循环执行 step，失败时调用 `finishStepFailure`
2. `scheduler_state.go:finishStepFailure` — 判断是否重试（`ShouldRetry`），如不重试则返回错误
3. `scheduler_run.go:executeStep` — 当 `shouldRetry == false` 时返回 `failure`
4. `scheduler_run.go:runLoop` — `executeBatch` 返回错误后调用 `failWorkflow`
5. `scheduler_state.go:failWorkflow` — 设置 `WorkflowFailed` + `_ = s.saveCheckpoint()` 保证失败态落盘

### 关键设计决策

- **失败态也要 saveCheckpoint**：`failWorkflow` 中使用 `_ = s.saveCheckpoint()`（best-effort），确保即使 workflow 失败，其状态也能被持久化到 Redis
- **重试次数上限由 RetryPolicy 控制**：`ShouldRetry` 检查 `state.RetryCount < step.Retry.MaxRetries`
- **重试不跨 step 执行**：每个 step 独立管理自己的重试计数器

---

## 2. Step 幂等性

### 测试用例

`internal/dag/scheduler_test.go` — `TestScheduler_StepIdempotency`

### 验证项

| # | 检查点 | 预期 | 实际 |
|---|--------|------|------|
| 1 | Step a 执行次数 | 恰好 1 次 | ✅ `executor.calls["a"] == 1` |
| 2 | Step b 执行次数 | 恰好 1 次 | ✅ `executor.calls["b"] == 1` |

### 代码路径

1. DAG 拓扑排序保证每个 step 只被调度一次
2. `graph.MarkComplete` 在 step 成功后将其从 ready set 移除
3. Step 状态机（`step.go`）禁止从 `StepSuccess` 状态转换到任何其他状态

### 幂等性边界说明

| 场景 | 幂等性 | 说明 |
|------|--------|------|
| step 成功后重复调度 | ✅ 幂等 | DAG 保证不会重复调度 |
| step 失败后重试 | ⚠️ 非幂等 | 重试可能产生副作用（如重复写数据库） |
| workflow cancel 后恢复 | ✅ 幂等 | checkpoint 恢复后只执行 pending step |
| llm_call 重试 | ❌ 非幂等 | LLM 调用可能产生 token 计费，需由调用方保证去重 |
| tool_call 重试 | ❌ 非幂等 | code_exec / web_search 可能产生副作用 |
| rag_search 重试 | ✅ 幂等 | 只读操作 |

---

## 3. 验证命令

```bash
cd agent-exec-engine

# 验证重试耗尽 + checkpoint 持久化
go test ./internal/dag -run 'TestScheduler_RetryExhaustedMarksFailed' -count=1 -v

# 验证 step 幂等性
go test ./internal/dag -run 'TestScheduler_StepIdempotency' -count=1 -v

# 验证 cancel/resume
go test ./internal/dag ./internal/api -run '(Checkpoint|Resume|Cancel|Retry|Human)' -count=1 -v
```

---

## 关联文档

- 系统边界: [`system-map.md`](system-map.md)
- 接口契约: [`contract-table.md`](contract-table.md)
- 沙箱策略: [`sandbox-policy-matrix.md`](sandbox-policy-matrix.md)
- MCP 设计决策: [`mcp-design-decision.md`](mcp-design-decision.md)
