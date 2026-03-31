package dag

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
)

func (s *Scheduler) setRunContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.rootCtx = runCtx
	s.cancel = cancel
	s.mu.Unlock()
	return runCtx
}

func (s *Scheduler) startStep(stepID string) error {
	s.mu.Lock()
	state := s.run.StepStates[stepID]
	if !TransitionWithLog(state.Status, types.StepRunning, nil) {
		s.mu.Unlock()
		return fmt.Errorf("cannot transition step %s from %s to running", stepID, state.Status)
	}

	startedAt := time.Now()
	state.Status = types.StepRunning
	state.StartedAt = &startedAt
	state.Error = ""
	s.mu.Unlock()
	s.emit(types.EventStepStarted, stepID, "")
	return nil
}

func (s *Scheduler) finishStepSuccess(stepID, output string) {
	s.mu.Lock()
	state := s.run.StepStates[stepID]
	TransitionWithLog(state.Status, types.StepSuccess, nil)
	completedAt := time.Now()
	state.Status = types.StepSuccess
	state.CompletedAt = &completedAt
	state.Output = output
	state.Error = ""
	s.stepOutputs[stepID] = output
	s.mu.Unlock()
	s.emit(types.EventStepCompleted, stepID, "")
}

func (s *Scheduler) finishStepSkipped(stepID string) {
	s.mu.Lock()
	state := s.run.StepStates[stepID]
	TransitionWithLog(state.Status, types.StepSkipped, nil)
	completedAt := time.Now()
	state.Status = types.StepSkipped
	state.CompletedAt = &completedAt
	state.Output = `{"matched":false}`
	state.Error = ""
	s.stepOutputs[stepID] = state.Output
	s.mu.Unlock()
	s.emit(types.EventStepCompleted, stepID, "")
}

func (s *Scheduler) finishStepFailure(
	stepID string,
	step *types.Step,
	err error,
	ctxErr error,
) (bool, error) {
	s.mu.Lock()
	state := s.run.StepStates[stepID]
	eventType := types.EventStepFailed
	switch {
	case errors.Is(ctxErr, context.DeadlineExceeded):
		TransitionWithLog(state.Status, types.StepTimeout, nil)
		state.Status = types.StepTimeout
		eventType = types.EventStepTimeout
	case errors.Is(ctxErr, context.Canceled):
		TransitionWithLog(state.Status, types.StepCancelled, nil)
		state.Status = types.StepCancelled
		eventType = ""
	default:
		TransitionWithLog(state.Status, types.StepFailed, nil)
		state.Status = types.StepFailed
	}
	state.Error = err.Error()
	shouldRetry := ShouldRetry(step, state)
	if shouldRetry {
		state.RetryCount++
	}
	s.mu.Unlock()

	if eventType != "" {
		s.emit(eventType, stepID, err.Error())
	}
	if shouldRetry && step.Retry != nil {
		s.emit(
			types.EventStepRetry,
			stepID,
			fmt.Sprintf("retry %d/%d", state.RetryCount, step.Retry.MaxRetries),
		)
	}
	return shouldRetry, err
}

func (s *Scheduler) stepTimeout(step *types.Step) time.Duration {
	if step.Timeout > 0 {
		return step.Timeout
	}
	return s.defaultTimeout
}

func (s *Scheduler) buildInput(step *types.Step) (map[string]any, error) {
	input := make(map[string]any, len(step.DependsOn))
	for _, depID := range step.DependsOn {
		raw, ok := s.dependencyOutput(depID)
		if !ok || raw == "" {
			continue
		}

		decoded, err := decodeStepOutput(raw)
		if err != nil {
			return nil, fmt.Errorf("decode output for step %s: %w", depID, err)
		}

		input[depID] = map[string]any{"output": decoded}
		flattenIntoInput(input, decoded)
	}
	return input, nil
}

func (s *Scheduler) dependencyOutput(stepID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	output, ok := s.stepOutputs[stepID]
	return output, ok
}

func (s *Scheduler) pauseWorkflow(step *types.Step) error {
	if err := s.startStep(step.ID); err != nil {
		return err
	}

	s.mu.Lock()
	s.run.Status = types.WorkflowPaused
	s.pausedStepID = step.ID
	s.mu.Unlock()
	s.emit(types.EventWorkflowPaused, step.ID, "")
	return s.saveCheckpoint()
}

func (s *Scheduler) saveCheckpoint() error {
	if s.checkpointer == nil {
		return nil
	}

	cp := &types.Checkpoint{
		RunID:      s.RunID(),
		WorkflowID: s.workflow.ID,
		StepStates: cloneStepStates(s.currentRun().StepStates),
		Version:    s.currentCheckpointVersion(),
	}
	if err := s.checkpointer.Save(context.Background(), cp); err != nil {
		return err
	}

	s.mu.Lock()
	s.checkpointVersion = cp.Version
	s.mu.Unlock()
	if s.metrics != nil {
		s.metrics.CheckpointSaved.Inc()
	}
	s.emit(types.EventCheckpointSaved, "", "")
	return nil
}

func (s *Scheduler) currentCheckpointVersion() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.checkpointVersion
}

func (s *Scheduler) markRunningStepsCancelled() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for _, state := range s.run.StepStates {
		if state.Status == types.StepRunning {
			state.Status = types.StepCancelled
			state.CompletedAt = &now
		}
	}
}

func (s *Scheduler) completedStepCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	completed := 0
	for _, state := range s.run.StepStates {
		if state.Status == types.StepSuccess || state.Status == types.StepSkipped {
			completed++
		}
	}
	return completed
}

func (s *Scheduler) currentRun() *types.WorkflowRun {
	s.mu.Lock()
	defer s.mu.Unlock()

	run := *s.run
	run.StepStates = cloneStepStates(s.run.StepStates)
	return &run
}

func encodeStepOutput(input map[string]any) (string, error) {
	if input == nil {
		return "{}", nil
	}
	data, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("marshal step output: %w", err)
	}
	return string(data), nil
}

func decodeStepOutput(raw string) (any, error) {
	if raw == "" {
		return map[string]any{}, nil
	}

	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return raw, nil
	}
	return decoded, nil
}

func flattenIntoInput(target map[string]any, decoded any) {
	object, ok := decoded.(map[string]any)
	if !ok {
		return
	}
	for key, value := range object {
		if _, exists := target[key]; !exists {
			target[key] = value
		}
	}
}

func dedupeStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

// emit sends a lifecycle event if the channel is configured.
func (s *Scheduler) emit(eventType types.EventType, stepID, message string) {
	if s.eventCh == nil {
		return
	}

	s.mu.Lock()
	runID := s.run.ID
	s.mu.Unlock()

	select {
	case s.eventCh <- types.Event{
		Type:      eventType,
		Timestamp: time.Now(),
		RunID:     runID,
		StepID:    stepID,
		Message:   message,
	}:
	default:
	}
}
