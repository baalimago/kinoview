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
	re         *regexp.Regexp
	hasSeason  bool // false means the regex captures only episode (season assumed 1)
}

var showREs = []showRE{
	// S01E02 or s01e02
	{re: regexp.MustCompile(`(?i)^(.+?)[.\s\-_]*[Ss](\d{1,2})[Ee](\d{1,3})`), hasSeason: true},
	// 1x02 or 01x02
	{re: regexp.MustCompile(`(?i)^(.+?)[.\s\-_]*(\d{1,2})[xX](\d{1,3})`), hasSeason: true},
	// Season 01 Episode 02
	{re: regexp.MustCompile(`(?i)^(.+?)[.\s\-_]*Season[.\s\-_]*(\d{1,2})[.\s\-_]*Episode[.\s\-_]*(\d{1,3})`), hasSeason: true},
	// E02 (single episode within a show-named folder, assume season 1)
	{re: regexp.MustCompile(`(?i)^(.+?)[.\s\-_]*[Ee](\d{1,3})`), hasSeason: false},
}

// extractShowMetadata attempts to extract show name, season, episode from Metadata JSON,
// and falls back to filename regex parsing.
func extractShowMetadata(it model.Item) (showName string, season int, episode int, ok bool) {
	// 1. Try Metadata
	if it.Metadata != nil {
		var md map[string]interface{}
		if err := json.Unmarshal(*it.Metadata, &md); err == nil {
			if n, found := md["name"]; found {
				if s, is := n.(string); is && s != "" {
					showName = s
				}
			}
			// alt_name can override
			if an, found := md["alt_name"]; found {
				if s, is := an.(string); is && s != "" && showName == "" {
					showName = s
				}
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
			if season > 0 && episode > 0 && showName != "" {
				ok = true
				return
			}
		}
	}

	// 2. Fallback: filename regex
	base := filepath.Base(it.Path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	// Clean up common separators
	stem = strings.ReplaceAll(stem, ".", " ")
	stem = strings.ReplaceAll(stem, "_", " ")

	for _, sre := range showREs {
		matches := sre.re.FindStringSubmatch(stem)
		if sre.hasSeason && len(matches) >= 4 {
			showName = strings.TrimSpace(matches[1])
			season, _ = strconv.Atoi(matches[2])
			episode, _ = strconv.Atoi(matches[3])
			if showName != "" && season > 0 && episode > 0 {
				ok = true
				return
			}
		} else if !sre.hasSeason && len(matches) >= 3 {
			showName = strings.TrimSpace(matches[1])
			season = 1
			episode, _ = strconv.Atoi(matches[2])
			if showName != "" && episode > 0 {
				ok = true
				return
			}
		}
	}

	return "", 0, 0, false
}

// normalizeShowName produces a canonical key for grouping.
func normalizeShowName(name string) string {
	return strings.TrimSpace(strings.ToLower(name))
}

// showsHandler groups all video items into shows → seasons → episodes.
func (i *Indexer) showsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		allItems := i.store.Snapshot()
		showMap := make(map[string]*model.ShowSeries)
		showOrder := []string{}

		for _, item := range allItems {
			if !strings.Contains(item.MIMEType, "video") {
				continue
			}

			showName, season, episode, ok := extractShowMetadata(item)
			if !ok {
				continue
			}

			key := normalizeShowName(showName)
			se := model.ShowEpisode{
				Item:     item,
				ShowName: showName,
				Season:   season,
				Episode:  episode,
			}

			show, exists := showMap[key]
			if !exists {
				show = &model.ShowSeries{
					Name:    showName,
					Seasons: []model.ShowSeason{},
				}
				showMap[key] = show
				showOrder = append(showOrder, key)
			}

			// Find or create season
			seasonIdx := -1
			for idx := range show.Seasons {
				if show.Seasons[idx].Season == season {
					seasonIdx = idx
					break
				}
			}
			if seasonIdx < 0 {
				show.Seasons = append(show.Seasons, model.ShowSeason{
					Season:   season,
					Episodes: []model.ShowEpisode{},
				})
				seasonIdx = len(show.Seasons) - 1
			}
			show.Seasons[seasonIdx].Episodes = append(show.Seasons[seasonIdx].Episodes, se)
		}

		// Sort seasons and episodes
		for _, show := range showMap {
			sort.Slice(show.Seasons, func(a, b int) bool {
				return show.Seasons[a].Season < show.Seasons[b].Season
			})
			for sIdx := range show.Seasons {
				sort.Slice(show.Seasons[sIdx].Episodes, func(a, b int) bool {
					return show.Seasons[sIdx].Episodes[a].Episode < show.Seasons[sIdx].Episodes[b].Episode
				})
			}
		}

		// Build ordered response
		resp := model.ShowsResponse{Shows: make([]model.ShowSeries, 0, len(showOrder))}
		for _, key := range showOrder {
			resp.Shows = append(resp.Shows, *showMap[key])
		}
		sort.Slice(resp.Shows, func(a, b int) bool {
			return strings.ToLower(resp.Shows[a].Name) < strings.ToLower(resp.Shows[b].Name)
		})

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, "failed to encode shows", http.StatusInternalServerError)
		}
	}
}
