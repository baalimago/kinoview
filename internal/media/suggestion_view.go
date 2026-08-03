package media

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/baalimago/kinoview/internal/model"
)

// resolveSuggestionView derives the display data for one suggestion card.
//
// The classifier metadata is not a reliable source for the series name: it
// historically had no showName field, and the LLM sometimes puts the episode
// title in "name" (Stargate) and sometimes the series name (The Office). The
// file path is the deterministic source of truth for show/season/episode —
// the same extraction the shows browser uses. Metadata showName (when the
// concierge or a newer classifier has filled it) is preferred over the path.
func resolveSuggestionView(item model.Item) model.SuggestionView {
	md := metadataMap(item.Metadata)

	season, episode, hasMetaPos := parseSeasonEpisodeFromMetadata(item)
	pathShowName, pathSeason, pathEpisode, hasPath := extractShowMetadata(item)

	// Prefer the metadata position; fall back to the path-derived one.
	if !hasMetaPos && hasPath {
		season, episode = pathSeason, pathEpisode
	}

	showName := mdString(md, "showName")
	if showName == "" {
		showName = pathShowName
	}

	name := mdString(md, "name")
	altName := mdString(md, "alt_name")

	view := model.SuggestionView{
		Kind:        suggestionKind(md, season, episode),
		Title:       showName,
		Season:      season,
		Episode:     episode,
		Year:        mdInt(md, "year"),
		DurationMin: mdInt(md, "duration_min"),
		Language:    mdLanguage(md),
		Description: mdString(md, "description"),
		Actors:      mdStrings(md, "actors"),
	}

	switch view.Kind {
	case "episode":
		// The title is the series name; the episode title is the metadata
		// name/alt_name that differs from it.
		if view.Title == "" {
			view.Title = cleanFilename(item)
		}
		view.EpisodeTitle = episodeTitle(name, altName, view.Title)
	case "extras":
		// Label the card with the main media; the extras' own name becomes
		// the secondary title.
		if view.Title == "" {
			view.Title = mdString(md, "extra_to")
		}
		if view.Title == "" {
			view.Title = cleanFilename(item)
		}
		view.EpisodeTitle = episodeTitle(name, altName, view.Title)
	default:
		view.Title = firstNonEmpty(name, altName, cleanFilename(item))
	}
	return view
}

// enrichSuggestions attaches the resolved card view to each suggestion at
// payload build time. It is the single choke point for both the GET handler
// and the websocket broadcast, so every suggestion renders identically no
// matter which agent created it, and legacy suggestions (whose metadata
// predates showName) still resolve through the file path.
func enrichSuggestions(recs []model.Suggestion) []model.Suggestion {
	out := make([]model.Suggestion, len(recs))
	for i, rec := range recs {
		view := resolveSuggestionView(rec.Item)
		rec.View = &view
		out[i] = rec
	}
	return out
}

func suggestionKind(md map[string]any, season, episode int) string {
	if mdString(md, "extra_to") != "" {
		return "extras"
	}
	if season > 0 && episode > 0 {
		return "episode"
	}
	if mdString(md, "name") != "" || mdString(md, "alt_name") != "" {
		return "movie"
	}
	return "media"
}

// episodeTitle picks the metadata title that identifies the episode itself,
// i.e. name/alt_name when it differs from the series name. The classifier has
// used both fields for this job, so try name first, then alt_name.
func episodeTitle(name, altName, series string) string {
	for _, cand := range []string{name, altName} {
		if cand == "" || strings.EqualFold(cand, series) {
			continue
		}
		return cand
	}
	return ""
}

func metadataMap(raw *json.RawMessage) map[string]any {
	if raw == nil {
		return nil
	}
	var md map[string]any
	if err := json.Unmarshal(*raw, &md); err != nil {
		return nil
	}
	return md
}

// mdLanguage reads the spoken-language field. The classifier format spells it
// "langugae" (historical typo); accept the corrected spelling too.
func mdLanguage(md map[string]any) string {
	if s := mdString(md, "language"); s != "" {
		return s
	}
	return mdString(md, "langugae")
}

func mdString(md map[string]any, key string) string {
	v, ok := md[key]
	if !ok {
		return ""
	}
	switch s := v.(type) {
	case string:
		return strings.TrimSpace(s)
	case float64:
		return strconv.FormatFloat(s, 'f', -1, 64)
	}
	return ""
}

func mdInt(md map[string]any, key string) int {
	v, ok := md[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	}
	return 0
}

func mdStrings(md map[string]any, key string) []string {
	v, ok := md[key]
	if !ok {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// cleanFilename strips the extension and replaces separators, mirroring the
// frontend's legacy fallback for unclassified items.
func cleanFilename(item model.Item) string {
	base := filepath.Base(strings.TrimSpace(item.Name))
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	stem = strings.ReplaceAll(stem, ".", " ")
	stem = strings.ReplaceAll(stem, "_", " ")
	return strings.Join(strings.Fields(stem), " ")
}
