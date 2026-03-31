package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/Xio-Shark/agent-exec-engine/internal/observability"
	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
	"go.opentelemetry.io/otel/trace"
)

// ToolHandler is the function signature for tool implementations.
type ToolHandler func(ctx context.Context, input map[string]any) (string, error)

// Registry manages tool registration and discovery.
type Registry struct {
	mu     sync.RWMutex
	tools  map[string]*registeredTool
	tracer *observability.Tracer
}

type registeredTool struct {
	Definition types.ToolDefinition
	Handler    ToolHandler
}

// RegistryOption configures a registry.
type RegistryOption func(*Registry)

// WithTracer attaches tracing to the registry call path.
func WithTracer(tracer *observability.Tracer) RegistryOption {
	return func(r *Registry) {
		r.tracer = tracer
	}
}

// NewRegistry creates an empty tool registry.
func NewRegistry(opts ...RegistryOption) *Registry {
	registry := &Registry{
		tools: make(map[string]*registeredTool),
	}
	for _, opt := range opts {
		opt(registry)
	}
	return registry
}

// Register adds a tool to the registry.
func (r *Registry) Register(def types.ToolDefinition, handler ToolHandler) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[def.Name]; exists {
		return fmt.Errorf("tool already registered: %s", def.Name)
	}

	r.tools[def.Name] = &registeredTool{
		Definition: def,
		Handler:    handler,
	}
	return nil
}

// Unregister removes a tool from the registry.
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, name)
}

// List returns all registered tool definitions (MCP tools/list response).
func (r *Registry) List() []types.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	defs := make([]types.ToolDefinition, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, t.Definition)
	}
	return defs
}

// Call invokes a tool by name (MCP tools/call).
func (r *Registry) Call(ctx context.Context, call types.ToolCall) (types.ToolResult, *RPCError) {
	ctx, span := r.startToolSpan(ctx, call.ToolName)
	defer span.End()

	r.mu.RLock()
	tool, ok := r.tools[call.ToolName]
	r.mu.RUnlock()

	if !ok {
		return types.ToolResult{}, NewRPCError(ErrToolNotFound, fmt.Sprintf("unknown tool: %s", call.ToolName))
	}

	if err := ValidateInput(tool.Definition.InputSchema, call.Input); err != nil {
		return types.ToolResult{}, NewRPCError(ErrInvalidParams, err.Error())
	}

	output, err := tool.Handler(ctx, call.Input)
	if err != nil {
		return types.ToolResult{
			ToolCallID: call.ID,
			Content:    fmt.Sprintf("tool error: %v", err),
			IsError:    true,
		}, nil
	}

	return types.ToolResult{
		ToolCallID: call.ID,
		Content:    output,
		IsError:    false,
	}, nil
}

func (r *Registry) startToolSpan(ctx context.Context, toolName string) (context.Context, trace.Span) {
	if r == nil || r.tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return r.tracer.StartToolSpan(ctx, toolName)
}

// -- JSON-RPC 2.0 types for MCP protocol --

// JSONRPCRequest is a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse is a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      any           `json:"id"`
	Result  any           `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC error.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
