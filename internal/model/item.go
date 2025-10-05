package model

import (
	"encoding/json"
	"time"
)

type Item struct {
	ID       string
	Path     string
	Name     string
	MIMEType string
	Metadata *json.RawMessage
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
