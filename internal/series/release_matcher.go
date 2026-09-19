package series

import (
	"path/filepath"
	"sort"
	"strings"
)

const (
	PreferenceIgnore  = "ignore"
	PreferencePrefer  = "prefer"
	PreferenceRequire = "require"
)

// ReleaseCandidate is intentionally independent of the Webshare client so
// tests and future providers can use the same matching rules.
type ReleaseCandidate struct {
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

type MatchOptions struct {
	Season                int
	Episode               int
	ExpectedTitle         string
	MinimumSize           int64
	Preferences           map[string]string
	AllowUnknownExtension bool
}

type ScoredCandidate struct {
	Candidate       ReleaseCandidate
	Profile         ReleaseProfile
	Score           float64
	Accepted        bool
	Reasons         []string
	RejectedReasons []string
	Exact           bool
}

// EvaluateCandidates returns both accepted and rejected results so previews
// can explain hard-filter failures. RankCandidates is the queue-oriented
// subset containing accepted results only.
func EvaluateCandidates(profile ReleaseProfile, candidates []ReleaseCandidate, opts MatchOptions) []ScoredCandidate {
	if opts.Season == 0 && len(profile.Episodes) > 0 {
		opts.Season = profile.Episodes[0].Season
	}
	if opts.Episode == 0 && len(profile.Episodes) > 0 {
		opts.Episode = profile.Episodes[0].Episode
	}
	if opts.ExpectedTitle == "" {
		opts.ExpectedTitle = profile.SearchTitle
	}
	if opts.MinimumSize == 0 {
		opts.MinimumSize = 20 * 1024 * 1024
	}
	result := make([]ScoredCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, scoreCandidate(profile, candidate, opts))
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Accepted != result[j].Accepted {
			return result[i].Accepted
		}
		if result[i].Score != result[j].Score {
			return result[i].Score > result[j].Score
		}
		return strings.ToLower(result[i].Candidate.Filename) < strings.ToLower(result[j].Candidate.Filename)
	})
	return result
}

// RankCandidates performs hard filtering before preference scoring. In
// particular episode identity can never be outweighed by quality preferences.
func RankCandidates(profile ReleaseProfile, candidates []ReleaseCandidate, opts MatchOptions) []ScoredCandidate {
	result := make([]ScoredCandidate, 0, len(candidates))
	for _, scored := range EvaluateCandidates(profile, candidates, opts) {
		if scored.Accepted {
			result = append(result, scored)
		}
	}
	return result
}

func scoreCandidate(reference ReleaseProfile, candidate ReleaseCandidate, opts MatchOptions) ScoredCandidate {
	got := ParseReleaseName(candidate.Filename)
	out := ScoredCandidate{Candidate: candidate, Profile: got, Accepted: true, Exact: true}
	ext := ext(candidate.Filename)
	if !candidate.Available || candidate.Removed {
		reject(&out, "file unavailable")
	}
	if candidate.Password {
		reject(&out, "password protected")
	}
	if candidate.Encrypted {
		reject(&out, "encrypted file")
	}
	if isNonVideo(candidate.Filename) {
		reject(&out, "not a video release")
	}
	if !opts.AllowUnknownExtension && !isVideoExtension(ext) {
		reject(&out, "unsupported video extension")
	}
	if candidate.Size > 0 && candidate.Size < opts.MinimumSize {
		reject(&out, "file is suspiciously small")
	}
	if !hasEpisode(got, opts.Season, opts.Episode) {
		reject(&out, "wrong episode")
	} else {
		out.Reasons = append(out.Reasons, "correct episode")
	}
	if opts.ExpectedTitle != "" && !titleMatches(opts.ExpectedTitle, got.SearchTitle) {
		reject(&out, "different series title")
	}
	if !out.Accepted {
		return out
	}
	out.Score = 1
	scorePreference(&out, "release_group", reference.ReleaseGroup, got.ReleaseGroup, opts.Preferences, 30)
	scorePreference(&out, "resolution", reference.Resolution, got.Resolution, opts.Preferences, 20)
	scorePreference(&out, "codec", reference.Codec, got.Codec, opts.Preferences, 15)
	scorePreference(&out, "source", reference.Source, got.Source, opts.Preferences, 12)
	scorePreference(&out, "hdr", reference.HDR, got.HDR, opts.Preferences, 6)
	scorePreference(&out, "container", reference.Container, got.Container, opts.Preferences, 4)
	if reference.DolbyVision && got.DolbyVision {
		out.Score += 5
		out.Reasons = append(out.Reasons, "Dolby Vision")
	}
	if reference.BitDepth != 0 && reference.BitDepth == got.BitDepth {
		out.Score += 3
		out.Reasons = append(out.Reasons, "matching bit depth")
	}
	if reference.ReleaseGroup != "" && strings.EqualFold(reference.ReleaseGroup, got.ReleaseGroup) {
		out.Score += 1
	}
	return out
}

func scorePreference(out *ScoredCandidate, key, want, got string, preferences map[string]string, weight float64) {
	if want == "" {
		return
	}
	mode := strings.ToLower(preferences[key])
	if mode == "" {
		mode = PreferencePrefer
	}
	equal := normalizeProperty(key, want) == normalizeProperty(key, got)
	if mode != PreferenceIgnore && !equal {
		out.Exact = false
	}
	switch mode {
	case PreferenceRequire:
		if !equal {
			reject(out, key+" does not match required profile")
		} else {
			out.Score += weight
			out.Reasons = append(out.Reasons, "matching "+key)
		}
	case PreferenceIgnore:
		return
	default:
		if equal {
			out.Score += weight
			out.Reasons = append(out.Reasons, "matching "+key)
		} else {
			out.RejectedReasons = append(out.RejectedReasons, "different "+key)
		}
	}
}

func reject(out *ScoredCandidate, reason string) {
	out.Accepted = false
	out.RejectedReasons = append(out.RejectedReasons, reason)
}
func hasEpisode(p ReleaseProfile, season, episode int) bool {
	for _, e := range p.Episodes {
		if e.Season == season && e.Episode == episode {
			return true
		}
	}
	return false
}
func ext(path string) string { return strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".") }
func isVideoExtension(e string) bool {
	switch e {
	case "mkv", "mp4", "m4v", "avi", "mov", "ts", "m2ts", "webm":
		return true
	}
	return false
}
func isNonVideo(name string) bool {
	l := strings.ToLower(name)
	for _, token := range []string{"sample", "trailer", "teaser", "subs", "subtitle", "nfo"} {
		if strings.Contains(l, token) {
			return true
		}
	}
	return false
}

func titleMatches(want, got string) bool {
	ws := titleWords(want)
	gs := titleWords(got)
	if len(ws) == 0 {
		return true
	}
	for _, w := range ws {
		found := false
		for _, g := range gs {
			if w == g {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
func titleWords(s string) []string {
	var out []string
	for _, w := range strings.Fields(strings.ToLower(strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(s))) {
		if len(w) > 1 {
			out = append(out, w)
		}
	}
	return out
}
func normalizeProperty(key, s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if key == "release_group" {
		s = strings.ReplaceAll(strings.ReplaceAll(s, "-", ""), "_", "")
	}
	if key == "codec" {
		if s == "hevc" || s == "h.265" {
			return "x265"
		}
		if s == "h.264" {
			return "x264"
		}
	}
	return s
}
