package queue

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMaybeDecryptAttemptsWithoutPassword(t *testing.T) {
	d := &commandArchiveDecryptor{command: "false"}
	attempted, err := d.MaybeDecrypt(context.Background(), "/data/a.zip", "/data", "")
	if !attempted {
		t.Fatalf("expected attempted=true when archive extension matches")
	}
	if err == nil {
		t.Fatalf("expected command error")
	}
}

func TestMaybeDecryptRejectsRARWithoutSignature(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.rar")
	if err := os.WriteFile(path, []byte("not-a-rar"), 0o644); err != nil {
		t.Fatalf("write sample: %v", err)
	}

	d := &commandArchiveDecryptor{command: "false"}
	attempted, err := d.MaybeDecrypt(context.Background(), path, dir, "")
	if !attempted {
		t.Fatalf("expected attempted=true for invalid rar input")
	}
	if err == nil {
		t.Fatalf("expected signature validation error")
	}
	if got := err.Error(); got != "missing RAR4/RAR5 signature" {
		t.Fatalf("unexpected error %q", got)
	}
}

func TestArchiveCommandArgs7zz(t *testing.T) {
	args := archiveCommandArgs("7zz", "/data/a.rar", "/out dir", "pw")
	want := []string{"x", "-y", "-aoa", "-o/out dir", "-ppw", "/data/a.rar"}
	if len(args) != len(want) {
		t.Fatalf("args len = %d, want %d (%v)", len(args), len(want), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q (all=%v)", i, args[i], want[i], args)
		}
	}
}

func TestArchiveCommandArgsUnar(t *testing.T) {
	args := archiveCommandArgs("unar", "/data/a.rar", "/out dir", "pw")
	want := []string{"-f", "-o", "/out dir", "-p", "pw", "/data/a.rar"}
	if len(args) != len(want) {
		t.Fatalf("args len = %d, want %d (%v)", len(args), len(want), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q (all=%v)", i, args[i], want[i], args)
		}
	}
}

func TestShouldTryUnarFallback(t *testing.T) {
	if !shouldTryUnarFallback("7zz", "/data/a.rar", "Cannot open the file as archive", nil) {
		t.Fatalf("expected fallback for rar open-as-archive error")
	}
	if shouldTryUnarFallback("unar", "/data/a.rar", "Cannot open the file as archive", nil) {
		t.Fatalf("did not expect fallback when unar is already primary")
	}
	if shouldTryUnarFallback("7zz", "/data/a.zip", "Cannot open the file as archive", nil) {
		t.Fatalf("did not expect fallback for non-rar extension")
	}
}

func TestIsArchiveFile(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"/data/a.zip", true},
		{"/data/a.rar", true},
		{"/data/a.7z", true},
		{"/data/a.tar.gz", true},
		{"/data/a.bin", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isArchiveFile(tc.in); got != tc.want {
			t.Fatalf("isArchiveFile(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestMultipartArchiveFirstVolume(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"/data/a.part2.rar", "/data/a.part1.rar"},
		{"/data/a.part02.rar", "/data/a.part01.rar"},
		{"/data/a.part1.rar", "/data/a.part1.rar"},
		{"/data/a.r00", "/data/a.rar"},
		{"/data/a.r01", "/data/a.rar"},
		{"/data/a.rar", "/data/a.rar"},
	}
	for _, tc := range cases {
		if got := multipartArchiveFirstVolume(tc.in); got != tc.want {
			t.Fatalf("multipartArchiveFirstVolume(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestResolveArchiveEntryPathUsesFirstPartWhenPresent(t *testing.T) {
	dir := t.TempDir()
	part1 := filepath.Join(dir, "sample.part1.rar")
	part2 := filepath.Join(dir, "sample.part2.rar")

	if err := os.WriteFile(part1, []byte("part1"), 0o644); err != nil {
		t.Fatalf("write part1: %v", err)
	}
	if err := os.WriteFile(part2, []byte("part2"), 0o644); err != nil {
		t.Fatalf("write part2: %v", err)
	}

	got := resolveArchiveEntryPath(part2)
	if got != part1 {
		t.Fatalf("resolveArchiveEntryPath(%q) = %q, want %q", part2, got, part1)
	}
}

func TestResolveArchiveEntryPathKeepsOriginalWhenFirstPartMissing(t *testing.T) {
	dir := t.TempDir()
	part2 := filepath.Join(dir, "sample.part2.rar")
	if err := os.WriteFile(part2, []byte("part2"), 0o644); err != nil {
		t.Fatalf("write part2: %v", err)
	}

	got := resolveArchiveEntryPath(part2)
	if got != part2 {
		t.Fatalf("resolveArchiveEntryPath(%q) = %q, want %q", part2, got, part2)
	}
}

func TestMultipartArchiveGroupKey(t *testing.T) {
	cases := []struct {
		in           string
		wantKey      string
		wantExplicit bool
	}{
		{"/data/Show.part2.rar", "/data|partrar|show", true},
		{"/data/Show.part1.rar", "/data|partrar|show", true},
		{"/data/Show.rar", "/data|rstyle|show", false},
		{"/data/Show.r00", "/data|rstyle|show", true},
		{"/data/Show.bin", "", false},
	}
	for _, tc := range cases {
		gotKey, gotExplicit := multipartArchiveGroupKey(tc.in)
		if gotKey != tc.wantKey || gotExplicit != tc.wantExplicit {
			t.Fatalf("multipartArchiveGroupKey(%q) = (%q, %v), want (%q, %v)", tc.in, gotKey, gotExplicit, tc.wantKey, tc.wantExplicit)
		}
	}
}

func TestMaybeDecryptRejectsHeaderEncryptedRARWithoutPassword(t *testing.T) {
	// Construct a minimal RAR5 file with an Archive Encryption Header (type 1).
	// Layout: RAR5 signature (8) + fake CRC32 (4) + header-size vint (1) + header-type vint (1=encrypt) + padding.
	dir := t.TempDir()
	path := filepath.Join(dir, "encrypted.rar")
	data := make([]byte, 32)
	copy(data, rarV5Signature)           // bytes 0-7: RAR5 signature
	data[8], data[9], data[10], data[11] = 0x01, 0x02, 0x03, 0x04 // fake CRC32
	data[12] = 0x08                      // header size vint (single byte, value=8)
	data[13] = 0x01                      // header type vint = 1 (archive encryption header)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	d := &commandArchiveDecryptor{command: "false"}
	attempted, err := d.MaybeDecrypt(context.Background(), path, dir, "")
	if !attempted {
		t.Fatalf("expected attempted=true")
	}
	if err == nil {
		t.Fatalf("expected error for header-encrypted rar without password")
	}
	if got := err.Error(); got != "archive has header encryption, a password is required" {
		t.Fatalf("unexpected error %q", got)
	}
}

func TestMaybeDecryptAllowsHeaderEncryptedRARWithPassword(t *testing.T) {
	// Same header-encrypted RAR — but a password is provided, so validation passes
	// and the tool is attempted (it will fail since "false" is the command, but no early reject).
	dir := t.TempDir()
	path := filepath.Join(dir, "encrypted.rar")
	data := make([]byte, 32)
	copy(data, rarV5Signature)
	data[8], data[9], data[10], data[11] = 0x01, 0x02, 0x03, 0x04
	data[12] = 0x08
	data[13] = 0x01
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	d := &commandArchiveDecryptor{command: "false"}
	attempted, err := d.MaybeDecrypt(context.Background(), path, dir, "secret")
	if !attempted {
		t.Fatalf("expected attempted=true")
	}
	// Error is expected (command "false" always fails), but it must not be the early-reject message.
	if err != nil && err.Error() == "archive has header encryption, a password is required" {
		t.Fatalf("should not early-reject when password is provided")
	}
}

func TestHasRARHeaderEncryption(t *testing.T) {
	dir := t.TempDir()

	// Encrypted: RAR5 sig + CRC32 + size vint + type vint 0x01
	encPath := filepath.Join(dir, "enc.rar")
	enc := make([]byte, 32)
	copy(enc, rarV5Signature)
	enc[8], enc[9], enc[10], enc[11] = 0xAA, 0xBB, 0xCC, 0xDD
	enc[12] = 0x08 // header size vint
	enc[13] = 0x01 // type = archive encryption
	if err := os.WriteFile(encPath, enc, 0o644); err != nil {
		t.Fatalf("write enc: %v", err)
	}
	if got, err := hasRARHeaderEncryption(encPath); err != nil || !got {
		t.Fatalf("hasRARHeaderEncryption(enc) = %v, %v; want true, nil", got, err)
	}

	// Not encrypted: type vint is 0x02 (main archive header)
	plainPath := filepath.Join(dir, "plain.rar")
	plain := make([]byte, 32)
	copy(plain, rarV5Signature)
	plain[8], plain[9], plain[10], plain[11] = 0x11, 0x22, 0x33, 0x44
	plain[12] = 0x08
	plain[13] = 0x02 // type = main archive header (not encrypted)
	if err := os.WriteFile(plainPath, plain, 0o644); err != nil {
		t.Fatalf("write plain: %v", err)
	}
	if got, err := hasRARHeaderEncryption(plainPath); err != nil || got {
		t.Fatalf("hasRARHeaderEncryption(plain) = %v, %v; want false, nil", got, err)
	}
}

func TestHasRARSignature(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name    string
		content []byte
		want    bool
	}{
		{name: "rar4", content: append([]byte{}, rarV4Signature...), want: true},
		{name: "rar5", content: append([]byte{}, rarV5Signature...), want: true},
		{name: "invalid", content: []byte("plain-data"), want: false},
	}
	for _, tc := range cases {
		path := filepath.Join(dir, tc.name+".rar")
		if err := os.WriteFile(path, tc.content, 0o644); err != nil {
			t.Fatalf("write %s: %v", tc.name, err)
		}
		got, err := hasRARSignature(path)
		if err != nil {
			t.Fatalf("hasRARSignature(%q): %v", path, err)
		}
		if got != tc.want {
			t.Fatalf("hasRARSignature(%q) = %v, want %v", path, got, tc.want)
		}
	}
}
