package series

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const DefaultTVMazeBaseURL = "https://api.tvmaze.com"

// TVMazeClient is a small context-aware client for the two endpoints needed
// when creating and synchronizing a watch. It does not cache responses; the
// scheduler controls refresh cadence and can inject a test HTTP client.
type TVMazeClient struct {
	BaseURL    string
	HTTPClient *http.Client
	UserAgent  string
}

func NewTVMazeClient(baseURL string, httpClient *http.Client) *TVMazeClient {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = DefaultTVMazeBaseURL
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &TVMazeClient{BaseURL: baseURL, HTTPClient: httpClient, UserAgent: "dlq-series-watcher/1"}
}

func (c *TVMazeClient) SearchShows(ctx context.Context, query string) ([]TVMazeSearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("TVmaze search query is empty")
	}
	if showID, isURL, err := tvMazeShowID(query); err != nil {
		return nil, err
	} else if isURL {
		show, err := c.Show(ctx, showID)
		if err != nil {
			return nil, err
		}
		return []TVMazeSearchResult{{Score: 1, Show: *show}}, nil
	}
	query = normalizeTVMazeSearchQuery(query)
	if query == "" {
		return nil, errors.New("TVmaze search query is empty")
	}
	var out []TVMazeSearchResult
	if err := c.getJSON(ctx, "/search/shows?q="+url.QueryEscape(query), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *TVMazeClient) Show(ctx context.Context, showID int64) (*TVMazeShow, error) {
	if showID <= 0 {
		return nil, errors.New("TVmaze show ID must be positive")
	}
	var show TVMazeShow
	if err := c.getJSON(ctx, "/shows/"+strconv.FormatInt(showID, 10), &show); err != nil {
		return nil, err
	}
	return &show, nil
}

// normalizeTVMazeSearchQuery makes release-style titles usable with TVmaze's
// free-text search. Dots and underscores are common separators in filenames,
// but TVmaze indexes ordinary words separated by spaces.
func normalizeTVMazeSearchQuery(query string) string {
	query = strings.NewReplacer(".", " ", "_", " ").Replace(strings.TrimSpace(query))
	return strings.Join(strings.Fields(query), " ")
}

// tvMazeShowID recognizes only the canonical TVmaze show URL shape. The
// hostname is checked separately from the path so a URL containing a lookalike
// domain cannot cause an arbitrary endpoint to be fetched.
func tvMazeShowID(raw string) (int64, bool, error) {
	raw = strings.TrimSpace(raw)
	parseRaw := raw
	if !strings.Contains(raw, "://") && (strings.HasPrefix(strings.ToLower(raw), "tvmaze.com/") || strings.HasPrefix(strings.ToLower(raw), "www.tvmaze.com/")) {
		parseRaw = "https://" + raw
	}
	u, err := url.Parse(parseRaw)
	if err != nil || u.Hostname() == "" {
		return 0, false, nil
	}
	host := strings.ToLower(u.Hostname())
	if host != "tvmaze.com" && host != "www.tvmaze.com" {
		return 0, false, nil
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return 0, true, errors.New("TVmaze show URL must use http or https")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || !strings.EqualFold(parts[0], "shows") {
		return 0, true, errors.New("invalid TVmaze show URL")
	}
	showID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || showID <= 0 {
		return 0, true, errors.New("invalid TVmaze show ID in URL")
	}
	return showID, true, nil
}

// Episodes fetches the complete non-special episode list. TVmaze may return
// specials when requested, but v1 intentionally ignores those because they do
// not map reliably to standard SxxExx release names.
func (c *TVMazeClient) Episodes(ctx context.Context, showID int64) ([]TVMazeEpisode, error) {
	if showID <= 0 {
		return nil, errors.New("TVmaze show ID must be positive")
	}
	var out []TVMazeEpisode
	path := "/shows/" + strconv.FormatInt(showID, 10) + "/episodes?specials=0"
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *TVMazeClient) getJSON(ctx context.Context, path string, dst any) error {
	base := strings.TrimRight(c.BaseURL, "/")
	u, err := url.Parse(base + "/" + strings.TrimLeft(path, "/"))
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = resp.Status
		}
		return fmt.Errorf("TVmaze request %s: %s: %s", req.URL.Path, resp.Status, message)
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, 8<<20))
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("decode TVmaze response: %w", err)
	}
	return nil
}
