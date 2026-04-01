package mcp

import (
	"strings"
	"testing"

	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
)

func TestRegistry_RegisterRejectsNilHandler(t *testing.T) {
	registry := NewRegistry()

	err := registry.Register(types.ToolDefinition{
		Name:        "broken",
		Description: "missing handler",
		InputSchema: types.ToolSchema{Type: "object"},
	}, nil)
	if err == nil {
		t.Fatal("expected nil handler registration to fail")
	}
	if !strings.Contains(err.Error(), "handler") {
		t.Fatalf("expected handler error, got %v", err)
	}
}
