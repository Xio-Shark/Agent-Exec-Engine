package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
)

const maxFileSizeBytes int64 = 1 << 20

// FileReaderTool reads files from a bounded workspace.
type FileReaderTool struct {
	basePath string
}

func NewFileReaderTool(basePath string) *FileReaderTool {
	if strings.TrimSpace(basePath) == "" {
		basePath = "."
	}
	return &FileReaderTool{basePath: basePath}
}

func (t *FileReaderTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "file_reader",
		Description: "Read a file from the workspace.",
		InputSchema: types.ToolSchema{
			Type: "object",
			Properties: map[string]types.Property{
				"path": {Type: "string", Description: "File path relative to workspace root"},
			},
			Required: []string{"path"},
		},
		Category: "file",
	}
}

func (t *FileReaderTool) Handle(ctx context.Context, input map[string]any) (string, error) {
	_ = ctx

	path, ok := input["path"].(string)
	if !ok || strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path must be a non-empty string")
	}

	resolvedPath, err := t.resolvePath(path)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		return "", fmt.Errorf("stat file: %w", err)
	}
	if info.Size() > maxFileSizeBytes {
		return "", fmt.Errorf("file exceeds %d bytes", maxFileSizeBytes)
	}

	content, err := os.ReadFile(resolvedPath)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	return string(content), nil
}

func (t *FileReaderTool) resolvePath(path string) (string, error) {
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("absolute paths are not allowed")
	}

	basePath, err := filepath.Abs(t.basePath)
	if err != nil {
		return "", fmt.Errorf("resolve base path: %w", err)
	}

	candidate := filepath.Clean(filepath.Join(basePath, path))
	relativePath, err := filepath.Rel(basePath, candidate)
	if err != nil {
		return "", fmt.Errorf("resolve relative path: %w", err)
	}
	if relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes base path")
	}
	return candidate, nil
}
