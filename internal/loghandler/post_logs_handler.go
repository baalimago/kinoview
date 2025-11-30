package loghandler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/model"
)

// Func will log the messages using ancli depending on the log level
func Func() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Decode the request body into a LogMessage
		var logMessage model.LogMessage
		err := json.NewDecoder(r.Body).Decode(&logMessage)
		if err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		loggerName := logMessage.Logger
		if loggerName == "" {
			loggerName = "client"
		}

		msg := fmt.Sprintf("[%v]: %v", loggerName, logMessage.Message)

		// Log the message based on the log level
		switch logMessage.Level {
		case model.DEBUG:
			ancli.Noticef("%v", msg)
		case model.INFO:
			ancli.Okf("%v", msg)
		case model.WARNING:
			ancli.Warnf("%v", msg)
		case model.ERROR:
			ancli.Errf("%v", msg)
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Log message received"))
	}
}
