# Deployment Guide

## Prerequisites

- Go 1.25+
- Docker 24+（含 Docker Compose v2）
- Redis 7+（生产环境）

## Local Development

### 1. 安装依赖

```bash
cd /path/to/agent-exec-engine
make deps
```

### 2. 启动基础设施

```bash
docker compose -f deployments/docker-compose.yaml up -d
```

这会启动：
- **Redis** — 端口 6379，Checkpoint 持久化
- **Prometheus** — 端口 9090，指标采集
- **Grafana** — 端口 3000，可视化大盘（admin/admin）
- **Jaeger** — 端口 16686，Trace 查询 UI

### 3. 启动服务

```bash
make run
# 或指定配置文件：
go run ./cmd/server --config configs/config.yaml
```

### 4. 验证

```bash
# 健康检查
curl http://localhost:8080/healthz
# 期望：{"status":"ok","version":"0.1.0"}

# Prometheus metrics
curl http://localhost:8080/metrics | grep agent_exec

# 创建测试工作流
curl -X POST http://localhost:8080/api/v1/workflows \
  -H 'Content-Type: application/json' \
  -d '{"name":"test","steps":[{"id":"a","type":"llm_call"}]}'

# 列出工具
curl http://localhost:8080/api/v1/tools | jq .
```

---

## Docker Deployment

### 构建镜像

```bash
make docker-build
# 或：
docker build -t agent-exec-engine:latest -f deployments/Dockerfile .
```

### 使用 Docker Compose

```bash
docker compose -f deployments/docker-compose.yaml up -d
```

### 环境变量配置

所有配置项都支持环境变量覆盖：

| 环境变量 | 配置项 | 默认值 |
|---------|--------|--------|
| `PORT` | `server.port` | `8080` |
| `REDIS_URL` | `redis.url` | `redis://localhost:6379` |
| `OTLP_ENDPOINT` | `observability.otlp_endpoint` | `localhost:4317` |
| `AI_INFRA_GATEWAY_URL` | `infra.gateway_url` | `http://localhost:8081` |
| `AI_INFRA_SCHEDULER_URL` | `infra.scheduler_url` | `http://localhost:8080` |
| `LLM_BASE_URL` | `llm.base_url` | 从 gateway_url 派生 |
| `LLM_API_KEY` | `llm.api_key` | 空 |
| `TAVILY_API_KEY` | `tools.web_search.api_key` | 空 |
| `DATABASE_URL` | `tools.sql_query.dsn` | 空 |
| `WORKSPACE_ROOT` | `tools.file_reader.base_path` | `.` |

也支持 `AGENT_EXEC_` 前缀（如 `AGENT_EXEC_SERVER_PORT=9090`）。

---

## Kubernetes Deployment

### 1. 创建 Namespace

```bash
kubectl create namespace agent-infra
```

### 2. 部署

```bash
kubectl apply -f deployments/k8s/deployment.yaml -n agent-infra
kubectl apply -f deployments/k8s/service.yaml -n agent-infra
```

### 3. 验证

```bash
kubectl get pods -n agent-infra
kubectl port-forward svc/agent-exec-engine 8080:8080 -n agent-infra
curl http://localhost:8080/healthz
```

### K8s Manifest 说明

**deployment.yaml:**
- 1 副本（可按需 scale）
- 资源限制：256Mi / 500m CPU
- 健康检查：/healthz
- 环境变量注入

**service.yaml:**
- ClusterIP Service
- 端口 8080
- 与 AI Infra Platform 同 namespace

---

## AI Infra Platform 集成

### 网络配置

Agent Exec Engine 通过 Docker 网络或 K8s 同 namespace 与 AI Infra Platform 通信：

```yaml
# docker-compose.yaml 已配置外部网络：
networks:
  default:
    external: true
    name: ai-job-orchestrator_default
```

### 推理网关

- `infra.gateway_url` 指向 AI Infra 推理网关
- LLM Client 的 `base_url` 默认为 `{gateway_url}/v1`
- 推理网关已实现 OpenAI-compatible 接口，无需额外适配

### GPU 调度

- `infra.scheduler_url` 指向 AI Infra API Server
- LLMStepExecutor 在执行前自动 `RequestGPU`，执行后通过 `POST /jobs/{id}/cancel` 释放预约

---

## Observability

### Prometheus

抓取配置位于 `deployments/prometheus.yml`：

```yaml
scrape_configs:
  - job_name: 'agent-exec-engine'
    static_configs:
      - targets: ['agent-exec-engine:8080']
    metrics_path: '/metrics'
```

### Grafana Dashboard

Dashboard JSON 位于 `configs/grafana/agent-exec.json`，包含 6 个面板：
1. 工作流概览（成功/失败计数 + 耗时分布）
2. 步骤明细（per-type 耗时）
3. 工具调用（调用量 + 耗时 + 错误率）
4. 沙箱监控（创建/OOM/超时）
5. Token 消耗（per-step 追踪）
6. Checkpoint（保存/加载频次）

启动后访问 `http://localhost:3000`，Dashboard 自动导入。

### Trace (OTLP / Jaeger)

配置 OTLP endpoint 后，所有 Span 自动上报：
- `agent-exec.workflow.run` — 工作流级 Span
- `agent-exec.step.execute` — 步骤级 Span
- `agent-exec.tool.call` — 工具调用级 Span
- `agent-exec.sandbox.execute` — 沙箱执行级 Span

本地 compose 默认把 `OTLP_ENDPOINT` 指向 `jaeger:4317`，可直接在 `http://localhost:16686` 查询 `agent-exec-engine` 服务的 trace。

---

## MCP Stdio Mode

除 HTTP 外，MCP Server 支持 stdio 传输：

```bash
go run ./cmd/server --stdio
```

配合 MCP Inspector 测试：

```bash
npx @modelcontextprotocol/inspector --cli -- go run ./cmd/server --stdio
```

---

## Production Checklist

- [ ] Redis 高可用（Sentinel 或 Cluster）
- [ ] OTLP endpoint 配置（Jaeger / Tempo）
- [ ] Docker image 推送到 registry
- [ ] K8s resource limits 按实际负载调整
- [ ] 配置 LLM_API_KEY 和 TAVILY_API_KEY
- [ ] 配置 TLS（反向代理或 ingress）
- [ ] 日志收集（Loki / ELK）
- [ ] 告警规则（Prometheus Alertmanager）
