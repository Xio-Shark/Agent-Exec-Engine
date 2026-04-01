package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Xio-Shark/agent-exec-engine/internal/mcp"
	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
)

// --- Request / Response DTOs ---

// CreateWorkflowRequest is the input body for POST /api/v1/workflows.
type CreateWorkflowRequest struct {
	Name     string            `json:"name"     binding:"required"`
	Steps    []types.Step      `json:"steps"    binding:"required,min=1"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// WorkflowResponse wraps a workflow definition for JSON output.
type WorkflowResponse struct {
	Workflow *types.Workflow `json:"workflow"`
}

// WorkflowRunResponse wraps a run snapshot for JSON output.
type WorkflowRunResponse struct {
	Run *types.WorkflowRun `json:"run"`
}

// ResumeRequest is the input body for POST /api/v1/workflows/:id/resume.
type ResumeRequest struct {
	StepID string         `json:"step_id" binding:"required"`
	Input  map[string]any `json:"input"`
}

// RegisterToolRequest is the input body for POST /api/v1/tools.
type RegisterToolRequest struct {
	Name        string           `json:"name"         binding:"required"`
	Description string           `json:"description"  binding:"required"`
	InputSchema types.ToolSchema `json:"input_schema" binding:"required"`
	Category    string           `json:"category,omitempty"`
	Sandboxed   bool             `json:"sandboxed"`
	RateLimit   int              `json:"rate_limit,omitempty"`
	Handler     ToolHandlerSpec  `json:"handler"      binding:"required"`
}

// ToolListResponse wraps the list of registered tools.
type ToolListResponse struct {
	Tools []types.ToolDefinition `json:"tools"`
}

// StepListResponse wraps a list of step states.
type StepListResponse struct {
	Steps map[string]*types.StepState `json:"steps"`
}

// --- Handlers ---

// Handler groups all API dependencies.
type Handler struct {
	manager  *WorkflowManager
	registry *mcp.Registry
}

// NewHandler creates a handler set.
func NewHandler(manager *WorkflowManager, registry *mcp.Registry) *Handler {
	return &Handler{manager: manager, registry: registry}
}

// CreateWorkflow handles POST /api/v1/workflows.
func (h *Handler) CreateWorkflow(c *gin.Context) {
	var req CreateWorkflowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errBadRequest(c, err.Error())
		return
	}

	wf := &types.Workflow{
		Name:     req.Name,
		Steps:    req.Steps,
		Metadata: req.Metadata,
	}

	run, err := h.manager.CreateAndRun(c.Request.Context(), wf)
	if err != nil {
		errBadRequest(c, err.Error())
		return
	}

	c.JSON(http.StatusCreated, WorkflowRunResponse{Run: run})
}

// GetWorkflow handles GET /api/v1/workflows/:id.
func (h *Handler) GetWorkflow(c *gin.Context) {
	id := c.Param("id")

	run, ok := h.manager.GetRun(id)
	if !ok {
		errNotFound(c, "workflow not found: "+id)
		return
	}

	c.JSON(http.StatusOK, WorkflowRunResponse{Run: run})
}

// ResumeWorkflow handles POST /api/v1/workflows/:id/resume.
func (h *Handler) ResumeWorkflow(c *gin.Context) {
	id := c.Param("id")

	var req ResumeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errBadRequest(c, err.Error())
		return
	}

	if err := h.manager.ResumeWorkflow(c.Request.Context(), id, req.StepID, req.Input); err != nil {
		errConflict(c, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "workflow resumed"})
}

// CancelWorkflow handles DELETE /api/v1/workflows/:id.
func (h *Handler) CancelWorkflow(c *gin.Context) {
	id := c.Param("id")

	if err := h.manager.CancelWorkflow(id); err != nil {
		errNotFound(c, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "workflow canceled"})
}

// ListSteps handles GET /api/v1/workflows/:id/steps.
func (h *Handler) ListSteps(c *gin.Context) {
	id := c.Param("id")

	run, ok := h.manager.GetRun(id)
	if !ok {
		errNotFound(c, "workflow not found: "+id)
		return
	}

	c.JSON(http.StatusOK, StepListResponse{Steps: run.StepStates})
}

// GetStep handles GET /api/v1/workflows/:id/steps/:step_id.
func (h *Handler) GetStep(c *gin.Context) {
	id := c.Param("id")
	stepID := c.Param("step_id")

	run, ok := h.manager.GetRun(id)
	if !ok {
		errNotFound(c, "workflow not found: "+id)
		return
	}

	state, ok := run.StepStates[stepID]
	if !ok {
		errNotFound(c, "step not found: "+stepID)
		return
	}

	c.JSON(http.StatusOK, gin.H{"step": state})
}

// RegisterTool handles POST /api/v1/tools.
func (h *Handler) RegisterTool(c *gin.Context) {
	var req RegisterToolRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errBadRequest(c, err.Error())
		return
	}

	handler, err := BuildDynamicToolHandler(req.Name, req.Handler)
	if err != nil {
		errBadRequest(c, err.Error())
		return
	}

	def := types.ToolDefinition{
		Name:        req.Name,
		Description: req.Description,
		InputSchema: req.InputSchema,
		Category:    req.Category,
		Sandboxed:   req.Sandboxed,
		RateLimit:   req.RateLimit,
	}
	if err := h.registry.Register(def, handler); err != nil {
		errConflict(c, err.Error())
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "tool registered", "name": req.Name})
}

// ListTools handles GET /api/v1/tools.
func (h *Handler) ListTools(c *gin.Context) {
	tools := h.registry.List()
	c.JSON(http.StatusOK, ToolListResponse{Tools: tools})
}

// UnregisterTool handles DELETE /api/v1/tools/:name.
func (h *Handler) UnregisterTool(c *gin.Context) {
	name := c.Param("name")
	h.registry.Unregister(name)
	c.JSON(http.StatusOK, gin.H{"message": "tool unregistered", "name": name})
}
