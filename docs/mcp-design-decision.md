# MCP Design Decision

## 决策

当前阶段继续在仓库内维护一层薄的 MCP 服务端实现：以 JSON-RPC 2.0 为线协议，只覆盖 `initialize`、`tools/list`、`tools/call` 与 batch request 这一组本项目真正需要的工具能力，而不把外部 SDK 直接引入为核心运行时依赖。

## 背景

- 这个引擎的目标不是“做一个最全的 MCP 平台”，而是给 Agent 工作流提供稳定、可审计、可限流的工具调用边界。
- 当前主路径同时需要 HTTP 和 stdio 两种传输，并且要在调用链上插入输入校验、限流、Guardrail、metrics 和 trace。
- 项目最常见的 reviewer / 面试追问不是“有没有接 SDK”，而是“协议是不是对的、边界是不是清楚、为什么这样拆”。

## 为什么现在不直接依赖完整 SDK

1. 协议面足够小。
   现阶段只需要工具发现和工具调用，不需要 `resources`、`prompts`、`sampling` 这类更大的 MCP 面。为一个很小的协议面引入更重的抽象，收益不高。

2. 运行时边界需要自己控制。
   本项目把输入 schema 校验、per-tool 令牌桶限流、Guardrail 和调用指标放在 registry / server 边界。这里如果完全交给外部 SDK，关键控制点会被藏起来，排障和面试解释都更弱。

3. 需要保留线协议可见性。
   JSON-RPC 2.0 请求和响应在这个仓库里是直接可读、可测、可抓包的。对于协议一致性、batch 行为和错误码校验，这种透明性比“SDK 帮你处理掉了”更有价值。

4. 双传输不是附带需求，而是主需求。
   仓库同时支持 HTTP 和 stdio。保持一套共享的 `handleMessage` 入口，比在 SDK 之上再包一层适配更容易保证两条传输面的行为一致。

## 当前实现边界

- 线协议：JSON-RPC 2.0
- 支持方法：`initialize`、`tools/list`、`tools/call`
- 传输：HTTP POST、stdio
- 边界能力：输入 schema 校验、per-tool 限流、Guardrail、Prometheus 指标、OTLP trace
- 明确未覆盖：`resources`、`prompts`、`sampling`、完整 hosted MCP 生态适配

## 已有验证

- [internal/mcp/protocol_conformance_test.go](../internal/mcp/protocol_conformance_test.go)
  以同一套断言同时覆盖 HTTP 与 stdio：`parse error`、`invalid request`、`method not found`、`invalid params`、empty batch 与 batch happy path。
- [internal/mcp/server_test.go](../internal/mcp/server_test.go)
  覆盖 `initialize`、`tools/list`、`tools/call`、rate limit、metrics 与 HTTP batch request。
- [internal/mcp/stdio_test.go](../internal/mcp/stdio_test.go)
  覆盖 stdio 传输下的 `initialize`、`tools/list`、`tools/call` 与 parse error。
- [internal/mcp/validator_test.go](../internal/mcp/validator_test.go)
  覆盖 required/type/enum 三类输入契约校验。
- [internal/mcp/guardrail_test.go](../internal/mcp/guardrail_test.go)
  覆盖安全规则对输入和输出的拦截 / 警告边界。
- 测试运行证据: [`../evidence/mcp-audit/test-run.txt`](../evidence/mcp-audit/test-run.txt)
  `go test ./internal/mcp -run '(Protocol|Validator|Guardrail|Stdio|RateLimit)' -count=1 -v` 全部通过。

## 什么时候重新评估 SDK

如果后续出现以下任一需求，应重新评估是否切到官方 SDK 或做适配层：

- 需要补齐 `resources` / `prompts` / `sampling` 等更完整的 MCP 面
- 需要与外部 MCP Inspector / hosted MCP server 做更严格的互操作性验证
- 当前自研边界开始成为维护负担，而不是审计优势

到那时，优先考虑“在现有 registry / guardrail / metrics 边界外包一层 adapter”，而不是推倒现在这层可审计实现重来。