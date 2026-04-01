package observability

import (
	"context"
	"testing"
	"time"
)

func TestNewTracer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	tracer, err := NewTracer(ctx, "agent-exec-engine-test", "127.0.0.1:4317")
	if err != nil {
		t.Fatalf("NewTracer() error = %v", err)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
	defer shutdownCancel()
	if err := tracer.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}
