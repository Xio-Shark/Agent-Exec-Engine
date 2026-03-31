package mcp

import (
	"testing"

	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
)

func TestValidator_RequiredFields(t *testing.T) {
	schema := types.ToolSchema{
		Type: "object",
		Properties: map[string]types.Property{
			"message": {Type: "string"},
		},
		Required: []string{"message"},
	}

	err := ValidateInput(schema, map[string]any{})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidator_TypeMismatch(t *testing.T) {
	schema := types.ToolSchema{
		Type: "object",
		Properties: map[string]types.Property{
			"count": {Type: "integer"},
		},
	}

	err := ValidateInput(schema, map[string]any{"count": "oops"})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidator_EnumMismatch(t *testing.T) {
	schema := types.ToolSchema{
		Type: "object",
		Properties: map[string]types.Property{
			"language": {Type: "string", Enum: []string{"python", "bash"}},
		},
	}

	err := ValidateInput(schema, map[string]any{"language": "ruby"})
	if err == nil {
		t.Fatal("expected enum validation error")
	}
}
