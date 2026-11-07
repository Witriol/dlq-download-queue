package queue

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
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
	// Group/other write bits to add to extracted entries.
	writeBits os.FileMode
}

// Read at package init, before any goroutine runs: the umask can only be read
// by setting it, which briefly changes it for the whole process.
var processUmask = readUmask()

const groupOtherWrite os.FileMode = 0o022

var (
	rarV4Signature = []byte{'R', 'a', 'r', '!', 0x1a, 0x07, 0x00}
	rarV5Signature = []byte{'R', 'a', 'r', '!', 0x1a, 0x07, 0x01, 0x00}
)

func NewArchiveDecryptor() ArchiveDecryptor {
	return &commandArchiveDecryptor{
		command:   "7z",
		writeBits: groupOtherWrite &^ processUmask,
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
		command = "7z"
	}
	password := strings.TrimSpace(overridePassword)
	out, err := runArchiveTool(ctx, command, resolvedArchivePath, outDir, password)
	extracted, out := splitExtractedPaths(out)
	if err == nil {
		addWriteBits(outDir, extracted, d.writeBits)
		return true, nil
	}
	if out == "" {
		out = err.Error()
	}
	return true, fmt.Errorf("archive decrypt command failed: %s", out)
}

// 7z -bb1 output, verified on 7-Zip 25.01 (Debian bookworm-backports). The
// Dockerfile installs whatever version backports carries; re-check this when
// it changes.
//
// On success, entries are one "- <path>" line each, relative to -o,
// directories ending in "/", overwritten entries included, as one unbroken
// block right before the last "Everything is Ok". The header above it can
// echo an archive comment verbatim, so "- " lines elsewhere are not entries.
// On failure, 7z prints a "T <path>" line per entry it tried.
const (
	extractedEntryPrefix = "- "
	attemptedEntryPrefix = "T "
	extractSuccessLine   = "Everything is Ok"
)

// splitExtractedPaths returns the extracted entries and the output without
// per-entry lines, so error messages are not a full file listing.
func splitExtractedPaths(out string) ([]string, string) {
	lines := strings.Split(out, "\n")

	var paths []string
	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i] != extractSuccessLine {
			continue
		}

		for j := i - 1; j >= 0; j-- {
			path, ok := strings.CutPrefix(lines[j], extractedEntryPrefix)
			if !ok {
				break
			}
			paths = append(paths, path)
		}
		break
	}

	var rest []string
	for _, line := range lines {
		if strings.HasPrefix(line, extractedEntryPrefix) || strings.HasPrefix(line, attemptedEntryPrefix) {
			continue
		}
		rest = append(rest, line)
	}
	return paths, strings.TrimSpace(strings.Join(rest, "\n"))
}

// 7z restores the modes stored in the archive and a umask can only clear bits,
// so a Unix-made archive extracts 0644/0755 even under UMASK=002. Only the
// entries 7z reports are touched; outDir also holds other jobs' files.
func addWriteBits(dir string, entries []string, bits os.FileMode) {
	if bits == 0 {
		return
	}

	if len(entries) == 0 {
		log.Printf("archive chmod dir=%s: no extracted entries in 7z output; check the -bb1 format", dir)
		return
	}

	for _, entry := range entries {
		rel := filepath.Clean(entry)
		if rel == "." || !filepath.IsLocal(rel) {
			continue
		}

		path := filepath.Join(dir, rel)
		info, err := os.Lstat(path)
		if err != nil {
			log.Printf("archive chmod path=%s error: %v", path, err)
			continue
		}

		// os.Chmod follows symlinks, which may point outside dir.
		if info.Mode()&fs.ModeSymlink != 0 {
			continue
		}

		mode := info.Mode().Perm()
		if mode&bits == bits {
			continue
		}

		// Perm() drops setgid/setuid/sticky and os.Chmod would clear them.
		special := info.Mode() & (os.ModeSetgid | os.ModeSetuid | os.ModeSticky)
		err = os.Chmod(path, mode|bits|special)
		if err != nil && !errors.Is(err, fs.ErrPermission) {
			log.Printf("archive chmod path=%s error: %v", path, err)
		}
	}
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
// Header type 4 is the Archive Encryption Header, meaning all headers are encrypted.
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
	if isTarCompressedArchive(archivePath) {
		return runTarCompressedArchiveTool(ctx, command, archivePath, outDir, password)
	}
	args := archiveCommandArgs(command, archivePath, outDir, password)
	cmd := exec.CommandContext(ctx, command, args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func archiveCommandArgs(command, archivePath, outDir, password string) []string {
	args := []string{
		"x",    // extract with full paths
		"-y",   // assume yes on all prompts
		"-aoa", // overwrite all existing files
		"-bb1", // list extracted entries (see extractedEntryPrefix)
		"-o" + outDir,
	}
	if strings.TrimSpace(password) != "" {
		args = append(args, "-p"+password)
	}
	args = append(args, archivePath)
	return args
}

func archiveStreamCommandArgs(archivePath, password string) []string {
	args := []string{"x", "-so"}
	if strings.TrimSpace(password) != "" {
		args = append(args, "-p"+password)
	}
	args = append(args, archivePath)
	return args
}

func archiveStreamTarCommandArgs(outDir string) []string {
	return []string{
		"x",
		"-y",
		"-aoa",
		"-si",
		"-ttar",
		"-bb1",
		"-o" + outDir,
	}
}

func runTarCompressedArchiveTool(ctx context.Context, command, archivePath, outDir, password string) (string, error) {
	source := exec.CommandContext(ctx, command, archiveStreamCommandArgs(archivePath, password)...)
	extract := exec.CommandContext(ctx, command, archiveStreamTarCommandArgs(outDir)...)

	pipeReader, pipeWriter := io.Pipe()
	var sourceErr bytes.Buffer
	var extractOut bytes.Buffer
	source.Stdout = pipeWriter
	source.Stderr = &sourceErr
	extract.Stdin = pipeReader
	extract.Stdout = &extractOut
	extract.Stderr = &extractOut

	if err := extract.Start(); err != nil {
		_ = pipeReader.Close()
		_ = pipeWriter.Close()
		return strings.TrimSpace(joinArchiveOutput(sourceErr.String(), extractOut.String())), err
	}
	sourceDone := make(chan error, 1)
	go func() {
		err := source.Run()
		_ = pipeWriter.CloseWithError(err)
		sourceDone <- err
	}()

	extractWaitErr := extract.Wait()
	_ = pipeReader.Close()
	sourceWaitErr := <-sourceDone
	out := strings.TrimSpace(joinArchiveOutput(sourceErr.String(), extractOut.String()))
	if sourceWaitErr != nil {
		return out, sourceWaitErr
	}
	if extractWaitErr != nil {
		return out, extractWaitErr
	}
	return out, nil
}

func joinArchiveOutput(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return strings.Join(out, "\n")
}

func isTarCompressedArchive(path string) bool {
	name := strings.ToLower(filepath.Base(strings.TrimSpace(path)))
	return strings.HasSuffix(name, ".tar.gz") ||
		strings.HasSuffix(name, ".tgz") ||
		strings.HasSuffix(name, ".tar.bz2") ||
		strings.HasSuffix(name, ".tbz2") ||
		strings.HasSuffix(name, ".tar.xz") ||
		strings.HasSuffix(name, ".txz")
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
