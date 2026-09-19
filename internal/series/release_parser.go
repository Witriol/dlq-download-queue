package series

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ReleaseEpisode identifies an episode embedded in a release name.
type ReleaseEpisode struct{ Season, Episode int }

// ReleaseProfile is the deliberately small, serializable subset of scene
// naming information used by the watcher. RawTokens lets the UI show what was
// recognized and Confidence prevents silent activation on ambiguous names.
type ReleaseProfile struct {
	Filename     string           `json:"filename,omitempty"`
	SearchTitle  string           `json:"search_title,omitempty"`
	Episodes     []ReleaseEpisode `json:"episodes,omitempty"`
	Resolution   string           `json:"resolution,omitempty"`
	Codec        string           `json:"codec,omitempty"`
	Source       string           `json:"source,omitempty"`
	HDR          string           `json:"hdr,omitempty"`
	DolbyVision  bool             `json:"dolby_vision,omitempty"`
	BitDepth     int              `json:"bit_depth,omitempty"`
	Audio        []string         `json:"audio,omitempty"`
	Container    string           `json:"container,omitempty"`
	Extension    string           `json:"extension,omitempty"`
	ReleaseGroup string           `json:"release_group,omitempty"`
	RawTokens    []string         `json:"raw_tokens,omitempty"`
	Confidence   float64          `json:"confidence"`
}

var (
	releaseEpisodeRe = regexp.MustCompile(`(?i)(?:\bS(\d{1,3})E(\d{1,3})(E(\d{1,3}))*\b)|(?:\b(\d{1,3})x(\d{1,3})\b)`)
	resolutionRe     = regexp.MustCompile(`(?i)^(?:480|576|720|1080|1440|2160|4320)(?:p|i)?$`)
	bitDepthRe       = regexp.MustCompile(`(?i)^(8|10|12)bit$`)
)

// ParseReleaseName extracts a profile from a filename. It accepts both scene
// separators (dots/underscores/spaces) and a normal URL/path basename.
func ParseReleaseName(filename string) ReleaseProfile {
	p := ReleaseProfile{Filename: filename}
	base := filepath.Base(strings.TrimSpace(filename))
	p.Extension = strings.TrimPrefix(strings.ToLower(filepath.Ext(base)), ".")
	if p.Extension != "" {
		p.Container = p.Extension
	}
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	p.RawTokens = releaseTokens(stem)
	match := releaseEpisodeRe.FindStringSubmatchIndex(stem)
	if match == nil {
		p.SearchTitle = cleanTitle(stem)
		p.Confidence = 0.15
		return p
	}
	p.Episodes = parseEpisodes(stem[match[0]:match[1]])
	p.SearchTitle = cleanTitle(stem[:match[0]])
	if p.SearchTitle == "" {
		p.SearchTitle = cleanTitle(stem)
	}
	after := stem[match[1]:]
	parts := releaseTokens(after)
	all := releaseTokens(stem)
	for _, token := range all {
		classifyToken(&p, token)
	}
	classifyCompoundFacts(&p, stem)
	if group := releaseGroup(stem[match[1]:]); group != "" {
		p.ReleaseGroup = canonicalGroup(group)
	}
	// A number and title are the only facts that are safe to require. Every
	// additional recognized token improves confidence, but never becomes an
	// implicit hard requirement.
	p.Confidence = 0.55
	if p.SearchTitle != "" {
		p.Confidence += 0.15
	}
	if p.Resolution != "" {
		p.Confidence += 0.08
	}
	if p.Source != "" || p.Codec != "" {
		p.Confidence += 0.07
	}
	if p.ReleaseGroup != "" {
		p.Confidence += 0.05
	}
	if len(parts) > 0 || p.Extension != "" {
		p.Confidence += 0.05
	}
	if p.Confidence > 1 {
		p.Confidence = 1
	}
	return p
}

func parseEpisodes(value string) []ReleaseEpisode {
	value = strings.ToUpper(value)
	m := regexp.MustCompile(`S(\d{1,3})E(\d{1,3})((?:E\d{1,3})*)`).FindStringSubmatch(value)
	if len(m) == 0 {
		m = regexp.MustCompile(`(\d{1,3})X(\d{1,3})`).FindStringSubmatch(value)
		if len(m) == 0 {
			return nil
		}
		e, _ := strconv.Atoi(m[2])
		s, _ := strconv.Atoi(m[1])
		return []ReleaseEpisode{{Season: s, Episode: e}}
	}
	s, _ := strconv.Atoi(m[1])
	e, _ := strconv.Atoi(m[2])
	out := []ReleaseEpisode{{Season: s, Episode: e}}
	for _, n := range regexp.MustCompile(`E(\d{1,3})`).FindAllStringSubmatch(m[3], -1) {
		v, _ := strconv.Atoi(n[1])
		out = append(out, ReleaseEpisode{Season: s, Episode: v})
	}
	return out
}

func releaseTokens(value string) []string {
	value = strings.ReplaceAll(value, "-", " ")
	value = strings.ReplaceAll(value, "_", " ")
	value = strings.ReplaceAll(value, ".", " ")
	return strings.Fields(value)
}

func classifyToken(p *ReleaseProfile, token string) {
	l := strings.ToLower(strings.TrimSpace(token))
	if l == "" {
		return
	}
	switch {
	case resolutionRe.MatchString(l):
		if !strings.HasSuffix(l, "p") && !strings.HasSuffix(l, "i") {
			l += "p"
		}
		p.Resolution = l
	case l == "x264" || l == "h264" || l == "h.264":
		p.Codec = "x264"
	case l == "x265" || l == "h265" || l == "h.265" || l == "hevc":
		p.Codec = "x265"
	case l == "av1":
		p.Codec = "av1"
	case l == "web-dl" || l == "webdl" || l == "web":
		p.Source = "WEB-DL"
	case l == "webrip":
		p.Source = "WEBRip"
	case l == "hdtv":
		p.Source = "HDTV"
	case l == "bluray" || l == "blu-ray":
		p.Source = "BluRay"
	case l == "hdr" || l == "hdr10" || l == "hdr10+":
		p.HDR = strings.ToUpper(l)
	case l == "dv" || l == "dolbyvision" || l == "dolby":
		p.DolbyVision = true
	case bitDepthRe.MatchString(l):
		p.BitDepth, _ = strconv.Atoi(l[:len(l)-3])
	case strings.HasPrefix(l, "atmos") || l == "ddp" || l == "dd+" || l == "aac" || l == "ac3" || l == "dts" || l == "truehd":
		p.Audio = appendUnique(p.Audio, strings.ToUpper(l))
	}
}

func classifyCompoundFacts(p *ReleaseProfile, stem string) {
	l := strings.ToLower(stem)
	if regexp.MustCompile(`web[- .]?dl`).MatchString(l) {
		p.Source = "WEB-DL"
	}
	if regexp.MustCompile(`web[- .]?rip`).MatchString(l) {
		p.Source = "WEBRip"
	}
	if regexp.MustCompile(`blu[- .]?ray`).MatchString(l) {
		p.Source = "BluRay"
	}
	if regexp.MustCompile(`h[.]?264|x264`).MatchString(l) {
		p.Codec = "x264"
	}
	if regexp.MustCompile(`h[.]?265|x265|hevc`).MatchString(l) {
		p.Codec = "x265"
	}
}

func releaseGroup(suffix string) string {
	// A hyphen is the strongest conventional group delimiter. Keep only the
	// final token and reject known technical fields.
	lower := strings.ToLower(suffix)
	last := -1
	for _, marker := range []string{"x265", "x264", "hevc", "av1", "web-dl", "web.dl", "webrip", "hdtv", "bluray"} {
		if i := strings.LastIndex(lower, marker); i > last {
			last = i
		}
	}
	if last >= 0 {
		candidate := ""
		// The marker is not always four bytes; use the actual marker length.
		for _, marker := range []string{"x265", "x264", "hevc", "av1", "web-dl", "web.dl", "webrip", "hdtv", "bluray"} {
			end := last + len(marker)
			if end <= len(suffix) && strings.EqualFold(suffix[last:end], marker) {
				candidate = strings.TrimLeft(suffix[last+len(marker):], "._ -")
				break
			}
		}
		if fields := strings.FieldsFunc(candidate, func(r rune) bool { return r == '.' || r == '_' }); len(fields) > 0 && fields[0] != "" {
			return fields[0]
		}
	}
	i := strings.LastIndex(suffix, "-")
	if i < 0 {
		return ""
	}
	fields := strings.FieldsFunc(suffix[i+1:], func(r rune) bool { return r == '.' || r == '_' || r == '-' })
	if len(fields) == 0 {
		return ""
	}
	g := strings.TrimSpace(fields[0])
	if g == "" || strings.ContainsAny(g, "[]()") {
		return ""
	}
	return g
}

func canonicalGroup(s string) string {
	return strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(s), "-", ""), "_", ""))
}
func appendUnique(in []string, value string) []string {
	for _, v := range in {
		if v == value {
			return in
		}
	}
	return append(in, value)
}

func cleanTitle(s string) string {
	s = strings.TrimSpace(strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(s))
	s = regexp.MustCompile(`(?i)\b(?:S\d{1,3}E\d{1,3}|\d{1,3}x\d{1,3})\b`).ReplaceAllString(s, "")
	return strings.Join(strings.Fields(s), " ")
}
