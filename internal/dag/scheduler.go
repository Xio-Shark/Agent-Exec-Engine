package dag

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Xio-Shark/agent-exec-engine/internal/observability"
	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
)

const (
	defaultMaxParallelSteps = 10
	defaultStepTimeout      = 120 * time.Second
)

// StepExecutor is the interface that step type handlers must implement.
type StepExecutor interface {
	Execute(ctx context.Context, step *types.Step, input map[string]any) (output string, err error)
}

// Scheduler orchestrates DAG-based workflow execution.
type Scheduler struct {
	mu                sync.Mutex
	workflow          *types.Workflow
	graph             *Graph
	run               *types.WorkflowRun
	executors         map[types.StepType]StepExecutor
	checkpointer      Checkpointer
	eventCh           chan types.Event
	defaultTimeout    time.Duration
	maxParallel       int
	stepOutputs       map[string]string
	checkpointVersion int
	rootCtx           context.Context
	cancel            context.CancelFunc
	pausedStepID      string
	tracer            *observability.Tracer
	metrics           *observability.Metrics
}

// SchedulerOption configures a Scheduler.
type SchedulerOption func(*Scheduler)

// WithCheckpointer sets the checkpoint backend.
func WithCheckpointer(cp Checkpointer) SchedulerOption {
	return func(s *Scheduler) { s.checkpointer = cp }
}

// WithEventChannel sets the event notification channel.
func WithEventChannel(ch chan types.Event) SchedulerOption {
	return func(s *Scheduler) { s.eventCh = ch }
}

// WithDefaultTimeout sets the default per-step timeout.
func WithDefaultTimeout(timeout time.Duration) SchedulerOption {
	return func(s *Scheduler) { s.defaultTimeout = timeout }
}

// WithMaxParallelSteps limits the number of concurrently running steps.
func WithMaxParallelSteps(maxParallel int) SchedulerOption {
	return func(s *Scheduler) {
		if maxParallel > 0 {
			s.maxParallel = maxParallel
		}
	}
}

// WithTracer sets the workflow tracer.
func WithTracer(tracer *observability.Tracer) SchedulerOption {
	return func(s *Scheduler) { s.tracer = tracer }
}

// WithMetrics sets Prometheus metrics for scheduler execution.
func WithMetrics(metrics *observability.Metrics) SchedulerOption {
	return func(s *Scheduler) { s.metrics = metrics }
}

// NewScheduler creates a scheduler for the given workflow.
func NewScheduler(
	wf *types.Workflow,
	executors map[types.StepType]StepExecutor,
	opts ...SchedulerOption,
) (*Scheduler, error) {
	graph, err := NewGraph(wf.Steps)
	if err != nil {
		return nil, err
	}

	scheduler := &Scheduler{
		workflow:       wf,
		graph:          graph,
		executors:      executors,
		defaultTimeout: defaultStepTimeout,
		maxParallel:    defaultMaxParallelSteps,
		stepOutputs:    make(map[string]string),
		run: &types.WorkflowRun{
			ID:         uuid.NewString(),
			WorkflowID: wf.ID,
			Status:     types.WorkflowPending,
			StepStates: make(map[string]*types.StepState, len(wf.Steps)),
			StartedAt:  time.Now(),
		},
	}
	for _, opt := range opts {
		opt(scheduler)
	}
	scheduler.initStepStates()
	return scheduler, nil
}

// RunID returns the current run's ID.
func (s *Scheduler) RunID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.run.ID
}

func (s *Scheduler) initStepStates() {
	for _, step := range s.workflow.Steps {
		s.run.StepStates[step.ID] = &types.StepState{
			StepID: step.ID,
			Status: types.StepPending,
		}
	}
}
