package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const maxChatAttempts = 3

// Client calls an OpenAI-compatible inference gateway.
type Client struct {
	baseURL    string
	model      string
	apiKey     string
	timeout    time.Duration
	httpClient *http.Client
}

// NewClient creates a new LLM client with timeout-aware HTTP transport.
func NewClient(baseURL, model, apiKey string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		apiKey:  apiKey,
		timeout: timeout,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Chat performs a chat completion request with bounded retries on transient failures.
func (c *Client) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	if req.Model == "" {
		req.Model = c.model
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal chat request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < maxChatAttempts; attempt++ {
		resp, callErr := c.doChat(ctx, body)
		if callErr == nil {
			return resp, nil
		}
		lastErr = callErr
		if !isRetryable(callErr) || attempt == maxChatAttempts-1 {
			break
		}
		if sleepErr := sleepWithContext(ctx, retryDelay(attempt)); sleepErr != nil {
			return nil, sleepErr
		}
	}
	return nil, lastErr
}

func (c *Client) doChat(ctx context.Context, body []byte) (*ChatResponse, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/chat/completions",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("build chat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chat request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read chat response: %w", err)
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		return nil, &retryableHTTPError{statusCode: resp.StatusCode, body: string(payload)}
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("chat request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}

	var decoded ChatResponse
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, fmt.Errorf("decode chat response: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return nil, fmt.Errorf("chat response contained no choices")
	}
	return &decoded, nil
}

func retryDelay(attempt int) time.Duration {
	return 100 * time.Millisecond << attempt
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isRetryable(err error) bool {
	var httpErr *retryableHTTPError
	if isNetError(err) {
		return true
	}
	return errors.As(err, &httpErr)
}

func isNetError(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr)
}

type retryableHTTPError struct {
	statusCode int
	body       string
}

func (e *retryableHTTPError) Error() string {
	return fmt.Sprintf("chat request failed with status %d: %s", e.statusCode, strings.TrimSpace(e.body))
}
