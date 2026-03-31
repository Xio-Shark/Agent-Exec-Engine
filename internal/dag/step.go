package dag

import (
	"time"

	"go.uber.org/zap"

	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
)

// StepFSM manages the state transitions for a workflow step.
type StepFSM struct{}

// ValidTransitions defines allowed state transitions.
var ValidTransitions = map[types.StepStatus][]types.StepStatus{
	types.StepPending:   {types.StepRunning, types.StepSkipped},
	types.StepRunning:   {types.StepSuccess, types.StepFailed, types.StepTimeout, types.StepCancelled},
	types.StepFailed:    {types.StepRunning},
	types.StepTimeout:   {types.StepRunning},
	types.StepSuccess:   {},
	types.StepSkipped:   {},
	types.StepCancelled: {},
}

// CanTransition checks if moving from `from` to `to` is valid.
func CanTransition(from, to types.StepStatus) bool {
	for _, candidate := range ValidTransitions[from] {
		if candidate == to {
			return true
		}
	}
	return false
}

// TransitionWithLog validates a state transition and emits a structured log.
func TransitionWithLog(from, to types.StepStatus, logger *zap.Logger) bool {
	if logger == nil {
		logger = zap.NewNop()
	}

	valid := CanTransition(from, to)
	fields := []zap.Field{
		zap.String("from", string(from)),
		zap.String("to", string(to)),
		zap.Time("timestamp", time.Now()),
	}
	if valid {
		logger.Info("step state transition", fields...)
		return true
	}
	logger.Warn("invalid step state transition", fields...)
	return false
}

// ShouldRetry determines if a failed/timed-out step should be retried.
func ShouldRetry(step *types.Step, state *types.StepState) bool {
	if step.Retry == nil {
		return false
	}
	if state.Status != types.StepFailed && state.Status != types.StepTimeout {
		return false
	}
	return state.RetryCount < step.Retry.MaxRetries
}
