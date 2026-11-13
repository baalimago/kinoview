package media

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/baalimago/kinoview/internal/model"
)

// showRE holds a compiled regex and whether it captures season+episode or just episode.
type showRE struct {
	re        *regexp.Regexp
	hasSeason bool // false means the regex captures only episode (season assumed 1)
}

var showREs = []showRE{
	// S01E02 or s01e02
	{re: regexp.MustCompile(`(?i)^(.+?)[.\s\-_]*[Ss](\d{1,2})[Ee](\d{1,3})`), hasSeason: true},
	// 1x02 or 01x02. The separator is required, so a resolution tag such as
	// "1280x720" is not read as season 80 episode 720.
	{re: regexp.MustCompile(`(?i)^(.+?)[.\s\-_]+(\d{1,2})[xX](\d{1,3})`), hasSeason: true},
	// Season 01 Episode 02
	{re: regexp.MustCompile(`(?i)^(.+?)[.\s\-_]*Season[.\s\-_]*(\d{1,2})[.\s\-_]*Episode[.\s\-_]*(\d{1,3})`), hasSeason: true},
	// E02 (single episode within a show-named folder, assume season 1)
	{re: regexp.MustCompile(`(?i)^(.+?)[.\s\-_]*[Ee](\d{1,3})`), hasSeason: false},
}

// extractShowNameFromPath runs filename regex to get the show name.
func extractShowNameFromPath(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	stem = strings.ReplaceAll(stem, ".", " ")
	stem = strings.ReplaceAll(stem, "_", " ")

	for _, sre := range showREs {
		matches := sre.re.FindStringSubmatch(stem)
		if len(matches) >= 2 {
			return strings.TrimSpace(matches[1])
		}
	}
	return ""
}

// parseSeasonEpisodeFromMetadata extracts season and episode from Metadata JSON.
func parseSeasonEpisodeFromMetadata(it model.Item) (season int, episode int, ok bool) {
	if it.Metadata == nil {
		return 0, 0, false
	}
	var md map[string]any
	if err := json.Unmarshal(*it.Metadata, &md); err != nil {
		return 0, 0, false
	}
	if s, found := md["season"]; found {
		switch v := s.(type) {
		case float64:
			season = int(v)
		case string:
			season, _ = strconv.Atoi(v)
		}
	}
	if e, found := md["episode"]; found {
		switch v := e.(type) {
		case float64:
			episode = int(v)
		case string:
			episode, _ = strconv.Atoi(v)
		}
	}
	if season > 0 && episode > 0 {
		return season, episode, true
	}
	return 0, 0, false
}

// parseSeasonEpisodeFromPath runs filename regex to extract season and episode.
func parseSeasonEpisodeFromPath(path string) (season int, episode int, ok bool) {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	stem = strings.ReplaceAll(stem, ".", " ")
	stem = strings.ReplaceAll(stem, "_", " ")

	for _, sre := range showREs {
		matches := sre.re.FindStringSubmatch(stem)
		if sre.hasSeason && len(matches) >= 4 {
			season, _ = strconv.Atoi(matches[2])
			episode, _ = strconv.Atoi(matches[3])
			if season > 0 && episode > 0 {
				return season, episode, true
			}
		} else if !sre.hasSeason && len(matches) >= 3 {
			season = 1
			episode, _ = strconv.Atoi(matches[2])
			if episode > 0 {
				return season, episode, true
			}
		}
	}
	return 0, 0, false
}

// extractShowMetadata extracts show name, season, episode.
// Strategy:
//  1. If Metadata has season+episode → use those, and extract show name from filename.
//  2. Fallback: filename regex for everything.
func extractShowMetadata(it model.Item) (showName string, season int, episode int, ok bool) {
	season, episode, hasMeta := parseSeasonEpisodeFromMetadata(it)
	if hasMeta {
		showName = extractShowNameFromPath(it.Path)
		if showName != "" {
			return showName, season, episode, true
		}
	}

	// Full filename fallback
	showName = extractShowNameFromPath(it.Path)
	if showName == "" {
		return "", 0, 0, false
	}
	season, episode, ok = parseSeasonEpisodeFromPath(it.Path)
	if !ok {
		return "", 0, 0, false
	}
	return showName, season, episode, true
}

// normalizeShowName produces a canonical key for grouping.
func normalizeShowName(name string) string {
	return strings.TrimSpace(strings.ToLower(name))
}

// showsHandler serves the library browser: series grouped into seasons and
// episodes, and the standalone movies beside them.
func (i *Indexer) showsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		resp := buildLibraryView(i.store.Snapshot())
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, "failed to encode shows", http.StatusInternalServerError)
		}
	}
}

// buildLibraryView splits the video items of a library into the two views the
// sidebar offers: series (season → episode) and standalone movies.
func buildLibraryView(items []model.Item) model.ShowsResponse {
	return model.ShowsResponse{
		Shows:  buildSeries(items),
		Movies: buildMovies(items),
	}
}

// seriesBuilder accumulates episodes into their show and season while keeping
// the order the shows were first seen.
type seriesBuilder struct {
	byKey map[string]*model.ShowSeries
	order []string
}

// buildSeries groups the episodes of the library into shows and seasons.
func buildSeries(items []model.Item) []model.ShowSeries {
	b := seriesBuilder{byKey: make(map[string]*model.ShowSeries)}
	for _, item := range items {
		b.add(item)
	}
	shows := make([]model.ShowSeries, 0, len(b.order))
	for _, key := range b.order {
		shows = append(shows, *b.byKey[key])
	}
	sort.Slice(shows, func(a, c int) bool {
		return strings.ToLower(shows[a].Name) < strings.ToLower(shows[c].Name)
	})
	return shows
}

// add places one item into its show and season. Items that are not episodes are
// ignored: buildMovies picks them up.
func (b *seriesBuilder) add(item model.Item) {
	if !strings.Contains(item.MIMEType, "video") {
		return
	}
	if isExtra(item) {
		return
	}
	showName, season, episode, ok := extractShowMetadata(item)
	if !ok {
		return
	}
	show := b.show(showName)
	show.Seasons = addEpisode(show.Seasons, model.ShowEpisode{
		Item:     item,
		ShowName: showName,
		Season:   season,
		Episode:  episode,
	})
}

// isExtra reports whether the classifier filed the item as a bonus feature of a
// series rather than a show or a movie of its own. An extra belongs to neither
// view: it has no position a season can hold and no title a movie row can show.
func isExtra(item model.Item) bool {
	return mdString(metadataMap(item.Metadata), "extra_to") != ""
}

// show returns the show for the name, creating it on first sight.
func (b *seriesBuilder) show(name string) *model.ShowSeries {
	key := normalizeShowName(name)
	show, exists := b.byKey[key]
	if exists {
		return show
	}
	show = &model.ShowSeries{Name: name, Seasons: []model.ShowSeason{}}
	b.byKey[key] = show
	b.order = append(b.order, key)
	return show
}

// addEpisode inserts the episode into its season, keeping the seasons ordered
// by number and the episodes by episode number.
func addEpisode(seasons []model.ShowSeason, ep model.ShowEpisode) []model.ShowSeason {
	for idx := range seasons {
		if seasons[idx].Season != ep.Season {
			continue
		}
		seasons[idx].Episodes = insertEpisode(seasons[idx].Episodes, ep)
		return seasons
	}
	seasons = append(seasons, model.ShowSeason{Season: ep.Season, Episodes: []model.ShowEpisode{ep}})
	sort.Slice(seasons, func(a, b int) bool { return seasons[a].Season < seasons[b].Season })
	return seasons
}

func insertEpisode(episodes []model.ShowEpisode, ep model.ShowEpisode) []model.ShowEpisode {
	episodes = append(episodes, ep)
	sort.SliceStable(episodes, func(a, b int) bool { return episodes[a].Episode < episodes[b].Episode })
	return episodes
}

// releaseYearRE matches the release year a file name carries, which is where
// the release tags that follow it begin.
var releaseYearRE = regexp.MustCompile(`(?i)\s(19|20)\d{2}(\s|$)`)

// buildMovies lists the video items that belong to no series. Items the
// classifier marked as extras belong to a series they cannot be sorted into, so
// they are left out of both views.
func buildMovies(items []model.Item) []model.ShowMovie {
	movies := make([]model.ShowMovie, 0, len(items))
	for _, item := range items {
		if !strings.Contains(item.MIMEType, "video") {
			continue
		}
		if _, _, _, isEpisode := extractShowMetadata(item); isEpisode {
			continue
		}
		view := resolveSuggestionView(item)
		// An episode the classifier placed in a series keeps that series, even
		// when its file name does not parse: it is not a movie.
		if isExtra(item) || view.Kind == "episode" {
			continue
		}
		title := view.Title
		year := view.Year
		if view.Kind == "media" {
			var fallbackYear int
			title, fallbackYear = movieTitleFallback(item)
			if year == 0 {
				year = fallbackYear
			}
		}
		movies = append(movies, model.ShowMovie{Item: item, Title: title, Year: year})
	}
	sort.SliceStable(movies, func(a, b int) bool {
		left, right := strings.ToLower(movies[a].Title), strings.ToLower(movies[b].Title)
		if left != right {
			return left < right
		}
		return movies[a].ID < movies[b].ID
	})
	return movies
}

// movieTitleFallback reads a title and its release year off the file name of an
// unclassified movie, cutting the release tags at the year:
// "Warfare.2025.1080p" reads "Warfare", 2025.
func movieTitleFallback(item model.Item) (string, int) {
	name := cleanFilename(item)
	loc := releaseYearRE.FindStringIndex(name)
	if loc == nil || loc[0] == 0 {
		return name, 0
	}
	year, _ := strconv.Atoi(strings.TrimSpace(name[loc[0]:loc[1]]))
	return strings.TrimSpace(name[:loc[0]]), year
}
