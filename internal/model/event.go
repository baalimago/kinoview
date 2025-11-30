package model

import "time"

type EventType string

const (
	// HealthEvent used to check if client is alive
	HealthEvent EventType = "health"
	// ClientContextEvent used to send context from client to server
	ClientContextEvent EventType = "clientContext"
)

type Health struct{}

// Event sent either from client -> server, or other way around
type Event[E any] struct {
	Type    EventType `json:"type"`
	Created time.Time `json:"time"`
	Payload E         `json:"payload"`
}
