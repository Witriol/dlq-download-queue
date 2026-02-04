package queue

import (
	"errors"
	"path/filepath"
	"strings"
)

func cleanOutDir(outDir string, allowedRoots []string) (string, error) {
	if outDir == "" {
		return "", errors.New("missing out_dir")
	}
	if len(allowedRoots) == 0 {
		return "", errors.New("no DATA_* volumes configured")
	}
	clean := filepath.Clean(outDir)
	if !filepath.IsAbs(clean) {
		return "", errors.New("out_dir must be absolute")
	}
	allowed := false
	for _, root := range allowedRoots {
		rootClean := filepath.Clean(root)
		rel, err := filepath.Rel(rootClean, clean)
		if err != nil {
			continue
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		allowed = true
		break
	}
	if !allowed {
		return "", errors.New("out_dir is not within an allowed DATA_* volume")
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
