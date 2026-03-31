package dag

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
)

type mockExecutor struct {
	mu      sync.Mutex
	output  string
	delay   time.Duration
	outputs map[string]string
	errors  map[string][]error
	inputs  map[string][]map[string]any
	calls   map[string]int
	started chan string
}

func (m *mockExecutor) Execute(ctx context.Context, step *types.Step, input map[string]any) (string, error) {
	m.recordCall(step.ID, input)
	m.notifyStart(step.ID)

	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	if err := m.nextError(step.ID); err != nil {
		return "", err
	}

	if output, ok := m.outputFor(step.ID); ok {
		return output, nil
	}
	if m.output != "" {
		return m.output, nil
	}
	return fmt.Sprintf("{\"step\":\"%s\"}", step.ID), nil
}

func (m *mockExecutor) recordCall(stepID string, input map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.inputs == nil {
		m.inputs = make(map[string][]map[string]any)
	}
	if m.calls == nil {
		m.calls = make(map[string]int)
	}
	m.calls[stepID]++
	m.inputs[stepID] = append(m.inputs[stepID], cloneMap(input))
}

func (m *mockExecutor) notifyStart(stepID string) {
	if m.started == nil {
		return
	}
	select {
	case m.started <- stepID:
	default:
	}
}

func (m *mockExecutor) nextError(stepID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	queue := m.errors[stepID]
	if len(queue) == 0 {
		return nil
	}
	err := queue[0]
	m.errors[stepID] = queue[1:]
	return err
}

func (m *mockExecutor) outputFor(stepID string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.outputs == nil {
		return "", false
	}
	output, ok := m.outputs[stepID]
	return output, ok
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}

	data, err := json.Marshal(input)
	if err != nil {
		return map[string]any{"marshal_error": err.Error()}
	}

	var cloned map[string]any
	if err := json.Unmarshal(data, &cloned); err != nil {
		return map[string]any{"unmarshal_error": err.Error()}
	}
	return cloned
}
