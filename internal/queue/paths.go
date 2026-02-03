package queue

import (
	"errors"
	"path/filepath"
	"strings"
)

const dataRoot = "/data"

func cleanOutDir(outDir string) (string, error) {
	if outDir == "" {
		return "", errors.New("missing out_dir")
	}
	clean := filepath.Clean(outDir)
	if !filepath.IsAbs(clean) {
		return "", errors.New("out_dir must be absolute")
	}
	rel, err := filepath.Rel(dataRoot, clean)
	if err != nil {
		return "", errors.New("invalid out_dir")
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("out_dir must be within /data")
	}
	return clean, nil
}

func cleanUserFilename(name string) (string, error) {
	if name == "" {
		return "", nil
	}
	if filepath.IsAbs(name) || strings.ContainsAny(name, "/\\") {
		return "", errors.New("name must not contain path separators")
	}
	clean := filepath.Clean(name)
	if clean == "." || clean == ".." || clean == string(filepath.Separator) {
		return "", errors.New("invalid name")
	}
	return clean, nil
}

func sanitizeFilename(name string) string {
	if name == "" {
		return ""
	}
	base := filepath.Base(strings.TrimSpace(name))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return ""
	}
	if strings.ContainsAny(base, "/\\") {
		return ""
	}
	return base
}
