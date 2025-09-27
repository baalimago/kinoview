package loghandler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/baalimago/go_away_boilerplate/pkg/testboil"
	"github.com/baalimago/kinoview/internal/loghandler"
)

func TestPostErrorsHandler(t *testing.T) {
	tests := []struct {
		name           string
		logLevel       loghandler.LogLevel
		expectedOutput string
	}{
		{
			name:           "Debug log",
			logLevel:       loghandler.DEBUG,
			expectedOutput: "DEBUG: Test debug message\n",
		},
		{
			name:           "Info log",
			logLevel:       loghandler.INFO,
			expectedOutput: "INFO: Test info message\n",
		},
		{
			name:           "Warning log",
			logLevel:       loghandler.WARNING,
			expectedOutput: "WARNING: Test warning message\n",
		},
		{
			name:           "Error log",
			logLevel:       loghandler.ERROR,
			expectedOutput: "ERROR: Test error message\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logMessage := loghandler.LogMessage{
				Level:   tt.logLevel,
				Message: "Test " + string(tt.logLevel) + " message",
			}
			body, _ := json.Marshal(logMessage)
			req, _ := http.NewRequest(http.MethodPost, "/", bytes.NewBuffer(body))
			rr := httptest.NewRecorder()

			handler := loghandler.Func()
			output := testboil.CaptureStdout(t, func(t *testing.T) {
				handler.ServeHTTP(rr, req)
			})

			testboil.FailTestIfDiff(t, output, tt.expectedOutput)
			testboil.FailTestIfDiff(t, rr.Code, http.StatusOK)
			testboil.FailTestIfDiff(t, rr.Body.String(), "Log message received")
		})
	}
}
