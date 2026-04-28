# Reviewer Quickstart

给第一次看这个仓库的 reviewer 一条最短路径：先确认“主链路能跑”，再确认“证据真实存在”，最后再决定要不要深入 894 行的 `PLAN.md`。

## 30 秒了解

- 这是一个生产级 LLM Agent 工作流编排引擎，主线能力是 DAG 调度、ReAct、MCP、Docker 沙箱和可观测性。
- 如果你只想判断“是不是作品集包装”，先看 demo、runtime evidence 和 A100 evidence，不要先读 `PLAN.md`。
- `PLAN.md` 更像完整实施工单，适合在确认项目值得深看后再展开。

## 5 分钟验证路径

### 0. 查看最新 CI 证据（最快方式）

在 GitHub Actions 的 **Demo Smoke** workflow 中查看最近一次运行结果和 `demo-smoke-evidence` artifact，无需本地环境即可确认主链路可跑。

### 1. 跑固定 demo workflow

```bash
docker compose -f deployments/docker-compose.yaml up -d
make run
make demo-workflow
```

预期：
- `GET /healthz` 返回 `{"status":"ok","version":"0.1.0"}`
- `POST /api/v1/workflows` 返回新的 `workflow_id`
- `GET /api/v1/workflows/:id` 最终返回 `status=completed`

固定输入：
- [`../examples/obs-metrics-workflow.json`](../examples/obs-metrics-workflow.json)

固定脚本：
- [`../scripts/run_demo_workflow.sh`](../scripts/run_demo_workflow.sh)

可选的跨项目延伸路径：
- [`cross-project-demo.md`](cross-project-demo.md)
- [`../examples/cross-project-workflow.json`](../examples/cross-project-workflow.json)
- [`../scripts/demo-cross-project.sh`](../scripts/demo-cross-project.sh)

### 2. 看真实运行证据

- [`runtime-evidence.md`](runtime-evidence.md)
- [`../evidence/runtime/workflow-run.json`](../evidence/runtime/workflow-run.json)
- [`../evidence/runtime/metrics.txt`](../evidence/runtime/metrics.txt)
- [`../evidence/runtime/jaeger-traces.json`](../evidence/runtime/jaeger-traces.json)

这里能回答三个问题：
- 工作流有没有真的跑完
- 指标和 trace 有没有真的落下来
- demo 输出和文档是不是一致

### 3. 看真机推理证据

- [`../evidence/a100/vllm-test.txt`](../evidence/a100/vllm-test.txt)
- [`../evidence/a100/vllm-models.json`](../evidence/a100/vllm-models.json)
- [`../evidence/a100/vllm-chat-smoke.json`](../evidence/a100/vllm-chat-smoke.json)

这里回答的是：这个仓库是不是只在本地 mock 过，还是确实对过真实模型服务。

## 深挖顺序

如果 5 分钟验证通过，再按这个顺序看：

1. [`system-map.md`](system-map.md) — 先看"主服务是谁、外部依赖是谁"
2. [`contract-table.md`](contract-table.md) — 再看跨项目接口契约
3. [`architecture.md`](architecture.md) — 再看内部模块架构
4. [`backend-resume-evidence.md`](backend-resume-evidence.md) — 再看简历 claim 映射
5. [`api.md`](api.md)
6. [`deployment.md`](deployment.md)
7. [`../PLAN.md`](../PLAN.md)

建议不要反过来。一上来读 `PLAN.md` 会先看到实施细节和 backlog，而不是当前已经落地的主链路。
