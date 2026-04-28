package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestKnowledgeQATool_Definition(t *testing.T) {
	tool := NewKnowledgeQATool("")
	def := tool.Definition()

	if def.Name != "knowledge_qa" {
		t.Fatalf("expected name knowledge_qa, got %s", def.Name)
	}
	if def.Category != "search" {
		t.Fatalf("expected category search, got %s", def.Category)
	}
	if def.RateLimit != 10 {
		t.Fatalf("expected rate limit 10, got %d", def.RateLimit)
	}
	if len(def.InputSchema.Required) == 0 || def.InputSchema.Required[0] != "question" {
		t.Fatalf("expected question as required field, got %v", def.InputSchema.Required)
	}
}

func TestKnowledgeQATool_StubWithoutRAGService(t *testing.T) {
	tool := NewKnowledgeQATool("")
	result, err := tool.Handle(context.Background(), map[string]any{"question": "What is checkpoint?"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty stub result")
	}
}

func TestKnowledgeQATool_StubRejectsEmptyQuestion(t *testing.T) {
	tool := NewKnowledgeQATool("")
	_, err := tool.Handle(context.Background(), map[string]any{"question": ""})
	if err == nil {
		t.Fatal("expected error for empty question")
	}
}

func TestKnowledgeQATool_CallsQAEndpoint(t *testing.T) {
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/qa/ask" {
			t.Fatalf("expected path /v1/qa/ask, got %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		_ = json.NewEncoder(w).Encode(qaResponse{
			Answer:     "Checkpoint saves workflow state to Redis for recovery.",
			AuditID:    "audit-abc123",
			Sources:    []string{"docs/checkpoint.md", "internal/dag/checkpoint.go"},
			Confidence: 0.92,
		})
	}))
	defer server.Close()

	tool := NewKnowledgeQATool(server.URL)
	result, err := tool.Handle(context.Background(), map[string]any{
		"question": "What is checkpoint?",
		"depth":    5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify request was sent correctly.
	if receivedBody["question"] != "What is checkpoint?" {
		t.Fatalf("expected question, got %v", receivedBody["question"])
	}

	// Verify response contains audit_id.
	var output qaOutput
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if output.AuditID != "audit-abc123" {
		t.Fatalf("expected audit_id audit-abc123, got %s", output.AuditID)
	}
	if output.Answer == "" {
		t.Fatal("expected non-empty answer")
	}
	if len(output.Sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(output.Sources))
	}
}

func TestKnowledgeQATool_PropagatesRequestID(t *testing.T) {
	var receivedRequestID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedRequestID = r.Header.Get("X-Request-ID")
		_ = json.NewEncoder(w).Encode(qaResponse{
			Answer:  "ok",
			AuditID: "audit-xyz",
		})
	}))
	defer server.Close()

	ctx := context.WithValue(context.Background(), requestIDKey{}, "req-42")
	tool := NewKnowledgeQATool(server.URL)
	_, err := tool.Handle(ctx, map[string]any{"question": "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedRequestID != "req-42" {
		t.Fatalf("expected X-Request-ID req-42, got %s", receivedRequestID)
	}
}

func TestKnowledgeQATool_HandlesServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer server.Close()

	tool := NewKnowledgeQATool(server.URL)
	_, err := tool.Handle(context.Background(), map[string]any{"question": "test"})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}
