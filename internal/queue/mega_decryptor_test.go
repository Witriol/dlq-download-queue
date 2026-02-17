package queue

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMegaDecryptorSkipsNonMega(t *testing.T) {
	d := NewMegaDecryptor()
	attempted, err := d.MaybeDecrypt(context.Background(), "http", "https://example.com/file.bin", "/tmp/file.bin")
	if attempted {
		t.Fatalf("expected attempted=false")
	}
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestMegaDecryptorDecryptsFileAndValidatesMAC(t *testing.T) {
	plain := bytes.Repeat([]byte("RAR5-test-data-"), 70000)
	ciphertext, keyToken := encryptMegaCiphertext16ForTest(t, plain)

	dir := t.TempDir()
	path := filepath.Join(dir, "sample.rar")
	if err := os.WriteFile(path, ciphertext, 0o644); err != nil {
		t.Fatalf("write ciphertext: %v", err)
	}

	d := NewMegaDecryptor()
	url := "https://mega.nz/file/AbCdEf12#" + keyToken
	attempted, err := d.MaybeDecrypt(context.Background(), "mega", url, path)
	if !attempted {
		t.Fatalf("expected attempted=true")
	}
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("decrypted bytes mismatch")
	}
}

func TestMegaDecryptorDetectsMACMismatch(t *testing.T) {
	plain := bytes.Repeat([]byte("RAR5-test-data-"), 6000)
	ciphertext, keyToken := encryptMegaCiphertext32ForTest(t, plain)

	dir := t.TempDir()
	path := filepath.Join(dir, "sample.rar")
	if err := os.WriteFile(path, ciphertext, 0o644); err != nil {
		t.Fatalf("write ciphertext: %v", err)
	}

	d := NewMegaDecryptor()
	url := "https://mega.nz/file/AbCdEf12#" + keyToken
	attempted, err := d.MaybeDecrypt(context.Background(), "mega", url, path)
	if !attempted {
		t.Fatalf("expected attempted=true")
	}
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "mac mismatch") {
		t.Fatalf("expected mac mismatch error, got: %v", err)
	}
}

func encryptMegaCiphertext16ForTest(t *testing.T, plain []byte) ([]byte, string) {
	t.Helper()
	key := []byte{
		0x10, 0x11, 0x12, 0x13, 0x20, 0x21, 0x22, 0x23,
		0x30, 0x31, 0x32, 0x33, 0x40, 0x41, 0x42, 0x43,
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes: %v", err)
	}
	var iv [16]byte

	out := make([]byte, len(plain))
	cipher.NewCTR(block, iv[:]).XORKeyStream(out, plain)
	token := base64.RawURLEncoding.EncodeToString(key)
	return out, token
}

func encryptMegaCiphertext32ForTest(t *testing.T, plain []byte) ([]byte, string) {
	t.Helper()
	rawKey := []byte{
		0x10, 0x11, 0x12, 0x13, 0x20, 0x21, 0x22, 0x23,
		0x30, 0x31, 0x32, 0x33, 0x40, 0x41, 0x42, 0x43,
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
	}
	key := make([]byte, 16)
	for i := 0; i < 16; i++ {
		key[i] = rawKey[i] ^ rawKey[i+16]
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes: %v", err)
	}
	var iv [16]byte
	copy(iv[:8], rawKey[16:24])

	out := make([]byte, len(plain))
	cipher.NewCTR(block, iv[:]).XORKeyStream(out, plain)
	token := base64.RawURLEncoding.EncodeToString(rawKey)
	return out, token
}
