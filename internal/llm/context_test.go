package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestContextManager_NoTrimNeeded(t *testing.T) {
	t.Parallel()

	cm := NewContextManager(10000, StrategyWrite, nil)
	messages := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hello"},
	}

	result, err := cm.Fit(context.Background(), messages)
	if err != nil {
		t.Fatalf("Fit() error = %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 messages, got %d", len(result))
	}
}

func TestContextManager_WriteStrategy(t *testing.T) {
	t.Parallel()

	// Use a very small token budget to force trimming.
	cm := NewContextManager(50, StrategyWrite, nil)
	messages := []Message{
		{Role: "system", Content: "Be concise."},
		{Role: "user", Content: "First question about Go programming"},
		{Role: "assistant", Content: "Go is a statically typed compiled language"},
		{Role: "user", Content: "Second question about Rust"},
		{Role: "assistant", Content: "Rust is a systems language focused on safety"},
		{Role: "user", Content: "Which is better for web servers?"},
	}

	result, err := cm.Fit(context.Background(), messages)
	if err != nil {
		t.Fatalf("Fit() error = %v", err)
	}

	// System message should always be preserved.
	if result[0].Role != "system" {
		t.Error("system message not preserved")
	}
	// The last user message should be preserved.
	lastMsg := result[len(result)-1]
	if lastMsg.Content != "Which is better for web servers?" {
		t.Errorf("last message not preserved: %q", lastMsg.Content)
	}
	// Total should be fewer messages than input.
	if len(result) >= len(messages) {
		t.Errorf("expected fewer messages, got %d", len(result))
	}
}

func TestContextManager_SelectStrategy(t *testing.T) {
	t.Parallel()

	cm := NewContextManager(80, StrategySelect, nil)
	messages := []Message{
		{Role: "system", Content: "You are an expert."},
		{Role: "user", Content: "Tell me about Docker containers"},
		{Role: "assistant", Content: "Docker uses cgroups and namespaces for isolation"},
		{Role: "user", Content: "What about Kubernetes pods?"},
		{Role: "assistant", Content: "K8s pods are the smallest deployable units"},
		{Role: "user", Content: "How does Docker handle networking?"},
	}

	result, err := cm.Fit(context.Background(), messages)
	if err != nil {
		t.Fatalf("Fit() error = %v", err)
	}

	// System should be preserved.
	if result[0].Role != "system" {
		t.Error("system message not preserved")
	}
	// Docker-related messages should score higher than K8s ones.
	hasDocker := false
	for _, m := range result {
		if strings.Contains(m.Content, "Docker") {
			hasDocker = true
		}
	}
	if !hasDocker {
		t.Error("Docker-related messages should be selected for Docker query")
	}
}

func TestContextManager_CompressStrategy(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := ChatResponse{
			Choices: []Choice{{
				Message:      Message{Role: "assistant", Content: "User discussed Go and Rust programming languages, comparing features."},
				FinishReason: "stop",
			}},
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-model", "", time.Second)
	cm := NewContextManager(60, StrategyCompress, client)

	messages := []Message{
		{Role: "system", Content: "Be helpful."},
		{Role: "user", Content: "Tell me about Go"},
		{Role: "assistant", Content: "Go is great for concurrency with goroutines"},
		{Role: "user", Content: "Tell me about Rust"},
		{Role: "assistant", Content: "Rust has ownership system for memory safety"},
		{Role: "user", Content: "Compare performance"},
	}

	result, err := cm.Fit(context.Background(), messages)
	if err != nil {
		t.Fatalf("Fit() error = %v", err)
	}

	// Should have: system + summary + last 2 messages.
	if len(result) != 4 {
		t.Errorf("expected 4 messages (system+summary+2 recent), got %d", len(result))
	}
	if result[0].Role != "system" {
		t.Error("system message not preserved")
	}
	if !strings.Contains(result[1].Content, "[Context Summary]") {
		t.Error("expected context summary message")
	}
}

func TestContextManager_CompressFallback(t *testing.T) {
	t.Parallel()

	// No client provided → falls back to Write strategy.
	cm := NewContextManager(50, StrategyCompress, nil)
	messages := []Message{
		{Role: "system", Content: "test"},
		{Role: "user", Content: "Hello this is a somewhat long message"},
		{Role: "assistant", Content: "Response to the long message above"},
		{Role: "user", Content: "Follow up question"},
	}

	result, err := cm.Fit(context.Background(), messages)
	if err != nil {
		t.Fatalf("Fit() error = %v", err)
	}
	if result[0].Role != "system" {
		t.Error("system message not preserved on fallback")
	}
}

func TestEstimateTokens(t *testing.T) {
	t.Parallel()

	messages := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hello, world!"},
	}
	tokens := estimateTokens(messages)
	if tokens <= 0 {
		t.Errorf("estimateTokens() = %d, want > 0", tokens)
	}
}

func TestOverlapScore(t *testing.T) {
	t.Parallel()

	a := tokenize("docker container networking cgroups")
	b := tokenize("docker networking bridge mode")

	score := overlapScore(a, b)
	if score <= 0 {
		t.Errorf("overlapScore() = %f, want > 0", score)
	}
	if score > 1 {
		t.Errorf("overlapScore() = %f, want <= 1", score)
	}

	// No overlap.
	c := tokenize("kubernetes pods")
	d := tokenize("machine learning training")
	score2 := overlapScore(c, d)
	if score2 != 0 {
		t.Errorf("overlapScore(no overlap) = %f, want 0", score2)
	}
}

func TestTokenize(t *testing.T) {
	t.Parallel()

	tokens := tokenize("Hello World hello")
	if tokens["hello"] != 2 {
		t.Errorf("hello count = %d, want 2", tokens["hello"])
	}
	if tokens["world"] != 1 {
		t.Errorf("world count = %d, want 1", tokens["world"])
	}
}

func TestContextManager_DefaultValues(t *testing.T) {
	t.Parallel()

	cm := NewContextManager(0, "", nil)
	if cm.maxTokens != 4096 {
		t.Errorf("default maxTokens = %d, want 4096", cm.maxTokens)
	}
	if cm.strategy != StrategyWrite {
		t.Errorf("default strategy = %s, want write", cm.strategy)
	}
}
