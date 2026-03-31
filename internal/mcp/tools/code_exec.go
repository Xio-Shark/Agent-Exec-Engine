package tools

import (
	"context"
	"fmt"

	"github.com/Xio-Shark/agent-exec-engine/internal/sandbox"
	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
)

type sandboxExecutor interface {
	Execute(ctx context.Context, req sandbox.ExecutionRequest) (*sandbox.ExecutionResult, error)
}

// CodeExecTool executes code inside a sandbox container.
type CodeExecTool struct {
	executor sandboxExecutor
}

func NewCodeExecTool(executor sandboxExecutor) *CodeExecTool {
	return &CodeExecTool{executor: executor}
}

func (t *CodeExecTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "code_exec",
		Description: "Execute code in an isolated sandbox. Supports Python and shell scripts.",
		InputSchema: types.ToolSchema{
			Type: "object",
			Properties: map[string]types.Property{
				"language": {Type: "string", Description: "Programming language", Enum: []string{"python", "bash"}},
				"code":     {Type: "string", Description: "Code to execute"},
			},
			Required: []string{"language", "code"},
		},
		Sandboxed: true,
		RateLimit: 30,
		Category:  "code",
	}
}

func (t *CodeExecTool) Handle(ctx context.Context, input map[string]any) (string, error) {
	lang, _ := input["language"].(string)
	code, _ := input["code"].(string)

	var image string
	var cmd []string

	switch lang {
	case "python":
		image = "python:3.12-slim"
		cmd = []string{"python", "-c", code}
	case "bash":
		image = "alpine:3.19"
		cmd = []string{"sh", "-c", code}
	default:
		return "", fmt.Errorf("unsupported language: %s", lang)
	}

	result, err := t.executor.Execute(ctx, sandbox.ExecutionRequest{
		Image:   image,
		Command: cmd,
		Policy:  sandbox.DefaultPolicy(),
	})
	if err != nil {
		return "", fmt.Errorf("sandbox execution failed: %w", err)
	}
	if result.OOMKilled {
		return "", fmt.Errorf("execution killed: out of memory")
	}
	if result.TimedOut {
		return "", fmt.Errorf("execution timed out")
	}

	output := result.Stdout
	if result.Stderr != "" {
		output += "\n[stderr]\n" + result.Stderr
	}
	if result.ExitCode != 0 {
		return output, fmt.Errorf("exit code %d", result.ExitCode)
	}
	return output, nil
}
