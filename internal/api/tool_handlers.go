package api

import (
	"bytes"
	"context"
	"fmt"
	"text/template"

	"github.com/Xio-Shark/agent-exec-engine/internal/mcp"
)

const dynamicHandlerTypeTemplate = "template"

// ToolHandlerSpec describes the executable behavior for dynamically registered tools.
type ToolHandlerSpec struct {
	Type     string `json:"type" binding:"required"`
	Template string `json:"template,omitempty"`
}

// BuildDynamicToolHandler converts an API request into an executable MCP tool handler.
func BuildDynamicToolHandler(name string, spec ToolHandlerSpec) (mcp.ToolHandler, error) {
	if spec.Type != dynamicHandlerTypeTemplate {
		return nil, fmt.Errorf("unsupported handler type: %s", spec.Type)
	}
	if spec.Template == "" {
		return nil, fmt.Errorf("handler template is required")
	}

	tmpl, err := template.New(name).Option("missingkey=error").Parse(spec.Template)
	if err != nil {
		return nil, fmt.Errorf("parse handler template: %w", err)
	}

	return func(_ context.Context, input map[string]any) (string, error) {
		var output bytes.Buffer
		if err := tmpl.Execute(&output, input); err != nil {
			return "", fmt.Errorf("render handler template: %w", err)
		}
		return output.String(), nil
	}, nil
}
