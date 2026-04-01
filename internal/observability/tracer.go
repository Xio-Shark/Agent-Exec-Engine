package observability

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "agent-exec-engine"

// Tracer wraps OpenTelemetry tracer for Agent execution tracing.
type Tracer struct {
	tracer   trace.Tracer
	provider *sdktrace.TracerProvider
}

// NewTracer creates a tracer with a real OTLP gRPC exporter.
func NewTracer(ctx context.Context, serviceName, otlpEndpoint string) (*Tracer, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	name := strings.TrimSpace(serviceName)
	if name == "" {
		name = tracerName
	}

	endpoint := strings.TrimSpace(otlpEndpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("otlp endpoint must not be empty")
	}

	exporter, err := otlptracegrpc.New(
		ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("create otlp trace exporter: %w", err)
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(semconv.ServiceNameKey.String(name)),
	)
	if err != nil {
		return nil, fmt.Errorf("build otel resource: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(provider)

	return &Tracer{
		tracer:   provider.Tracer(tracerName),
		provider: provider,
	}, nil
}

// Shutdown flushes and stops the tracer provider.
func (t *Tracer) Shutdown(ctx context.Context) error {
	if t == nil || t.provider == nil {
		return nil
	}
	return t.provider.Shutdown(ctx)
}

// StartWorkflowSpan creates a root span for a workflow execution.
func (t *Tracer) StartWorkflowSpan(ctx context.Context, runID, workflowName string) (context.Context, trace.Span) {
	return t.tracer.Start(ctx, "workflow.execute",
		trace.WithAttributes(
			attribute.String("workflow.run_id", runID),
			attribute.String("workflow.name", workflowName),
		),
	)
}

// StartStepSpan creates a child span for a workflow step.
func (t *Tracer) StartStepSpan(ctx context.Context, stepID string, stepType string) (context.Context, trace.Span) {
	return t.tracer.Start(ctx, "step.execute",
		trace.WithAttributes(
			attribute.String("step.id", stepID),
			attribute.String("step.type", stepType),
		),
	)
}

// StartToolSpan creates a child span for a tool call.
func (t *Tracer) StartToolSpan(ctx context.Context, toolName string) (context.Context, trace.Span) {
	return t.tracer.Start(ctx, "tool.call",
		trace.WithAttributes(
			attribute.String("tool.name", toolName),
		),
	)
}

// StartSandboxSpan creates a child span for sandbox execution.
func (t *Tracer) StartSandboxSpan(ctx context.Context, image string) (context.Context, trace.Span) {
	return t.tracer.Start(ctx, "sandbox.execute",
		trace.WithAttributes(
			attribute.String("sandbox.image", image),
		),
	)
}
