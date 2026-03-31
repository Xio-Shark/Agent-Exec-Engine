package sandbox

import (
	"context"
	"fmt"
	"io"
	"slices"

	"github.com/docker/docker/api/types/image"
)

var defaultImages = []string{
	"python:3.12-slim",
	"alpine:3.19",
	"node:20-slim",
}

// DefaultImages returns the common sandbox images that should be pre-pulled on startup.
func DefaultImages() []string {
	return slices.Clone(defaultImages)
}

// PrePullImages pulls sandbox images eagerly so the first execution avoids image-download latency.
func (e *Executor) PrePullImages(ctx context.Context, images []string) error {
	if len(images) == 0 {
		images = DefaultImages()
	}

	for _, imageRef := range images {
		reader, err := e.dockerCli.ImagePull(ctx, imageRef, image.PullOptions{})
		if err != nil {
			return fmt.Errorf("pull image %s: %w", imageRef, err)
		}
		if _, err := io.Copy(io.Discard, reader); err != nil {
			reader.Close()
			return fmt.Errorf("drain image pull %s: %w", imageRef, err)
		}
		if err := reader.Close(); err != nil {
			return fmt.Errorf("close image pull stream %s: %w", imageRef, err)
		}
	}
	return nil
}
