package butler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/go_away_boilerplate/pkg/debug"
	"github.com/baalimago/go_away_boilerplate/pkg/misc"
	"github.com/baalimago/kinoview/internal/model"
)

// filterSubtitleStreams returns only the subtitle streams from the input.
func filterSubtitleStreams(streams []model.Stream) []model.Stream {
	var subs []model.Stream
	for _, st := range streams {
		if st.CodecType == "subtitle" {
			subs = append(subs, st)
		}
	}
	return subs
}

// textCodecs is the set of subtitle codecs extractable as text.
var textCodecs = map[string]bool{
	"subrip":   true,
	"ass":      true,
	"ssa":      true,
	"webvtt":   true,
	"mov_text": true,
}

// isEnglish returns true if the language tag represents English.
func isEnglish(lang string) bool {
	lower := strings.ToLower(lang)
	switch lower {
	case "eng", "en", "english":
		return true
	}
	// Handle regional tags like "eng-US", "en-GB"
	if strings.HasPrefix(lower, "eng-") || strings.HasPrefix(lower, "en-") {
		return true
	}
	return false
}

// rankSubtitle scores a subtitle stream; higher is better, negative means unusable.
func rankSubtitle(st model.Stream) int {
	lang := strings.ToLower(st.Tags.Language)
	title := strings.ToLower(st.Tags.Title)

	// --- Unusable checks (return negative immediately) ---

	// Non-English language is unusable (but "und" = undefined/unknown counts as empty)
	if lang != "" && lang != "und" && !isEnglish(lang) {
		return -1
	}

	// Commentary tracks are unusable
	if st.Disposition.Comment == 1 {
		return -1
	}
	if strings.Contains(title, "commentary") {
		return -1
	}

	// --- Scoring ---

	score := 0

	// Language signal
	if lang == "" || lang == "und" {
		score += 10 // unknown/untagged is weakly acceptable
	} else {
		score += 100 // confirmed English
	}

	// Sign/song/lyric/karaoke tracks are not dialogue
	if matchAny(title, "sign", "song", "lyric", "karaoke") {
		score -= 60
	}

	// Disposition signals
	if st.Disposition.Default == 1 {
		score += 20
	}
	if st.Disposition.Forced == 1 {
		score -= 40
	}
	if st.Disposition.HearingImpaired == 1 {
		score -= 10
	}

	// External (sidecar) files are user-curated
	if st.ExternalPath != "" {
		score += 5
	}

	// Codec type
	codec := strings.ToLower(st.CodecName)
	if textCodecs[codec] {
		score += 15
	}
	if codec == "hdmv_pgs_subtitle" || codec == "dvd_subtitle" {
		score -= 50
	}

	return score
}

// matchAny returns true if s contains any of the given substrings (case-insensitive).
func matchAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// rankBest returns the index of the highest-scoring usable subtitle stream.
// The second return value is false when every candidate scored negative (unusable).
// Ties are broken on lowest Index.
func rankBest(subs []model.Stream) (int, bool) {
	bestIdx := -1
	bestScore := -1

	for _, st := range subs {
		score := rankSubtitle(st)
		if score < 0 {
			continue
		}
		if score > bestScore || (score == bestScore && st.Index < bestIdx) {
			bestScore = score
			bestIdx = st.Index
		}
	}

	return bestIdx, bestIdx >= 0
}

// selectViaLLM is the LLM-based fallback for subtitle selection. It sends the
// subtitle streams to the LLM and parses the JSON response.
func (s *selector) selectViaLLM(ctx context.Context, subs []model.Stream) (int, error) {
	var sb strings.Builder
	for _, st := range subs {
		title := st.Tags.Title
		lang := st.Tags.Language
		if lang == "" {
			lang = "unknown"
		}
		sb.WriteString(fmt.Sprintf("- Index: %d, Language: %s, Title: %s, Codec: %s, Default: %d, Forced: %d\n",
			st.Index, lang, title, st.CodecName, st.Disposition.Default, st.Disposition.Forced))
	}

	chat := models.Chat{
		Messages: []models.Message{
			{
				Role:    "system",
				Content: selectorSystemPrompt,
			},
			{
				Role:    "user",
				Content: sb.String(),
			},
		},
	}

	if misc.Truthy(os.Getenv("DEBUG")) {
		ancli.Noticef("Selector prompt:\n%v", debug.IndentedJsonFmt(chat))
	}

	resp, err := s.llm.Query(ctx, chat)
	if err != nil {
		return -1, fmt.Errorf("selector llm query failed: %w", err)
	}

	lastMsg, _, err := resp.LastOfRole("assistant")
	if err != nil {
		if len(resp.Messages) > 0 {
			lastMsg = resp.Messages[len(resp.Messages)-1]
		} else {
			return -1, fmt.Errorf("empty response from selector llm")
		}
	}

	content := lastMsg.Content
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start == -1 || end == -1 || end < start {
		return -1, fmt.Errorf("invalid json response from selector")
	}
	jsonStr := content[start : end+1]

	var res selectorResponse
	if err := json.Unmarshal([]byte(jsonStr), &res); err != nil {
		return -1, fmt.Errorf("failed to parse selector response: %w", err)
	}

	if res.Error != nil {
		return -1, fmt.Errorf("%s", *res.Error)
	}

	if res.Index == nil {
		return -1, fmt.Errorf("no index returned")
	}

	return *res.Index, nil
}
