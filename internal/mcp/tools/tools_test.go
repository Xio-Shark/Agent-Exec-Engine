package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileReaderTool_ReadsFile(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "note.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tool := NewFileReaderTool(tempDir)
	output, err := tool.Handle(context.Background(), map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if output != "hello" {
		t.Fatalf("unexpected content: %s", output)
	}
}

func TestFileReaderTool_RejectsPathEscape(t *testing.T) {
	tempDir := t.TempDir()
	tool := NewFileReaderTool(tempDir)

	_, err := tool.Handle(context.Background(), map[string]any{"path": "../secret.txt"})
	if err == nil {
		t.Fatal("expected path escape error")
	}
}

func TestFileReaderTool_RejectsLargeFiles(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "large.txt")
	largeContent := strings.Repeat("a", int(maxFileSizeBytes)+1)
	if err := os.WriteFile(filePath, []byte(largeContent), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tool := NewFileReaderTool(tempDir)
	_, err := tool.Handle(context.Background(), map[string]any{"path": "large.txt"})
	if err == nil {
		t.Fatal("expected file size error")
	}
}

func TestSQLQueryTool_RejectsNonSelect(t *testing.T) {
	tool := NewSQLQueryTool("")

	_, err := tool.Handle(context.Background(), map[string]any{"sql": "DELETE FROM users"})
	if err == nil {
		t.Fatal("expected non-select rejection")
	}
}

func TestWebSearchTool_UsesStubWithoutAPIKey(t *testing.T) {
	tool := NewWebSearchTool("")

	output, err := tool.Handle(context.Background(), map[string]any{"query": "agent execution"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(output, "stub") {
		t.Fatalf("expected stub output, got %s", output)
	}
}
