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
