package sandbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"go.opentelemetry.io/otel/trace"

	"github.com/Xio-Shark/agent-exec-engine/internal/observability"
)

const (
	sandboxLabelKey   = "agent-exec-sandbox"
	sandboxLabelValue = "true"
	workspaceDir      = "/workspace"
	outputDir         = "/output"
	tmpDir            = "/tmp"
	cpuPeriodMicros   = 100000
	dockerPingTimeout = 5 * time.Second
	cleanupTimeout    = 10 * time.Second
	immediateStop     = 0
)

// Runner executes sandbox requests.
type Runner interface {
	Execute(context.Context, ExecutionRequest) (*ExecutionResult, error)
}

// ExecutionPolicy defines resource limits and isolation rules for sandbox containers.
type ExecutionPolicy struct {
	CPUQuota     int64         `json:"cpu_quota"`     // CPU quota in microseconds (100000 = 1 core)
	MemoryLimit  int64         `json:"memory_limit"`  // bytes
	PidsLimit    int64         `json:"pids_limit"`    // max number of processes
	DiskLimit    int64         `json:"disk_limit"`    // bytes, tmpfs size
	Timeout      time.Duration `json:"timeout"`       // hardkill after this duration
	NetworkMode  string        `json:"network_mode"`  // "none", "host", "bridge"
	ReadOnlyFS   bool          `json:"read_only_fs"`  // mount root filesystem as read-only
	AllowedHosts []string      `json:"allowed_hosts"` // outbound whitelist (for bridge mode)
}

// DefaultPolicy returns a conservative default policy.
func DefaultPolicy() ExecutionPolicy {
	return ExecutionPolicy{
		CPUQuota:    100000,
		MemoryLimit: 256 * 1024 * 1024,
		PidsLimit:   64,
		DiskLimit:   64 * 1024 * 1024,
		Timeout:     30 * time.Second,
		NetworkMode: "none",
		ReadOnlyFS:  true,
	}
}

// ExecutionRequest describes what to run inside the sandbox.
type ExecutionRequest struct {
	Image   string            `json:"image"`
	Command []string          `json:"command"`
	Env     map[string]string `json:"env"`
	Files   map[string][]byte `json:"files"`
	Policy  ExecutionPolicy   `json:"policy"`
}

// ExecutionResult captures the sandbox output.
type ExecutionResult struct {
	ExitCode  int               `json:"exit_code"`
	Stdout    string            `json:"stdout"`
	Stderr    string            `json:"stderr"`
	Duration  time.Duration     `json:"duration"`
	OOMKilled bool              `json:"oom_killed"`
	TimedOut  bool              `json:"timed_out"`
	Files     map[string][]byte `json:"files,omitempty"`
}

// Executor manages sandboxed container execution.
type Executor struct {
	dockerCli     *client.Client
	defaultPolicy ExecutionPolicy
	tracer        *observability.Tracer
	metrics       *observability.Metrics
}

// ExecutorOption configures the sandbox executor.
type ExecutorOption func(*Executor)

// WithTracer attaches tracing to sandbox execution.
func WithTracer(tracer *observability.Tracer) ExecutorOption {
	return func(e *Executor) {
		e.tracer = tracer
	}
}

// WithMetrics attaches Prometheus metrics to sandbox execution.
func WithMetrics(metrics *observability.Metrics) ExecutorOption {
	return func(e *Executor) {
		e.metrics = metrics
	}
}

// NewExecutor creates a sandbox executor with Docker.
func NewExecutor(opts ...ExecutorOption) (*Executor, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), dockerPingTimeout)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		if closeErr := cli.Close(); closeErr != nil {
			return nil, errors.Join(fmt.Errorf("ping docker daemon: %w", err), fmt.Errorf("close docker client: %w", closeErr))
		}
		return nil, fmt.Errorf("ping docker daemon: %w", err)
	}

	executor := &Executor{
		dockerCli:     cli,
		defaultPolicy: DefaultPolicy(),
	}
	for _, opt := range opts {
		opt(executor)
	}
	return executor, nil
}

// Execute runs a command inside an isolated Docker container.
func (e *Executor) Execute(ctx context.Context, req ExecutionRequest) (_ *ExecutionResult, retErr error) {
	if err := validateExecutionRequest(req); err != nil {
		return nil, err
	}

	started := time.Now()
	ctx, span := e.startSandboxSpan(ctx, req.Image)
	defer span.End()

	policy := mergePolicy(e.defaultPolicy, req.Policy)
	containerID, err := e.createContainer(ctx, req, policy)
	if err != nil {
		return nil, err
	}
	if e.metrics != nil {
		e.metrics.SandboxCreated.Inc()
	}
	defer func() {
		retErr = errors.Join(retErr, e.removeContainer(containerID))
	}()

	copyErr := e.copyInputFiles(ctx, containerID, req.Files)
	if copyErr != nil {
		return nil, copyErr
	}
	startErr := e.dockerCli.ContainerStart(ctx, containerID, container.StartOptions{})
	if startErr != nil {
		return nil, fmt.Errorf("start container %s: %w", containerID, startErr)
	}

	exitCode, timedOut, err := e.waitForCompletion(ctx, containerID, policy.Timeout)
	if err != nil {
		return nil, err
	}

	postCtx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()

	stdout, stderr, err := e.collectLogs(postCtx, containerID)
	if err != nil {
		return nil, err
	}

	inspect, err := e.dockerCli.ContainerInspect(postCtx, containerID)
	if err != nil {
		return nil, fmt.Errorf("inspect container %s: %w", containerID, err)
	}

	files, err := e.collectOutputFiles(postCtx, containerID)
	if err != nil {
		return nil, err
	}
	if inspect.State != nil {
		exitCode = inspect.State.ExitCode
	}

	result := &ExecutionResult{
		ExitCode:  exitCode,
		Stdout:    stdout,
		Stderr:    stderr,
		Duration:  time.Since(started),
		OOMKilled: inspect.State != nil && inspect.State.OOMKilled,
		TimedOut:  timedOut,
		Files:     files,
	}
	e.observeExecutionResult(result)
	return result, nil
}

// Cleanup removes all sandbox-related containers and resources.
func (e *Executor) Cleanup(ctx context.Context) error {
	containers, err := e.dockerCli.ContainerList(ctx, container.ListOptions{
		All: true,
		Filters: filters.NewArgs(filters.Arg(
			"label",
			fmt.Sprintf("%s=%s", sandboxLabelKey, sandboxLabelValue),
		)),
	})
	if err != nil {
		return fmt.Errorf("list sandbox containers: %w", err)
	}

	var errs []error
	for _, item := range containers {
		removeErr := e.dockerCli.ContainerRemove(ctx, item.ID, container.RemoveOptions{Force: true})
		if removeErr != nil && !errdefs.IsNotFound(removeErr) {
			errs = append(errs, fmt.Errorf("remove container %s: %w", item.ID, removeErr))
		}
	}
	return errors.Join(errs...)
}

func (e *Executor) startSandboxSpan(ctx context.Context, image string) (context.Context, trace.Span) {
	if e == nil || e.tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return e.tracer.StartSandboxSpan(ctx, image)
}

func (e *Executor) observeExecutionResult(result *ExecutionResult) {
	if e == nil || e.metrics == nil || result == nil {
		return
	}
	e.metrics.SandboxDuration.Observe(result.Duration.Seconds())
	if result.OOMKilled {
		e.metrics.SandboxOOM.Inc()
	}
	if result.TimedOut {
		e.metrics.SandboxTimeout.Inc()
	}
}
