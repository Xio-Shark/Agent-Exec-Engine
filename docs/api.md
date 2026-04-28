# API Reference

Base URL: `http://localhost:8080`

## Health & Metrics

### GET /healthz

健康检查端点。

**Response:**
```json
{
  "status": "ok",
  "version": "0.1.0"
}
```

`status` 可能的值：
- `ok` — 所有依赖正常
- `degraded` — Redis 不可达，功能降级

```bash
curl http://localhost:8080/healthz
```

---

### GET /metrics

Prometheus metrics 端点。

```bash
curl http://localhost:8080/metrics | grep agent_exec
```

---

## Workflow API

### POST /api/v1/workflows

创建并启动一个工作流。

**Request Body:**
```json
{
  "name": "code-review-agent",
  "steps": [
    {
      "id": "plan",
      "type": "llm_call",
      "config": {"system_prompt": "分析代码变更并制定审查计划"}
    },
    {
      "id": "search",
      "type": "tool_call",
      "tool": "code_exec",
      "depends_on": ["plan"]
    },
    {
      "id": "review",
      "type": "llm_call",
      "depends_on": ["search"],
      "config": {"system_prompt": "根据搜索结果生成审查报告"}
    }
  ],
  "metadata": {
    "owner": "team-infra",
    "priority": "high"
  }
}
```

**Response (201 Created):**
```json
{
  "run": {
    "id": "run-uuid",
    "workflow_id": "wf-uuid",
    "status": "pending",
    "step_states": {
      "plan": {"step_id": "plan", "status": "pending"},
      "search": {"step_id": "search", "status": "pending"},
      "review": {"step_id": "review", "status": "pending"}
    },
    "started_at": "2026-03-31T08:00:00Z"
  }
}
```

**Error (400):**
```json
{
  "code": 400,
  "message": "workflow name is required"
}
```

```bash
curl -X POST http://localhost:8080/api/v1/workflows \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "simple-agent",
    "steps": [
      {"id": "step1", "type": "llm_call", "config": {"system_prompt": "Say hello"}}
    ]
  }'
```

---

### GET /api/v1/workflows/:id

查询工作流运行状态。

**Response (200):**
```json
{
  "run": {
    "id": "run-uuid",
    "workflow_id": "wf-uuid",
    "status": "completed",
    "step_states": {
      "step1": {
        "step_id": "step1",
        "status": "success",
        "output": "{\"step\":\"step1\",\"status\":\"completed\"}",
        "started_at": "2026-03-31T08:00:01Z",
        "completed_at": "2026-03-31T08:00:02Z"
      }
    },
    "started_at": "2026-03-31T08:00:00Z",
    "completed_at": "2026-03-31T08:00:02Z"
  }
}
```

```bash
curl http://localhost:8080/api/v1/workflows/{workflow_id}
```

---

### POST /api/v1/workflows/:id/resume

恢复一个处于 `paused` 状态的工作流（human-in-the-loop 场景）。

**Request Body:**
```json
{
  "step_id": "approval-step",
  "input": {
    "approved": true,
    "comment": "LGTM"
  }
}
```

**Response (200):**
```json
{
  "message": "workflow resumed"
}
```

```bash
curl -X POST http://localhost:8080/api/v1/workflows/{id}/resume \
  -H 'Content-Type: application/json' \
  -d '{"step_id": "approval", "input": {"approved": true}}'
```

---

### DELETE /api/v1/workflows/:id

取消一个运行中的工作流。

**Response (200):**
```json
{
  "message": "workflow cancelled"
}
```

```bash
curl -X DELETE http://localhost:8080/api/v1/workflows/{id}
```

---

## Step API

### GET /api/v1/workflows/:id/steps

列出工作流的所有步骤状态。

**Response (200):**
```json
{
  "steps": {
    "plan": {"step_id": "plan", "status": "success", "output": "..."},
    "search": {"step_id": "search", "status": "running"},
    "review": {"step_id": "review", "status": "pending"}
  }
}
```

```bash
curl http://localhost:8080/api/v1/workflows/{id}/steps
```

---

### GET /api/v1/workflows/:id/steps/:step_id

查询单个步骤的详细状态。

**Response (200):**
```json
{
  "step": {
    "step_id": "plan",
    "status": "success",
    "started_at": "2026-03-31T08:00:01Z",
    "completed_at": "2026-03-31T08:00:03Z",
    "output": "{...}",
    "retry_count": 0,
    "tokens_used": 1234
  }
}
```

```bash
curl http://localhost:8080/api/v1/workflows/{id}/steps/{step_id}
```

---

## Tool API

### POST /api/v1/tools

注册声明式动态工具。当前服务端仅支持 `template` handler，不支持上传任意代码执行逻辑。

**Request Body:**
```json
{
  "name": "custom_linter",
  "description": "Run custom linting rules on Python code",
  "input_schema": {
    "type": "object",
    "properties": {
      "code": {"type": "string", "description": "Python source code to lint"},
      "rules": {"type": "array", "description": "List of rule IDs to apply"}
    },
    "required": ["code"]
  },
  "category": "code",
  "sandboxed": true,
  "rate_limit": 10,
  "handler": {
    "type": "template",
    "template": "linted: {{.code}}"
  }
}
```

**Response (201):**
```json
{
  "message": "tool registered",
  "name": "custom_linter"
}
```

```bash
curl -X POST http://localhost:8080/api/v1/tools \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "custom_linter",
    "description": "Run custom linting rules on Python code",
    "input_schema": {"type": "object", "properties": {"code": {"type": "string"}}, "required": ["code"]},
    "handler": {"type": "template", "template": "linted: {{.code}}"}
  }'
```

`handler.template` 使用 Go `text/template` 执行，缺失变量会返回 `400 Bad Request` 或运行期错误，不会静默吞掉参数问题。

---

### GET /api/v1/tools

列出所有已注册的工具。

**Response (200):**
```json
{
  "tools": [
    {
      "name": "code_exec",
      "description": "Execute code in a sandboxed Docker container",
      "input_schema": {...},
      "category": "code",
      "sandboxed": true,
      "rate_limit": 0
    },
    {
      "name": "web_search",
      "description": "Search the web using Tavily API",
      "input_schema": {...}
    }
  ]
}
```

```bash
curl http://localhost:8080/api/v1/tools | jq .
```

---

### DELETE /api/v1/tools/:name

注销一个工具（幂等操作）。

**Response (200):**
```json
{
  "message": "tool unregistered",
  "name": "custom_linter"
}
```

```bash
curl -X DELETE http://localhost:8080/api/v1/tools/custom_linter
```

---

## Error Format

所有错误响应使用统一格式：

```json
{
  "code": 400,
  "message": "human-readable error description",
  "details": "optional technical details for debugging"
}
```

| Status | 含义 |
|--------|------|
| 400 | 请求参数错误（缺字段、格式不对） |
| 404 | 资源不存在（workflow/step/tool not found） |
| 409 | 冲突（重复注册工具、恢复非暂停工作流） |
| 500 | 服务端内部错误 |

## Headers

| Header | 方向 | 说明 |
|--------|------|------|
| `X-Request-ID` | Request/Response | 请求追踪 ID。客户端可传入，未传则自动生成 UUID |
| `Content-Type` | Request | 必须为 `application/json` |

## Step Types

| Type | 说明 |
|------|------|
| `llm_call` | 调用 LLM 进行推理 |
| `tool_call` | 调用注册的工具 |
| `react` | ReAct（Reason + Act）循环，允许 LLM 在步骤内多轮调用工具 |
| `human` | 人工审批节点（暂停等待 resume） |
| `branch` | 条件分支（CEL 表达式） |
| `parallel` | 并行子步骤 |

## Workflow Status Lifecycle

```
pending → running → completed
                  → failed
                  → paused (human step) → running (resume) → completed / failed
```

## Step Status Lifecycle

```
pending → running → success
                  → failed (may retry → running)
                  → timeout
                  → skipped (branch condition false)
                  → cancelled (workflow cancel)
```
