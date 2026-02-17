package queue

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// MegaDecryptor decrypts MEGA file payloads downloaded via temporary URLs.
type MegaDecryptor interface {
	MaybeDecrypt(ctx context.Context, site, rawURL, filePath string) (bool, error)
}

type megaContentDecryptor struct{}

type megaContentKey struct {
	aesKey    [16]byte
	nonce     [8]byte
	expectMAC [8]byte
	verifyMAC bool
}

func NewMegaDecryptor() MegaDecryptor {
	return &megaContentDecryptor{}
}

func (d *megaContentDecryptor) MaybeDecrypt(ctx context.Context, site, rawURL, filePath string) (bool, error) {
	if d == nil {
		return false, nil
	}
	if !IsMegaJob(site, rawURL) {
		return false, nil
	}
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return true, errors.New("missing file path")
	}
	_, keyToken, err := parseMegaFileLink(rawURL)
	if err != nil {
		return true, err
	}
	contentKey, err := parseMegaContentKey(keyToken)
	if err != nil {
		return true, err
	}
	if err := decryptMegaFileInPlace(ctx, filePath, contentKey); err != nil {
		return true, err
	}
	return true, nil
}

func parseMegaContentKey(token string) (megaContentKey, error) {
	var out megaContentKey
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(token))
	if err != nil {
		return out, fmt.Errorf("mega key decode failed: %w", err)
	}
	switch len(raw) {
	case 32:
		for i := 0; i < len(out.aesKey); i++ {
			out.aesKey[i] = raw[i] ^ raw[i+16]
		}
		copy(out.nonce[:], raw[16:24])
		copy(out.expectMAC[:], raw[24:32])
		out.verifyMAC = true
		return out, nil
	case 16:
		copy(out.aesKey[:], raw)
		out.verifyMAC = false
		return out, nil
	default:
		return out, fmt.Errorf("mega invalid key length: %d", len(raw))
	}
}

func decryptMegaFileInPlace(ctx context.Context, path string, key megaContentKey) error {
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(key.aesKey[:])
	if err != nil {
		return err
	}
	var iv [16]byte
	copy(iv[:8], key.nonce[:])
	stream := cipher.NewCTR(block, iv[:])

	dir := filepath.Dir(path)
	prefix := "." + filepath.Base(path) + ".dlq-mega-"
	tmp, err := os.CreateTemp(dir, prefix)
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	keepTemp := false
	defer func() {
		if !keepTemp {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		_ = tmp.Close()
		return err
	}

	var macCalc *megaMACCalculator
	if key.verifyMAC {
		macCalc = newMegaMACCalculator(block, key.nonce)
	}

	buf := make([]byte, 1024*1024)
	outBuf := make([]byte, len(buf))
	for {
		if err := ctx.Err(); err != nil {
			_ = tmp.Close()
			return err
		}
		n, readErr := in.Read(buf)
		if n > 0 {
			stream.XORKeyStream(outBuf[:n], buf[:n])
			if macCalc != nil {
				macCalc.Write(outBuf[:n])
			}
			if _, err := tmp.Write(outBuf[:n]); err != nil {
				_ = tmp.Close()
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			_ = tmp.Close()
			return readErr
		}
	}

	if macCalc != nil {
		gotMAC := macCalc.Sum()
		if subtle.ConstantTimeCompare(gotMAC[:], key.expectMAC[:]) != 1 {
			_ = tmp.Close()
			return errors.New("mega content mac mismatch")
		}
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := in.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	keepTemp = true
	return nil
}

type megaMACCalculator struct {
	block          cipher.Block
	base           [16]byte
	fileMAC        [16]byte
	chunkMAC       [16]byte
	pendingBlock   [16]byte
	pendingLen     int
	chunkIndex     int64
	chunkRemaining int64
	chunkHasData   bool
}

func newMegaMACCalculator(block cipher.Block, nonce [8]byte) *megaMACCalculator {
	m := &megaMACCalculator{block: block}
	copy(m.base[0:8], nonce[:])
	copy(m.base[8:16], nonce[:])
	// Per MEGA reference flow file MAC starts at zero, chunk MAC starts from IV||IV.
	m.fileMAC = [16]byte{}
	m.chunkMAC = m.base
	m.chunkRemaining = megaChunkSize(0)
	return m
}

func megaChunkSize(index int64) int64 {
	if index < 8 {
		return (index + 1) * 0x20000
	}
	return 0x100000
}

func (m *megaMACCalculator) Write(p []byte) {
	for len(p) > 0 {
		take := int64(len(p))
		if take > m.chunkRemaining {
			take = m.chunkRemaining
		}
		m.writeChunkBytes(p[:take])
		p = p[take:]
		m.chunkRemaining -= take
		if m.chunkRemaining == 0 {
			m.finalizeChunk()
			m.chunkIndex++
			m.chunkMAC = m.base
			m.chunkRemaining = megaChunkSize(m.chunkIndex)
		}
	}
}

func (m *megaMACCalculator) writeChunkBytes(p []byte) {
	if len(p) == 0 {
		return
	}
	m.chunkHasData = true
	for len(p) > 0 {
		n := copy(m.pendingBlock[m.pendingLen:], p)
		m.pendingLen += n
		p = p[n:]
		if m.pendingLen == len(m.pendingBlock) {
			m.mixBlock(m.pendingBlock[:])
			m.pendingLen = 0
		}
	}
}

func (m *megaMACCalculator) finalizeChunk() {
	if !m.chunkHasData {
		return
	}
	if m.pendingLen > 0 {
		var padded [16]byte
		copy(padded[:], m.pendingBlock[:m.pendingLen])
		m.mixBlock(padded[:])
		m.pendingLen = 0
	}
	for i := 0; i < len(m.fileMAC); i++ {
		m.fileMAC[i] ^= m.chunkMAC[i]
	}
	m.block.Encrypt(m.fileMAC[:], m.fileMAC[:])
	m.chunkHasData = false
}

func (m *megaMACCalculator) mixBlock(block []byte) {
	for i := 0; i < len(m.chunkMAC); i++ {
		m.chunkMAC[i] ^= block[i]
	}
	m.block.Encrypt(m.chunkMAC[:], m.chunkMAC[:])
}

func (m *megaMACCalculator) Sum() [8]byte {
	m.finalizeChunk()
	var out [8]byte
	w0 := binary.BigEndian.Uint32(m.fileMAC[0:4])
	w1 := binary.BigEndian.Uint32(m.fileMAC[4:8])
	w2 := binary.BigEndian.Uint32(m.fileMAC[8:12])
	w3 := binary.BigEndian.Uint32(m.fileMAC[12:16])
	binary.BigEndian.PutUint32(out[0:4], w0^w1)
	binary.BigEndian.PutUint32(out[4:8], w2^w3)
	return out
}

func parseMegaFileLink(raw string) (string, string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", err
	}
	path := strings.Trim(u.Path, "/")
	fragment := strings.TrimSpace(u.Fragment)

	var fileID, fileKey string
	if strings.HasPrefix(path, "file/") {
		parts := strings.Split(path, "/")
		if len(parts) >= 2 {
			fileID = strings.TrimSpace(parts[1])
		}
		fileKey = strings.TrimSpace(fragment)
	} else if strings.HasPrefix(fragment, "!") {
		parts := strings.Split(strings.TrimPrefix(fragment, "!"), "!")
		if len(parts) >= 2 {
			fileID = strings.TrimSpace(parts[0])
			fileKey = strings.TrimSpace(parts[1])
		}
	}
	if fileID == "" || fileKey == "" {
		return "", "", errors.New("mega public file link required")
	}
	if !isValidMegaToken(fileID) || !isValidMegaToken(fileKey) {
		return "", "", errors.New("mega link invalid tokens")
	}
	return fileID, fileKey, nil
}

func isValidMegaToken(v string) bool {
	if v == "" {
		return false
	}
	for _, c := range v {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_':
		default:
			return false
		}
	}
	return true
}
