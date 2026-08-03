package model

import (
	"encoding/json"
	"image"
	"strings"
	"time"
)

type PaginatedRequest struct {
	Start int `json:"start"`
	Am    int `json:"amount"`
	// Search is an optional global search query (case-insensitive) across name, path, and metadata.
	Search   string `json:"search"`
	MIMEType string `json:"MIMEType"`
}

type PaginatedResponse[T any] struct {
	Total int `json:"total"`
	Start int `json:"start"`
	End   int `json:"end"`
	Items []T `json:"items"`
}

type Image struct {
	ID       string
	Path     string
	Encoding string
	Width    int
	Height   int
	Raw      image.Image `json:"-"`
}

type Item struct {
	ID        string
	Path      string
	Thumbnail Image
	Name      string
	MIMEType  string
	Metadata  *json.RawMessage

	ClassificationAttempts int       `json:"classificationAttempts,omitempty"`
	ClassificationLastTry  time.Time `json:"classificationLastTry"`
	ClassificationError    string    `json:"classificationError,omitempty"`

	// SubtitlePaths are user-specified absolute paths to external subtitle files
	// associated with this media item. Files must exist and have a subtitle extension
	// (.srt, .vtt, .sub, .ass, .ssa). Used by the stream manager's findExternal
	// discovery and surfaced in the media list command.
	SubtitlePaths []string `json:"subtitlePaths,omitempty"`
}

type ViewMetadata struct {
	Name         string    `json:"name"`
	ViewedAt     time.Time `json:"viewedAt"`
	PlayedForSec string    `json:"playedFor"`
}

// UnmarshalJSON handles JSON unmarshaling for ViewMetadata, supporting RFC3339 format
func (vm *ViewMetadata) UnmarshalJSON(data []byte) error {
	type Alias ViewMetadata
	aux := &struct {
		ViewedAt string `json:"viewedAt"`
		*Alias
	}{
		Alias: (*Alias)(vm),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if aux.ViewedAt != "" {
		t, err := time.Parse(time.RFC3339, aux.ViewedAt)
		if err != nil {
			// Try ISO8601 without Z suffix
			t, err = time.Parse("2006-01-02T15:04:05", aux.ViewedAt)
			if err != nil {
				return err
			}
		}
		vm.ViewedAt = t
	}
	return nil
}

type ClientContext struct {
	SessionID      string         `json:"sessionId"`
	StartTime      time.Time      `json:"startTime"`
	ViewingHistory []ViewMetadata `json:"viewingHistory"`
	LastPlayedName string         `json:"lastPlayedName"`
}

type ClientContextDelta struct {
	SessionID      string         `json:"sessionId"`
	ViewingHistory []ViewMetadata `json:"viewingHistory"`
}

// UnmarshalJSON handles JSON unmarshaling for ClientContext, supporting RFC3339 format
func (cc *ClientContext) UnmarshalJSON(data []byte) error {
	type Alias ClientContext
	aux := &struct {
		StartTime string `json:"startTime"`
		*Alias
	}{
		Alias: (*Alias)(cc),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if aux.StartTime != "" {
		t, err := time.Parse(time.RFC3339, aux.StartTime)
		if err != nil {
			// Try ISO8601 without Z suffix
			t, err = time.Parse("2006-01-02T15:04:05", aux.StartTime)
			if err != nil {
				return err
			}
		}
		cc.StartTime = t
	}
	return nil
}

type UserRequest struct {
	// Request from user, explicitly stated
	Request string `json:"request"`
	// Context from user, containing things such as view-duration of media,
	// time of day, usage trends etc
	Context ClientContext `json:"context"`
}

type Suggestion struct {
	Item
	Motivation string `json:"motivation"`
	SubtitleID string `json:"subtitleID"`
	// View is the resolved card display data, attached server-side at payload
	// build time (see internal/media). It is never persisted with meaning:
	// stored suggestions keep View nil and the payload builder recomputes it.
	View *SuggestionView `json:"view,omitempty"`
}

// SuggestionView is the display data for one suggestion card. It is derived
// deterministically from the item's metadata and file path so the frontend
// never has to re-derive series names or season/episode positions, and so
// both the concierge and the butler render identically.
type SuggestionView struct {
	// Kind is "movie", "episode", "extras" or "media" (unclassified fallback).
	Kind         string   `json:"kind"`
	Title        string   `json:"title"` // movie title, or series name for episodes/extras
	EpisodeTitle string   `json:"episodeTitle,omitempty"`
	Season       int      `json:"season,omitempty"`
	Episode      int      `json:"episode,omitempty"`
	Year         int      `json:"year,omitempty"`
	DurationMin  int      `json:"durationMin,omitempty"`
	Language     string   `json:"language,omitempty"`
	Description  string   `json:"description,omitempty"`
	Actors       []string `json:"actors,omitempty"`
}

// SuggestionsPayload is the payload for the suggestions websocket event
// and the body of GET /gallery/suggestions.
type SuggestionsPayload struct {
	State       string       `json:"state"`
	Suggestions []Suggestion `json:"suggestions"`
	Generated   string       `json:"generated,omitempty"`
}

// SuggestionFingerprint identifies the inputs a butler answer depends on.
type SuggestionFingerprint struct {
	Library string `json:"library"` // sha256 over the marshalled []butlerItemView
	Context string `json:"context"` // sha256 over the context fields
	Version int    `json:"version"` // bump to invalidate all caches on a prompt/schema change
}

// SuggestionsFile is the on-disk envelope for persisted suggestions and their
// cache fingerprint. When Fingerprint is nil the file was written by an older
// version and the next cascade is a cache miss.
type SuggestionsFile struct {
	Suggestions []Suggestion           `json:"suggestions"`
	Fingerprint *SuggestionFingerprint `json:"fingerprint,omitempty"`
	Generated   string                 `json:"generated,omitempty"`
}

// MatchesGlobalSearch performs a global search across item metadata and basic fields.
// It searches through the item's name, path, and metadata fields for the given needle.
// An empty needle matches everything.
func MatchesGlobalSearch(it Item, needle string) bool {
	if needle == "" {
		return true
	}
	needle = strings.ToLower(needle)

	if strings.Contains(strings.ToLower(it.Name), needle) ||
		strings.Contains(strings.ToLower(it.Path), needle) {
		return true
	}

	if it.Metadata != nil {
		var metadata map[string]any
		if err := json.Unmarshal(*it.Metadata, &metadata); err == nil {
			if SearchMetadata(metadata, needle) {
				return true
			}
		}
	}
	return false
}

// SearchMetadata recursively searches through metadata for a substring match.
func SearchMetadata(data any, needle string) bool {
	switch v := data.(type) {
	case map[string]any:
		for _, val := range v {
			if SearchMetadata(val, needle) {
				return true
			}
		}
	case []any:
		for _, val := range v {
			if SearchMetadata(val, needle) {
				return true
			}
		}
	case string:
		if strings.Contains(strings.ToLower(v), needle) {
			return true
		}
	}
	return false
}
