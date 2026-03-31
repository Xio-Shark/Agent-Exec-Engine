package observability

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestLogger_WithContextAddsTraceFields(t *testing.T) {
	core, recorded := observer.New(zap.InfoLevel)
	logger := &Logger{Logger: zap.New(core)}

	provider := sdktrace.NewTracerProvider()
	ctx, span := provider.Tracer("test").Start(context.Background(), "span")
	defer span.End()

	logger.WithContext(ctx).Info("hello")

	entries := recorded.All()
	if len(entries) != 1 {
		t.Fatalf("expected one log entry, got %d", len(entries))
	}
	fields := entries[0].ContextMap()
	if fields["trace_id"] == "" {
		t.Fatalf("expected trace_id field, got %#v", fields)
	}
	if fields["span_id"] == "" {
		t.Fatalf("expected span_id field, got %#v", fields)
	}
}

func TestLogger_WithContextWithoutSpanKeepsLoggerUsable(t *testing.T) {
	core, recorded := observer.New(zap.InfoLevel)
	logger := &Logger{Logger: zap.New(core)}

	logger.WithContext(context.Background()).Info("plain")

	entries := recorded.All()
	if len(entries) != 1 {
		t.Fatalf("expected one log entry, got %d", len(entries))
	}
	fields := entries[0].ContextMap()
	if _, exists := fields["trace_id"]; exists {
		t.Fatalf("did not expect trace_id field, got %#v", fields)
	}
}
