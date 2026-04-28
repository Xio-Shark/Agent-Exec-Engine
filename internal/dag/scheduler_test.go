package dag

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Xio-Shark/agent-exec-engine/internal/store"
	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
)

func TestScheduler_SimpleWorkflow(t *testing.T) {
	wf := &types.Workflow{
		ID: "wf",
		Steps: []types.Step{
			{ID: "a", Type: types.StepTypeLLMCall},
			{ID: "b", Type: types.StepTypeLLMCall, DependsOn: []string{"a"}},
		},
	}

	executor := &mockExecutor{output: `{"ok":true}`}
	scheduler, err := NewScheduler(
		wf,
		map[types.StepType]StepExecutor{types.StepTypeLLMCall: executor},
	)
	if err != nil {
		t.Fatalf("new scheduler failed: %v", err)
	}

	run, err := scheduler.Run(context.Background())
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if run.Status != types.WorkflowCompleted {
		t.Fatalf("expected completed, got %s", run.Status)
	}
}

func TestScheduler_PassesDependencyOutput(t *testing.T) {
	wf := &types.Workflow{
		ID: "wf",
		Steps: []types.Step{
			{ID: "a", Type: types.StepTypeLLMCall},
			{ID: "b", Type: types.StepTypeLLMCall, DependsOn: []string{"a"}},
		},
	}

	executor := &mockExecutor{
		outputs: map[string]string{
			"a": `{"status":"approved","score":0.9}`,
			"b": `{"done":true}`,
		},
	}
	scheduler, err := NewScheduler(
		wf,
		map[types.StepType]StepExecutor{types.StepTypeLLMCall: executor},
	)
	if err != nil {
		t.Fatalf("new scheduler failed: %v", err)
	}

	if _, err := scheduler.Run(context.Background()); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	inputs := executor.inputs["b"]
	if len(inputs) != 1 {
		t.Fatalf("expected one call for b, got %d", len(inputs))
	}
	if inputs[0]["status"] != "approved" {
		t.Fatalf("expected flattened input, got %#v", inputs[0])
	}
}

func TestScheduler_BranchSkipStillUnblocksDownstream(t *testing.T) {
	wf := &types.Workflow{
		ID: "wf",
		Steps: []types.Step{
			{ID: "a", Type: types.StepTypeLLMCall},
			{ID: "gate", Type: types.StepTypeBranch, DependsOn: []string{"a"}, Condition: `approved == true`},
			{ID: "tail", Type: types.StepTypeLLMCall, DependsOn: []string{"gate"}},
		},
	}

	executor := &mockExecutor{
		outputs: map[string]string{
			"a":    `{"approved":false}`,
			"tail": `{"done":true}`,
		},
	}
	scheduler, err := NewScheduler(
		wf,
		map[types.StepType]StepExecutor{types.StepTypeLLMCall: executor},
	)
	if err != nil {
		t.Fatalf("new scheduler failed: %v", err)
	}

	run, err := scheduler.Run(context.Background())
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}

	if run.StepStates["gate"].Status != types.StepSkipped {
		t.Fatalf("expected gate skipped, got %s", run.StepStates["gate"].Status)
	}
	if run.StepStates["tail"].Status != types.StepSuccess {
		t.Fatalf("expected tail success, got %s", run.StepStates["tail"].Status)
	}
}

func TestScheduler_PauseAndResumeHumanStep(t *testing.T) {
	wf := &types.Workflow{
		ID: "wf",
		Steps: []types.Step{
			{ID: "a", Type: types.StepTypeLLMCall},
			{ID: "review", Type: types.StepTypeHuman, DependsOn: []string{"a"}},
			{ID: "b", Type: types.StepTypeLLMCall, DependsOn: []string{"review"}},
		},
	}

	executor := &mockExecutor{
		outputs: map[string]string{
			"a": `{"ready":true}`,
			"b": `{"done":true}`,
		},
	}
	scheduler, err := NewScheduler(
		wf,
		map[types.StepType]StepExecutor{types.StepTypeLLMCall: executor},
	)
	if err != nil {
		t.Fatalf("new scheduler failed: %v", err)
	}

	run, err := scheduler.Run(context.Background())
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if run.Status != types.WorkflowPaused {
		t.Fatalf("expected paused workflow, got %s", run.Status)
	}

	if err := scheduler.Resume(context.Background(), "review", map[string]any{"approved": true}); err != nil {
		t.Fatalf("resume failed: %v", err)
	}
	resumed := scheduler.currentRun()
	if resumed.Status != types.WorkflowCompleted {
		t.Fatalf("expected completed after resume, got %s", resumed.Status)
	}
}

func TestScheduler_CancelMarksRunningSteps(t *testing.T) {
	wf := &types.Workflow{
		ID: "wf",
		Steps: []types.Step{
			{ID: "slow", Type: types.StepTypeLLMCall},
		},
	}

	executor := &mockExecutor{
		delay:   250 * time.Millisecond,
		started: make(chan string, 1),
	}
	scheduler, err := NewScheduler(
		wf,
		map[types.StepType]StepExecutor{types.StepTypeLLMCall: executor},
	)
	if err != nil {
		t.Fatalf("new scheduler failed: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		_, runErr := scheduler.Run(context.Background())
		errCh <- runErr
	}()

	<-executor.started
	scheduler.Cancel()

	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
	run := scheduler.currentRun()
	if run.StepStates["slow"].Status != types.StepCancelled {
		t.Fatalf("expected slow step canceled, got %s", run.StepStates["slow"].Status)
	}
}

func TestScheduler_RetryExhaustedMarksFailed(t *testing.T) {
	wf := &types.Workflow{
		ID: "wf",
		Steps: []types.Step{
			{
				ID:    "fragile",
				Type:  types.StepTypeLLMCall,
				Retry: &types.RetryPolicy{MaxRetries: 2},
			},
		},
	}

	mem := store.NewMemoryStore()
	checkpointer := NewRedisCheckpointer(mem)
	executor := &mockExecutor{
		errors: map[string][]error{
			"fragile": {errors.New("err1"), errors.New("err2"), errors.New("err3")},
		},
	}
	scheduler, err := NewScheduler(
		wf,
		map[types.StepType]StepExecutor{types.StepTypeLLMCall: executor},
		WithCheckpointer(checkpointer),
	)
	if err != nil {
		t.Fatalf("new scheduler failed: %v", err)
	}

	run, err := scheduler.Run(context.Background())
	if err == nil {
		t.Fatal("expected error when retries exhausted")
	}
	if run.Status != types.WorkflowFailed {
		t.Fatalf("expected workflow failed, got %s", run.Status)
	}
	if run.StepStates["fragile"].Status != types.StepFailed {
		t.Fatalf("expected step failed, got %s", run.StepStates["fragile"].Status)
	}
	if run.StepStates["fragile"].RetryCount != 2 {
		t.Fatalf("expected retry count 2, got %d", run.StepStates["fragile"].RetryCount)
	}

	// Verify checkpoint was persisted with failed state
	cp, err := checkpointer.Load(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("load checkpoint failed: %v", err)
	}
	if cp.StepStates["fragile"].Status != types.StepFailed {
		t.Fatalf("expected checkpoint step failed, got %s", cp.StepStates["fragile"].Status)
	}
}

func TestScheduler_StepIdempotency(t *testing.T) {
	wf := &types.Workflow{
		ID: "wf",
		Steps: []types.Step{
			{ID: "a", Type: types.StepTypeLLMCall},
			{ID: "b", Type: types.StepTypeLLMCall, DependsOn: []string{"a"}},
		},
	}

	executor := &mockExecutor{
		outputs: map[string]string{
			"a": `{"ok":true}`,
			"b": `{"done":true}`,
		},
	}
	scheduler, err := NewScheduler(
		wf,
		map[types.StepType]StepExecutor{types.StepTypeLLMCall: executor},
	)
	if err != nil {
		t.Fatalf("new scheduler failed: %v", err)
	}

	if _, err := scheduler.Run(context.Background()); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	if executor.calls["a"] != 1 {
		t.Fatalf("expected step a called exactly once, got %d", executor.calls["a"])
	}
	if executor.calls["b"] != 1 {
		t.Fatalf("expected step b called exactly once, got %d", executor.calls["b"])
	}
}
