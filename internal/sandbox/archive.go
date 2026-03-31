package sandbox

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"
)

const defaultFileMode int64 = 0o644

func buildWorkspaceArchive(files map[string][]byte) ([]byte, error) {
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)

	keys := make([]string, 0, len(files))
	for name := range files {
		keys = append(keys, name)
	}
	slices.Sort(keys)

	createdDirs := make(map[string]struct{})
	for _, name := range keys {
		relativePath, err := normalizeWorkspacePath(name)
		if err != nil {
			return nil, err
		}
		if err := writeParentDirs(writer, createdDirs, relativePath); err != nil {
			return nil, err
		}
		if err := writeArchiveFile(writer, relativePath, files[name]); err != nil {
			return nil, err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close workspace archive: %w", err)
	}
	return buffer.Bytes(), nil
}

func normalizeWorkspacePath(name string) (string, error) {
	candidate := strings.TrimSpace(strings.TrimPrefix(name, "/"))
	cleaned := path.Clean(candidate)
	cleaned = strings.TrimPrefix(cleaned, strings.TrimPrefix(workspaceDir, "/")+"/")
	if cleaned == "" || cleaned == "." {
		return "", fmt.Errorf("workspace file path %q is invalid", name)
	}
	if strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", fmt.Errorf("workspace file path %q escapes workspace", name)
	}
	return cleaned, nil
}

func writeParentDirs(writer *tar.Writer, created map[string]struct{}, filename string) error {
	dir := path.Dir(filename)
	if dir == "." {
		return nil
	}

	segments := strings.Split(dir, "/")
	current := ""
	for _, segment := range segments {
		if current == "" {
			current = segment
		} else {
			current = current + "/" + segment
		}
		if _, exists := created[current]; exists {
			continue
		}
		header := &tar.Header{
			Name:     current,
			Typeflag: tar.TypeDir,
			Mode:     0o755,
		}
		if err := writer.WriteHeader(header); err != nil {
			return fmt.Errorf("write directory %s to archive: %w", current, err)
		}
		created[current] = struct{}{}
	}
	return nil
}

func writeArchiveFile(writer *tar.Writer, filename string, content []byte) error {
	header := &tar.Header{
		Name: filename,
		Mode: defaultFileMode,
		Size: int64(len(content)),
	}
	if err := writer.WriteHeader(header); err != nil {
		return fmt.Errorf("write file header %s: %w", filename, err)
	}
	if _, err := writer.Write(content); err != nil {
		return fmt.Errorf("write file %s: %w", filename, err)
	}
	return nil
}

func extractOutputArchive(reader io.Reader) (map[string][]byte, error) {
	tarReader := tar.NewReader(reader)
	files := make(map[string][]byte)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			return files, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read output archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			continue
		}

		relativePath, ok := normalizeOutputPath(header.Name)
		if !ok {
			continue
		}

		content, err := io.ReadAll(tarReader)
		if err != nil {
			return nil, fmt.Errorf("read output file %s: %w", header.Name, err)
		}
		files[relativePath] = content
	}
}

func normalizeOutputPath(name string) (string, bool) {
	cleaned := strings.TrimPrefix(path.Clean("/"+name), "/")
	root := strings.TrimPrefix(outputDir, "/")

	switch {
	case cleaned == root:
		return "", false
	case strings.HasPrefix(cleaned, root+"/"):
		cleaned = strings.TrimPrefix(cleaned, root+"/")
	case strings.HasPrefix(cleaned, "."), cleaned == "":
		return "", false
	}
	return cleaned, cleaned != "" && cleaned != "."
}
