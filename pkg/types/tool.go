package types

// ToolSchema describes a tool's input interface (MCP-compatible).
type ToolSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties"`
	Required   []string            `json:"required,omitempty"`
}

// Property describes a single input property.
type Property struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

// ToolDefinition is the full definition of a registered tool.
type ToolDefinition struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	InputSchema ToolSchema `json:"input_schema"`
	Category    string     `json:"category,omitempty"`    // e.g. "code", "search", "data"
	Sandboxed   bool       `json:"sandboxed"`             // whether to run in sandbox
	RateLimit   int        `json:"rate_limit,omitempty"`  // calls per minute, 0 = unlimited
}

// ToolCall represents a single tool invocation.
type ToolCall struct {
	ID       string         `json:"id"`
	ToolName string         `json:"tool_name"`
	Input    map[string]any `json:"input"`
}

// ToolResult is the output of a tool invocation.
type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
	IsError    bool   `json:"is_error"`
}
