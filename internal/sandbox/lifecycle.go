package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/errdefs"
	"github.com/docker/docker/pkg/stdcopy"
)

func validateExecutionRequest(req ExecutionRequest) error {
	switch {
	case req.Image == "":
		return errors.New("sandbox image is required")
	case len(req.Command) == 0:
		return errors.New("sandbox command is required")
	case len(req.Policy.AllowedHosts) > 0:
		return errors.New("sandbox allowed_hosts is not implemented for docker executor")
	default:
		return nil
	}
}

func mergePolicy(base, override ExecutionPolicy) ExecutionPolicy {
	if override.CPUQuota > 0 {
		base.CPUQuota = override.CPUQuota
	}
	if override.MemoryLimit > 0 {
		base.MemoryLimit = override.MemoryLimit
	}
	if override.PidsLimit > 0 {
		base.PidsLimit = override.PidsLimit
	}
	if override.DiskLimit > 0 {
		base.DiskLimit = override.DiskLimit
	}
	if override.Timeout > 0 {
		base.Timeout = override.Timeout
	}
	if override.NetworkMode != "" {
		base.NetworkMode = override.NetworkMode
	}
	if override.ReadOnlyFS != base.ReadOnlyFS {
		base.ReadOnlyFS = override.ReadOnlyFS
	}
	if len(override.AllowedHosts) > 0 {
		base.AllowedHosts = slices.Clone(override.AllowedHosts)
	}
	return base
}

func (e *Executor) createContainer(ctx context.Context, req ExecutionRequest, policy ExecutionPolicy) (string, error) {
	pidsLimit := policy.PidsLimit
	resp, err := e.dockerCli.ContainerCreate(
		ctx,
		&container.Config{
			Image:      req.Image,
			Cmd:        req.Command,
			Env:        envToSlice(req.Env),
			WorkingDir: workspaceDir,
			Labels: map[string]string{
				sandboxLabelKey: sandboxLabelValue,
			},
		},
		&container.HostConfig{
			Resources: container.Resources{
				CPUPeriod: cpuPeriodMicros,
				CPUQuota:  policy.CPUQuota,
				Memory:    policy.MemoryLimit,
				PidsLimit: &pidsLimit,
			},
			ReadonlyRootfs: policy.ReadOnlyFS,
			Tmpfs:          tmpfsMounts(policy.DiskLimit),
			NetworkMode:    container.NetworkMode(policy.NetworkMode),
		},
		nil,
		nil,
		"",
	)
	if err != nil {
		return "", fmt.Errorf("create container for image %s: %w", req.Image, err)
	}
	return resp.ID, nil
}

func envToSlice(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}

	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	values := make([]string, 0, len(keys))
	for _, key := range keys {
		values = append(values, fmt.Sprintf("%s=%s", key, env[key]))
	}
	return values
}

func tmpfsMounts(diskLimit int64) map[string]string {
	mountSize := fmt.Sprintf("size=%d", diskLimit)
	return map[string]string{
		tmpDir:       mountSize,
		workspaceDir: mountSize,
		outputDir:    mountSize,
	}
}

func (e *Executor) copyInputFiles(ctx context.Context, containerID string, files map[string][]byte) error {
	if len(files) == 0 {
		return nil
	}

	archive, err := buildWorkspaceArchive(files)
	if err != nil {
		return err
	}
	return e.dockerCli.CopyToContainer(
		ctx,
		containerID,
		workspaceDir,
		bytes.NewReader(archive),
		container.CopyToContainerOptions{},
	)
}

func (e *Executor) waitForCompletion(ctx context.Context, containerID string, timeout time.Duration) (int, bool, error) {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	statusCh, errCh := e.dockerCli.ContainerWait(waitCtx, containerID, container.WaitConditionNotRunning)
	select {
	case waitErr := <-errCh:
		if waitErr == nil {
			return 0, false, nil
		}
		if errors.Is(waitErr, context.DeadlineExceeded) || errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
			if err := e.stopContainerImmediately(containerID); err != nil {
				return 0, true, err
			}
			return 0, true, nil
		}
		return 0, false, fmt.Errorf("wait for container %s: %w", containerID, waitErr)
	case result := <-statusCh:
		if result.Error != nil {
			return int(result.StatusCode), false, errors.New(result.Error.Message)
		}
		return int(result.StatusCode), false, nil
	}
}

func (e *Executor) stopContainerImmediately(containerID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()

	stopOptions := container.StopOptions{Timeout: intPtr(immediateStop)}
	if err := e.dockerCli.ContainerStop(ctx, containerID, stopOptions); err != nil && !errdefs.IsNotFound(err) {
		return fmt.Errorf("stop timed out container %s: %w", containerID, err)
	}
	return nil
}

func (e *Executor) collectLogs(ctx context.Context, containerID string) (string, string, error) {
	reader, err := e.dockerCli.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	if err != nil {
		return "", "", fmt.Errorf("read container logs %s: %w", containerID, err)
	}
	defer reader.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, reader); err != nil {
		return "", "", fmt.Errorf("demux logs for container %s: %w", containerID, err)
	}
	return stdout.String(), stderr.String(), nil
}

func (e *Executor) collectOutputFiles(ctx context.Context, containerID string) (map[string][]byte, error) {
	reader, _, err := e.dockerCli.CopyFromContainer(ctx, containerID, outputDir)
	if err != nil {
		if errdefs.IsNotFound(err) || strings.Contains(err.Error(), "No such container:path") {
			return nil, nil
		}
		return nil, fmt.Errorf("copy output from container %s: %w", containerID, err)
	}
	defer reader.Close()

	files, err := extractOutputArchive(reader)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}
	return files, nil
}

func (e *Executor) removeContainer(containerID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()

	removeErr := e.dockerCli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
	if removeErr != nil && !errdefs.IsNotFound(removeErr) {
		return fmt.Errorf("remove container %s: %w", containerID, removeErr)
	}
	return nil
}

func intPtr(value int) *int {
	return &value
}
