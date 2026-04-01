package dag

import "github.com/Xio-Shark/agent-exec-engine/pkg/types"

// Snapshot returns a thread-safe copy of the current workflow run state.
func (s *Scheduler) Snapshot() *types.WorkflowRun {
	return s.currentRun()
}
