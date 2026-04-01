package infra

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// SchedulerClient reserves GPU capacity through the AI Infra Platform API.
type SchedulerClient struct {
	baseURL      string
	httpClient   *http.Client
	mu           sync.Mutex
	reservations map[string]string
}

// NewSchedulerClient creates a scheduler client.
func NewSchedulerClient(baseURL string, timeout time.Duration) *SchedulerClient {
	return &SchedulerClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
		reservations: make(map[string]string),
	}
}

// RequestGPU creates and schedules a lightweight inference reservation job.
func (c *SchedulerClient) RequestGPU(ctx context.Context, taskID string, gpuCount int, memoryMin int64) error {
	jobID, err := c.createReservation(ctx, taskID, gpuCount, memoryMin)
	if err != nil {
		return err
	}
	if err := c.scheduleReservation(ctx, jobID); err != nil {
		return err
	}
	c.mu.Lock()
	c.reservations[taskID] = jobID
	c.mu.Unlock()
	return nil
}

// ReleaseGPU attempts to release a previously reserved GPU allocation.
func (c *SchedulerClient) ReleaseGPU(ctx context.Context, taskID string) error {
	jobID, ok := c.reservationID(taskID)
	if !ok {
		return fmt.Errorf("no GPU reservation found for task %s", taskID)
	}
	status, payload, err := c.doJSON(ctx, http.MethodPost, "/jobs/"+jobID+"/cancel", nil)
	if err != nil {
		return err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return fmt.Errorf("cancel reservation %s failed with status %d: %s", jobID, status, payload)
	}
	c.mu.Lock()
	delete(c.reservations, taskID)
	c.mu.Unlock()
	return nil
}

func (c *SchedulerClient) createReservation(ctx context.Context, taskID string, gpuCount int, memoryMin int64) (string, error) {
	request := map[string]any{
		"name":     "agent-exec-" + taskID,
		"job_type": "inference",
		"executor": "shell",
		"command":  []string{"true"},
		"priority": 100,
		"metadata": map[string]string{
			"task_id": taskID,
			"source":  "agent-exec-engine",
		},
		"resource_spec": map[string]any{
			"gpu":        gpuCount,
			"gpu_memory": formatGPUMemory(memoryMin),
		},
	}

	status, payload, err := c.doJSON(ctx, http.MethodPost, "/jobs", request)
	if err != nil {
		return "", err
	}
	if status != http.StatusCreated {
		return "", fmt.Errorf("create reservation failed with status %d: %s", status, payload)
	}

	var response struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		return "", fmt.Errorf("decode reservation response: %w", err)
	}
	if response.ID == "" {
		return "", fmt.Errorf("reservation response missing job id")
	}
	return response.ID, nil
}

func (c *SchedulerClient) scheduleReservation(ctx context.Context, jobID string) error {
	status, payload, err := c.doJSON(ctx, http.MethodPost, "/jobs/"+jobID+"/schedule", nil)
	if err != nil {
		return err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return fmt.Errorf("schedule reservation %s failed with status %d: %s", jobID, status, payload)
	}
	return nil
}

func (c *SchedulerClient) doJSON(ctx context.Context, method, path string, body any) (int, []byte, error) {
	payload, err := marshalBody(body)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return 0, nil, fmt.Errorf("build scheduler request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("scheduler request failed: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("read scheduler response: %w", err)
	}
	return resp.StatusCode, responseBody, nil
}

func marshalBody(body any) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal scheduler request: %w", err)
	}
	return payload, nil
}

func formatGPUMemory(memoryMin int64) string {
	if memoryMin <= 0 {
		return ""
	}
	return fmt.Sprintf("%dMi", memoryMin)
}

func (c *SchedulerClient) reservationID(taskID string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	jobID, ok := c.reservations[taskID]
	return jobID, ok
}
