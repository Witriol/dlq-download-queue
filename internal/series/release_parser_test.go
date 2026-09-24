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

func TestReleaseGroupIgnoresSiteTag(t *testing.T) {
	tagged := ParseReleaseName("X.S01E09.1080p.HEVC.x265-MeGusta[EZTVx.to].mkv")
	plain := ParseReleaseName("X.S01E09.1080p.HEVC.x265-MeGusta.mkv")
	if tagged.ReleaseGroup != "megusta" || tagged.ReleaseGroup != plain.ReleaseGroup {
		t.Fatalf("groups = %q, %q; want megusta", tagged.ReleaseGroup, plain.ReleaseGroup)
	}
	if got := ParseReleaseName("Show.S01E01.720p.HDTV.x264-KILLERS[rartv].mkv").ReleaseGroup; got != "killers" {
		t.Fatalf("rartv group = %q", got)
	}
	// Profiles stored before the fix hold the tag fragment.
	stored := plain
	stored.ReleaseGroup = "megusta[eztvx"
	scored := EvaluateCandidates(stored, []ReleaseCandidate{{Ident: "a", Filename: "X.S01E09.1080p.HEVC.x265-MeGusta.mkv", Size: 700 << 20, Available: true}}, MatchOptions{Season: 1, Episode: 9})
	if len(scored) != 1 || !scored[0].Exact {
		t.Fatalf("stored tagged group did not match exactly: %+v", scored)
	}
}
