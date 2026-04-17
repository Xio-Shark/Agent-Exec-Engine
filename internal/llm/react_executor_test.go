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

func TestReActExecutor_FinishWithoutTools(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := ChatResponse{
			Choices: []Choice{{
				Message: Message{
					Role:    "assistant",
					Content: "Thought: The user asked a simple question, I can answer directly.\nAction: finish\nAction Input: The answer is 42.",
				},
				FinishReason: "stop",
			}},
			Usage: Usage{PromptTokens: 50, CompletionTokens: 30},
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	executor := NewReActExecutor(
		NewClient(server.URL, "test-model", "", time.Second),
		nil, nil, nil,
	)

	step := &types.Step{
		ID:   "react-1",
		Type: types.StepTypeReAct,
		Config: map[string]any{
			"prompt": "What is the answer to life?",
		},
	}

	output, err := executor.Execute(context.Background(), step, nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result["answer"] != "The answer is 42." {
		t.Errorf("answer = %v, want %q", result["answer"], "The answer is 42.")
	}
	if result["rounds"] != float64(1) {
		t.Errorf("rounds = %v, want 1", result["rounds"])
	}
}

func TestReActExecutor_ToolCallThenFinish(t *testing.T) {
	t.Parallel()

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var response ChatResponse
		if callCount == 1 {
			// First round: agent wants to search
			response = ChatResponse{
				Choices: []Choice{{
					Message: Message{
						Role:    "assistant",
						Content: "Thought: I need to search for information.\nAction: web_search\nAction Input: {\"query\": \"golang ReAct pattern\"}",
					},
					FinishReason: "stop",
				}},
			}
		} else {
			// Second round: agent finishes after observation
			response = ChatResponse{
				Choices: []Choice{{
					Message: Message{
						Role:    "assistant",
						Content: "Thought: I found the information I needed from the search results.\nAction: finish\nAction Input: ReAct is a reasoning paradigm combining thought and action.",
					},
					FinishReason: "stop",
				}},
			}
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	registry := mcp.NewRegistry()
	_ = registry.Register(types.ToolDefinition{
		Name:        "web_search",
		Description: "search the web",
		InputSchema: types.ToolSchema{Type: "object", Properties: map[string]types.Property{"query": {Type: "string"}}, Required: []string{"query"}},
	}, func(ctx context.Context, input map[string]any) (string, error) {
		return "ReAct was introduced by Yao et al. 2022. It combines reasoning and acting.", nil
	})

	executor := NewReActExecutor(
		NewClient(server.URL, "test-model", "", time.Second),
		registry, nil, nil,
	)

	step := &types.Step{
		ID:   "react-2",
		Type: types.StepTypeReAct,
		Config: map[string]any{
			"prompt": "What is the ReAct pattern?",
		},
	}

	output, err := executor.Execute(context.Background(), step, nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result["rounds"] != float64(2) {
		t.Errorf("rounds = %v, want 2", result["rounds"])
	}

	trajectory, ok := result["trajectory"].([]any)
	if !ok || len(trajectory) != 2 {
		t.Fatalf("trajectory len = %d, want 2", len(trajectory))
	}

	// Verify first step used web_search
	step1 := trajectory[0].(map[string]any)
	if step1["action"] != "web_search" {
		t.Errorf("step 1 action = %v, want web_search", step1["action"])
	}
	if step1["observation"] != "ReAct was introduced by Yao et al. 2022. It combines reasoning and acting." {
		t.Errorf("step 1 observation unexpected: %v", step1["observation"])
	}
}

func TestReActExecutor_ToolCallError(t *testing.T) {
	t.Parallel()

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var response ChatResponse
		if callCount == 1 {
			response = ChatResponse{
				Choices: []Choice{{
					Message: Message{
						Role:    "assistant",
						Content: "Thought: Let me try the broken tool.\nAction: broken_tool\nAction Input: {}",
					},
					FinishReason: "stop",
				}},
			}
		} else {
			// After error, agent should finish
			response = ChatResponse{
				Choices: []Choice{{
					Message: Message{
						Role:    "assistant",
						Content: "Thought: The tool failed. Let me provide what I know.\nAction: finish\nAction Input: I could not retrieve the information due to a tool error.",
					},
					FinishReason: "stop",
				}},
			}
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	registry := mcp.NewRegistry()

	executor := NewReActExecutor(
		NewClient(server.URL, "test-model", "", time.Second),
		registry, nil, nil,
	)

	step := &types.Step{
		ID:   "react-3",
		Type: types.StepTypeReAct,
		Config: map[string]any{
			"prompt": "Use the broken tool",
		},
	}

	output, err := executor.Execute(context.Background(), step, nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	// Should have 2 rounds: failed tool call + finish
	if result["rounds"] != float64(2) {
		t.Errorf("rounds = %v, want 2", result["rounds"])
	}
}

func TestParseReActResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		content     string
		wantThought string
		wantAction  string
		wantInput   string
	}{
		{
			name:        "standard format",
			content:     "Thought: I need to search\nAction: web_search\nAction Input: {\"query\": \"test\"}",
			wantThought: "I need to search",
			wantAction:  "web_search",
			wantInput:   "{\"query\": \"test\"}",
		},
		{
			name:        "finish action",
			content:     "Thought: Done with the task\nAction: finish\nAction Input: The final answer",
			wantThought: "Done with the task",
			wantAction:  "finish",
			wantInput:   "The final answer",
		},
		{
			name:        "multiline thought",
			content:     "Thought: First I need to consider\nthe problem carefully\nAction: code_exec\nAction Input: {\"code\": \"print(1)\"}",
			wantThought: "First I need to consider the problem carefully",
			wantAction:  "code_exec",
			wantInput:   "{\"code\": \"print(1)\"}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			thought, action, actionInput := parseReActResponse(tt.content)
			if thought != tt.wantThought {
				t.Errorf("thought = %q, want %q", thought, tt.wantThought)
			}
			if action != tt.wantAction {
				t.Errorf("action = %q, want %q", action, tt.wantAction)
			}
			if actionInput != tt.wantInput {
				t.Errorf("actionInput = %q, want %q", actionInput, tt.wantInput)
			}
		})
	}
}

func TestParseActionInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want map[string]any
	}{
		{
			name: "valid JSON",
			raw:  `{"query": "test"}`,
			want: map[string]any{"query": "test"},
		},
		{
			name: "plain text",
			raw:  "just a string",
			want: map[string]any{"query": "just a string"},
		},
		{
			name: "empty",
			raw:  "",
			want: map[string]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseActionInput(tt.raw)
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(tt.want)
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("parseActionInput(%q) = %s, want %s", tt.raw, gotJSON, wantJSON)
			}
		})
	}
}
