package media

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/baalimago/kinoview/internal/model"
)

func TestResolveSuggestionView(t *testing.T) {
	tests := []struct {
		name string
		item model.Item
		want model.SuggestionView
	}{
		{
			name: "movie with full metadata",
			item: itemWithMeta("/mnt/usb_b/movies/Warfare.2025.1080p.WEB.h264-ETHEL/Warfare.2025.1080p.WEB.h264-ETHEL.mkv",
				`{"name":"Warfare","alt_name":"Warfare (2025)","year":2025,"langugae":"English","duration_min":95,"description":"Feature film titled Warfare.","actors":["Actor One"]}`),
			want: model.SuggestionView{
				Kind:        "movie",
				Title:       "Warfare",
				Year:        2025,
				DurationMin: 95,
				Language:    "English",
				Description: "Feature film titled Warfare.",
				Actors:      []string{"Actor One"},
			},
		},
		{
			name: "episode where name is the episode title (Stargate production data)",
			item: itemWithMeta("/mnt/usb_b/movies/Stargate.SG-1.S08.1080p.BluRay.DD.5.1.x265-edge2020/Stargate.SG-1.S08E10.Endgame.1080p.BluRay.DD.5.1.x265-edge2020.mkv",
				`{"name":"Endgame","alt_name":"","year":2004,"season":8,"episode":10,"langugae":"English","duration_min":44,"description":"The Trust steals the Stargate."}`),
			want: model.SuggestionView{
				Kind:         "episode",
				Title:        "Stargate SG-1",
				EpisodeTitle: "Endgame",
				Season:       8,
				Episode:      10,
				Year:         2004,
				DurationMin:  44,
				Language:     "English",
				Description:  "The Trust steals the Stargate.",
			},
		},
		{
			name: "episode where name is the series and alt_name the episode title (Office production data)",
			item: itemWithMeta("/mnt/usb_b/movies/The.Office.US.S06.1080p.BluRay.x265-RARBG/The.Office.S06E09.1080p.BluRay.x265-RARBG.mp4",
				`{"name":"The Office","alt_name":"Double Date","year":2009,"season":6,"episode":9,"langugae":"English","duration_min":22}`),
			want: model.SuggestionView{
				Kind:         "episode",
				Title:        "The Office",
				EpisodeTitle: "Double Date",
				Season:       6,
				Episode:      9,
				Year:         2009,
				DurationMin:  22,
				Language:     "English",
			},
		},
		{
			name: "episode prefers metadata showName over the path",
			item: itemWithMeta("/mnt/usb_b/movies/Some.Folder/S01E02.The.Twist.mkv",
				`{"name":"The Twist","showName":"Twin Peaks","season":1,"episode":2}`),
			want: model.SuggestionView{
				Kind:         "episode",
				Title:        "Twin Peaks",
				EpisodeTitle: "The Twist",
				Season:       1,
				Episode:      2,
			},
		},
		{
			name: "unclassified episode falls back to the path",
			item: model.Item{
				Name: "Breaking.Bad.S05E08.Gliding.Over.All.1080p.mkv",
				Path: "/mnt/usb_b/movies/Breaking.Bad.S05/Breaking.Bad.S05E08.Gliding.Over.All.1080p.mkv",
			},
			want: model.SuggestionView{
				Kind:    "episode",
				Title:   "Breaking Bad",
				Season:  5,
				Episode: 8,
			},
		},
		{
			name: "extras are labelled with the main media",
			item: itemWithMeta("/mnt/usb_b/movies/The.Lord.of.the.Rings/The.Lord.of.the.Rings.Extras.mkv",
				`{"name":"Behind the Scenes","extra_to":"The Lord of the Rings","year":2002}`),
			want: model.SuggestionView{
				Kind:         "extras",
				Title:        "The Lord of the Rings",
				EpisodeTitle: "Behind the Scenes",
				Year:         2002,
			},
		},
		{
			name: "unclassified file without position is generic media",
			item: model.Item{
				Name: "random_clip.mkv",
				Path: "/mnt/usb_b/movies/random_clip.mkv",
			},
			want: model.SuggestionView{
				Kind:  "media",
				Title: "random clip",
			},
		},
		{
			name: "movie metadata with string-typed year",
			item: itemWithMeta("/mnt/usb_b/movies/Blade.Runner/Blade.Runner.mkv",
				`{"name":"Blade Runner","year":"1982","duration_min":"117"}`),
			want: model.SuggestionView{
				Kind:        "movie",
				Title:       "Blade Runner",
				Year:        1982,
				DurationMin: 117,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveSuggestionView(tt.item)
			if got.Kind != tt.want.Kind {
				t.Errorf("kind = %q, want %q", got.Kind, tt.want.Kind)
			}
			if got.Title != tt.want.Title {
				t.Errorf("title = %q, want %q", got.Title, tt.want.Title)
			}
			if got.EpisodeTitle != tt.want.EpisodeTitle {
				t.Errorf("episodeTitle = %q, want %q", got.EpisodeTitle, tt.want.EpisodeTitle)
			}
			if got.Season != tt.want.Season {
				t.Errorf("season = %d, want %d", got.Season, tt.want.Season)
			}
			if got.Episode != tt.want.Episode {
				t.Errorf("episode = %d, want %d", got.Episode, tt.want.Episode)
			}
			if got.Year != tt.want.Year {
				t.Errorf("year = %d, want %d", got.Year, tt.want.Year)
			}
			if got.DurationMin != tt.want.DurationMin {
				t.Errorf("durationMin = %d, want %d", got.DurationMin, tt.want.DurationMin)
			}
			if got.Language != tt.want.Language {
				t.Errorf("language = %q, want %q", got.Language, tt.want.Language)
			}
			if got.Description != tt.want.Description {
				t.Errorf("description = %q, want %q", got.Description, tt.want.Description)
			}
			if len(got.Actors) != len(tt.want.Actors) {
				t.Errorf("actors = %v, want %v", got.Actors, tt.want.Actors)
			}
		})
	}
}

func TestEnrichSuggestions_AttachesView(t *testing.T) {
	item := itemWithMeta("/mnt/usb_b/movies/Stargate.SG-1.S08E10.Endgame.mkv",
		`{"name":"Endgame","season":8,"episode":10}`)
	recs := enrichSuggestions([]model.Suggestion{
		{Item: item, Motivation: "resume the campaign"},
	})

	if len(recs) != 1 {
		t.Fatalf("got %d suggestions, want 1", len(recs))
	}
	if recs[0].View == nil {
		t.Fatal("expected view to be attached")
	}
	if recs[0].View.Title != "Stargate SG-1" {
		t.Errorf("view title = %q, want %q", recs[0].View.Title, "Stargate SG-1")
	}
	if recs[0].Motivation != "resume the campaign" {
		t.Errorf("motivation not preserved: %q", recs[0].Motivation)
	}
}

func TestSuggestionView_EmptyViewNotPersisted(t *testing.T) {
	// A suggestion created by an agent must not carry a view until the payload
	// builder attaches one — the persisted JSON stays lean.
	b, err := json.Marshal(model.Suggestion{Item: model.Item{Name: "X"}, Motivation: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if contains(b, `"view"`) {
		t.Errorf("persisted suggestion unexpectedly contains view: %s", b)
	}
}

func itemWithMeta(path, meta string) model.Item {
	raw := json.RawMessage(meta)
	return model.Item{
		Name:     path,
		Path:     path,
		Metadata: &raw,
	}
}

func contains(b []byte, sub string) bool {
	return bytes.Contains(b, []byte(sub))
}
