# MCP Inspector Interoperability Evidence

> 对应 `EXECUTION-LIST.md` 的 A4。
> 生成时间：2026-04-24

---

## 当前 MCP 协议一致性状态

### 已验证的协议面

| 能力 | 方法 | 传输 | 测试 | 状态 |
|------|------|------|------|------|
| 初始化握手 | `initialize` | HTTP + stdio | `server_test.go` / `stdio_test.go` | ✅ |
| 工具发现 | `tools/list` | HTTP + stdio | `server_test.go` / `stdio_test.go` | ✅ |
| 工具调用 | `tools/call` | HTTP + stdio | `server_test.go` / `stdio_test.go` | ✅ |
| Batch 请求 | JSON-RPC batch | HTTP | `server_test.go` | ✅ |
| 输入校验 | schema validation | HTTP + stdio | `validator_test.go` | ✅ |
| 安全规则 | guardrail | HTTP | `guardrail_test.go` | ✅ |
| 限流 | per-tool rate limit | HTTP | `server_test.go` | ✅ |

### 协议错误码覆盖

| 错误码 | 含义 | 测试 | 状态 |
|--------|------|------|------|
| `-32700` | Parse error | `protocol_conformance_test.go` | ✅ |
| `-32600` | Invalid Request | `protocol_conformance_test.go` | ✅ |
| `-32601` | Method not found | `protocol_conformance_test.go` | ✅ |
| `-32602` | Invalid params | `protocol_conformance_test.go` | ✅ |

### Inspector 互操作验证

**验证方式**：使用 MCP Inspector（官方调试工具）对当前 server 进行端到端验证。

**验证命令**：

```bash
# 1. 启动 agent-exec-engine 服务
cd agent-exec-engine
make run  # 或 go run ./cmd/server

# 2. 使用 npx 启动 MCP Inspector 连接 HTTP 端点
npx @modelcontextprotocol/inspector http://localhost:8080/mcp

# 3. 在 Inspector UI 中验证：
#    - 连接成功（initialize 握手通过）
#    - tools/list 返回 5 个工具（code_exec, web_search, file_reader, sql_query, rag_search, knowledge_qa）
#    - tools/call 对 rag_search 返回 stub 或真实结果
#    - tools/call 对 knowledge_qa 返回 stub 或带 audit_id 的结果
```

**预期结果**：

| Inspector 操作 | 预期行为 |
|---------------|---------|
| 连接 HTTP endpoint | `initialize` 响应含 `protocolVersion: "2024-11-05"` 和 `capabilities.tools` |
| 列出工具 | 返回所有已注册工具及其 `inputSchema` |
| 调用 `rag_search` | 返回搜索结果或 stub 文本 |
| 调用 `knowledge_qa` | 返回 JSON 含 `answer` + `audit_id` + `sources` |
| 发送无效 JSON | 返回 `-32700` parse error |
| 调用不存在的方法 | 返回 `-32601` method not found |

**stdio 模式验证**：

```bash
# 启动 stdio 模式
go run ./cmd/server --stdio

# 通过 Inspector stdio 传输连接
npx @modelcontextprotocol/inspector --command "go run ./cmd/server --stdio"
```

---

## 明确未覆盖的 MCP 能力面

| 能力 | 状态 | 说明 |
|------|------|------|
| `resources` | ❌ 未实现 | 当前不需要文件资源暴露 |
| `prompts` | ❌ 未实现 | prompt 模板由 workflow config 管理 |
| `sampling` | ❌ 未实现 | 不需要 server-side LLM 请求 |
| `logging` | ❌ 未实现 | 日志走 zap + OTLP，不走 MCP 通道 |
| `completion` | ❌ 未实现 | 不需要自动补全 |

---

## 验证命令速查

```bash
# 运行全部 MCP 协议一致性测试
cd agent-exec-engine
go test ./internal/mcp -run '(Protocol|Validator|Guardrail|Stdio|RateLimit)' -count=1 -v

# 运行 knowledge_qa 测试（R4）
go test ./internal/mcp/tools -run 'KnowledgeQA' -count=1 -v
```

---

## 关联文档

- MCP 设计决策: [`mcp-design-decision.md`](mcp-design-decision.md)
- 接口契约: [`contract-table.md`](contract-table.md)
- 测试证据: [`../evidence/mcp-audit/test-run.txt`](../evidence/mcp-audit/test-run.txt)
