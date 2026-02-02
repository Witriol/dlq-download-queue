package resolver

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	webshareAPI = "https://webshare.cz/api"
)

type webshareResolver struct {
	client *http.Client
}

func NewWebshareResolver() Resolver {
	return &webshareResolver{
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

func (r *webshareResolver) CanHandle(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Host)
	return strings.Contains(host, "webshare.cz")
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
	info, err := r.fileInfo(ctx, ident)
	if err != nil {
		return nil, err
	}
	link, err := r.fileLink(ctx, ident)
	if err != nil {
		return nil, err
	}
	return &ResolvedTarget{
		Kind:     "aria2",
		URL:      link,
		Filename: info.Filename,
		Size:     info.Size,
	}, nil
}

type wsInfoResponse struct {
	Status   string `xml:"status"`
	Name     string `xml:"name"`
	Size     string `xml:"size"`
	Message  string `xml:"message"`
	Code     string `xml:"code"`
	Removed  string `xml:"removed"`
	Password string `xml:"password"`
	Available string `xml:"available"`
}

type wsLinkResponse struct {
	Status  string `xml:"status"`
	Link    string `xml:"link"`
	Message string `xml:"message"`
	Code    string `xml:"code"`
}

func (r *webshareResolver) fileInfo(ctx context.Context, ident string) (*ResolvedTarget, error) {
	form := url.Values{}
	form.Set("ident", ident)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webshareAPI+"/file_info/", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out wsInfoResponse
	if err := xml.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if strings.ToUpper(out.Status) != "OK" {
		return nil, fmt.Errorf("webshare_info_error:%s:%s", out.Code, out.Message)
	}
	size, _ := strconv.ParseInt(strings.TrimSpace(out.Size), 10, 64)
	return &ResolvedTarget{Filename: out.Name, Size: size}, nil
}

func (r *webshareResolver) fileLink(ctx context.Context, ident string) (string, error) {
	form := url.Values{}
	form.Set("ident", ident)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webshareAPI+"/file_link/", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out wsLinkResponse
	if err := xml.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if strings.ToUpper(out.Status) != "OK" {
		switch out.Code {
		case "FILE_LINK_FATAL_1":
			return "", ErrLoginRequired
		case "FILE_LINK_FATAL_2":
			return "", ErrCaptchaNeeded
		case "FILE_LINK_FATAL_3":
			return "", ErrQuotaExceeded
		case "FILE_LINK_FATAL_4":
			return "", ErrTemporarilyOff
		default:
			return "", fmt.Errorf("webshare_link_error:%s:%s", out.Code, out.Message)
		}
	}
	return out.Link, nil
}

var wsIdentRe = regexp.MustCompile(`^[A-Za-z0-9]{5,}$`)

func extractWebshareIdent(u *url.URL) string {
	q := u.Query().Get("ident")
	if q != "" {
		return q
	}
	q = u.Query().Get("id")
	if q != "" {
		return q
	}
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i := len(segments) - 1; i >= 0; i-- {
		seg := segments[i]
		if wsIdentRe.MatchString(seg) {
			return seg
		}
	}
	if u.Fragment != "" {
		parts := strings.Split(u.Fragment, "/")
		for i := len(parts) - 1; i >= 0; i-- {
			seg := parts[i]
			if wsIdentRe.MatchString(seg) {
				return seg
			}
		}
	}
	return ""
}
