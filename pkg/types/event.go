package types

import "time"

// EventType categorizes lifecycle events.
type EventType string

const (
	EventWorkflowStarted   EventType = "workflow.started"
	EventWorkflowCompleted EventType = "workflow.completed"
	EventWorkflowFailed    EventType = "workflow.failed"
	EventWorkflowPaused    EventType = "workflow.paused"
	EventStepStarted       EventType = "step.started"
	EventStepCompleted     EventType = "step.completed"
	EventStepFailed        EventType = "step.failed"
	EventStepTimeout       EventType = "step.timeout"
	EventStepRetry         EventType = "step.retry"
	EventToolCalled        EventType = "tool.called"
	EventToolCompleted     EventType = "tool.completed"
	EventToolError         EventType = "tool.error"
	EventSandboxCreated    EventType = "sandbox.created"
	EventSandboxDestroyed  EventType = "sandbox.destroyed"
	EventSandboxOOM        EventType = "sandbox.oom"
	EventCheckpointSaved   EventType = "checkpoint.saved"
	EventCheckpointLoaded  EventType = "checkpoint.loaded"
)

// Event represents a lifecycle event in the execution engine.
type Event struct {
	Type      EventType      `json:"type"`
	Timestamp time.Time      `json:"timestamp"`
	RunID     string         `json:"run_id,omitempty"`
	StepID    string         `json:"step_id,omitempty"`
	ToolName  string         `json:"tool_name,omitempty"`
	Message   string         `json:"message,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}
