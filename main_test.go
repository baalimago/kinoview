package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/go_away_boilerplate/pkg/testboil"
)

func TestMain(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantContains string
		wantExitCode int
	}{
		{
			name:         "version_command",
			args:         []string{"kinoview", "version"},
			wantContains: "version:",
			wantExitCode: 0,
		},
		{
			name:         "help_command",
			args:         []string{"kinoview", "--help"},
			wantContains: "== Kinoview ==",
			wantExitCode: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture output
			oldArgs := os.Args
			defer func() { os.Args = oldArgs }()

			// Run with timeout to avoid hanging
			_, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			gotCode := -1
			gotOut := testboil.CaptureStdout(t, func(t *testing.T) {
				gotCode = run(tt.args)
			})

			if !strings.Contains(gotOut, tt.wantContains) {
				t.Fatalf("wanted output to contain: '%v', output: %v", tt.wantContains, gotOut)
			}

			testboil.FailTestIfDiff(t, gotCode, tt.wantExitCode)
		})
	}
}
