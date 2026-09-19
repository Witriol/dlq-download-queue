package series

import "testing"

func TestParseReleaseName(t *testing.T) {
	p := ParseReleaseName("Some.Show.S01E05E06.1080p.WEB-DL.x265-MeGusta.mkv")
	if p.SearchTitle != "Some Show" {
		t.Fatalf("title = %q", p.SearchTitle)
	}
	if len(p.Episodes) != 2 || p.Episodes[0] != (ReleaseEpisode{1, 5}) || p.Episodes[1] != (ReleaseEpisode{1, 6}) {
		t.Fatalf("episodes = %#v", p.Episodes)
	}
	if p.Resolution != "1080p" || p.Source != "WEB-DL" || p.Codec != "x265" {
		t.Fatalf("quality = %#v", p)
	}
	if p.ReleaseGroup != "megusta" || p.Container != "mkv" || p.Confidence < .8 {
		t.Fatalf("profile = %#v", p)
	}
}

func TestParseReleaseNameAlternateEpisodeAndNormalization(t *testing.T) {
	p := ParseReleaseName("Some Show 1x05 1080 H.265 Me-Gusta.mp4")
	if len(p.Episodes) != 1 || p.Episodes[0] != (ReleaseEpisode{1, 5}) {
		t.Fatalf("episodes = %#v", p.Episodes)
	}
	if p.Resolution != "1080p" || p.Codec != "x265" {
		t.Fatalf("quality = %#v", p)
	}
}
