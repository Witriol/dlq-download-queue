package resolver

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const megaAPI = "https://g.api.mega.co.nz/cs"

type megaResolver struct {
	client    *http.Client
	apiURL    string
	requestID uint64
}

func NewMegaResolver() Resolver {
	return &megaResolver{
		client:    &http.Client{Timeout: 20 * time.Second},
		apiURL:    megaAPI,
		requestID: uint64(time.Now().UnixNano()),
	}
}

func (r *megaResolver) CanHandle(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Host)
	return strings.Contains(host, "mega.nz") || strings.Contains(host, "mega.co.nz")
}

func (r *megaResolver) Resolve(ctx context.Context, rawURL string) (*ResolvedTarget, error) {
	fileID, fileKey, err := parseMegaFileLink(rawURL)
	if err != nil {
		return nil, err
	}
	resp, err := r.fileInfo(ctx, fileID)
	if err != nil {
		return nil, err
	}
	filename, err := decryptMegaFilename(resp.Attributes, fileKey)
	if err != nil {
		return nil, err
	}
	return &ResolvedTarget{
		Kind:     "aria2",
		URL:      resp.DownloadURL,
		Filename: filename,
		Size:     resp.Size,
	}, nil
}

type megaFileInfoResponse struct {
	DownloadURL string `json:"g"`
	Size        int64  `json:"s"`
	Attributes  string `json:"at"`
	ErrorCode   int    `json:"e"`
}

func (r *megaResolver) fileInfo(ctx context.Context, fileID string) (*megaFileInfoResponse, error) {
	apiURL := strings.TrimSpace(r.apiURL)
	if apiURL == "" {
		apiURL = megaAPI
	}
	requestID := atomic.AddUint64(&r.requestID, 1)
	requestURL := apiURL
	if strings.Contains(requestURL, "?") {
		requestURL += "&id=" + strconv.FormatUint(requestID, 10)
	} else {
		requestURL += "?id=" + strconv.FormatUint(requestID, 10)
	}

	payload, err := json.Marshal([]map[string]any{
		{
			"a": "g",
			"g": 1,
			"p": fileID,
		},
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("mega_api_http_status:%d", resp.StatusCode)
	}

	var body []json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, errors.New("mega_api_empty_response")
	}

	var apiErrorCode int
	if err := json.Unmarshal(body[0], &apiErrorCode); err == nil {
		return nil, mapMegaAPIError(apiErrorCode)
	}

	var out megaFileInfoResponse
	if err := json.Unmarshal(body[0], &out); err != nil {
		return nil, err
	}
	if out.ErrorCode != 0 {
		return nil, mapMegaAPIError(out.ErrorCode)
	}
	if out.DownloadURL == "" {
		return nil, errors.New("mega_download_url_missing")
	}
	if out.Attributes == "" {
		return nil, errors.New("mega_attributes_missing")
	}
	return &out, nil
}

func parseMegaFileLink(rawURL string) (string, string, error) {
	u, err := url.Parse(rawURL)
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
		return "", "", errors.New("mega_public_file_link_required")
	}
	if !isValidMegaToken(fileID) || !isValidMegaToken(fileKey) {
		return "", "", errors.New("mega_link_invalid_tokens")
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

func decodeMegaBase64(raw string) ([]byte, error) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("mega_base64_decode_failed:%w", err)
	}
	return b, nil
}

func decryptMegaFilename(attributes, fileKey string) (string, error) {
	aesKey, err := deriveMegaAESKey(fileKey)
	if err != nil {
		return "", err
	}
	encAttrs, err := decodeMegaBase64(attributes)
	if err != nil {
		return "", err
	}
	if len(encAttrs) == 0 || len(encAttrs)%aes.BlockSize != 0 {
		return "", errors.New("mega_attributes_invalid_length")
	}

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return "", err
	}
	plain := make([]byte, len(encAttrs))
	cipher.NewCBCDecrypter(block, make([]byte, aes.BlockSize)).CryptBlocks(plain, encAttrs)
	plain = bytes.TrimRight(plain, "\x00")
	if len(plain) == 0 || !bytes.HasPrefix(plain, []byte("MEGA")) {
		return "", errors.New("mega_attributes_invalid_prefix")
	}

	var attrs struct {
		Name string `json:"n"`
	}
	if err := json.Unmarshal(plain[4:], &attrs); err != nil {
		return "", err
	}
	if strings.TrimSpace(attrs.Name) == "" {
		return "", errors.New("mega_filename_missing")
	}
	return attrs.Name, nil
}

func deriveMegaAESKey(fileKey string) ([]byte, error) {
	rawKey, err := decodeMegaBase64(fileKey)
	if err != nil {
		return nil, err
	}
	switch len(rawKey) {
	case 16:
		return rawKey, nil
	case 32:
		out := make([]byte, 16)
		for i := range out {
			out[i] = rawKey[i] ^ rawKey[i+16]
		}
		return out, nil
	default:
		return nil, fmt.Errorf("mega_invalid_key_length:%d", len(rawKey))
	}
}

func mapMegaAPIError(code int) error {
	switch code {
	case -17:
		return ErrQuotaExceeded
	case -18, -4:
		return ErrTemporarilyOff
	case -11, -14, -16:
		return ErrLoginRequired
	default:
		return fmt.Errorf("mega_api_error:%d", code)
	}
}
