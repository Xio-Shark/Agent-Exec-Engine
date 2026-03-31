package mcp

import (
	"fmt"
	"math"

	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
)

// ValidateInput validates tool input against the declared schema.
func ValidateInput(schema types.ToolSchema, input map[string]any) error {
	if input == nil {
		input = map[string]any{}
	}

	for _, field := range schema.Required {
		if _, ok := input[field]; !ok {
			return fmt.Errorf("missing required field: %s", field)
		}
	}

	for name, value := range input {
		property, ok := schema.Properties[name]
		if !ok {
			continue
		}
		if !matchesType(property.Type, value) {
			return fmt.Errorf("field %s must be %s", name, property.Type)
		}
		if len(property.Enum) > 0 {
			stringValue, ok := value.(string)
			if !ok {
				return fmt.Errorf("field %s must be string for enum validation", name)
			}
			if !contains(property.Enum, stringValue) {
				return fmt.Errorf("field %s must be one of %v", name, property.Enum)
			}
		}
	}

	return nil
}

func matchesType(expected string, value any) bool {
	switch expected {
	case "string":
		_, ok := value.(string)
		return ok
	case "integer":
		switch v := value.(type) {
		case int, int8, int16, int32, int64:
			return true
		case uint, uint8, uint16, uint32, uint64:
			return true
		case float64:
			return math.Mod(v, 1) == 0
		default:
			return false
		}
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "object":
		_, ok := value.(map[string]any)
		return ok
	default:
		return true
	}
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
