# Agent Execution Engine — Roadmap

> 完整实施工单见 [`docs/roadmap/full-implementation-plan.md`](docs/roadmap/full-implementation-plan.md)。
> 本文件只保留阶段总览和当前状态。

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

## 当前已落地

- **CI**: `lint + redis-backed tests + build + demo-smoke`
- **测试**: `go test ./...` 全部通过（config 4 + dag 4 + llm + mcp + api）
- **证据**: `evidence/runtime/` 包含真实 workflow 运行结果、Prometheus metrics、Jaeger traces
- **真机**: `evidence/a100/` 包含 vLLM 真机推理证据

## 依赖关系

```
P0 (DONE) ──→ P1 ──→ P2 ──→ P3 ──→ P4 ──→ P5
                │      │      │      │      │
                │      │      │      │      └──→ P6 ──→ P7 (DONE)
                │      │      │      │
                └──────┴──────┴──────┴── 部分可并行，详见完整计划
```

## 验收标准

| 阶段 | 验收命令 | 期望结果 |
|------|---------|---------| 
| P1 | `go test ./internal/dag/ -v -race -count=1` | ≥ 20 tests PASS |
| P2 | `go test ./internal/sandbox/ -v -tags=docker` | 8 tests PASS |
| P3 | `go test ./internal/mcp/... -v -count=1` | ≥ 8 tests PASS |
| P4 | `go test ./internal/llm/... -v -count=1` | ≥ 4 tests PASS |
| P5 | `curl http://localhost:8080/healthz` | `{"status":"ok"}` |
| P6 | `curl http://localhost:8080/metrics \| grep agent_exec` | 可见 metrics |
| P7 | `go test ./... -v -race -count=1` | ≥ 50 tests PASS |
