package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientChat_UsesDefaultModelAndAuth(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v1/chat/completions" {
			t.Fatalf("path = %s, want /v1/chat/completions", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("auth header = %s, want bearer token", got)
		}

		var request ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.Model != "gateway-model" {
			t.Fatalf("model = %s, want gateway-model", request.Model)
		}

		_ = json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{Message: Message{Role: "assistant", Content: "ok"}}},
			Usage:   Usage{PromptTokens: 5, CompletionTokens: 2, TotalTokens: 7},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL+"/v1", "gateway-model", "secret", time.Second)
	response, err := client.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if got := response.Choices[0].Message.Content; got != "ok" {
		t.Fatalf("content = %s, want ok", got)
	}
}

func TestClientChat_RetriesServerErrors(t *testing.T) {
	t.Parallel()

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			http.Error(w, "retry", http.StatusBadGateway)
			return
		}
		_ = json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{Message: Message{Role: "assistant", Content: "done"}}},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "retry-model", "", time.Second)
	response, err := client.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if got := response.Choices[0].Message.Content; got != "done" {
		t.Fatalf("content = %s, want done", got)
	}
}
