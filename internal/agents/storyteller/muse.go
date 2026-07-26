package storyteller

import (
	"sort"
	"strings"

	"github.com/baalimago/kinoview/internal/model"
)

// Muse supplies what the next play should be about.
//
// The storyteller asks it at generation time rather than being handed a value,
// because preparation happens long after the story was requested — by then the
// household may have watched something else.
type Muse interface {
	// Theme returns a short description of what to riff on, or "" for none.
	Theme() string
}

// MuseFunc adapts a plain function to a Muse.
type MuseFunc func() string

func (f MuseFunc) Theme() string { return f() }

// LatestTheme picks the most recently watched title across every session.
//
// Sessions are per-client, so "most recent" has to be resolved across all of
// them rather than trusting any single context's LastPlayedName.
func LatestTheme(ctxs []model.ClientContext) string {
	type seen struct {
		name string
		at   int64
	}
	var all []seen
	for _, c := range ctxs {
		for _, v := range c.ViewingHistory {
			name := cleanTitle(v.Name)
			if name == "" {
				continue
			}
			all = append(all, seen{name: name, at: v.ViewedAt.Unix()})
		}
		// LastPlayedName has no timestamp of its own; treat it as the end of
		// that session so it still beats older history entries.
		if n := cleanTitle(c.LastPlayedName); n != "" {
			all = append(all, seen{name: n, at: c.StartTime.Unix()})
		}
	}
	if len(all) == 0 {
		return ""
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].at > all[j].at })
	return all[0].name
}

// cleanTitle turns a filename into something a playwright can read: drops the
// extension, separators and the usual release-scene noise.
func cleanTitle(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.LastIndex(s, "."); i > 0 && len(s)-i <= 5 {
		s = s[:i]
	}
	s = strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(s)

	noise := map[string]bool{
		"1080p": true, "720p": true, "2160p": true, "480p": true, "4k": true,
		"x264": true, "x265": true, "h264": true, "h265": true, "hevc": true,
		"bluray": true, "webrip": true, "web": true, "dl": true, "hdtv": true,
		"aac": true, "ac3": true, "dts": true, "remux": true, "proper": true,
		"repack": true, "extended": true, "unrated": true,
	}
	var keep []string
	for _, f := range strings.Fields(s) {
		if noise[strings.ToLower(f)] {
			// Everything after the first noise token is almost always more of
			// the same, so stop rather than filter word by word.
			break
		}
		keep = append(keep, f)
	}
	out := strings.Join(keep, " ")
	if out == "" {
		out = s
	}
	if len(out) > 60 {
		out = strings.TrimSpace(out[:60])
	}
	return strings.TrimSpace(out)
}
