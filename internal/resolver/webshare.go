package resolver

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const webshareAPI = "https://webshare.cz/api"

var (
	ErrFileNotFound     = errors.New("file_not_found")
	ErrWrongPassword    = errors.New("wrong_password")
	ErrFileUnavailable  = errors.New("file_unavailable")
	ErrTooManyDownloads = errors.New("too_many_downloads")
	ErrCopyrightedFile  = errors.New("copyrighted_file")
)

// WebshareOptions configures a client. DLQ_WEBSHARE_WST, WEBSHARE_WST, and
// WS_WST are recognized when WST is not supplied explicitly.
type WebshareOptions struct {
	HTTPClient *http.Client
	BaseURL    string
	WST        string
}

type WebshareClient struct {
	client  *http.Client
	baseURL string
	mu      sync.RWMutex
	wst     string
}

func NewWebshareClient(opts WebshareOptions) *WebshareClient {
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	base := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if base == "" {
		base = webshareAPI
	}
	wst := strings.TrimSpace(opts.WST)
	if wst == "" {
		wst = firstEnv("DLQ_WEBSHARE_WST", "WEBSHARE_WST", "WS_WST")
	}
	return &WebshareClient{client: hc, baseURL: base, wst: wst}
}

func firstEnv(names ...string) string {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v
		}
	}
	return ""
}

// WebshareFile is shared by file_info and search responses.
type WebshareFile struct {
	Ident     string
	Filename  string
	Size      int64
	Category  string
	Available bool
	Password  bool
	Removed   bool
	Encrypted bool
	Rating    float64
}

type WebshareSearchResult = WebshareFile

// SearchVideos searches Webshare's video index. page is one-based; offset is
// used by the API and is calculated from page and limit.
func (c *WebshareClient) SearchVideos(ctx context.Context, query string, limit, page int) ([]WebshareSearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("webshare_search_query_empty")
	}
	if limit <= 0 {
		limit = 50
	}
	if page <= 0 {
		page = 1
	}
	values := url.Values{"what": {query}, "sort": {"recent"}, "limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa((page - 1) * limit)}, "category": {"video"}}
	var out wsSearchResponse
	if err := c.postXML(ctx, "/search/", values, &out); err != nil {
		return nil, err
	}
	if !okStatus(out.Status) {
		return nil, wsAPIError("webshare_search_error", out.Code, out.Message)
	}
	raw := append(out.Files, out.NestedFiles...)
	results := make([]WebshareSearchResult, 0, len(raw))
	for _, f := range raw {
		results = append(results, f.toPublic())
	}
	return results, nil
}

func (c *WebshareClient) FileInfo(ctx context.Context, ident string) (*WebshareFile, error) {
	ident = strings.TrimSpace(ident)
	if ident == "" {
		return nil, errors.New("webshare_ident_empty")
	}
	var out wsInfoResponse
	if err := c.postXML(ctx, "/file_info/", url.Values{"ident": {ident}}, &out); err != nil {
		return nil, err
	}
	if !okStatus(out.Status) {
		return nil, wsAPIError("webshare_info_error", out.Code, out.Message)
	}
	size, _ := strconv.ParseInt(strings.TrimSpace(out.Size), 10, 64)
	return &WebshareFile{Ident: ident, Filename: out.Name, Size: size, Available: !isFalse(out.Available), Password: isTrue(out.Password), Removed: isTrue(out.Removed)}, nil
}

func (c *WebshareClient) FileLink(ctx context.Context, ident string) (string, error) {
	ident = strings.TrimSpace(ident)
	if ident == "" {
		return "", errors.New("webshare_ident_empty")
	}
	var out wsLinkResponse
	if err := c.postXML(ctx, "/file_link/", url.Values{"ident": {ident}}, &out); err != nil {
		return "", err
	}
	if !okStatus(out.Status) {
		return "", mapFileLinkError(out.Code, out.Message)
	}
	if strings.TrimSpace(out.Link) == "" {
		return "", errors.New("webshare_link_empty")
	}
	return strings.TrimSpace(out.Link), nil
}

func (c *WebshareClient) postXML(ctx context.Context, path string, values url.Values, out any) error {
	c.mu.RLock()
	wst := c.wst
	c.mu.RUnlock()
	if wst != "" {
		values.Set("wst", wst)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/xml")
	// Some Webshare deployments expect WST as a cookie/header in addition to
	// the form field. This does not reveal the token in logs or URLs.
	if wst != "" {
		req.Header.Set("X-WST", wst)
		req.AddCookie(&http.Cookie{Name: "wst", Value: wst})
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webshare_http_status:%d", resp.StatusCode)
	}
	return xml.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(out)
}

type webshareResolver struct {
	api *WebshareClient
}

func NewWebshareResolver() Resolver {
	api := NewWebshareClient(WebshareOptions{})
	return &webshareResolver{api: api}
}

// NewWebshareResolverWithClient shares an authenticated client with search.
func NewWebshareResolverWithClient(client *WebshareClient) Resolver {
	if client == nil {
		client = NewWebshareClient(WebshareOptions{})
	}
	return &webshareResolver{api: client}
}

func (r *webshareResolver) CanHandle(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "webshare.cz" || strings.HasSuffix(host, ".webshare.cz")
}

func (r *webshareResolver) Resolve(ctx context.Context, rawURL string) (*ResolvedTarget, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	ident := extractWebshareIdent(u)
	if ident == "" {
		return nil, errors.New("webshare_ident_not_found")
	}
	info, err := r.api.FileInfo(ctx, ident)
	if err != nil {
		return nil, err
	}
	link, err := r.api.FileLink(ctx, ident)
	if err != nil {
		return nil, err
	}
	return &ResolvedTarget{Kind: "aria2", URL: link, Options: map[string]string{
		"max-connection-per-server": "1", "split": "1", "continue": "false", "always-resume": "false", "allow-overwrite": "true", "auto-file-renaming": "false",
	}, Headers: map[string]string{"Referer": "https://webshare.cz/", "User-Agent": "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"}, Filename: info.Filename, Size: info.Size}, nil
}

type wsInfoResponse struct {
	Status    string `xml:"status"`
	Name      string `xml:"name"`
	Size      string `xml:"size"`
	Message   string `xml:"message"`
	Code      string `xml:"code"`
	Removed   string `xml:"removed"`
	Password  string `xml:"password"`
	Available string `xml:"available"`
}
type wsLinkResponse struct {
	Status  string `xml:"status"`
	Link    string `xml:"link"`
	Message string `xml:"message"`
	Code    string `xml:"code"`
}
type wsSearchFile struct {
	Ident     string `xml:"ident"`
	Name      string `xml:"name"`
	Filename  string `xml:"filename"`
	Type      string `xml:"type"`
	Size      string `xml:"size"`
	Votes     string `xml:"votes"`
	Category  string `xml:"category"`
	Available string `xml:"available"`
	Password  string `xml:"password"`
	Removed   string `xml:"removed"`
	Encrypted string `xml:"encrypted"`
	Rating    string `xml:"rating"`
}
type wsSearchResponse struct {
	Status      string         `xml:"status"`
	Message     string         `xml:"message"`
	Code        string         `xml:"code"`
	Files       []wsSearchFile `xml:"file"`
	NestedFiles []wsSearchFile `xml:"files>file"`
}

func (f wsSearchFile) toPublic() WebshareFile {
	name := f.Name
	if name == "" {
		name = f.Filename
	}
	category := f.Category
	if category == "" {
		category = f.Type
	}
	size, _ := strconv.ParseInt(strings.TrimSpace(f.Size), 10, 64)
	rating, _ := strconv.ParseFloat(strings.TrimSpace(f.Rating), 64)
	return WebshareFile{Ident: f.Ident, Filename: name, Size: size, Category: category, Available: !isFalse(f.Available), Password: isTrue(f.Password), Removed: isTrue(f.Removed), Encrypted: isTrue(f.Encrypted), Rating: rating}
}

func okStatus(status string) bool { return strings.EqualFold(strings.TrimSpace(status), "OK") }
func isTrue(s string) bool {
	s = strings.TrimSpace(s)
	return s == "1" || strings.EqualFold(s, "true") || strings.EqualFold(s, "yes")
}
func isFalse(s string) bool {
	s = strings.TrimSpace(s)
	return s != "" && (s == "0" || strings.EqualFold(s, "false") || strings.EqualFold(s, "no"))
}
func wsAPIError(prefix, code, message string) error {
	return fmt.Errorf("%s:%s:%s", prefix, strings.TrimSpace(code), strings.TrimSpace(message))
}

func mapFileLinkError(code, message string) error {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "FILE_LINK_FATAL_1":
		return fmt.Errorf("%w: %s", ErrFileNotFound, message)
	case "FILE_LINK_FATAL_2":
		return ErrCaptchaNeeded
	case "FILE_LINK_FATAL_3":
		return fmt.Errorf("%w: %s", ErrWrongPassword, message)
	case "FILE_LINK_FATAL_4":
		return fmt.Errorf("%w: %s", ErrFileUnavailable, message)
	case "FILE_LINK_FATAL_5":
		return fmt.Errorf("%w: %s", ErrTooManyDownloads, message)
	case "FILE_LINK_FATAL_6":
		return fmt.Errorf("%w: %s", ErrCopyrightedFile, message)
	default:
		return fmt.Errorf("webshare_link_error:%s:%s", code, message)
	}
}

var wsIdentRe = regexp.MustCompile(`^[A-Za-z0-9]{5,}$`)

// IsValidWebshareIdent reports whether value is safe to persist as a stable
// Webshare file identifier.
func IsValidWebshareIdent(value string) bool {
	return wsIdentRe.MatchString(strings.TrimSpace(value))
}

// ExtractWebshareIdent returns the stable file identifier from a Webshare URL.
func ExtractWebshareIdent(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host != "webshare.cz" && !strings.HasSuffix(host, ".webshare.cz") {
		return ""
	}
	return extractWebshareIdent(u)
}

func extractWebshareIdent(u *url.URL) string {
	if q := u.Query().Get("ident"); IsValidWebshareIdent(q) {
		return q
	}
	if q := u.Query().Get("id"); IsValidWebshareIdent(q) {
		return q
	}
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	if ident := identFromSegments(segments); ident != "" {
		return ident
	}
	if u.Fragment != "" {
		parts := strings.Split(u.Fragment, "/")
		if ident := identFromSegments(parts); ident != "" {
			return ident
		}
	}
	return ""
}

func identFromSegments(segments []string) string {
	for i := 0; i+1 < len(segments); i++ {
		if strings.EqualFold(segments[i], "file") && IsValidWebshareIdent(segments[i+1]) {
			return segments[i+1]
		}
	}
	for i := len(segments) - 1; i >= 0; i-- {
		if IsValidWebshareIdent(segments[i]) {
			return segments[i]
		}
	}
	return ""
}
