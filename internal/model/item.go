package model

import (
	"encoding/json"
	"image"
	"strings"
	"time"
)

type PaginatedRequest struct {
	Start    int    `json:"start"`
	Am       int    `json:"amount"`
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
		var metadata map[string]interface{}
		if err := json.Unmarshal(*it.Metadata, &metadata); err == nil {
			if SearchMetadata(metadata, needle) {
				return true
			}
		}
	}
	return false
}

// SearchMetadata recursively searches through metadata for a substring match.
func SearchMetadata(data interface{}, needle string) bool {
	switch v := data.(type) {
	case map[string]interface{}:
		for _, val := range v {
			if SearchMetadata(val, needle) {
				return true
			}
		}
	case []interface{}:
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
