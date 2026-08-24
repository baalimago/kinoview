package loghandler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/model"
)

// Print renders one log message the house way — the format the /log endpoint
// gives a posted client or agent line: "[<logger>]: <message>" through ancli
// with the level's severity. The /log endpoint is the only server-side
// producer today; the troupe's observability may reuse it in a later phase.
func Print(lm model.LogMessage) {
	loggerName := lm.Logger
	if loggerName == "" {
		loggerName = "client"
	}

	msg := fmt.Sprintf("[%v]: %v", loggerName, lm.Message)

	switch lm.Level {
	case model.DEBUG:
		ancli.Noticef("%v", msg)
	case model.INFO:
		ancli.Okf("%v", msg)
	case model.WARNING:
		ancli.Warnf("%v", msg)
	case model.ERROR:
		ancli.Errf("%v", msg)
	}
}

// Func is the /log endpoint: it decodes a posted LogMessage and prints it the
// house way.
func Func() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var logMessage model.LogMessage
		err := json.NewDecoder(r.Body).Decode(&logMessage)
		if err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		Print(logMessage)

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Log message received"))
	}
}
