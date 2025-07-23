package model

import "encoding/json"

type Item struct {
	ID       string
	Path     string
	Name     string
	MIMEType string
	Metadata *json.RawMessage
}
