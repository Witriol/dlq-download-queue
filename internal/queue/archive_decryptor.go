package queue

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ArchiveDecryptor tries to decrypt/extract downloaded archive files.
type ArchiveDecryptor interface {
	MaybeDecrypt(ctx context.Context, archivePath, outDir, overridePassword string) (bool, error)
}

type commandArchiveDecryptor struct {
	command string
}

func NewArchiveDecryptor() ArchiveDecryptor {
	return &commandArchiveDecryptor{
		command: "7zz",
	}
}

func (d *commandArchiveDecryptor) MaybeDecrypt(ctx context.Context, archivePath, outDir, overridePassword string) (bool, error) {
	if d == nil {
		return false, nil
	}
	resolvedArchivePath := resolveArchiveEntryPath(archivePath)
	if !isArchiveFile(resolvedArchivePath) {
		return false, nil
	}
	command := strings.TrimSpace(d.command)
	if command == "" {
		command = "7zz"
	}
	password := strings.TrimSpace(overridePassword)
	out, err := runArchiveTool(ctx, command, resolvedArchivePath, outDir, password)
	if err == nil {
		return true, nil
	}
	if shouldTryUnarFallback(command, resolvedArchivePath, out, err) {
		if _, lookErr := exec.LookPath("unar"); lookErr == nil {
			fallbackOut, fallbackErr := runArchiveTool(ctx, "unar", resolvedArchivePath, outDir, password)
			if fallbackErr == nil {
				return true, nil
			}
			if fallbackOut == "" {
				fallbackOut = fallbackErr.Error()
			}
			if out != "" {
				out = out + "\n--- unar fallback failed ---\n" + fallbackOut
			} else {
				out = fallbackOut
			}
		}
	}
	if out == "" {
		out = err.Error()
	}
	return true, fmt.Errorf("archive decrypt command failed: %s", out)
}

func isArchiveFile(path string) bool {
	name := strings.ToLower(filepath.Base(strings.TrimSpace(path)))
	switch {
	case strings.HasSuffix(name, ".zip"),
		strings.HasSuffix(name, ".7z"),
		strings.HasSuffix(name, ".rar"),
		strings.HasSuffix(name, ".tar"),
		strings.HasSuffix(name, ".tar.gz"),
		strings.HasSuffix(name, ".tgz"),
		strings.HasSuffix(name, ".tar.bz2"),
		strings.HasSuffix(name, ".tbz2"),
		strings.HasSuffix(name, ".tar.xz"),
		strings.HasSuffix(name, ".txz"),
		strings.HasSuffix(name, ".gz"),
		strings.HasSuffix(name, ".bz2"),
		strings.HasSuffix(name, ".xz"):
		return true
	default:
		return false
	}
}

func runArchiveTool(ctx context.Context, command, archivePath, outDir, password string) (string, error) {
	args := archiveCommandArgs(command, archivePath, outDir, password)
	cmd := exec.CommandContext(ctx, command, args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func archiveCommandArgs(command, archivePath, outDir, password string) []string {
	switch archiveToolKind(command) {
	case "unar":
		args := []string{
			"-f", // overwrite without prompt
			"-o", outDir,
		}
		if strings.TrimSpace(password) != "" {
			args = append(args, "-p", password)
		}
		args = append(args, archivePath)
		return args
	default:
		args := []string{
			"x",    // extract with full paths
			"-y",   // assume yes on all prompts
			"-aoa", // overwrite all existing files
			"-o" + outDir,
		}
		if strings.TrimSpace(password) != "" {
			args = append(args, "-p"+password)
		}
		args = append(args, archivePath)
		return args
	}
}

func archiveToolKind(command string) string {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(command)))
	switch base {
	case "unar":
		return "unar"
	case "7z", "7zz":
		return "7z"
	default:
		// Keep compatibility with custom 7z-compatible commands.
		return "7z"
	}
}

func shouldTryUnarFallback(command, archivePath, output string, runErr error) bool {
	if archiveToolKind(command) == "unar" {
		return false
	}
	if isCommandNotFound(runErr) {
		return true
	}
	name := strings.ToLower(strings.TrimSpace(archivePath))
	if !strings.HasSuffix(name, ".rar") {
		return false
	}
	lower := strings.ToLower(output)
	return strings.Contains(lower, "cannot open the file as archive") ||
		strings.Contains(lower, "can't open as archive") ||
		strings.Contains(lower, "unsupported method")
}

func isCommandNotFound(err error) bool {
	if err == nil {
		return false
	}
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return errors.Is(execErr.Err, exec.ErrNotFound)
	}
	return false
}

func resolveArchiveEntryPath(archivePath string) string {
	clean := strings.TrimSpace(archivePath)
	if clean == "" {
		return clean
	}
	candidate := multipartArchiveFirstVolume(clean)
	if candidate == clean {
		return clean
	}
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return clean
}

func multipartArchiveFirstVolume(path string) string {
	clean := strings.TrimSpace(path)
	if clean == "" {
		return clean
	}
	dir := filepath.Dir(clean)
	base := filepath.Base(clean)
	lower := strings.ToLower(base)

	if strings.HasSuffix(lower, ".rar") {
		stem := strings.TrimSuffix(base, base[len(base)-4:])
		stemLower := strings.ToLower(stem)
		idx := strings.LastIndex(stemLower, ".part")
		if idx >= 0 {
			numPart := stem[idx+len(".part"):]
			if numPart != "" && isAllDigits(numPart) {
				if num, err := strconv.Atoi(numPart); err == nil && num > 1 {
					firstNum := "1"
					if len(numPart) > 1 {
						firstNum = fmt.Sprintf("%0*d", len(numPart), 1)
					}
					first := stem[:idx] + ".part" + firstNum + ".rar"
					return filepath.Join(dir, first)
				}
			}
		}
		return clean
	}

	if len(base) > 4 && lower[len(lower)-4] == '.' && lower[len(lower)-3] == 'r' {
		extDigits := lower[len(lower)-2:]
		if isAllDigits(extDigits) {
			if num, err := strconv.Atoi(extDigits); err == nil && num >= 0 {
				return filepath.Join(dir, base[:len(base)-4]+".rar")
			}
		}
	}

	return clean
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func multipartArchiveGroupKey(path string) (string, bool) {
	clean := strings.TrimSpace(path)
	if clean == "" {
		return "", false
	}
	dir := strings.ToLower(filepath.Clean(filepath.Dir(clean)))
	base := filepath.Base(clean)
	lower := strings.ToLower(base)

	if strings.HasSuffix(lower, ".rar") {
		stem := strings.TrimSuffix(base, base[len(base)-4:])
		stemLower := strings.ToLower(stem)
		if idx := strings.LastIndex(stemLower, ".part"); idx >= 0 {
			numPart := stem[idx+len(".part"):]
			if numPart != "" && isAllDigits(numPart) {
				prefix := strings.ToLower(stem[:idx])
				return dir + "|partrar|" + prefix, true
			}
		}
		return dir + "|rstyle|" + strings.ToLower(stem), false
	}

	if len(base) > 4 && lower[len(lower)-4] == '.' && lower[len(lower)-3] == 'r' {
		extDigits := lower[len(lower)-2:]
		if isAllDigits(extDigits) {
			return dir + "|rstyle|" + strings.ToLower(base[:len(base)-4]), true
		}
	}

	return "", false
}
