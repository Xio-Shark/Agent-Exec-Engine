package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/Xio-Shark/agent-exec-engine/internal/dag"
	"github.com/Xio-Shark/agent-exec-engine/internal/mcp"
	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
)

type blockingExecutor struct {
	started chan struct{}
	release chan struct{}
}

func (e *blockingExecutor) Execute(ctx context.Context, step *types.Step, _ map[string]any) (string, error) {
	select {
	case e.started <- struct{}{}:
	default:
	}

	select {
	case <-e.release:
		return `{"step":"` + step.ID + `","status":"completed"}`, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func setupRuntimeTestRouter(t *testing.T, executors map[types.StepType]dag.StepExecutor) http.Handler {
	t.Helper()

	manager := NewWorkflowManager(executors)
	registry := mcp.NewRegistry()

	return SetupRouter(RouterConfig{
		Manager:  manager,
		Registry: registry,
		Logger:   zap.NewNop(),
	})
}

func TestRegisterTool_TemplateHandler(t *testing.T) {
	_, registry, router := setupTestRouter(t)

	req := RegisterToolRequest{
		Name:        "custom_linter",
		Description: "lint code",
		InputSchema: types.ToolSchema{Type: "object"},
		Handler: ToolHandlerSpec{
			Type:     dynamicHandlerTypeTemplate,
			Template: `linted: {{.code}}`,
		},
	}

	w := doRequest(router, http.MethodPost, "/api/v1/tools", req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	result, rpcErr := registry.Call(context.Background(), types.ToolCall{
		ID:       "call-1",
		ToolName: "custom_linter",
		Input:    map[string]any{"code": "print('ok')"},
	})
	if rpcErr != nil {
		t.Fatalf("expected no rpc error, got %+v", rpcErr)
	}
	if result.Content != "linted: print('ok')" {
		t.Fatalf("unexpected dynamic tool output: %s", result.Content)
	}
}

func TestCreateWorkflow_StatusRefresh(t *testing.T) {
	executor := &blockingExecutor{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	router := setupRuntimeTestRouter(t, map[types.StepType]dag.StepExecutor{
		types.StepTypeLLMCall: executor,
	})

	req := CreateWorkflowRequest{
		Name: "runtime-refresh",
		Steps: []types.Step{
			{ID: "step1", Type: types.StepTypeLLMCall},
		},
	}

	w := doRequest(router, http.MethodPost, "/api/v1/workflows", req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var createResp WorkflowRunResponse
	if err := json.Unmarshal(w.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	select {
	case <-executor.started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for workflow execution to start")
	}

	w = doRequest(router, http.MethodGet, "/api/v1/workflows/"+createResp.Run.WorkflowID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var runResp WorkflowRunResponse
	if err := json.Unmarshal(w.Body.Bytes(), &runResp); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if runResp.Run.Status != types.WorkflowRunning {
		t.Fatalf("expected running status, got %s", runResp.Run.Status)
	}
	if runResp.Run.StepStates["step1"].Status != types.StepRunning {
		t.Fatalf("expected step running, got %s", runResp.Run.StepStates["step1"].Status)
	}

	close(executor.release)

	assertEventually(t, 2*time.Second, func() bool {
		w = doRequest(router, http.MethodGet, "/api/v1/workflows/"+createResp.Run.WorkflowID, nil)
		if w.Code != http.StatusOK {
			return false
		}
		var refreshed WorkflowRunResponse
		if err := json.Unmarshal(w.Body.Bytes(), &refreshed); err != nil {
			return false
		}
		return refreshed.Run.Status == types.WorkflowCompleted &&
			refreshed.Run.StepStates["step1"].Status == types.StepSuccess
	})
}

func assertEventually(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}
