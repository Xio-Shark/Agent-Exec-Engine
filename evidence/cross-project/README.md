# Cross-Project Demo Evidence

> 本目录存放跨项目黄金路径 demo 的运行证据。
>
> 对应 `docs/cross-project-demo.md` 和 `EXECUTION-LIST.md` 的 P2 / R2。

---

## 预期证据结构

一次完整的跨项目 demo 运行后，本目录应包含以下文件：

| 文件 | 来源 | 说明 |
|------|------|------|
| `workflow-run.json` | `demo-cross-project.sh` | Workflow 最终状态与元数据 |
| `workflow-steps.json` | `demo-cross-project.sh` | 各步骤执行详情 |
| `gateway-health.json` | `collect-cross-project-evidence.sh` | AI Infra Gateway 健康状态 |
| `agent-metrics.json` | `collect-cross-project-evidence.sh` | Prometheus agent_exec 指标 |
| `gateway-metrics.json` | `collect-cross-project-evidence.sh` | Prometheus gateway 指标 |
| `aee-metrics-raw.txt` | `collect-cross-project-evidence.sh` | AEE `/metrics` 原始输出 |
| `jaeger-trace.json` | `collect-cross-project-evidence.sh` | Jaeger trace 详情（如 trace_id 可提取） |
| `trace-id.txt` | `collect-cross-project-evidence.sh` | 本次运行的 trace_id |

## 收集命令

### 方式 1：一键运行 demo + 自动收集

```bash
cd agent-exec-engine
OUTPUT_DIR=evidence/cross-project/run-$(date +%Y%m%d-%H%M%S) bash scripts/demo-cross-project.sh
```

### 方式 2：手动收集已有 workflow 的证据

```bash
cd agent-exec-engine
./scripts/collect-cross-project-evidence.sh <workflow_id> evidence/cross-project/run-xxx
```

### 方式 3：单独收集外围证据

```bash
# Gateway health
curl -s http://localhost:8081/gateway/health | python3 -m json.tool

# Prometheus metrics
curl -s 'http://localhost:9090/api/v1/query?query=agent_exec_workflows_total'
curl -s 'http://localhost:9090/api/v1/query?query=gateway_backend_unhealthy_total'

# Jaeger trace（替换 <trace_id>）
curl -s http://localhost:16686/api/traces/<trace_id> | python3 -m json.tool

# AEE metrics
curl -s http://localhost:8080/metrics
```

## 当前缺口（下一批补齐）

| 缺口 | 说明 | 计划 |
|------|------|------|
| `RAG audit_id` | 当前 demo 使用 `rag_search` 工具，不产出 `audit_id` | 新增 `knowledge_qa` adapter 调用 RAG 的 `/v1/qa/ask` |
| `gateway failover 日志` | 当前需要手动触发 backend 5xx 才能收集 | 在 `ai-job-orchestrator` 侧增加自动化 failover 测试并落盘 |
| `sandbox artifact` | 当前 sandbox 产物只在 stdout 中，未独立收集 | 增强 sandbox executor 的产物收集路径 |

## 验证检查清单

- [ ] `workflow-run.json` 中 `status=completed`
- [ ] `workflow-steps.json` 中 6 个步骤都有执行记录
- [ ] `gateway-health.json` 中 `healthy_backends >= 1`
- [ ] `agent-metrics.json` 中包含 `agent_exec_workflows_total{status="completed"}`
- [ ] `jaeger-trace.json` 或 `trace-id.txt` 存在（trace 可能因采样率缺失）
