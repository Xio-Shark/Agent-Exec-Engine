package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
)

const (
	defaultCollection = "default"
	defaultTopK       = 5
	maxTopK           = 50
	embeddingTimeout  = 10 * time.Second
	searchTimeout     = 10 * time.Second
)

// RAGSearchTool performs semantic search against a Qdrant vector database.
type RAGSearchTool struct {
	qdrantURL  string
	embedModel string
	llmBaseURL string
	httpClient *http.Client
}

// NewRAGSearchTool creates a RAG search tool. If qdrantURL is empty,
// calls return a stub response (safe for environments without Qdrant).
func NewRAGSearchTool(qdrantURL, llmBaseURL, embedModel string) *RAGSearchTool {
	return &RAGSearchTool{
		qdrantURL:  strings.TrimRight(strings.TrimSpace(qdrantURL), "/"),
		llmBaseURL: strings.TrimRight(strings.TrimSpace(llmBaseURL), "/"),
		embedModel: embedModel,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (t *RAGSearchTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "rag_search",
		Description: "Search a vector knowledge base using semantic similarity. Returns the most relevant documents.",
		InputSchema: types.ToolSchema{
			Type: "object",
			Properties: map[string]types.Property{
				"query":      {Type: "string", Description: "Semantic search query"},
				"collection": {Type: "string", Description: "Vector collection name (default: 'default')"},
				"top_k":      {Type: "integer", Description: "Number of results to return (default: 5, max: 50)"},
			},
			Required: []string{"query"},
		},
		RateLimit: 20,
		Category:  "search",
	}
}

func (t *RAGSearchTool) Handle(ctx context.Context, input map[string]any) (string, error) {
	query, ok := input["query"].(string)
	if !ok || strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("query must be a non-empty string")
	}

	collection := stringOrDefault(input["collection"], defaultCollection)
	topK := intOrDefault(input["top_k"], defaultTopK)
	if topK > maxTopK {
		topK = maxTopK
	}

	if t.qdrantURL == "" {
		return fmt.Sprintf("[rag_search] results for: %s (stub: qdrant not configured)", query), nil
	}

	embedding, err := t.getEmbedding(ctx, query)
	if err != nil {
		return "", fmt.Errorf("get embedding: %w", err)
	}

	results, err := t.searchQdrant(ctx, collection, embedding, topK)
	if err != nil {
		return "", fmt.Errorf("search qdrant: %w", err)
	}
	if len(results) == 0 {
		return fmt.Sprintf("[rag_search] no results for: %s", query), nil
	}

	lines := make([]string, 0, len(results))
	for _, r := range results {
		lines = append(lines, fmt.Sprintf("- [%.4f] %s | source: %s", r.Score, r.Text, r.Source))
	}
	return strings.Join(lines, "\n"), nil
}

type searchResult struct {
	Score  float64
	Text   string
	Source string
}

func (t *RAGSearchTool) getEmbedding(ctx context.Context, text string) ([]float64, error) {
	reqBody, err := json.Marshal(map[string]any{
		"model": t.embedModel,
		"input": text,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal embedding request: %w", err)
	}

	embedCtx, cancel := context.WithTimeout(ctx, embeddingTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(embedCtx, http.MethodPost, t.llmBaseURL+"/embeddings", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("build embedding request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call embedding API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read embedding response: %w", err)
	}
	if resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("embedding API error: status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if len(result.Data) == 0 || len(result.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("empty embedding response")
	}
	return result.Data[0].Embedding, nil
}

func (t *RAGSearchTool) searchQdrant(ctx context.Context, collection string, vector []float64, topK int) ([]searchResult, error) {
	reqBody, err := json.Marshal(map[string]any{
		"vector":     vector,
		"limit":      topK,
		"with_payload": true,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal qdrant request: %w", err)
	}

	searchCtx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()

	url := fmt.Sprintf("%s/collections/%s/points/search", t.qdrantURL, collection)
	req, err := http.NewRequestWithContext(searchCtx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("build qdrant request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call qdrant: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read qdrant response: %w", err)
	}
	if resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("qdrant error: status %d: %s", resp.StatusCode, string(body))
	}

	var qdrantResp struct {
		Result []struct {
			Score   float64        `json:"score"`
			Payload map[string]any `json:"payload"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &qdrantResp); err != nil {
		return nil, fmt.Errorf("decode qdrant response: %w", err)
	}

	results := make([]searchResult, 0, len(qdrantResp.Result))
	for _, item := range qdrantResp.Result {
		results = append(results, searchResult{
			Score:  item.Score,
			Text:   stringFromPayload(item.Payload, "text"),
			Source: stringFromPayload(item.Payload, "source"),
		})
	}
	return results, nil
}

func stringFromPayload(payload map[string]any, key string) string {
	if v, ok := payload[key].(string); ok {
		return v
	}
	return ""
}

func stringOrDefault(v any, fallback string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return fallback
}

func intOrDefault(v any, fallback int) int {
	switch val := v.(type) {
	case float64:
		return int(val)
	case int:
		return val
	default:
		return fallback
	}
}
