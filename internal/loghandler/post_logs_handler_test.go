package loghandler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/go_away_boilerplate/pkg/testboil"
	"github.com/baalimago/kinoview/internal/loghandler"
	"github.com/baalimago/kinoview/internal/model"
)

func TestPostErrorsHandler(t *testing.T) {
	tests := []struct {
		name           string
		message        string
		logLevel       model.LogLevel
		expectedOutput string
	}{
		{
			name:           "Debug log",
			message:        "debug",
			logLevel:       model.DEBUG,
			expectedOutput: "notice: [client]: Test debug message",
		},
		{
			name:           "Info log",
			message:        "info",
			logLevel:       model.INFO,
			expectedOutput: "ok: [client]: Test info message",
		},
		{
			name:           "Warning log",
			message:        "warning",
			logLevel:       model.WARNING,
			expectedOutput: "warning: [client]: Test warning message",
		},
		{
			name:           "Error log",
			message:        "error",
			logLevel:       model.ERROR,
			expectedOutput: "error: [client]: Test error message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ancli.UseColor = false
			ancli.Newline = false
			logMessage := model.LogMessage{
				Level:   tt.logLevel,
				Message: "Test " + tt.message + " message",
			}
			body, err := json.Marshal(logMessage)
			if err != nil {
				t.Fatal(err)
			}
			req, _ := http.NewRequest(http.MethodPost, "/", bytes.NewBuffer(body))
			rr := httptest.NewRecorder()

			handler := loghandler.Func()
			output := testboil.CaptureStdout(t, func(t *testing.T) {
				handler.ServeHTTP(rr, req)
			})

			if output == "" {
				// Slighty hack to re-record but check stderr instead in case of error message
				rr = httptest.NewRecorder()
				req, _ = http.NewRequest(http.MethodPost, "/", bytes.NewBuffer(body))
				output = testboil.CaptureStderr(t, func(t *testing.T) {
					handler.ServeHTTP(rr, req)
				})
			}

			testboil.FailTestIfDiff(t, rr.Body.String(), "Log message received")
			testboil.FailTestIfDiff(t, rr.Code, http.StatusOK)
			testboil.FailTestIfDiff(t, output, tt.expectedOutput)
		})
	}
}
