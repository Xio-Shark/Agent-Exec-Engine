package types

import "time"

// StepStatus represents the state of a workflow step.
type StepStatus string

const (
	StepPending   StepStatus = "pending"
	StepRunning   StepStatus = "running"
	StepSuccess   StepStatus = "success"
	StepFailed    StepStatus = "failed"
	StepTimeout   StepStatus = "timeout"
	StepSkipped   StepStatus = "skipped"
	StepCancelled StepStatus = "canceled"
)

// WorkflowStatus represents the state of an entire workflow.
type WorkflowStatus string

const (
	WorkflowPending   WorkflowStatus = "pending"
	WorkflowRunning   WorkflowStatus = "running"
	WorkflowCompleted WorkflowStatus = "completed"
	WorkflowFailed    WorkflowStatus = "failed"
	WorkflowPaused    WorkflowStatus = "paused" // awaiting human intervention
)

// StepType defines what kind of action a step performs.
type StepType string

const (
	StepTypeLLMCall  StepType = "llm_call"
	StepTypeToolCall StepType = "tool_call"
	StepTypeHuman    StepType = "human"    // human-in-the-loop approval
	StepTypeBranch   StepType = "branch"   // conditional routing
	StepTypeParallel StepType = "parallel" // fan-out sub-steps
)

// RetryPolicy configures retry behavior for a step.
type RetryPolicy struct {
	MaxRetries  int           `json:"max_retries"`
	Backoff     time.Duration `json:"backoff"` // initial backoff
	MaxBackoff  time.Duration `json:"max_backoff"`
	BackoffMult float64       `json:"backoff_mult"` // multiplier per retry
}

// Step is a single node in the workflow DAG.
type Step struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Type      StepType          `json:"type"`
	DependsOn []string          `json:"depends_on,omitempty"` // step IDs this step waits for
	Config    map[string]any    `json:"config,omitempty"`     // type-specific config
	ToolName  string            `json:"tool,omitempty"`       // for tool_call type
	Timeout   time.Duration     `json:"timeout,omitempty"`    // 0 = use default
	Retry     *RetryPolicy      `json:"retry,omitempty"`
	Condition string            `json:"condition,omitempty"` // CEL expression for branch
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// StepState tracks runtime state of a step.
type StepState struct {
	StepID      string     `json:"step_id"`
	Status      StepStatus `json:"status"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Output      string     `json:"output,omitempty"`
	Error       string     `json:"error,omitempty"`
	RetryCount  int        `json:"retry_count"`
	TokensUsed  int64      `json:"tokens_used"`
}

// Workflow is the top-level workflow definition.
type Workflow struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Steps     []Step            `json:"steps"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

// WorkflowRun tracks the execution of a workflow instance.
type WorkflowRun struct {
	ID          string                `json:"id"`
	WorkflowID  string                `json:"workflow_id"`
	Status      WorkflowStatus        `json:"status"`
	StepStates  map[string]*StepState `json:"step_states"`
	Input       map[string]any        `json:"input,omitempty"`
	Output      map[string]any        `json:"output,omitempty"`
	StartedAt   time.Time             `json:"started_at"`
	CompletedAt *time.Time            `json:"completed_at,omitempty"`
}

// Checkpoint captures the full state of a workflow run for persistence/recovery.
type Checkpoint struct {
	RunID      string                `json:"run_id"`
	WorkflowID string                `json:"workflow_id"`
	StepStates map[string]*StepState `json:"step_states"`
	Timestamp  time.Time             `json:"timestamp"`
	Version    int                   `json:"version"` // optimistic locking
}
