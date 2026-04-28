# Cross-Project Demo

这份文档服务于 `EXECUTION-LIST.md` 的 P2：把 `agent-exec-engine`、`ai-job-orchestrator` 和 `rag` 串成一条能落地复现的黄金路径，而不是只保留分散证据。

## 目标链路

```text
Agent workflow
  -> react 规划
  -> rag_search 检索
  -> gateway-backed llm_call
  -> code_exec sandbox
  -> human review pause/resume
  -> final report
```

这条链路当前优先证明 5 件事：

1. workflow 确实能同时覆盖 ReAct、tool_call、llm_call、human 四种控制面能力。
2. `llm_call` 默认通过 `AI_INFRA_GATEWAY_URL` 派生的 `/v1` 网关出站。
3. `rag_search` 已经是主仓内置工具，不需要额外写 demo 专用 executor。
4. Docker sandbox、checkpoint、resume 与 trace/metrics 都沿用现有主链路。
5. 现阶段先交付可执行 demo 和入口文档；`RAG audit_id` 归档仍留在下一批联调中补齐。

## 前置条件

### 1. 启动 AI Infra 相关服务

- `ai-job-orchestrator` 需要提供推理网关，默认地址：`http://localhost:8081`
- `agent-exec-engine/deployments/docker-compose.yaml` 已默认接入外部网络 `ai-job-orchestrator_default`

### 2. 启动 agent-exec-engine

至少保证以下环境变量：

```bash
export AI_INFRA_GATEWAY_URL=http://localhost:8081
export QDRANT_URL=http://localhost:6333
go run ./cmd/server
```

说明：

- `AI_INFRA_GATEWAY_URL` 会让 `llm.base_url` 默认派生为 `{gateway_url}/v1`
- `QDRANT_URL` 未配置时，`rag_search` 仍然能返回 stub 文本，适合先验证控制流

## 固定输入

- Workflow: `examples/cross-project-workflow.json`
- Script: `scripts/demo-cross-project.sh`

## 一键运行

```bash
bash scripts/demo-cross-project.sh
```

如果希望把 workflow API 输出落盘：

```bash
OUTPUT_DIR=evidence/cross-project bash scripts/demo-cross-project.sh
```

此命令会自动调用 `scripts/collect-cross-project-evidence.sh`，额外收集：
- Gateway 健康状态 (`gateway-health.json`)
- Prometheus 指标 (`agent-metrics.json`, `gateway-metrics.json`)
- Jaeger trace (`jaeger-trace.json`, `trace-id.txt`)
- AEE 原始 metrics (`aee-metrics-raw.txt`)

完整证据结构和验证清单见 [`../evidence/cross-project/README.md`](../evidence/cross-project/README.md)。

## 你会看到什么

| 阶段 | 观测点 | 说明 |
| --- | --- | --- |
| 工具检查 | `/api/v1/tools` 包含 `rag_search`、`code_exec` | 验证主仓工具注册完整 |
| workflow 创建 | 返回 `workflow_id` 与 `run.id` | 证明 demo 已进入统一 DAG 执行面 |
| human pause | `status=paused` | 证明 checkpoint / human review 进入控制面 |
| resume 后完成 | `status=completed` | 证明断点恢复和后续步骤协同工作 |
| 观测入口 | Prometheus / Jaeger / workflow API | 证明 trace / metrics / runtime API 可回查 |

## 当前边界

- 当前 demo 使用的是 `rag_search` 工具，而不是 `rag` 的 `/v1/qa/ask` 问答接口，因此不会自动产出 `audit_id`。
- 如果要补齐 `audit_id`、RAG 运行记录和 failover 截图，应在下一批把 retrieval step 升级为 `knowledge_qa` 风格的 HTTP tool adapter。
- `ai-job-orchestrator` 的 5xx failover 已有独立证据，当前 demo 先证明 `llm_call -> gateway` 的调用边界，而不是重复造一套 failover 文档。

## 关联证据

- 系统边界: [`system-map.md`](system-map.md)
- 接口契约: [`contract-table.md`](contract-table.md)
- Gateway 5xx failover: `../../ai-job-orchestrator/docs/gateway-failover-evidence.md`
- Agent 可观测性案例: `observability-case-study.md`
- 主线运行证据: `runtime-evidence.md`
- 跨项目证据目录: [`../evidence/cross-project/README.md`](../evidence/cross-project/README.md)
- Reviewer 快速入口: `reviewer-quickstart.md`