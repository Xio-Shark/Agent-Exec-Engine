package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ChatStream opens an SSE stream for chat completions.
func (c *Client) ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	req.Stream = true
	if req.Model == "" {
		req.Model = c.model
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal stream request: %w", err)
	}

	httpResp, cancel, err := c.openStreamRequest(ctx, payload)
	if err != nil {
		return nil, err
	}
	if httpResp.StatusCode < http.StatusOK || httpResp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		cancel()
		return nil, fmt.Errorf("stream request failed with status %d: %s", httpResp.StatusCode, strings.TrimSpace(string(body)))
	}

	out := make(chan StreamChunk)
	go c.consumeStream(httpResp.Body, cancel, out)

	return out, nil
}

func (c *Client) openStreamRequest(ctx context.Context, payload []byte) (*http.Response, context.CancelFunc, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("build stream request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("open stream request: %w", err)
	}
	return httpResp, cancel, nil
}

func (c *Client) consumeStream(body io.ReadCloser, cancel context.CancelFunc, out chan<- StreamChunk) {
	defer cancel()
	defer body.Close()
	defer close(out)

	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		chunk, done, err := parseStreamLine(scanner.Text())
		if err != nil {
			out <- StreamChunk{Err: err}
			return
		}
		if done {
			return
		}
		if chunk != nil {
			out <- *chunk
		}
	}

	if err := scanner.Err(); err != nil {
		out <- StreamChunk{Err: fmt.Errorf("scan stream: %w", err)}
	}
}

func parseStreamLine(raw string) (*StreamChunk, bool, error) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data: ") {
		return nil, false, nil
	}

	payload := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
	if payload == "[DONE]" {
		return nil, true, nil
	}

	var chunk StreamChunk
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		return nil, false, fmt.Errorf("decode stream chunk: %w", err)
	}
	return &chunk, false, nil
}
