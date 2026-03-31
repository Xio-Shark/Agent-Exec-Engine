package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics for the execution engine.
type Metrics struct {
	// Workflow metrics
	WorkflowsTotal   *prometheus.CounterVec
	WorkflowDuration *prometheus.HistogramVec

	// Step metrics
	StepDuration *prometheus.HistogramVec
	StepRetries  *prometheus.CounterVec

	// Tool metrics
	ToolCallsTotal   *prometheus.CounterVec
	ToolCallDuration *prometheus.HistogramVec
	ToolErrors       *prometheus.CounterVec

	// Sandbox metrics
	SandboxCreated  prometheus.Counter
	SandboxOOM      prometheus.Counter
	SandboxTimeout  prometheus.Counter
	SandboxDuration prometheus.Histogram

	// Token metrics
	TokensUsed *prometheus.CounterVec

	// Checkpoint metrics
	CheckpointSaved  prometheus.Counter
	CheckpointLoaded prometheus.Counter

	// Rate limiter metrics
	RateLimitRejected *prometheus.CounterVec
}

// NewMetrics registers and returns all Prometheus metrics.
func NewMetrics() *Metrics {
	return NewMetricsWithRegisterer(prometheus.DefaultRegisterer)
}

// NewMetricsWithRegisterer registers metrics on the provided registerer.
func NewMetricsWithRegisterer(registerer prometheus.Registerer) *Metrics {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	factory := promauto.With(registerer)

	return &Metrics{
		WorkflowsTotal: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_exec_workflows_total",
			Help: "Total workflows by status",
		}, []string{"status"}),

		WorkflowDuration: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "agent_exec_workflow_duration_seconds",
			Help:    "Workflow execution duration",
			Buckets: prometheus.ExponentialBuckets(0.1, 2, 15), // 0.1s to ~3276s
		}, []string{"workflow_name"}),

		StepDuration: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "agent_exec_step_duration_seconds",
			Help:    "Per-step execution duration",
			Buckets: prometheus.ExponentialBuckets(0.01, 2, 12),
		}, []string{"step_type", "status"}),

		StepRetries: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_exec_step_retries_total",
			Help: "Total step retries",
		}, []string{"step_id"}),

		ToolCallsTotal: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_exec_tool_calls_total",
			Help: "Total tool calls by tool name and status",
		}, []string{"tool_name", "status"}),

		ToolCallDuration: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "agent_exec_tool_call_duration_seconds",
			Help:    "Tool call execution duration",
			Buckets: prometheus.ExponentialBuckets(0.01, 2, 10),
		}, []string{"tool_name"}),

		ToolErrors: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_exec_tool_errors_total",
			Help: "Total tool call errors",
		}, []string{"tool_name", "error_type"}),

		SandboxCreated: factory.NewCounter(prometheus.CounterOpts{
			Name: "agent_exec_sandbox_created_total",
			Help: "Total sandbox containers created",
		}),

		SandboxOOM: factory.NewCounter(prometheus.CounterOpts{
			Name: "agent_exec_sandbox_oom_total",
			Help: "Total sandbox OOM kills",
		}),

		SandboxTimeout: factory.NewCounter(prometheus.CounterOpts{
			Name: "agent_exec_sandbox_timeout_total",
			Help: "Total sandbox timeouts",
		}),

		SandboxDuration: factory.NewHistogram(prometheus.HistogramOpts{
			Name:    "agent_exec_sandbox_duration_seconds",
			Help:    "Sandbox container lifetime duration",
			Buckets: prometheus.ExponentialBuckets(0.1, 2, 10),
		}),

		TokensUsed: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_exec_tokens_used_total",
			Help: "Total LLM tokens consumed, attributed per step",
		}, []string{"step_id", "token_type"}), // token_type: "input" or "output"

		CheckpointSaved: factory.NewCounter(prometheus.CounterOpts{
			Name: "agent_exec_checkpoint_saved_total",
			Help: "Total checkpoints saved",
		}),

		CheckpointLoaded: factory.NewCounter(prometheus.CounterOpts{
			Name: "agent_exec_checkpoint_loaded_total",
			Help: "Total checkpoints loaded (recoveries)",
		}),

		RateLimitRejected: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_exec_ratelimit_rejected_total",
			Help: "Total rate-limited tool call rejections",
		}, []string{"tool_name"}),
	}
}
