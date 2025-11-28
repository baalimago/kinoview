package model

import (
	"testing"
)

func TestLogLevel_UnmarshalText(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    LogLevel
		wantErr bool
	}{
		{
			name:  "debug level",
			input: "debug",
			want:  DEBUG,
		},
		{
			name:  "info level",
			input: "info",
			want:  INFO,
		},
		{
			name:  "warning level",
			input: "warning",
			want:  WARNING,
		},
		{
			name:  "error level",
			input: "error",
			want:  ERROR,
		},
		{
			name:    "invalid level",
			input:   "critical",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var l LogLevel
			err := l.UnmarshalText([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("LogLevel.UnmarshalText() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && l != tt.want {
				t.Errorf("LogLevel.UnmarshalText() = %v, want %v", l, tt.want)
			}
		})
	}
}

func TestLogLevel_MarshalText(t *testing.T) {
	tests := []struct {
		name    string
		level   LogLevel
		want    string
		wantErr bool
	}{
		{
			name:  "debug level",
			level: DEBUG,
			want:  "debug",
		},
		{
			name:  "info level",
			level: INFO,
			want:  "info",
		},
		{
			name:  "warning level",
			level: WARNING,
			want:  "warning",
		},
		{
			name:  "error level",
			level: ERROR,
			want:  "error",
		},
		{
			name:    "invalid level",
			level:   LogLevel(255),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.level.MarshalText()
			if (err != nil) != tt.wantErr {
				t.Errorf("LogLevel.MarshalText() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && string(got) != tt.want {
				t.Errorf("LogLevel.MarshalText() = %s, want %s", got, tt.want)
			}
		})
	}
}
