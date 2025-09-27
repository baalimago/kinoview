package loghandler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
)

type LogLevel uint8

const (
	DEBUG LogLevel = iota
	INFO
	WARNING
	ERROR
)

func (l *LogLevel) UnmarshalText(text []byte) error {
	switch string(text) {
	case "debug":
		*l = DEBUG
	case "info":
		*l = INFO
	case "warning":
		*l = WARNING
	case "error":
		*l = ERROR
	default:
		return errors.New("invalid log level")
	}
	return nil
}

type LogMessage struct {
	Level   LogLevel `json:"level"`
	Message string   `json:"message"`
}

// Func will log the messages using ancli depending on the log level
func Func() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Decode the request body into a LogMessage
		var logMessage LogMessage
		err := json.NewDecoder(r.Body).Decode(&logMessage)
		if err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		msg := fmt.Sprintf("[client]: %v", logMessage.Message)

		// Log the message based on the log level
		switch logMessage.Level {
		case DEBUG:
			ancli.Noticef("%v", msg)
		case INFO:
			ancli.Okf("%v", msg)
		case WARNING:
			ancli.Warnf("%v", msg)
		case ERROR:
			ancli.Errf("%v", msg)
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Log message received"))
	}
}
