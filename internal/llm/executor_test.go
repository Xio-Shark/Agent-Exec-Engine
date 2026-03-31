package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Xio-Shark/agent-exec-engine/internal/mcp"
	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
)

func TestLLMStepExecutor_ExecuteWithToolLoopAndGPU(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		hasToolResult := false
		for _, message := range request.Messages {
			if message.Role == "tool" && message.Content == "tool-output" {
				hasToolResult = true
			}
		}

		response := ChatResponse{}
		if !hasToolResult {
			response = ChatResponse{
				Choices: []Choice{{
					Message: Message{
						Role: "assistant",
						ToolCalls: []ToolCall{{
							ID:   "call-1",
							Type: "function",
							Function: FunctionCall{
								Name:      "echo",
								Arguments: `{"value":"tool-output"}`,
							},
						}},
					},
					FinishReason: "tool_calls",
				}},
			}
		} else {
			response = ChatResponse{
				Choices: []Choice{{
					Message:      Message{Role: "assistant", Content: "final-answer"},
					FinishReason: "stop",
				}},
			}
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	registry := mcp.NewRegistry()
	if err := registry.Register(types.ToolDefinition{
		Name:        "echo",
		Description: "echo value",
		InputSchema: types.ToolSchema{Type: "object"},
	}, func(ctx context.Context, input map[string]any) (string, error) {
		return input["value"].(string), nil
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	scheduler := &mockGPUAllocator{}
	executor := NewLLMStepExecutor(
		NewClient(server.URL, "tool-model", "", time.Second),
		registry,
		scheduler,
		nil,
		nil,
	)

	step := &types.Step{
		ID:   "step-a",
		Type: types.StepTypeLLMCall,
		Config: map[string]any{
			"system_prompt":  "You are helpful",
			"prompt":         "Use tools if needed",
			"gpu_count":      1,
			"gpu_memory_min": 4096,
		},
	}

	output, err := executor.Execute(context.Background(), step, map[string]any{"previous": "context"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if output != "final-answer" {
		t.Fatalf("output = %s, want final-answer", output)
	}
	if scheduler.requestCount != 1 || scheduler.releaseCount != 1 {
		t.Fatalf("scheduler counts = (%d,%d), want (1,1)", scheduler.requestCount, scheduler.releaseCount)
	}
}

type mockGPUAllocator struct {
	requestCount int
	releaseCount int
}

func (m *mockGPUAllocator) RequestGPU(_ context.Context, _ string, _ int, _ int64) error {
	m.requestCount++
	return nil
}

func (m *mockGPUAllocator) ReleaseGPU(_ context.Context, _ string) error {
	m.releaseCount++
	return nil
}
