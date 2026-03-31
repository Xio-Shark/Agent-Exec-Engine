package sandbox

import (
	"archive/tar"
	"bytes"
	"testing"
	"time"
)

func TestBuildWorkspaceArchive(t *testing.T) {
	archive, err := buildWorkspaceArchive(map[string][]byte{
		"/workspace/input.txt":          []byte("hello"),
		"/workspace/nested/config.json": []byte(`{"ok":true}`),
	})
	if err != nil {
		t.Fatalf("build workspace archive: %v", err)
	}

	reader := tar.NewReader(bytes.NewReader(archive))
	found := map[string]bool{}
	for {
		header, err := reader.Next()
		if err != nil {
			break
		}
		found[header.Name] = true
	}

	if !found["input.txt"] {
		t.Fatalf("expected input.txt in archive, found=%v", found)
	}
	if !found["nested/config.json"] {
		t.Fatalf("expected nested/config.json in archive, found=%v", found)
	}
}

func TestNormalizeWorkspacePathRejectsTraversal(t *testing.T) {
	if _, err := normalizeWorkspacePath("../../etc/passwd"); err == nil {
		t.Fatal("expected traversal path to be rejected")
	}
}

func TestMergePolicyKeepsDefaults(t *testing.T) {
	merged := mergePolicy(DefaultPolicy(), ExecutionPolicy{
		Timeout:     5 * time.Second,
		NetworkMode: "bridge",
	})

	if merged.Timeout != 5*time.Second {
		t.Fatalf("expected timeout override, got %s", merged.Timeout)
	}
	if merged.NetworkMode != "bridge" {
		t.Fatalf("expected network override, got %s", merged.NetworkMode)
	}
	if merged.MemoryLimit != DefaultPolicy().MemoryLimit {
		t.Fatalf("expected memory limit default to remain, got %d", merged.MemoryLimit)
	}
}
