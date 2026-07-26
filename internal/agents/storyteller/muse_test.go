package storyteller

import (
	"math/rand"
	"testing"
	"time"

	"github.com/baalimago/clai/pkg/text/models"

	"github.com/baalimago/kinoview/internal/model"
)

func TestLatestTheme_PicksMostRecentAcrossSessions(t *testing.T) {
	now := time.Now()
	ctxs := []model.ClientContext{
		{
			SessionID: "a", StartTime: now.Add(-5 * time.Hour),
			ViewingHistory: []model.ViewMetadata{
				{Name: "Old.Movie.2011.1080p.mkv", ViewedAt: now.Add(-4 * time.Hour)},
			},
		},
		{
			SessionID: "b", StartTime: now.Add(-2 * time.Hour),
			ViewingHistory: []model.ViewMetadata{
				{Name: "The.Newest.Thing.2024.mkv", ViewedAt: now.Add(-1 * time.Minute)},
				{Name: "Middle.Show.mkv", ViewedAt: now.Add(-90 * time.Minute)},
			},
		},
	}
	if got := LatestTheme(ctxs); got != "The Newest Thing 2024" {
		t.Errorf("LatestTheme = %q, want %q", got, "The Newest Thing 2024")
	}
}

func TestLatestTheme_EmptyWhenNothingWatched(t *testing.T) {
	if got := LatestTheme(nil); got != "" {
		t.Errorf("expected empty theme, got %q", got)
	}
	if got := LatestTheme([]model.ClientContext{{SessionID: "a"}}); got != "" {
		t.Errorf("expected empty theme, got %q", got)
	}
}

func TestCleanTitle(t *testing.T) {
	cases := map[string]string{
		"The.Godfather.1972.1080p.BluRay.x264.mkv": "The Godfather 1972",
		"Big_Buck_Bunny.mp4":                       "Big Buck Bunny",
		"Some-Show-S01E04-720p-WEBRip.mkv":         "Some Show S01E04",
		"  Plain Name  ":                           "Plain Name",
		"":                                         "",
	}
	for in, want := range cases {
		if got := cleanTitle(in); got != want {
			t.Errorf("cleanTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

// The theme must reach the marquee even with no LLM configured.
func TestComposeThemed_BillsTheTheme(t *testing.T) {
	s := ComposeThemed(newRand(1), "The Godfather 1972")
	if s.Theme != "The Godfather 1972" {
		t.Errorf("theme not recorded: %q", s.Theme)
	}
	if s.Title == "" {
		t.Fatal("empty title")
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("themed story invalid: %v", err)
	}
}

// A hostile or broken muse must never take the splash (or the server) down.
func TestTheme_SurvivesPanickingMuse(t *testing.T) {
	dir := t.TempDir()
	tl := New(cfgNone(), dir, time.Hour, WithMuse(MuseFunc(func() string {
		panic("muse exploded")
	}))).(*teller)

	if got := tl.theme(); got != "" {
		t.Errorf("expected empty theme after panic, got %q", got)
	}
	if s := tl.Next(); len(s.Beats) == 0 {
		t.Error("Next produced nothing after a panicking muse")
	}
}

func newRand(seed int64) *rand.Rand { return rand.New(rand.NewSource(seed)) }

func cfgNone() models.Configurations { return models.Configurations{} }
