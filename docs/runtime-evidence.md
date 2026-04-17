# Runtime Evidence

## P3 MCP Inspector

- 计划中的包名 `@anthropics/mcp-inspector` / `@anthropic-ai/mcp-inspector` 已失效，实际可用的是 `@modelcontextprotocol/inspector`
- 手测命令：

```bash
npx @modelcontextprotocol/inspector go run ./cmd/server --stdio
```

- 通过 Inspector proxy 完成的实际请求：

```bash
curl -X POST \
  'http://localhost:6277/mcp?transportType=stdio&command=go&args=run%20./cmd/server%20--stdio' \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -H 'X-MCP-Proxy-Auth: Bearer <token>' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"curl","version":"1.0"}}}'
```

- 返回包含：
  - `protocolVersion: 2024-11-05`
  - `serverInfo.name: agent-exec-engine`
  - `capabilities.tools.listChanged: true`
- 同一 session 下继续验证：
  - `tools/list` 返回 `code_exec`、`web_search`、`file_reader`、`sql_query`、`rag_search`
  - `tools/call(name=file_reader, path=README.md)` 成功返回文件内容

## P4 A100 / vLLM 真机验收

- 2026-03-31 已在云服务器 `NVIDIA A100-SXM4-80GB` 上补齐计划要求的真实 `-tags=vllm` 集成测试
- 运行环境：
  - Python `3.11.14`
  - Go `1.25.0`
  - `torch 2.10.0+cu128`
  - `transformers 4.57.6`
  - `vllm 0.18.0`
  - `triton 3.6.0`
- 启动命令：

```bash
python3.11 -m vllm.entrypoints.openai.api_server \
  --model /infra/data/models/models/Qwen2.5-7B-Instruct \
  --host 0.0.0.0 \
  --port 8000
```

- Smoke test：

```bash
curl -s http://127.0.0.1:8000/v1/models
curl -s http://127.0.0.1:8000/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"/infra/data/models/models/Qwen2.5-7B-Instruct","messages":[{"role":"user","content":"hello"}],"max_tokens":16}'
```

- 测试命令：

```bash
export PATH=/usr/local/go/bin:$PATH
export LLM_BASE_URL=http://127.0.0.1:8000/v1
export LLM_API_KEY=dummy
go test ./internal/llm -v -tags=vllm -count=1 -timeout=120s
```

- 实际结果：
  - `/v1/models` 正常返回模型列表
  - `/v1/chat/completions` 正常返回 assistant 输出
  - `go test ./internal/llm -v -tags=vllm -count=1 -timeout=120s` 通过
  - 最新输出：`ok github.com/Xio-Shark/agent-exec-engine/internal/llm 0.310s`

- 证据文件：
  - [`../evidence/a100/nvidia-smi.txt`](../evidence/a100/nvidia-smi.txt)
  - [`../evidence/a100/go-version.txt`](../evidence/a100/go-version.txt)
  - [`../evidence/a100/python-packages.txt`](../evidence/a100/python-packages.txt)
  - [`../evidence/a100/vllm-models.json`](../evidence/a100/vllm-models.json)
  - [`../evidence/a100/vllm-chat-smoke.json`](../evidence/a100/vllm-chat-smoke.json)
  - [`../evidence/a100/vllm-test.txt`](../evidence/a100/vllm-test.txt)

## P5 GPU 释放闭环

- `agent-exec-engine` 的 `ReleaseGPU` 已从不存在的 release endpoint 切到 AI Infra 当前实际可用的 `POST /jobs/{id}/cancel`
- 对端仓 `ai-job-orchestrator` 同步补上了 scheduled job cancel 时的 GPU 释放逻辑
- 相关验证：

```bash
go test ./internal/infra -run 'TestSchedulerClient_ReleaseGPU' -count=1
```

## P6 Docker / Grafana / Trace

- 启动命令：

```bash
docker network inspect ai-job-orchestrator_default >/dev/null 2>&1 || docker network create ai-job-orchestrator_default
docker compose -f deployments/docker-compose.yaml up -d
```

- 实际运行结果：
  - `agent-exec-engine`、`redis`、`prometheus`、`grafana`、`jaeger` 全部处于 `Up`
  - `GET http://localhost:8080/healthz` 返回 `{"status":"ok","version":"0.1.0"}`
  - `GET http://localhost:3000/api/health` 返回 Grafana `database: ok`
  - `GET http://localhost:9090/api/v1/targets` 显示 `agent-exec-engine` target health=`up`
  - `GET http://localhost:16686/api/services` 返回 `agent-exec-engine`

- 触发 workflow：

```bash
curl -X POST http://localhost:8080/api/v1/workflows \
  -H 'Content-Type: application/json' \
  -d '{"name":"obs-metrics","steps":[{"id":"plan","type":"llm_call"},{"id":"exec","type":"tool_call","depends_on":["plan"]}]}'
```

- 指标证据：
  - `agent_exec_step_duration_seconds{step_type="llm_call",status="success"}`
  - `agent_exec_step_duration_seconds{step_type="tool_call",status="success"}`
  - `agent_exec_workflow_duration_seconds{workflow_name="obs-metrics"}`
  - `agent_exec_workflows_total{status="completed"} 1`

- Trace 证据：
  - Jaeger trace `b376f00835489947038881031ddb7ec5`
  - operation 树包含 `workflow.execute` 与两个 `step.execute`

## P7 Benchmark / 截图 / 文档

- Benchmark：

```bash
go test -run '^$' -bench BenchmarkWorkflowCreate -benchtime=2s ./internal/api
```

- 最近一次结果：

```text
BenchmarkWorkflowCreate-10    	  260830	     11194 ns/op	   15620 B/op	     125 allocs/op
```

- 原始文件：
  - [`../evidence/runtime/benchmark.txt`](../evidence/runtime/benchmark.txt)
  - [`../evidence/runtime/workflow-run.json`](../evidence/runtime/workflow-run.json)
  - [`../evidence/runtime/metrics.txt`](../evidence/runtime/metrics.txt)
  - [`../evidence/runtime/jaeger-traces.json`](../evidence/runtime/jaeger-traces.json)

- 截图文件：
  - [`../evidence/screenshots/grafana-dashboard.png`](../evidence/screenshots/grafana-dashboard.png)
  - [`../evidence/screenshots/jaeger-trace.png`](../evidence/screenshots/jaeger-trace.png)
  - [`../evidence/screenshots/api-response.png`](../evidence/screenshots/api-response.png)
  - [`../evidence/screenshots/benchmark.png`](../evidence/screenshots/benchmark.png)
  - [`../evidence/screenshots/go-test.png`](../evidence/screenshots/go-test.png)
