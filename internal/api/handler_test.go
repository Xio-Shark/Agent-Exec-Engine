package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/Xio-Shark/agent-exec-engine/internal/mcp"
	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
)

func setupTestRouter(t *testing.T) (*WorkflowManager, *mcp.Registry, http.Handler) {
	t.Helper()
	executors := DefaultExecutors(nil, nil)
	manager := NewWorkflowManager(executors)
	registry := mcp.NewRegistry()
	logger, _ := zap.NewDevelopment()

	router := SetupRouter(RouterConfig{
		Manager:  manager,
		Registry: registry,
		Store:    nil,
		Logger:   logger,
	})
	return manager, registry, router
}

func doRequest(handler http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

// --- Health & Metrics ---

func TestHealthz(t *testing.T) {
	_, _, router := setupTestRouter(t)
	w := doRequest(router, http.MethodGet, "/healthz", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %s", resp["status"])
	}
}

func TestMetrics(t *testing.T) {
	_, _, router := setupTestRouter(t)
	w := doRequest(router, http.MethodGet, "/metrics", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.Len() == 0 {
		t.Error("expected non-empty metrics body")
	}
}

// --- Workflow CRUD ---

func TestCreateWorkflow_Success(t *testing.T) {
	_, _, router := setupTestRouter(t)

	req := CreateWorkflowRequest{
		Name: "test-workflow",
		Steps: []types.Step{
			{ID: "step1", Type: types.StepTypeLLMCall},
		},
	}

	w := doRequest(router, http.MethodPost, "/api/v1/workflows", req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp WorkflowRunResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Run == nil {
		t.Fatal("expected non-nil run")
	}
	if resp.Run.ID == "" {
		t.Error("expected non-empty run ID")
	}
}

func TestCreateWorkflow_MissingName(t *testing.T) {
	_, _, router := setupTestRouter(t)

	req := CreateWorkflowRequest{
		Steps: []types.Step{
			{ID: "step1", Type: types.StepTypeLLMCall},
		},
	}

	w := doRequest(router, http.MethodPost, "/api/v1/workflows", req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateWorkflow_NoSteps(t *testing.T) {
	_, _, router := setupTestRouter(t)

	req := CreateWorkflowRequest{Name: "empty"}
	w := doRequest(router, http.MethodPost, "/api/v1/workflows", req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetWorkflow_NotFound(t *testing.T) {
	_, _, router := setupTestRouter(t)
	w := doRequest(router, http.MethodGet, "/api/v1/workflows/nonexistent", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestCreateAndGetWorkflow(t *testing.T) {
	manager, _, router := setupTestRouter(t)

	req := CreateWorkflowRequest{
		Name: "e2e-test",
		Steps: []types.Step{
			{ID: "a", Type: types.StepTypeLLMCall},
		},
	}

	w := doRequest(router, http.MethodPost, "/api/v1/workflows", req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create failed: %d %s", w.Code, w.Body.String())
	}

	var createResp WorkflowRunResponse
	json.Unmarshal(w.Body.Bytes(), &createResp)
	workflowID := createResp.Run.WorkflowID

	// Wait a bit for async execution
	time.Sleep(50 * time.Millisecond)

	// Verify manager has the workflow
	if _, ok := manager.GetWorkflow(workflowID); !ok {
		t.Fatal("workflow not found in manager")
	}

	// Get via API
	w = doRequest(router, http.MethodGet, "/api/v1/workflows/"+workflowID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get failed: %d %s", w.Code, w.Body.String())
	}
}

func TestCancelWorkflow_NotFound(t *testing.T) {
	_, _, router := setupTestRouter(t)
	w := doRequest(router, http.MethodDelete, "/api/v1/workflows/nonexistent", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// --- Steps ---

func TestListSteps_NotFound(t *testing.T) {
	_, _, router := setupTestRouter(t)
	w := doRequest(router, http.MethodGet, "/api/v1/workflows/bad-id/steps", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestGetStep_NotFound(t *testing.T) {
	_, _, router := setupTestRouter(t)
	w := doRequest(router, http.MethodGet, "/api/v1/workflows/bad-id/steps/bad-step", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// --- Tool CRUD ---

func TestListTools(t *testing.T) {
	_, _, router := setupTestRouter(t)
	w := doRequest(router, http.MethodGet, "/api/v1/tools", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp ToolListResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	// Should be empty since no tools registered in test setup
	if resp.Tools == nil {
		t.Error("expected non-nil tools list")
	}
}

func TestUnregisterTool(t *testing.T) {
	_, _, router := setupTestRouter(t)
	w := doRequest(router, http.MethodDelete, "/api/v1/tools/nonexistent", nil)
	// Unregister is idempotent — always 200
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// --- Middleware ---

func TestRequestIDMiddleware(t *testing.T) {
	_, _, router := setupTestRouter(t)
	w := doRequest(router, http.MethodGet, "/healthz", nil)
	reqID := w.Header().Get("X-Request-ID")
	if reqID == "" {
		t.Error("expected X-Request-ID header")
	}
}

func TestRequestIDMiddleware_PreserveExisting(t *testing.T) {
	_, _, router := setupTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-ID", "custom-id-123")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Header().Get("X-Request-ID") != "custom-id-123" {
		t.Errorf("expected preserved request ID, got %s", w.Header().Get("X-Request-ID"))
	}
}

// --- E2E: Create → Execute → Query → Verify ---

func TestE2E_LinearWorkflow(t *testing.T) {
	_, _, router := setupTestRouter(t)

	// 1. Create a 3-step linear workflow
	req := CreateWorkflowRequest{
		Name: "linear-e2e",
		Steps: []types.Step{
			{ID: "a", Type: types.StepTypeLLMCall},
			{ID: "b", Type: types.StepTypeToolCall, DependsOn: []string{"a"}},
			{ID: "c", Type: types.StepTypeLLMCall, DependsOn: []string{"b"}},
		},
	}

	w := doRequest(router, http.MethodPost, "/api/v1/workflows", req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}

	var createResp WorkflowRunResponse
	json.Unmarshal(w.Body.Bytes(), &createResp)
	wfID := createResp.Run.WorkflowID

	// 2. Wait for async execution to complete
	time.Sleep(200 * time.Millisecond)

	// 3. Query workflow status
	w = doRequest(router, http.MethodGet, "/api/v1/workflows/"+wfID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get workflow: %d %s", w.Code, w.Body.String())
	}

	// 4. Query steps
	w = doRequest(router, http.MethodGet, "/api/v1/workflows/"+wfID+"/steps", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list steps: %d %s", w.Code, w.Body.String())
	}

	var stepsResp StepListResponse
	json.Unmarshal(w.Body.Bytes(), &stepsResp)
	if len(stepsResp.Steps) != 3 {
		t.Errorf("expected 3 steps, got %d", len(stepsResp.Steps))
	}

	// 5. Query individual step
	w = doRequest(router, http.MethodGet, "/api/v1/workflows/"+wfID+"/steps/a", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get step: %d %s", w.Code, w.Body.String())
	}
}
