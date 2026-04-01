package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/Xio-Shark/agent-exec-engine/internal/observability"
	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
)

func TestServer_Initialize(t *testing.T) {
	server := NewServer(NewRegistry())

	response := performRequest(t, server, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	if response.Error != nil {
		t.Fatalf("expected no error, got %+v", response.Error)
	}

	result, ok := response.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", response.Result)
	}
	if result["protocolVersion"] != "2024-11-05" {
		t.Fatalf("unexpected protocolVersion: %v", result["protocolVersion"])
	}
}

func TestServer_ListTools(t *testing.T) {
	registry := NewRegistry()
	mustRegisterEchoTool(t, registry)
	mustRegisterTool(t, registry, types.ToolDefinition{
		Name:        "noop",
		Description: "Noop tool",
		InputSchema: types.ToolSchema{Type: "object"},
	}, func(ctx context.Context, input map[string]any) (string, error) {
		return "ok", nil
	})

	server := NewServer(registry)
	response := performRequest(t, server, `{"jsonrpc":"2.0","id":"list","method":"tools/list","params":{}}`)
	if response.Error != nil {
		t.Fatalf("expected no error, got %+v", response.Error)
	}

	result := mustMap(t, response.Result)
	toolsList := mustSlice(t, result["tools"])
	if len(toolsList) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(toolsList))
	}
}

func TestServer_CallTool_Success(t *testing.T) {
	registry := NewRegistry()
	mustRegisterEchoTool(t, registry)

	server := NewServer(registry)
	response := performRequest(t, server, `{"jsonrpc":"2.0","id":"call","method":"tools/call","params":{"name":"echo","arguments":{"message":"hello"}}}`)
	if response.Error != nil {
		t.Fatalf("expected no error, got %+v", response.Error)
	}

	result := mustMap(t, response.Result)
	if result["isError"] != false {
		t.Fatalf("expected isError=false, got %v", result["isError"])
	}

	content := mustSlice(t, result["content"])
	first := mustMap(t, content[0])
	if first["text"] != "hello" {
		t.Fatalf("unexpected content: %v", first["text"])
	}
}

func TestServer_CallTool_NotFound(t *testing.T) {
	server := NewServer(NewRegistry())
	response := performRequest(t, server, `{"jsonrpc":"2.0","id":"missing","method":"tools/call","params":{"name":"missing","arguments":{}}}`)
	if response.Error == nil {
		t.Fatal("expected error response")
	}
	if response.Error.Code != ErrToolNotFound {
		t.Fatalf("expected tool not found code, got %d", response.Error.Code)
	}
}

func TestServer_CallTool_RateLimited(t *testing.T) {
	registry := NewRegistry()
	mustRegisterEchoTool(t, registry)
	server := NewServer(registry)
	server.rateLimiter.SetLimit("echo", 1)

	first := performRequest(t, server, `{"jsonrpc":"2.0","id":"1","method":"tools/call","params":{"name":"echo","arguments":{"message":"hello"}}}`)
	if first.Error != nil {
		t.Fatalf("expected first request to pass, got %+v", first.Error)
	}

	second := performRequest(t, server, `{"jsonrpc":"2.0","id":"2","method":"tools/call","params":{"name":"echo","arguments":{"message":"hello"}}}`)
	if second.Error == nil {
		t.Fatal("expected rate limit error")
	}
	if second.Error.Code != ErrRateLimited {
		t.Fatalf("expected rate limit code, got %d", second.Error.Code)
	}
}

func TestServer_CallTool_RecordsMetrics(t *testing.T) {
	registry := NewRegistry()
	mustRegisterEchoTool(t, registry)
	promRegistry := prometheus.NewRegistry()
	metrics := observability.NewMetricsWithRegisterer(promRegistry)
	server := NewServer(registry, WithMetrics(metrics))

	response := performRequest(t, server, `{"jsonrpc":"2.0","id":"call","method":"tools/call","params":{"name":"echo","arguments":{"message":"hello"}}}`)
	if response.Error != nil {
		t.Fatalf("expected no error, got %+v", response.Error)
	}

	if got := counterValue(t, metrics.ToolCallsTotal.WithLabelValues("echo", "success")); got != 1 {
		t.Fatalf("expected success tool call count 1, got %v", got)
	}
	if got := histogramCountByName(t, promRegistry, "agent_exec_tool_call_duration_seconds", map[string]string{"tool_name": "echo"}); got != 1 {
		t.Fatalf("expected tool duration count 1, got %d", got)
	}
}

func TestServer_CallTool_RateLimitMetrics(t *testing.T) {
	registry := NewRegistry()
	mustRegisterEchoTool(t, registry)
	metrics := observability.NewMetricsWithRegisterer(prometheus.NewRegistry())
	server := NewServer(registry, WithMetrics(metrics))
	server.rateLimiter.SetLimit("echo", 1)

	_ = performRequest(t, server, `{"jsonrpc":"2.0","id":"1","method":"tools/call","params":{"name":"echo","arguments":{"message":"hello"}}}`)
	response := performRequest(t, server, `{"jsonrpc":"2.0","id":"2","method":"tools/call","params":{"name":"echo","arguments":{"message":"hello"}}}`)
	if response.Error == nil {
		t.Fatal("expected rate limit error")
	}

	if got := counterValue(t, metrics.RateLimitRejected.WithLabelValues("echo")); got != 1 {
		t.Fatalf("expected rate limit rejection count 1, got %v", got)
	}
	if got := counterValue(t, metrics.ToolCallsTotal.WithLabelValues("echo", "rate_limited")); got != 1 {
		t.Fatalf("expected rate_limited tool call count 1, got %v", got)
	}
}

func TestServer_BatchRequest(t *testing.T) {
	registry := NewRegistry()
	mustRegisterEchoTool(t, registry)
	server := NewServer(registry)

	requestBody := `[
		{"jsonrpc":"2.0","id":"1","method":"initialize","params":{}},
		{"jsonrpc":"2.0","id":"2","method":"tools/call","params":{"name":"echo","arguments":{"message":"hello"}}}
	]`

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(requestBody))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", rec.Code)
	}

	var responses []JSONRPCResponse
	if err := json.NewDecoder(rec.Body).Decode(&responses); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	if len(responses) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(responses))
	}
	if responses[0].Error != nil {
		t.Fatalf("unexpected error in first response: %+v", responses[0].Error)
	}
	if responses[1].Error != nil {
		t.Fatalf("unexpected error in second response: %+v", responses[1].Error)
	}
}

func performRequest(t *testing.T, server *Server, body string) JSONRPCResponse {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	var response JSONRPCResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return response
}

func mustRegisterEchoTool(t *testing.T, registry *Registry) {
	t.Helper()

	mustRegisterTool(t, registry, types.ToolDefinition{
		Name:        "echo",
		Description: "Echo message",
		InputSchema: types.ToolSchema{
			Type: "object",
			Properties: map[string]types.Property{
				"message": {Type: "string"},
			},
			Required: []string{"message"},
		},
	}, func(ctx context.Context, input map[string]any) (string, error) {
		message, ok := input["message"].(string)
		if !ok {
			return "", nil
		}
		return message, nil
	})
}

func mustRegisterTool(
	t *testing.T,
	registry *Registry,
	definition types.ToolDefinition,
	handler ToolHandler,
) {
	t.Helper()

	if err := registry.Register(definition, handler); err != nil {
		t.Fatalf("register tool: %v", err)
	}
}

func counterValue(t *testing.T, collector interface{ Write(*dto.Metric) error }) float64 {
	t.Helper()

	metric := &dto.Metric{}
	if err := collector.Write(metric); err != nil {
		t.Fatalf("write metric: %v", err)
	}
	return metric.GetCounter().GetValue()
}

func histogramCountByName(
	t *testing.T,
	registry *prometheus.Registry,
	metricName string,
	labels map[string]string,
) uint64 {
	t.Helper()

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, family := range families {
		if family.GetName() != metricName {
			continue
		}
		for _, metric := range family.GetMetric() {
			if hasLabels(metric, labels) {
				return metric.GetHistogram().GetSampleCount()
			}
		}
	}
	t.Fatalf("metric %s with labels %#v not found", metricName, labels)
	return 0
}

func hasLabels(metric *dto.Metric, labels map[string]string) bool {
	for key, expected := range labels {
		found := false
		for _, pair := range metric.GetLabel() {
			if pair.GetName() == key && pair.GetValue() == expected {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func mustMap(t testing.TB, value any) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", value)
	}
	return result
}

func mustSlice(t testing.TB, value any) []any {
	t.Helper()
	result, ok := value.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", value)
	}
	return result
}
