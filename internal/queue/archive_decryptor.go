package queue

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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

var (
	rarV4Signature = []byte{'R', 'a', 'r', '!', 0x1a, 0x07, 0x00}
	rarV5Signature = []byte{'R', 'a', 'r', '!', 0x1a, 0x07, 0x01, 0x00}
)

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
	if err := validateArchiveInput(resolvedArchivePath, overridePassword); err != nil {
		return true, err
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

func validateArchiveInput(path string, password string) error {
	name := strings.ToLower(filepath.Base(strings.TrimSpace(path)))
	if !strings.HasSuffix(name, ".rar") {
		return nil
	}
	ok, err := hasRARSignature(path)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("missing RAR4/RAR5 signature")
	}
	if strings.TrimSpace(password) == "" {
		encrypted, _ := hasRARHeaderEncryption(path)
		if encrypted {
			return errors.New("archive has header encryption, a password is required")
		}
	}
	return nil
}

func hasRARSignature(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	header := make([]byte, len(rarV5Signature))
	n, err := io.ReadFull(f, header)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return false, err
	}
	header = header[:n]
	if len(header) >= len(rarV4Signature) && bytes.Equal(header[:len(rarV4Signature)], rarV4Signature) {
		return true, nil
	}
	if len(header) >= len(rarV5Signature) && bytes.Equal(header[:len(rarV5Signature)], rarV5Signature) {
		return true, nil
	}
	return false, nil
}

// hasRARHeaderEncryption returns true if the RAR5 archive has header encryption enabled.
// In RAR5 the first block after the 8-byte signature is structured as:
// CRC32 (4 bytes) | header-size vint | header-type vint | ...
// Header type 1 is the Archive Encryption Header, meaning all headers are encrypted.
func hasRARHeaderEncryption(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	// Only RAR5 has the archive encryption header block; verify signature first.
	sig := make([]byte, len(rarV5Signature))
	if _, err := io.ReadFull(f, sig); err != nil {
		return false, nil
	}
	if !bytes.Equal(sig, rarV5Signature) {
		return false, nil
	}

	// Skip header CRC32 (4 bytes) that follows the 8-byte RAR5 signature.
	if _, err := f.Seek(int64(len(rarV5Signature)+4), io.SeekStart); err != nil {
		return false, nil
	}
	// Skip header size vint.
	if err := skipRAR5Vint(f); err != nil {
		return false, nil
	}
	// Read header type vint; value 4 = archive encryption header.
	// (1=main archive, 2=file, 3=service, 4=encryption, 5=end of archive)
	headerType, err := readRAR5Vint(f)
	if err != nil {
		return false, nil
	}
	return headerType == 4, nil
}

func skipRAR5Vint(r io.Reader) error {
	b := make([]byte, 1)
	for {
		if _, err := io.ReadFull(r, b); err != nil {
			return err
		}
		if b[0]&0x80 == 0 {
			return nil
		}
	}
}

func readRAR5Vint(r io.Reader) (uint64, error) {
	var result uint64
	var shift uint
	b := make([]byte, 1)
	for {
		if _, err := io.ReadFull(r, b); err != nil {
			return 0, err
		}
		result |= uint64(b[0]&0x7f) << shift
		if b[0]&0x80 == 0 {
			return result, nil
		}
		shift += 7
		if shift >= 64 {
			return 0, errors.New("vint overflow")
		}
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
