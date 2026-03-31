//go:build docker

package sandbox

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
)

var (
	dockerPrepOnce sync.Once
	dockerPrepErr  error
)

func TestExecute_PythonHelloWorld(t *testing.T) {
	executor := newDockerExecutor(t)

	result, err := executor.Execute(context.Background(), ExecutionRequest{
		Image:   "python:3.12-slim",
		Command: []string{"python", "-c", "print('hello')"},
		Policy:  DefaultPolicy(),
	})
	if err != nil {
		t.Fatalf("execute python hello world: %v", err)
	}
	if !strings.Contains(result.Stdout, "hello") {
		t.Fatalf("expected stdout to contain hello, got %q", result.Stdout)
	}
}

func TestExecute_BashCommand(t *testing.T) {
	executor := newDockerExecutor(t)

	result, err := executor.Execute(context.Background(), ExecutionRequest{
		Image:   "alpine:3.19",
		Command: []string{"sh", "-c", `echo "test" && ls /`},
		Policy:  DefaultPolicy(),
	})
	if err != nil {
		t.Fatalf("execute shell command: %v", err)
	}
	if !strings.Contains(result.Stdout, "test") {
		t.Fatalf("expected stdout to contain test, got %q", result.Stdout)
	}
}

func TestExecute_Timeout(t *testing.T) {
	executor := newDockerExecutor(t)

	result, err := executor.Execute(context.Background(), ExecutionRequest{
		Image:   "alpine:3.19",
		Command: []string{"sh", "-c", "sleep 30"},
		Policy: ExecutionPolicy{
			Timeout: 1 * time.Second,
		},
	})
	if err != nil {
		t.Fatalf("execute timeout case: %v", err)
	}
	if !result.TimedOut {
		t.Fatalf("expected timeout result, got %+v", result)
	}
}

func TestExecute_OOM(t *testing.T) {
	executor := newDockerExecutor(t)

	result, err := executor.Execute(context.Background(), ExecutionRequest{
		Image: "python:3.12-slim",
		Command: []string{
			"python",
			"-c",
			"chunks=[]\nwhile True:\n chunks.append(bytearray(1024 * 1024))",
		},
		Policy: ExecutionPolicy{
			MemoryLimit: 32 * 1024 * 1024,
			Timeout:     20 * time.Second,
		},
	})
	if err != nil {
		t.Fatalf("execute oom case: %v", err)
	}
	if !result.OOMKilled {
		t.Fatalf("expected oom kill, got %+v", result)
	}
}

func TestExecute_NetworkNone(t *testing.T) {
	executor := newDockerExecutor(t)

	result, err := executor.Execute(context.Background(), ExecutionRequest{
		Image:   "alpine:3.19",
		Command: []string{"sh", "-c", "wget -T 2 -qO- https://example.com"},
		Policy:  DefaultPolicy(),
	})
	if err != nil {
		t.Fatalf("execute network none case: %v", err)
	}
	if result.ExitCode == 0 {
		t.Fatalf("expected network-none command to fail, got %+v", result)
	}
}

func TestExecute_ReadOnlyFS(t *testing.T) {
	executor := newDockerExecutor(t)

	result, err := executor.Execute(context.Background(), ExecutionRequest{
		Image:   "alpine:3.19",
		Command: []string{"sh", "-c", "touch /test"},
		Policy:  DefaultPolicy(),
	})
	if err != nil {
		t.Fatalf("execute readonly filesystem case: %v", err)
	}
	if result.ExitCode == 0 {
		t.Fatalf("expected readonly filesystem command to fail, got %+v", result)
	}
}

func TestExecute_FileInjectAndCollect(t *testing.T) {
	executor := newDockerExecutor(t)

	result, err := executor.Execute(context.Background(), ExecutionRequest{
		Image: "alpine:3.19",
		Command: []string{
			"sh",
			"-c",
			"cat /workspace/input.txt > /output/result.txt",
		},
		Files: map[string][]byte{
			"/workspace/input.txt": []byte("sandbox"),
		},
		Policy: DefaultPolicy(),
	})
	if err != nil {
		t.Fatalf("execute file inject and collect case: %v", err)
	}
	if got := string(result.Files["result.txt"]); got != "sandbox" {
		t.Fatalf("expected collected output file to match input, got %q", got)
	}
}

func TestCleanup(t *testing.T) {
	executor := newDockerExecutor(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := executor.Cleanup(ctx); err != nil {
		t.Fatalf("cleanup existing sandbox containers: %v", err)
	}

	for i := 0; i < 3; i++ {
		_, err := executor.dockerCli.ContainerCreate(
			ctx,
			&container.Config{
				Image: "alpine:3.19",
				Cmd:   []string{"sleep", "60"},
				Labels: map[string]string{
					sandboxLabelKey: sandboxLabelValue,
				},
			},
			nil,
			nil,
			nil,
			"",
		)
		if err != nil {
			t.Fatalf("create cleanup fixture container %d: %v", i, err)
		}
	}

	if err := executor.Cleanup(ctx); err != nil {
		t.Fatalf("cleanup sandbox containers: %v", err)
	}

	containers, err := executor.dockerCli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		t.Fatalf("list containers after cleanup: %v", err)
	}
	for _, item := range containers {
		if item.Labels[sandboxLabelKey] == sandboxLabelValue {
			t.Fatalf("expected no sandbox containers after cleanup, found %s", item.ID)
		}
	}
}

func newDockerExecutor(t *testing.T) *Executor {
	t.Helper()

	executor, err := NewExecutor()
	if err != nil {
		t.Skipf("docker unavailable: %v", err)
	}
	t.Cleanup(func() {
		executor.dockerCli.Close()
	})

	dockerPrepOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		dockerPrepErr = executor.PrePullImages(ctx, []string{
			"python:3.12-slim",
			"alpine:3.19",
		})
	})
	if dockerPrepErr != nil {
		t.Fatalf("pre-pull docker images: %v", dockerPrepErr)
	}

	return executor
}
