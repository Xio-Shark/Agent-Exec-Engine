//go:build integration

package dag

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Xio-Shark/agent-exec-engine/internal/store"
	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
)

func TestIntegration_LinearWorkflow(t *testing.T) {
	run := runWorkflow(t, []types.Step{
		{ID: "a", Type: types.StepTypeLLMCall},
		{ID: "b", Type: types.StepTypeLLMCall, DependsOn: []string{"a"}},
		{ID: "c", Type: types.StepTypeLLMCall, DependsOn: []string{"b"}},
	}, &mockExecutor{
		outputs: map[string]string{
			"a": `{"a":"step-a-output"}`,
			"b": `{"b":"step-b-output"}`,
			"c": `{"c":"step-c-output"}`,
		},
	})
	if run.Status != types.WorkflowCompleted {
		t.Fatalf("expected completed, got %s", run.Status)
	}
}

func TestIntegration_DiamondDAG(t *testing.T) {
	executor := &mockExecutor{
		delay: 80 * time.Millisecond,
		outputs: map[string]string{
			"a": `{"root":true}`,
			"b": `{"branch":"b"}`,
			"c": `{"branch":"c"}`,
			"d": `{"done":true}`,
		},
	}
	run := runWorkflow(t, []types.Step{
		{ID: "a", Type: types.StepTypeLLMCall},
		{ID: "b", Type: types.StepTypeLLMCall, DependsOn: []string{"a"}},
		{ID: "c", Type: types.StepTypeLLMCall, DependsOn: []string{"a"}},
		{ID: "d", Type: types.StepTypeLLMCall, DependsOn: []string{"b", "c"}},
	}, executor)
	if run.StepStates["d"].Status != types.StepSuccess {
		t.Fatalf("expected d success, got %s", run.StepStates["d"].Status)
	}
}

func TestIntegration_RetrySuccess(t *testing.T) {
	run := runWorkflow(t, []types.Step{
		{ID: "a", Type: types.StepTypeLLMCall},
		{
			ID:        "b",
			Type:      types.StepTypeLLMCall,
			DependsOn: []string{"a"},
			Retry:     &types.RetryPolicy{MaxRetries: 3},
		},
	}, &mockExecutor{
		outputs: map[string]string{"a": `{"ok":true}`, "b": `{"ok":true}`},
		errors:  map[string][]error{"b": {errors.New("first"), errors.New("second")}},
	})
	if run.StepStates["b"].RetryCount != 2 {
		t.Fatalf("expected retry count 2, got %d", run.StepStates["b"].RetryCount)
	}
}

func TestIntegration_Timeout(t *testing.T) {
	wf := &types.Workflow{
		ID:    "wf",
		Steps: []types.Step{{ID: "slow", Type: types.StepTypeLLMCall, Timeout: 50 * time.Millisecond}},
	}
	scheduler, err := NewScheduler(wf, map[types.StepType]StepExecutor{
		types.StepTypeLLMCall: &mockExecutor{delay: 200 * time.Millisecond},
	})
	if err != nil {
		t.Fatalf("new scheduler failed: %v", err)
	}
	run, err := scheduler.Run(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
	if run.StepStates["slow"].Status != types.StepTimeout {
		t.Fatalf("expected timeout, got %s", run.StepStates["slow"].Status)
	}
}

func TestIntegration_CheckpointRestore(t *testing.T) {
	mem := store.NewMemoryStore()
	checkpointer := NewRedisCheckpointer(mem)
	cp := &types.Checkpoint{
		RunID:      "run",
		WorkflowID: "wf",
		StepStates: map[string]*types.StepState{
			"a": {StepID: "a", Status: types.StepSuccess, Output: `{"a":1}`},
			"b": {StepID: "b", Status: types.StepSuccess, Output: `{"b":2}`},
			"c": {StepID: "c", Status: types.StepPending},
		},
	}
	if err := checkpointer.Save(context.Background(), cp); err != nil {
		t.Fatalf("save checkpoint failed: %v", err)
	}
	loaded, err := checkpointer.Load(context.Background(), cp.RunID)
	if err != nil {
		t.Fatalf("load checkpoint failed: %v", err)
	}
	executor := &mockExecutor{outputs: map[string]string{"c": `{"c":3}`}}
	wf := &types.Workflow{
		ID: "wf",
		Steps: []types.Step{
			{ID: "a", Type: types.StepTypeLLMCall},
			{ID: "b", Type: types.StepTypeLLMCall, DependsOn: []string{"a"}},
			{ID: "c", Type: types.StepTypeLLMCall, DependsOn: []string{"b"}},
		},
	}
	scheduler, err := RestoreScheduler(context.Background(), wf, loaded, map[types.StepType]StepExecutor{
		types.StepTypeLLMCall: executor,
	})
	if err != nil {
		t.Fatalf("restore failed: %v", err)
	}
	run, err := scheduler.Run(context.Background())
	if err != nil {
		t.Fatalf("restored run failed: %v", err)
	}
	if run.StepStates["c"].Status != types.StepSuccess {
		t.Fatalf("expected c success, got %s", run.StepStates["c"].Status)
	}
}

func runWorkflow(t *testing.T, steps []types.Step, executor *mockExecutor) *types.WorkflowRun {
	t.Helper()
	scheduler, err := NewScheduler(&types.Workflow{ID: "wf", Steps: steps}, map[types.StepType]StepExecutor{
		types.StepTypeLLMCall: executor,
	}, WithMaxParallelSteps(4))
	if err != nil {
		t.Fatalf("new scheduler failed: %v", err)
	}
	run, err := scheduler.Run(context.Background())
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	return run
}
