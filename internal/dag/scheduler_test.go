package dag

import (
	"context"
	"errors"
	"testing"
	"time"

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
		t.Fatalf("expected slow step cancelled, got %s", run.StepStates["slow"].Status)
	}
}
