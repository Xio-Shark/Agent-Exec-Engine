package dag

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Xio-Shark/agent-exec-engine/internal/store"
	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
)

const checkpointTTLSeconds = 86400

var ErrCheckpointConflict = errors.New("checkpoint conflict")

// Checkpointer persists and loads workflow run checkpoints.
type Checkpointer interface {
	Save(ctx context.Context, cp *types.Checkpoint) error
	Load(ctx context.Context, runID string) (*types.Checkpoint, error)
}

// RedisCheckpointer implements Checkpointer using the shared Store abstraction.
type RedisCheckpointer struct {
	mu    sync.Mutex
	store store.Store
}

// NewRedisCheckpointer creates a store-backed checkpointer.
func NewRedisCheckpointer(backingStore store.Store) *RedisCheckpointer {
	return &RedisCheckpointer{store: backingStore}
}

func (r *RedisCheckpointer) Save(ctx context.Context, cp *types.Checkpoint) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	current, err := r.Load(ctx, cp.RunID)
	switch {
	case err == nil:
		if current.Version != cp.Version {
			return fmt.Errorf(
				"%w: run %s current=%d expected=%d",
				ErrCheckpointConflict,
				cp.RunID,
				current.Version,
				cp.Version,
			)
		}
	case isNotFound(err):
	default:
		return err
	}

	next := cloneCheckpoint(cp)
	next.Timestamp = time.Now()
	next.Version++

	payload, err := json.Marshal(next)
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}
	if err := r.store.Set(ctx, checkpointKey(cp.RunID), payload, checkpointTTLSeconds); err != nil {
		return fmt.Errorf("save checkpoint: %w", err)
	}

	cp.Timestamp = next.Timestamp
	cp.Version = next.Version
	cp.StepStates = cloneStepStates(next.StepStates)
	return nil
}

func (r *RedisCheckpointer) Load(ctx context.Context, runID string) (*types.Checkpoint, error) {
	payload, err := r.store.Get(ctx, checkpointKey(runID))
	if err != nil {
		return nil, err
	}

	var cp types.Checkpoint
	if err := json.Unmarshal(payload, &cp); err != nil {
		return nil, fmt.Errorf("unmarshal checkpoint: %w", err)
	}
	return &cp, nil
}

// RestoreScheduler rebuilds the scheduler state from a checkpoint.
func RestoreScheduler(
	ctx context.Context,
	wf *types.Workflow,
	cp *types.Checkpoint,
	executors map[types.StepType]StepExecutor,
	opts ...SchedulerOption,
) (*Scheduler, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	scheduler, err := NewScheduler(wf, executors, opts...)
	if err != nil {
		return nil, err
	}

	scheduler.mu.Lock()
	scheduler.run.ID = cp.RunID
	scheduler.run.WorkflowID = cp.WorkflowID
	scheduler.run.StepStates = cloneStepStates(cp.StepStates)
	scheduler.checkpointVersion = cp.Version
	for _, step := range wf.Steps {
		state, ok := scheduler.run.StepStates[step.ID]
		if !ok {
			scheduler.run.StepStates[step.ID] = &types.StepState{
				StepID: step.ID,
				Status: types.StepPending,
			}
			continue
		}
		if state.Status == types.StepRunning {
			state.Status = types.StepPending
			state.StartedAt = nil
		}
	}
	scheduler.mu.Unlock()

	for _, step := range wf.Steps {
		state := scheduler.currentRun().StepStates[step.ID]
		switch state.Status {
		case types.StepSuccess:
			scheduler.mu.Lock()
			scheduler.stepOutputs[step.ID] = state.Output
			scheduler.mu.Unlock()
			scheduler.graph.MarkComplete(step.ID)
		case types.StepSkipped:
			scheduler.graph.SkipStep(step.ID)
		}
	}

	if scheduler.metrics != nil {
		scheduler.metrics.CheckpointLoaded.Inc()
	}
	scheduler.emit(types.EventCheckpointLoaded, "", "")
	return scheduler, nil
}

func checkpointKey(runID string) string {
	return fmt.Sprintf("checkpoint:%s", runID)
}

func isNotFound(err error) bool {
	var notFound *store.ErrNotFound
	return errors.As(err, &notFound)
}

func cloneCheckpoint(cp *types.Checkpoint) *types.Checkpoint {
	if cp == nil {
		return nil
	}
	return &types.Checkpoint{
		RunID:      cp.RunID,
		WorkflowID: cp.WorkflowID,
		StepStates: cloneStepStates(cp.StepStates),
		Timestamp:  cp.Timestamp,
		Version:    cp.Version,
	}
}

func cloneStepStates(src map[string]*types.StepState) map[string]*types.StepState {
	dst := make(map[string]*types.StepState, len(src))
	for key, state := range src {
		if state == nil {
			continue
		}
		copyState := *state
		copyState.StartedAt = cloneTimePtr(state.StartedAt)
		copyState.CompletedAt = cloneTimePtr(state.CompletedAt)
		dst[key] = &copyState
	}
	return dst
}

func cloneTimePtr(ts *time.Time) *time.Time {
	if ts == nil {
		return nil
	}
	copyValue := *ts
	return &copyValue
}
