package api

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Xio-Shark/agent-exec-engine/internal/dag"
	"github.com/Xio-Shark/agent-exec-engine/internal/observability"
	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
)

// WorkflowManager tracks workflow definitions and their running instances.
type WorkflowManager struct {
	mu         sync.RWMutex
	workflows  map[string]*types.Workflow    // workflow ID → definition
	schedulers map[string]*dag.Scheduler     // run ID → scheduler
	runs       map[string]*types.WorkflowRun // run ID → snapshot
	runIndex   map[string]string             // workflow ID → latest run ID
	executors  map[types.StepType]dag.StepExecutor
	opts       []dag.SchedulerOption
}

// NewWorkflowManager creates a manager with shared executor set and scheduler options.
func NewWorkflowManager(
	executors map[types.StepType]dag.StepExecutor,
	opts ...dag.SchedulerOption,
) *WorkflowManager {
	return &WorkflowManager{
		workflows:  make(map[string]*types.Workflow),
		schedulers: make(map[string]*dag.Scheduler),
		runs:       make(map[string]*types.WorkflowRun),
		runIndex:   make(map[string]string),
		executors:  executors,
		opts:       opts,
	}
}

// CreateAndRun registers a workflow, creates a scheduler, and runs it async.
// Returns the workflow definition and initial run state.
func (m *WorkflowManager) CreateAndRun(ctx context.Context, wf *types.Workflow) (*types.WorkflowRun, error) {
	if wf.ID == "" {
		wf.ID = uuid.NewString()
	}
	if wf.CreatedAt.IsZero() {
		wf.CreatedAt = time.Now()
	}
	if err := validateWorkflow(wf); err != nil {
		return nil, err
	}

	sched, err := dag.NewScheduler(wf, m.executors, m.opts...)
	if err != nil {
		return nil, fmt.Errorf("create scheduler: %w", err)
	}

	runID := sched.RunID()
	initialRun := sched.Snapshot()

	m.mu.Lock()
	m.workflows[wf.ID] = wf
	m.schedulers[runID] = sched
	m.runs[runID] = initialRun
	m.runIndex[wf.ID] = runID
	m.mu.Unlock()

	// Run async — update run snapshot on completion.
	go func() {
		result, _ := sched.Run(context.Background())
		if result != nil {
			m.mu.Lock()
			m.runs[runID] = result
			m.mu.Unlock()
		}
	}()

	return initialRun, nil
}

// GetWorkflow returns a workflow definition by ID.
func (m *WorkflowManager) GetWorkflow(id string) (*types.Workflow, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	wf, ok := m.workflows[id]
	return wf, ok
}

// GetRun returns the latest run snapshot by workflow ID.
func (m *WorkflowManager) GetRun(workflowID string) (*types.WorkflowRun, bool) {
	m.mu.RLock()
	runID, ok := m.runIndex[workflowID]
	if !ok {
		m.mu.RUnlock()
		return nil, false
	}
	run := m.runs[runID]
	m.mu.RUnlock()

	m.mu.RLock()
	sched, schedOK := m.schedulers[runID]
	m.mu.RUnlock()
	if schedOK {
		snapshot := sched.Snapshot()
		m.mu.Lock()
		m.runs[runID] = snapshot
		m.mu.Unlock()
		return snapshot, true
	}
	return run, run != nil
}

// ResumeWorkflow resumes a paused workflow by injecting input for a human step.
func (m *WorkflowManager) ResumeWorkflow(
	ctx context.Context,
	workflowID, stepID string,
	input map[string]any,
) error {
	m.mu.RLock()
	runID, ok := m.runIndex[workflowID]
	if !ok {
		m.mu.RUnlock()
		return fmt.Errorf("workflow %s not found", workflowID)
	}
	sched, ok := m.schedulers[runID]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("run %s not found", runID)
	}
	return sched.Resume(ctx, stepID, input)
}

// CancelWorkflow cancels a running workflow.
func (m *WorkflowManager) CancelWorkflow(workflowID string) error {
	m.mu.RLock()
	runID, ok := m.runIndex[workflowID]
	if !ok {
		m.mu.RUnlock()
		return fmt.Errorf("workflow %s not found", workflowID)
	}
	sched, ok := m.schedulers[runID]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("run %s not found", runID)
	}
	sched.Cancel()
	return nil
}

// ListWorkflows returns all registered workflow definitions.
func (m *WorkflowManager) ListWorkflows() []*types.Workflow {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*types.Workflow, 0, len(m.workflows))
	for _, wf := range m.workflows {
		result = append(result, wf)
	}
	return result
}

func validateWorkflow(wf *types.Workflow) error {
	if wf.Name == "" {
		return fmt.Errorf("workflow name is required")
	}
	if len(wf.Steps) == 0 {
		return fmt.Errorf("workflow must have at least one step")
	}
	seen := make(map[string]bool, len(wf.Steps))
	for _, s := range wf.Steps {
		if s.ID == "" {
			return fmt.Errorf("step ID is required")
		}
		if seen[s.ID] {
			return fmt.Errorf("duplicate step ID: %s", s.ID)
		}
		seen[s.ID] = true
	}
	return nil
}

// NoOpExecutor is a pass-through executor for testing and placeholder steps.
type NoOpExecutor struct{}

// Execute returns a simple success output.
func (e *NoOpExecutor) Execute(_ context.Context, step *types.Step, _ map[string]any) (string, error) {
	return fmt.Sprintf(`{"step":"%s","status":"completed"}`, step.ID), nil
}

// DefaultExecutors returns a minimal executor map for use when no LLM/sandbox is configured.
func DefaultExecutors(
	tracer *observability.Tracer,
	metrics *observability.Metrics,
) map[types.StepType]dag.StepExecutor {
	noop := &NoOpExecutor{}
	return map[types.StepType]dag.StepExecutor{
		types.StepTypeLLMCall:  noop,
		types.StepTypeToolCall: noop,
		types.StepTypeParallel: noop,
		types.StepTypeReAct:    noop,
	}
}
