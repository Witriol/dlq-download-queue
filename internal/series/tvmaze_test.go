package series

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestTVMazeSearchAndEpisodes(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `[{"score":0.98,"show":{"id":123,"name":"The Office","genres":["Comedy"]}}]`
		if r.URL.Path == "/shows/123/episodes" {
			if r.URL.Query().Get("specials") != "0" {
				t.Errorf("specials query = %q", r.URL.Query().Get("specials"))
			}
			body = `[{"id":500,"name":"Pilot","season":1,"number":1,"airdate":"2005-03-24","airtime":"20:30","airstamp":"2005-03-25T00:30:00+00:00"},{"id":501,"name":"Special","season":0,"number":null,"airdate":"","airtime":"","airstamp":null}]`
		} else if r.URL.Path == "/search/shows" {
			if got := r.URL.Query().Get("q"); got != "The Office" {
				t.Errorf("query = %q", got)
			}
		} else {
			return &http.Response{StatusCode: http.StatusNotFound, Status: "404 Not Found", Body: ioNopCloser("not found"), Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: ioNopCloser(body), Header: http.Header{"Content-Type": []string{"application/json"}}}, nil
	})
	client := NewTVMazeClient("https://example.test", &http.Client{Transport: transport})
	shows, err := client.SearchShows(context.Background(), "The Office")
	if err != nil {
		t.Fatal(err)
	}
	if len(shows) != 1 || shows[0].Show.ID != 123 || shows[0].Show.Name != "The Office" {
		t.Fatalf("unexpected search result: %+v", shows)
	}
	episodes, err := client.Episodes(context.Background(), 123)
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 2 || episodes[0].Airstamp == nil || episodes[1].Number != nil {
		t.Fatalf("unexpected episodes: %+v", episodes)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type stringReadCloser struct{ *strings.Reader }

func (stringReadCloser) Close() error { return nil }

func ioNopCloser(s string) stringReadCloser { return stringReadCloser{strings.NewReader(s)} }

func TestTVMazeErrors(t *testing.T) {
	client := NewTVMazeClient("http://127.0.0.1:1", nil)
	if _, err := client.SearchShows(context.Background(), " "); err == nil {
		t.Fatal("expected empty query error")
	}
	if _, err := client.Episodes(context.Background(), 0); err == nil {
		t.Fatal("expected invalid id error")
	}
	if !strings.Contains(client.UserAgent, "dlq-series") {
		t.Fatal("expected identifying user agent")
	}
}

func TestTVMazeSearchNormalizesReleaseTitle(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.URL.Query().Get("q"); got != "Outlander Blood of My Blood" {
			t.Errorf("normalized query = %q", got)
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: ioNopCloser("[]"), Header: make(http.Header)}, nil
	})
	client := NewTVMazeClient("https://example.test", &http.Client{Transport: transport})
	if _, err := client.SearchShows(context.Background(), "Outlander.Blood_of.My.Blood"); err != nil {
		t.Fatal(err)
	}
}

func TestTVMazeSearchURLFetchesExactShow(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/shows/123" {
			t.Errorf("path = %q", r.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: ioNopCloser(`{"id":123,"name":"Outlander: Blood of My Blood"}`), Header: make(http.Header)}, nil
	})
	client := NewTVMazeClient("https://example.test", &http.Client{Transport: transport})
	shows, err := client.SearchShows(context.Background(), "https://www.tvmaze.com/shows/123/outlander-blood-of-my-blood")
	if err != nil {
		t.Fatal(err)
	}
	if len(shows) != 1 || shows[0].Show.ID != 123 || shows[0].Show.Name != "Outlander: Blood of My Blood" {
		t.Fatalf("unexpected show: %+v", shows)
	}
}

func TestTVMazeShowReturnsStatus(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/shows/123" {
			t.Errorf("path = %q", r.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: ioNopCloser(`{"id":123,"name":"Star Trek","status":"Ended","network":null,"webChannel":{"name":"Hulu","country":{"timezone":"America/New_York"}}}`), Header: make(http.Header)}, nil
	})
	client := NewTVMazeClient("https://example.test", &http.Client{Transport: transport})
	show, err := client.Show(context.Background(), 123)
	if err != nil {
		t.Fatal(err)
	}
	if show.ID != 123 || show.Status != "Ended" || show.airTimezone() != "America/New_York" {
		t.Fatalf("unexpected show: %+v", show)
	}
	if _, err := client.Show(context.Background(), 0); err == nil {
		t.Fatal("expected invalid id error")
	}
}

func TestTVMazeSearchURLRejectsInvalidShowID(t *testing.T) {
	client := NewTVMazeClient("https://example.test", nil)
	if _, err := client.SearchShows(context.Background(), "https://tvmaze.com/shows/not-a-number/foo"); err == nil {
		t.Fatal("expected invalid show URL error")
	}
	if _, isURL, err := tvMazeShowID("https://tvmaze.com.evil/shows/123/foo"); err != nil || isURL {
		t.Fatalf("lookalike hostname incorrectly recognized as TVmaze URL: isURL=%v err=%v", isURL, err)
	}
}
