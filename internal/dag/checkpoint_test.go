package dag

import (
	"context"
	"errors"
	"testing"

	"github.com/Xio-Shark/agent-exec-engine/internal/store"
	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
)

func TestCheckpoint_SaveLoad(t *testing.T) {
	mem := store.NewMemoryStore()
	checkpointer := NewRedisCheckpointer(mem)
	cp := &types.Checkpoint{
		RunID:      "run-1",
		WorkflowID: "wf-1",
		StepStates: map[string]*types.StepState{"a": {StepID: "a", Status: types.StepSuccess}},
	}

	if err := checkpointer.Save(context.Background(), cp); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	loaded, err := checkpointer.Load(context.Background(), cp.RunID)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if loaded.Version != 1 || loaded.WorkflowID != cp.WorkflowID {
		t.Fatalf("unexpected checkpoint: %#v", loaded)
	}
}

func TestCheckpoint_OptimisticLock(t *testing.T) {
	mem := store.NewMemoryStore()
	checkpointer := NewRedisCheckpointer(mem)
	first := &types.Checkpoint{RunID: "run-1", WorkflowID: "wf-1", StepStates: map[string]*types.StepState{}}
	second := &types.Checkpoint{RunID: "run-1", WorkflowID: "wf-1", StepStates: map[string]*types.StepState{}}

	if err := checkpointer.Save(context.Background(), first); err != nil {
		t.Fatalf("first save failed: %v", err)
	}
	if err := checkpointer.Save(context.Background(), second); !errors.Is(err, ErrCheckpointConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestCheckpoint_TTL(t *testing.T) {
	mem := store.NewMemoryStore()
	checkpointer := NewRedisCheckpointer(mem)
	cp := &types.Checkpoint{RunID: "run-ttl", WorkflowID: "wf-ttl", StepStates: map[string]*types.StepState{}}

	if err := checkpointer.Save(context.Background(), cp); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	ttl, ok := mem.TTL(checkpointKey(cp.RunID))
	if !ok || ttl != checkpointTTLSeconds {
		t.Fatalf("expected ttl %d, got %d ok=%v", checkpointTTLSeconds, ttl, ok)
	}
}

func TestRestoreScheduler(t *testing.T) {
	wf := &types.Workflow{
		ID: "wf",
		Steps: []types.Step{
			{ID: "a", Type: types.StepTypeLLMCall},
			{ID: "b", Type: types.StepTypeLLMCall, DependsOn: []string{"a"}},
			{ID: "c", Type: types.StepTypeLLMCall, DependsOn: []string{"b"}},
		},
	}

	cp := &types.Checkpoint{
		RunID:      "run-restore",
		WorkflowID: wf.ID,
		Version:    2,
		StepStates: map[string]*types.StepState{
			"a": {StepID: "a", Status: types.StepSuccess, Output: `{"a":1}`},
			"b": {StepID: "b", Status: types.StepSuccess, Output: `{"b":2}`},
			"c": {StepID: "c", Status: types.StepPending},
		},
	}

	executor := &mockExecutor{outputs: map[string]string{"c": `{"c":3}`}}
	scheduler, err := RestoreScheduler(
		context.Background(),
		wf,
		cp,
		map[types.StepType]StepExecutor{types.StepTypeLLMCall: executor},
	)
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
	if executor.calls["a"] != 0 || executor.calls["b"] != 0 {
		t.Fatalf("expected only c to execute, got calls %#v", executor.calls)
	}
}
