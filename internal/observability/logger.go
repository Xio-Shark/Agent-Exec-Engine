package observability

import (
	"context"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Logger wraps zap for structured logging with trace correlation.
type Logger struct {
	*zap.Logger
}

// NewLogger creates a production-ready structured logger.
func NewLogger() (*Logger, error) {
	cfg := zap.NewProductionConfig()
	cfg.OutputPaths = []string{"stdout"}
	cfg.Level = zap.NewAtomicLevelAt(zap.InfoLevel)

	logger, err := cfg.Build(
		zap.AddCallerSkip(1),
	)
	if err != nil {
		return nil, err
	}

	return &Logger{Logger: logger}, nil
}

// WithRunID returns a logger with run_id field attached.
func (l *Logger) WithRunID(runID string) *Logger {
	return &Logger{Logger: l.With(zap.String("run_id", runID))}
}

// WithStepID returns a logger with step_id field attached.
func (l *Logger) WithStepID(stepID string) *Logger {
	return &Logger{Logger: l.With(zap.String("step_id", stepID))}
}

// WithContext extracts trace context and attaches to log fields.
func (l *Logger) WithContext(ctx context.Context) *Logger {
	if l == nil || ctx == nil {
		return l
	}

	spanCtx := trace.SpanFromContext(ctx).SpanContext()
	if !spanCtx.IsValid() {
		return l
	}

	return &Logger{Logger: l.With(
		zap.String("trace_id", spanCtx.TraceID().String()),
		zap.String("span_id", spanCtx.SpanID().String()),
	)}
}
