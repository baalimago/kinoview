package model

import (
	"encoding/json"
	"image"
	"time"
)

type PaginatedRequest struct {
	Start    int    `json:"start"`
	Am       int    `json:"amount"`
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
