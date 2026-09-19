package series

import "testing"

func TestRankCandidatesHardFiltersBeforePreferences(t *testing.T) {
	ref := ParseReleaseName("Some.Show.S01E05.1080p.WEB-DL.x265-MeGusta.mkv")
	got := RankCandidates(ref, []ReleaseCandidate{
		{Ident: "wrong", Filename: "Some.Show.S01E06.1080p.WEB-DL.x265-MeGusta.mkv", Size: 700 << 20, Available: true},
		{Ident: "right", Filename: "Some.Show.S01E05.1080p.WEB-DL.x265-Me-Gusta.mkv", Size: 700 << 20, Available: true},
		{Ident: "tiny", Filename: "Some.Show.S01E05.1080p.WEB-DL.x265-MeGusta.mkv", Size: 100, Available: true},
	}, MatchOptions{MinimumSize: 20 << 20, Preferences: map[string]string{"release_group": PreferenceRequire}})
	if len(got) != 1 || got[0].Candidate.Ident != "right" {
		t.Fatalf("ranked = %#v", got)
	}
	if len(got[0].Reasons) == 0 {
		t.Fatal("expected explanations")
	}
}

func TestRankCandidatesRejectsUnavailableFiles(t *testing.T) {
	ref := ParseReleaseName("Some.Show.S01E05.1080p.WEB-DL.x265-MeGusta.mkv")
	got := EvaluateCandidates(ref, []ReleaseCandidate{{
		Ident: "gone", Filename: "Some.Show.S01E05.1080p.WEB-DL.x265-MeGusta.mkv", Size: 700 << 20,
	}}, MatchOptions{})
	if len(got) != 1 || got[0].Accepted {
		t.Fatalf("expected unavailable file to be rejected, got %#v", got)
	}
}
