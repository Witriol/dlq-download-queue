package queue

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const archiveGroupIDPrefix = "ag_"

var (
	archivePartRARPattern = regexp.MustCompile(`(?i)^(.*)\.part([0-9]+)\.rar$`)
	archiveRStylePattern  = regexp.MustCompile(`(?i)^(.*)\.r([0-9]{2})$`)
)

func archiveGroupLabelAndPartForJob(job Job) (string, int) {
	name := sanitizeFilename(job.Name)
	if name != "" {
		if label, part := archiveGroupLabelAndPart(name); label != "" {
			return label, part
		}
	}
	if job.Filename.Valid {
		if label, part := archiveGroupLabelAndPart(job.Filename.String); label != "" {
			return label, part
		}
	}
	path := archivePathForJob(job)
	if path != "" {
		return archiveGroupLabelAndPart(path)
	}
	return "", 0
}

func archiveGroupIDFromKey(key string) string {
	clean := strings.TrimSpace(key)
	if clean == "" {
		return ""
	}
	return archiveGroupIDPrefix + base64.RawURLEncoding.EncodeToString([]byte(clean))
}

func archiveGroupKeyFromID(groupID string) (string, error) {
	clean := strings.TrimSpace(groupID)
	if clean == "" {
		return "", fmt.Errorf("%w: missing archive group id", ErrInvalidArchiveGroupID)
	}
	if !strings.HasPrefix(clean, archiveGroupIDPrefix) {
		return "", fmt.Errorf("%w: invalid archive group id", ErrInvalidArchiveGroupID)
	}
	raw := clean[len(archiveGroupIDPrefix):]
	if raw == "" {
		return "", fmt.Errorf("%w: invalid archive group id", ErrInvalidArchiveGroupID)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return "", fmt.Errorf("%w: invalid archive group id", ErrInvalidArchiveGroupID)
	}
	key := strings.TrimSpace(string(decoded))
	if key == "" {
		return "", fmt.Errorf("%w: invalid archive group id", ErrInvalidArchiveGroupID)
	}
	return key, nil
}

func archiveGroupMatchesJob(job Job, groupKey string) bool {
	path := archivePathForJob(job)
	if strings.TrimSpace(path) == "" || strings.TrimSpace(groupKey) == "" {
		return false
	}
	key, _ := multipartArchiveGroupKey(path)
	return key == groupKey
}

func archiveGroupLabelAndPart(name string) (string, int) {
	base := filepath.Base(strings.TrimSpace(name))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "", 0
	}
	if m := archivePartRARPattern.FindStringSubmatch(base); len(m) == 3 {
		part, _ := strconv.Atoi(m[2])
		return m[1], part
	}
	if m := archiveRStylePattern.FindStringSubmatch(base); len(m) == 3 {
		// For .rNN style, .rar is volume 1 and .r00 is volume 2.
		n, _ := strconv.Atoi(m[2])
		return m[1], n + 2
	}
	lower := strings.ToLower(base)
	if strings.HasSuffix(lower, ".rar") {
		return base[:len(base)-4], 1
	}
	return "", 0
}

func archiveGroupLabelFromKey(groupKey string) string {
	parts := strings.Split(strings.TrimSpace(groupKey), "|")
	if len(parts) < 3 {
		return ""
	}
	return strings.TrimSpace(parts[2])
}

func archivePartKeyForJob(job Job) string {
	path := archivePathForJob(job)
	base := filepath.Base(strings.TrimSpace(path))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return fmt.Sprintf("job:%d", job.ID)
	}
	if _, part := archiveGroupLabelAndPart(base); part > 0 {
		return fmt.Sprintf("part:%06d", part)
	}
	return "file:" + strings.ToLower(base)
}

func latestArchiveJobsByPart(jobs []Job) []Job {
	if len(jobs) <= 1 {
		out := make([]Job, len(jobs))
		copy(out, jobs)
		return out
	}
	latest := make(map[string]Job, len(jobs))
	for _, job := range jobs {
		partKey := archivePartKeyForJob(job)
		if existing, ok := latest[partKey]; !ok || job.ID > existing.ID {
			latest[partKey] = job
		}
	}
	out := make([]Job, 0, len(latest))
	for _, job := range latest {
		out = append(out, job)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID == out[j].ID {
			return archivePartKeyForJob(out[i]) < archivePartKeyForJob(out[j])
		}
		return out[i].ID < out[j].ID
	})
	return out
}
