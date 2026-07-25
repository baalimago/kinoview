package media

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/baalimago/kinoview/internal/agents/butler"
	"github.com/baalimago/kinoview/internal/model"
)

// computeLibraryFingerprint returns a sha256 over the marshalled butlerItemView
// projection — exactly the bytes the butler receives, minus the Index field
// which is a transport artifact (depends on input order, not on library
// content). Marshalling guards against struct drift: adding a field to
// butlerItemView automatically invalidates the cache.
func computeLibraryFingerprint(items []model.Item) string {
	views := butler.ProjectItems(items)
	// Zero the Index field — it reflects original input position, not
	// library content. Two different slice orderings of the same items
	// produce different Index values in the sorted output.
	for i := range views {
		views[i].Index = 0
	}
	b, err := json.Marshal(views)
	if err != nil {
		// Marshalling a slice of simple structs never fails; guard anyway.
		return ""
	}
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%x", sum[:])
}

// computeContextFingerprint returns a sha256 over a deliberate subset of
// ClientContext. SessionID and StartTime are excluded so reconnects do not
// evict the cache.
func computeContextFingerprint(clientCtx model.ClientContext, now time.Time) string {
	h := sha256.New()

	// LastPlayedName — strongest signal for hints 1 and 2.
	fmt.Fprintf(h, "lpn:%s|", clientCtx.LastPlayedName)

	// ViewingHistory, digested: (name, coarse progress bucket).
	for _, vh := range clientCtx.ViewingHistory {
		bucket := progressBucket(vh.PlayedForSec)
		fmt.Fprintf(h, "vh:%s:%d|", vh.Name, bucket)
	}

	// Day-of-week and part-of-day.
	fmt.Fprintf(h, "dow:%s|", now.Weekday().String())
	fmt.Fprintf(h, "pod:%s|", partOfDay(now))

	return fmt.Sprintf("%x", h.Sum(nil))
}

// progressBucket parses a PlayedForSec duration string and returns a coarse
// bucket in 600-second (10-minute) steps. Small playback deltas land in the
// same bucket; crossing a boundary evicts the cache. Unparseable durations
// are hashed raw.
func progressBucket(playedForSec string) int {
	secs, err := parseDurationSeconds(playedForSec)
	if err != nil {
		// Unparseable — hash the raw string so it still contributes to
		// uniqueness without a magic sentinel.
		h := sha256.New()
		h.Write([]byte(playedForSec))
		return int(h.Sum(nil)[0]) | (int(h.Sum(nil)[1]) << 8)
	}
	return secs / 600
}

// parseDurationSeconds handles both "300" (plain seconds) and "1:30:45"
// (H:MM:SS) formats.
func parseDurationSeconds(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	// Try plain seconds first.
	if secs, err := strconv.Atoi(s); err == nil {
		return secs, nil
	}
	// Try H:MM:SS or MM:SS.
	parts := strings.Split(s, ":")
	switch len(parts) {
	case 3:
		h, _ := strconv.Atoi(parts[0])
		m, _ := strconv.Atoi(parts[1])
		sec, _ := strconv.Atoi(parts[2])
		return h*3600 + m*60 + sec, nil
	case 2:
		m, _ := strconv.Atoi(parts[0])
		sec, _ := strconv.Atoi(parts[1])
		return m*60 + sec, nil
	default:
		return 0, fmt.Errorf("unparseable duration: %q", s)
	}
}

// partOfDay buckets the hour into morning (6-11), afternoon (12-17),
// evening (18-23), and night (0-5).
func partOfDay(t time.Time) string {
	h := t.Hour()
	switch {
	case h >= 6 && h < 12:
		return "morning"
	case h >= 12 && h < 18:
		return "afternoon"
	case h >= 18 && h < 24:
		return "evening"
	default:
		return "night"
	}
}
