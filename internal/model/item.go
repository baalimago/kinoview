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

type ClientContext struct {
	ViewingHistory []ViewMetadata `json:"viewingHistory"`
	TimeOfDay      string         `json:"timeOfDay"`
	LastPlayedName string         `json:"lastPlayedName"`
}

type UserRequest struct {
	// Request from user, explicitly stated
	Request string `json:"request"`
	// Context from user, containing things such as view-duration of media,
	// time of day, usage trends etc
	Context ClientContext `json:"context"`
}

type Recommendation struct {
	Item
	Motivation string `json:"motivation"`
	SubtitleID string `json:"subtitleID"`
}
