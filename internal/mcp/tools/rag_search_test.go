package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRAGSearch_StubWithoutQdrant(t *testing.T) {
	tool := NewRAGSearchTool("", "", "test-model")

	output, err := tool.Handle(context.Background(), map[string]any{"query": "what is agent infra"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(output, "stub") {
		t.Fatalf("expected stub output, got %s", output)
	}
}

func TestRAGSearch_EmptyQuery(t *testing.T) {
	tool := NewRAGSearchTool("http://localhost:6333", "", "test-model")

	_, err := tool.Handle(context.Background(), map[string]any{"query": ""})
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestRAGSearch_MockEmbeddingAndSearch(t *testing.T) {
	// Mock embedding server
	embedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"data": []map[string]any{
				{"embedding": []float64{0.1, 0.2, 0.3}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer embedServer.Close()

	// Mock Qdrant server
	qdrantServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/points/search") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"result": []map[string]any{
				{
					"score":   0.95,
					"payload": map[string]any{"text": "Agent infra overview", "source": "docs/intro.md"},
				},
				{
					"score":   0.87,
					"payload": map[string]any{"text": "DAG scheduler design", "source": "docs/dag.md"},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer qdrantServer.Close()

	tool := NewRAGSearchTool(qdrantServer.URL, embedServer.URL, "test-model")
	output, err := tool.Handle(context.Background(), map[string]any{
		"query": "agent infra",
		"top_k": float64(2),
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(output, "0.95") {
		t.Fatalf("expected score in output, got %s", output)
	}
	if !strings.Contains(output, "Agent infra overview") {
		t.Fatalf("expected text in output, got %s", output)
	}
}

func TestRAGSearch_QdrantError(t *testing.T) {
	embedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"embedding": []float64{0.1, 0.2}},
			},
		})
	}))
	defer embedServer.Close()

	qdrantServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer qdrantServer.Close()

	tool := NewRAGSearchTool(qdrantServer.URL, embedServer.URL, "test-model")
	_, err := tool.Handle(context.Background(), map[string]any{"query": "test"})
	if err == nil {
		t.Fatal("expected error for Qdrant 500")
	}
	if !strings.Contains(err.Error(), "qdrant error") {
		t.Fatalf("expected qdrant error, got %v", err)
	}
}
