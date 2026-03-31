package dag

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"

	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
)

type stepResult struct {
	ready []string
}

// Run executes the workflow until completion, pause, or failure.
func (s *Scheduler) Run(ctx context.Context) (*types.WorkflowRun, error) {
	runCtx := s.setRunContext(ctx)
	runCtx, span := s.startWorkflowSpan(runCtx)
	defer span.End()

	s.mu.Lock()
	s.run.Status = types.WorkflowRunning
	s.mu.Unlock()
	s.emit(types.EventWorkflowStarted, "", "")

	err := s.runLoop(runCtx, s.graph.ReadySteps())
	return s.currentRun(), err
}

// Resume continues a paused human step with externally provided input.
func (s *Scheduler) Resume(ctx context.Context, stepID string, input map[string]any) error {
	ready, err := s.completeHumanStep(stepID, input)
	if err != nil {
		return err
	}

	runCtx := s.setRunContext(ctx)
	s.mu.Lock()
	s.run.Status = types.WorkflowRunning
	s.mu.Unlock()
	return s.runLoop(runCtx, append(s.graph.ReadySteps(), ready...))
}

// Cancel actively cancels the current workflow execution.
func (s *Scheduler) Cancel() {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Scheduler) runLoop(ctx context.Context, readySteps []string) error {
	for {
		if err := ctx.Err(); err != nil {
			return s.failWorkflow(err)
		}
		if s.completedStepCount() == s.graph.StepCount() {
			s.completeWorkflow()
			return nil
		}

		if len(readySteps) == 0 {
			readySteps = s.graph.ReadySteps()
		}
		if len(readySteps) == 0 {
			return s.failWorkflow(fmt.Errorf("workflow stalled with no ready steps"))
		}

		asyncSteps, inlineReady, paused, err := s.processReadySteps(ctx, readySteps)
		if err != nil {
			return s.failWorkflow(err)
		}
		if paused {
			return nil
		}
		if len(asyncSteps) == 0 {
			readySteps = dedupeStrings(inlineReady)
			continue
		}

		batchReady, err := s.executeBatch(ctx, asyncSteps)
		if err != nil {
			return s.failWorkflow(err)
		}
		readySteps = dedupeStrings(append(inlineReady, batchReady...))
	}
}

func (s *Scheduler) processReadySteps(
	ctx context.Context,
	readySteps []string,
) ([]string, []string, bool, error) {
	asyncSteps := make([]string, 0, len(readySteps))
	inlineReady := make([]string, 0, len(readySteps))

	for _, stepID := range dedupeStrings(readySteps) {
		step, ok := s.graph.Step(stepID)
		if !ok {
			return nil, nil, false, fmt.Errorf("step %q not found", stepID)
		}

		switch step.Type {
		case types.StepTypeBranch:
			ready, err := s.handleBranchStep(step)
			if err != nil {
				return nil, nil, false, err
			}
			inlineReady = append(inlineReady, ready...)
		case types.StepTypeHuman:
			if err := s.pauseWorkflow(step); err != nil {
				return nil, nil, false, err
			}
			return nil, inlineReady, true, nil
		default:
			asyncSteps = append(asyncSteps, stepID)
		}
	}

	return asyncSteps, inlineReady, false, nil
}

func (s *Scheduler) executeBatch(ctx context.Context, stepIDs []string) ([]string, error) {
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(s.maxParallel)

	results := make(chan stepResult, len(stepIDs))
	for _, stepID := range stepIDs {
		currentID := stepID
		group.Go(func() error {
			step, _ := s.graph.Step(currentID)
			input, err := s.buildInput(step)
			if err != nil {
				return err
			}
			if err := s.executeStep(groupCtx, step, input); err != nil {
				return err
			}
			results <- stepResult{ready: s.graph.MarkComplete(currentID)}
			return nil
		})
	}

	err := group.Wait()
	close(results)
	if err != nil {
		return nil, err
	}

	ready := make([]string, 0, len(stepIDs))
	for result := range results {
		ready = append(ready, result.ready...)
	}
	return ready, nil
}

func (s *Scheduler) executeStep(
	ctx context.Context,
	step *types.Step,
	input map[string]any,
) error {
	executor, ok := s.executors[step.Type]
	if !ok {
		return fmt.Errorf("no executor for step type %s", step.Type)
	}

	for {
		if err := s.startStep(step.ID); err != nil {
			return err
		}

		attemptStarted := time.Now()
		spanCtx, span := s.startStepSpan(ctx, step)
		stepCtx, cancel := context.WithTimeout(spanCtx, s.stepTimeout(step))
		output, err := executor.Execute(stepCtx, step, input)
		ctxErr := stepCtx.Err()
		cancel()
		span.End()
		if err == nil {
			s.finishStepSuccess(step.ID, output)
			s.observeStepDuration(step, types.StepSuccess, attemptStarted)
			return s.saveCheckpoint()
		}

		shouldRetry, failure := s.finishStepFailure(step.ID, step, err, ctxErr)
		s.observeStepDuration(step, s.stepStatus(step.ID), attemptStarted)
		if !shouldRetry {
			return failure
		}
		s.observeStepRetry(step.ID)
	}
}

func (s *Scheduler) handleBranchStep(step *types.Step) ([]string, error) {
	if err := s.startStep(step.ID); err != nil {
		return nil, err
	}

	env, err := s.buildInput(step)
	if err != nil {
		return nil, err
	}
	matched, err := s.graph.EvaluateCondition(step.ID, env)
	if err != nil {
		return nil, err
	}

	var ready []string
	if matched {
		s.finishStepSuccess(step.ID, `{"matched":true}`)
		ready = s.graph.MarkComplete(step.ID)
	} else {
		s.finishStepSkipped(step.ID)
		ready = s.graph.SkipStep(step.ID)
	}

	if err := s.saveCheckpoint(); err != nil {
		return nil, err
	}
	return ready, nil
}

func (s *Scheduler) completeHumanStep(stepID string, input map[string]any) ([]string, error) {
	s.mu.Lock()
	if s.pausedStepID != stepID {
		s.mu.Unlock()
		return nil, fmt.Errorf("workflow is not paused on step %q", stepID)
	}

	state := s.run.StepStates[stepID]
	output, err := encodeStepOutput(input)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if !TransitionWithLog(state.Status, types.StepSuccess, nil) {
		s.mu.Unlock()
		return nil, fmt.Errorf("cannot transition step %s from %s to success", stepID, state.Status)
	}

	completedAt := time.Now()
	state.Status = types.StepSuccess
	state.CompletedAt = &completedAt
	state.Output = output
	s.stepOutputs[stepID] = output
	s.pausedStepID = ""
	s.mu.Unlock()

	s.emit(types.EventStepCompleted, stepID, "")
	ready := s.graph.MarkComplete(stepID)
	if err := s.saveCheckpoint(); err != nil {
		return nil, err
	}
	return ready, nil
}

func (s *Scheduler) failWorkflow(err error) error {
	if errors.Is(err, context.Canceled) {
		s.markRunningStepsCancelled()
	}

	s.mu.Lock()
	now := time.Now()
	s.run.Status = types.WorkflowFailed
	s.run.CompletedAt = &now
	s.mu.Unlock()
	s.observeWorkflowResult(types.WorkflowFailed)
	s.emit(types.EventWorkflowFailed, "", err.Error())
	return err
}

func (s *Scheduler) completeWorkflow() {
	s.mu.Lock()
	now := time.Now()
	s.run.Status = types.WorkflowCompleted
	s.run.CompletedAt = &now
	s.mu.Unlock()
	s.observeWorkflowResult(types.WorkflowCompleted)
	s.emit(types.EventWorkflowCompleted, "", "")
}

func (s *Scheduler) startWorkflowSpan(ctx context.Context) (context.Context, trace.Span) {
	if s == nil || s.tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return s.tracer.StartWorkflowSpan(ctx, s.RunID(), s.workflowMetricName())
}

func (s *Scheduler) startStepSpan(ctx context.Context, step *types.Step) (context.Context, trace.Span) {
	if s == nil || s.tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return s.tracer.StartStepSpan(ctx, step.ID, string(step.Type))
}

func (s *Scheduler) observeStepDuration(step *types.Step, status types.StepStatus, started time.Time) {
	if s == nil || s.metrics == nil || step == nil {
		return
	}
	s.metrics.StepDuration.WithLabelValues(string(step.Type), string(status)).Observe(time.Since(started).Seconds())
}

func (s *Scheduler) observeStepRetry(stepID string) {
	if s == nil || s.metrics == nil {
		return
	}
	s.metrics.StepRetries.WithLabelValues(stepID).Inc()
}

func (s *Scheduler) observeWorkflowResult(status types.WorkflowStatus) {
	if s == nil || s.metrics == nil {
		return
	}
	s.metrics.WorkflowsTotal.WithLabelValues(string(status)).Inc()
	s.metrics.WorkflowDuration.WithLabelValues(s.workflowMetricName()).Observe(time.Since(s.run.StartedAt).Seconds())
}

func (s *Scheduler) stepStatus(stepID string) types.StepStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.run.StepStates[stepID]
	if !ok {
		return types.StepFailed
	}
	return state.Status
}

func (s *Scheduler) workflowMetricName() string {
	if s.workflow == nil {
		return "unknown"
	}
	if s.workflow.Name != "" {
		return s.workflow.Name
	}
	if s.workflow.ID != "" {
		return s.workflow.ID
	}
	return "unknown"
}
