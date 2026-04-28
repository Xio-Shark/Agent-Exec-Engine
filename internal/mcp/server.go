package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/Xio-Shark/agent-exec-engine/internal/observability"
	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
)

// Server implements the MCP protocol over HTTP (JSON-RPC 2.0).
type Server struct {
	registry    *Registry
	rateLimiter *RateLimiter
	metrics     *observability.Metrics
}

// ServerOption configures the MCP server.
type ServerOption func(*Server)

// WithMetrics attaches Prometheus metrics to the MCP server.
func WithMetrics(metrics *observability.Metrics) ServerOption {
	return func(s *Server) {
		s.metrics = metrics
	}
}

// NewServer creates an MCP server with the given registry.
func NewServer(registry *Registry, opts ...ServerOption) *Server {
	server := &Server{
		registry:    registry,
		rateLimiter: NewRateLimiter(),
	}
	for _, opt := range opts {
		opt(server)
	}
	return server
}

// ServeHTTP handles incoming MCP JSON-RPC requests.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeResponse(w, http.StatusBadRequest, JSONRPCResponse{
			JSONRPC: "2.0",
			Error:   &JSONRPCError{Code: ErrInternal, Message: "failed to read request body"},
		})
		return
	}

	statusCode := http.StatusOK
	resp := s.handleMessage(r.Context(), body)
	if single, ok := resp.(JSONRPCResponse); ok && single.Error != nil && single.Error.Code == ErrParse {
		statusCode = http.StatusBadRequest
	}

	s.writeResponse(w, statusCode, resp)
}

func (s *Server) handleInitialize(req JSONRPCRequest) JSONRPCResponse {
	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"protocolVersion": "2024-11-05",
			"serverInfo": map[string]string{
				"name":    "agent-exec-engine",
				"version": "0.1.0",
			},
			"capabilities": map[string]any{
				"tools": map[string]bool{"listChanged": true},
			},
		},
	}
}

func (s *Server) handleMessage(ctx context.Context, body []byte) any {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return s.invalidRequestResponse(nil, "empty request")
	}

	if trimmed[0] == '[' {
		return s.handleBatchMessage(ctx, trimmed)
	}

	return s.handleRequestBytes(ctx, trimmed)
}

func (s *Server) handleBatchMessage(ctx context.Context, body []byte) any {
	var rawRequests []json.RawMessage
	if err := json.Unmarshal(body, &rawRequests); err != nil {
		return s.parseErrorResponse()
	}
	if len(rawRequests) == 0 {
		return s.invalidRequestResponse(nil, "batch request must not be empty")
	}

	responses := make([]JSONRPCResponse, len(rawRequests))
	var wg sync.WaitGroup
	for idx, raw := range rawRequests {
		wg.Add(1)
		go func(i int, payload json.RawMessage) {
			defer wg.Done()
			responses[i] = s.handleRequestBytes(ctx, payload)
		}(idx, raw)
	}
	wg.Wait()

	return responses
}

func (s *Server) handleRequestBytes(ctx context.Context, body []byte) JSONRPCResponse {
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return s.parseErrorResponse()
	}

	normalized, err := json.Marshal(payload)
	if err != nil {
		return JSONRPCResponse{
			JSONRPC: "2.0",
			Error:   &JSONRPCError{Code: ErrInternal, Message: "failed to normalize request"},
		}
	}

	var req JSONRPCRequest
	if err := json.Unmarshal(normalized, &req); err != nil {
		return s.invalidRequestResponse(nil, "invalid request")
	}
	return s.handleRequest(ctx, req)
}

func (s *Server) handleRequest(ctx context.Context, req JSONRPCRequest) JSONRPCResponse {
	if req.JSONRPC != "2.0" || req.Method == "" {
		return s.invalidRequestResponse(req.ID, "invalid request")
	}

	switch req.Method {
	case "tools/list":
		return s.handleListTools(req)
	case "tools/call":
		return s.handleCallTool(ctx, req)
	case "initialize":
		return s.handleInitialize(req)
	default:
		return JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &JSONRPCError{Code: ErrMethodNotFound, Message: fmt.Sprintf("method not found: %s", req.Method)},
		}
	}
}

func (s *Server) handleListTools(req JSONRPCRequest) JSONRPCResponse {
	tools := s.registry.List()

	// Convert to MCP format
	mcpTools := make([]map[string]any, len(tools))
	for i, t := range tools {
		mcpTools[i] = map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": t.InputSchema,
		}
	}

	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  map[string]any{"tools": mcpTools},
	}
}

func (s *Server) handleCallTool(ctx context.Context, req JSONRPCRequest) JSONRPCResponse {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &JSONRPCError{Code: ErrInvalidParams, Message: "invalid params"},
		}
	}

	started := time.Now()
	// Rate limit check
	if !s.rateLimiter.Allow(params.Name) {
		s.observeToolCall(params.Name, "rate_limited", started)
		if s.metrics != nil {
			s.metrics.RateLimitRejected.WithLabelValues(params.Name).Inc()
		}
		return JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &JSONRPCError{Code: ErrRateLimited, Message: fmt.Sprintf("rate limit exceeded for tool: %s", params.Name)},
		}
	}

	result, rpcErr := s.registry.Call(ctx, types.ToolCall{
		ID:       fmt.Sprintf("%v", req.ID),
		ToolName: params.Name,
		Input:    params.Arguments,
	})
	if rpcErr != nil {
		s.observeToolCall(params.Name, "error", started)
		return JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &JSONRPCError{Code: rpcErr.Code, Message: rpcErr.Message},
		}
	}
	status := "success"
	if result.IsError {
		status = "error"
	}
	s.observeToolCall(params.Name, status, started)

	content := []map[string]any{
		{"type": "text", "text": result.Content},
	}

	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"content": content,
			"isError": result.IsError,
		},
	}
}

func (s *Server) observeToolCall(toolName, status string, started time.Time) {
	if s == nil || s.metrics == nil || toolName == "" {
		return
	}
	s.metrics.ToolCallsTotal.WithLabelValues(toolName, status).Inc()
	s.metrics.ToolCallDuration.WithLabelValues(toolName).Observe(time.Since(started).Seconds())
}

func (s *Server) invalidRequestResponse(id any, message string) JSONRPCResponse {
	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &JSONRPCError{Code: ErrInvalidRequest, Message: message},
	}
}

func (s *Server) parseErrorResponse() JSONRPCResponse {
	return JSONRPCResponse{
		JSONRPC: "2.0",
		Error:   &JSONRPCError{Code: ErrParse, Message: "parse error"},
	}
}

func (s *Server) writeResponse(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}
