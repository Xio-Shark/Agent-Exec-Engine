package sandbox

import "context"

// Pool limits concurrent sandbox executions with a buffered channel semaphore.
type Pool struct {
	sem      chan struct{}
	executor *Executor
}

// NewPool creates a concurrency-limited sandbox execution pool.
func NewPool(maxConcurrent int, executor *Executor) *Pool {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	return &Pool{
		sem:      make(chan struct{}, maxConcurrent),
		executor: executor,
	}
}

// Execute acquires a slot, runs the sandbox request, and releases the slot.
func (p *Pool) Execute(ctx context.Context, req ExecutionRequest) (*ExecutionResult, error) {
	select {
	case p.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() {
		<-p.sem
	}()

	return p.executor.Execute(ctx, req)
}
