package queue

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
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
	if !isArchiveFile(archivePath) {
		return false, nil
	}
	command := strings.TrimSpace(d.command)
	if command == "" {
		command = "7zz"
	}
	password := strings.TrimSpace(overridePassword)
	out, err := runArchiveTool(ctx, command, archivePath, outDir, password)
	if err == nil {
		return true, nil
	}
	if shouldTryUnarFallback(command, archivePath, out, err) {
		if _, lookErr := exec.LookPath("unar"); lookErr == nil {
			fallbackOut, fallbackErr := runArchiveTool(ctx, "unar", archivePath, outDir, password)
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
