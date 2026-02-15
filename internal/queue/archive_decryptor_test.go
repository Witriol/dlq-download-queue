package queue

import (
	"context"
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
