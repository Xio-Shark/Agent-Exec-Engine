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

const tavilySearchURL = "https://api.tavily.com/search"

// WebSearchTool performs web searches via Tavily, with a stub fallback.
type WebSearchTool struct {
	apiKey     string
	httpClient *http.Client
}

func NewWebSearchTool(apiKey string) *WebSearchTool {
	return &WebSearchTool{
		apiKey: strings.TrimSpace(apiKey),
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (t *WebSearchTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "web_search",
		Description: "Search the web for information.",
		InputSchema: types.ToolSchema{
			Type: "object",
			Properties: map[string]types.Property{
				"query": {Type: "string", Description: "Search query"},
			},
			Required: []string{"query"},
		},
		RateLimit: 10,
		Category:  "search",
	}
}

func (t *WebSearchTool) Handle(ctx context.Context, input map[string]any) (string, error) {
	query, ok := input["query"].(string)
	if !ok || strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("query must be a non-empty string")
	}
	if t.apiKey == "" {
		return fmt.Sprintf("[web_search] results for: %s (stub: missing api key)", query), nil
	}

	body, err := json.Marshal(map[string]any{
		"api_key":             t.apiKey,
		"query":               query,
		"search_depth":        "basic",
		"max_results":         5,
		"include_raw_content": false,
	})
	if err != nil {
		return "", fmt.Errorf("marshal tavily request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tavilySearchURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build tavily request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("call tavily: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read tavily response: %w", err)
	}
	if resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("tavily error: status %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	var result struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return "", fmt.Errorf("decode tavily response: %w", err)
	}
	if len(result.Results) == 0 {
		return fmt.Sprintf("[web_search] no results for: %s", query), nil
	}

	lines := make([]string, 0, len(result.Results))
	for _, item := range result.Results {
		lines = append(lines, fmt.Sprintf("- %s | %s | %s", item.Title, item.URL, item.Content))
	}
	return strings.Join(lines, "\n"), nil
}
