package model

import (
	"encoding/json"
	"testing"
	"time"
)

func TestClientContextUnmarshalJSON_RFC3339Format(t *testing.T) {
	tests := []struct {
		name      string
		jsonData  string
		expectErr bool
		validate  func(t *testing.T, cc *ClientContext)
	}{
		{
			name: "RFC3339 with Z suffix",
			jsonData: `{
				"sessionId": "session-123",
				"startTime": "2024-01-15T10:30:45Z",
				"viewingHistory": [],
				"timeOfDay": "morning",
				"lastPlayedName": "movie1"
			}`,
			expectErr: false,
			validate: func(t *testing.T, cc *ClientContext) {
				if cc.StartTime.IsZero() {
					t.Errorf("expected non-zero StartTime, got zero time")
				}
				if cc.StartTime.Year() != 2024 || cc.StartTime.Month() != 1 || cc.StartTime.Day() != 15 {
					t.Errorf("expected 2024-01-15, got %v", cc.StartTime)
				}
				if cc.SessionID != "session-123" {
					t.Errorf("expected sessionId 'session-123', got %s", cc.SessionID)
				}
			},
		},
		{
			name: "RFC3339 without Z suffix",
			jsonData: `{
				"sessionId": "session-456",
				"startTime": "2024-02-20T14:25:30",
				"viewingHistory": [],
				"timeOfDay": "afternoon",
				"lastPlayedName": "movie2"
			}`,
			expectErr: false,
			validate: func(t *testing.T, cc *ClientContext) {
				if cc.StartTime.IsZero() {
					t.Errorf("expected non-zero StartTime, got zero time")
				}
				if cc.StartTime.Year() != 2024 || cc.StartTime.Month() != 2 || cc.StartTime.Day() != 20 {
					t.Errorf("expected 2024-02-20, got %v", cc.StartTime)
				}
			},
		},
		{
			name: "Empty startTime should remain zero",
			jsonData: `{
				"sessionId": "session-789",
				"startTime": "",
				"viewingHistory": [],
				"timeOfDay": "evening",
				"lastPlayedName": ""
			}`,
			expectErr: false,
			validate: func(t *testing.T, cc *ClientContext) {
				if !cc.StartTime.IsZero() {
					t.Errorf("expected zero StartTime, got %v", cc.StartTime)
				}
			},
		},
		{
			name: "Invalid time format should error",
			jsonData: `{
				"sessionId": "session-999",
				"startTime": "not-a-valid-time",
				"viewingHistory": [],
				"timeOfDay": "night",
				"lastPlayedName": ""
			}`,
			expectErr: true,
			validate: nil,
		},
		{
			name: "With viewing history and valid startTime",
			jsonData: `{
				"sessionId": "session-with-history",
				"startTime": "2024-03-10T09:15:00Z",
				"viewingHistory": [
					{
						"name": "movie1",
						"viewedAt": "2024-03-10T09:20:00Z",
						"playedFor": "300"
					}
				],
				"timeOfDay": "morning",
				"lastPlayedName": "movie1"
			}`,
			expectErr: false,
			validate: func(t *testing.T, cc *ClientContext) {
				if cc.StartTime.IsZero() {
					t.Errorf("expected non-zero StartTime, got zero time")
				}
				if len(cc.ViewingHistory) != 1 {
					t.Errorf("expected 1 viewing history item, got %d", len(cc.ViewingHistory))
				}
				if cc.ViewingHistory[0].ViewedAt.IsZero() {
					t.Errorf("expected non-zero ViewedAt for history item, got zero time")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cc ClientContext
			err := json.Unmarshal([]byte(tt.jsonData), &cc)

			if (err != nil) != tt.expectErr {
				t.Errorf("UnmarshalJSON() error = %v, expectErr %v", err, tt.expectErr)
				return
			}

			if !tt.expectErr && tt.validate != nil {
				tt.validate(t, &cc)
			}
		})
	}
}

func TestViewMetadataUnmarshalJSON_RFC3339Format(t *testing.T) {
	tests := []struct {
		name      string
		jsonData  string
		expectErr bool
		validate  func(t *testing.T, vm *ViewMetadata)
	}{
		{
			name: "RFC3339 with Z suffix",
			jsonData: `{
				"name": "test-movie",
				"viewedAt": "2024-01-10T15:45:30Z",
				"playedFor": "1200"
			}`,
			expectErr: false,
			validate: func(t *testing.T, vm *ViewMetadata) {
				if vm.ViewedAt.IsZero() {
					t.Errorf("expected non-zero ViewedAt, got zero time")
				}
				if vm.Name != "test-movie" {
					t.Errorf("expected name 'test-movie', got %s", vm.Name)
				}
			},
		},
		{
			name: "RFC3339 without Z suffix",
			jsonData: `{
				"name": "another-movie",
				"viewedAt": "2024-02-15T12:30:45",
				"playedFor": "2400"
			}`,
			expectErr: false,
			validate: func(t *testing.T, vm *ViewMetadata) {
				if vm.ViewedAt.IsZero() {
					t.Errorf("expected non-zero ViewedAt, got zero time")
				}
				if vm.ViewedAt.Year() != 2024 || vm.ViewedAt.Month() != 2 || vm.ViewedAt.Day() != 15 {
					t.Errorf("expected 2024-02-15, got %v", vm.ViewedAt)
				}
			},
		},
		{
			name: "Empty viewedAt should remain zero",
			jsonData: `{
				"name": "empty-time-movie",
				"viewedAt": "",
				"playedFor": "0"
			}`,
			expectErr: false,
			validate: func(t *testing.T, vm *ViewMetadata) {
				if !vm.ViewedAt.IsZero() {
					t.Errorf("expected zero ViewedAt, got %v", vm.ViewedAt)
				}
			},
		},
		{
			name: "Invalid time format should error",
			jsonData: `{
				"name": "bad-time-movie",
				"viewedAt": "invalid-time",
				"playedFor": "100"
			}`,
			expectErr: true,
			validate: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var vm ViewMetadata
			err := json.Unmarshal([]byte(tt.jsonData), &vm)

			if (err != nil) != tt.expectErr {
				t.Errorf("UnmarshalJSON() error = %v, expectErr %v", err, tt.expectErr)
				return
			}

			if !tt.expectErr && tt.validate != nil {
				tt.validate(t, &vm)
			}
		})
	}
}

func TestClientContextRoundTrip(t *testing.T) {
	// Test that marshaling and unmarshaling preserves time values
	now := time.Now().UTC().Truncate(time.Second) // Truncate to seconds for JSON precision

	original := ClientContext{
		SessionID: "test-session",
		StartTime: now,
		ViewingHistory: []ViewMetadata{
			{
				Name:         "movie1",
				ViewedAt:     now.Add(5 * time.Minute),
				PlayedForSec: "300",
			},
		},
		TimeOfDay:      "morning",
		LastPlayedName: "movie1",
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Unmarshal back
	var unmarshaled ClientContext
	err = json.Unmarshal(jsonData, &unmarshaled)
	if err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Verify times match
	if !original.StartTime.Equal(unmarshaled.StartTime) {
		t.Errorf("StartTime mismatch: expected %v, got %v", original.StartTime, unmarshaled.StartTime)
	}

	if len(unmarshaled.ViewingHistory) != 1 {
		t.Fatalf("expected 1 viewing history item, got %d", len(unmarshaled.ViewingHistory))
	}

	if !original.ViewingHistory[0].ViewedAt.Equal(unmarshaled.ViewingHistory[0].ViewedAt) {
		t.Errorf("ViewedAt mismatch: expected %v, got %v",
			original.ViewingHistory[0].ViewedAt,
			unmarshaled.ViewingHistory[0].ViewedAt)
	}
}

func TestClientContextTimeNotZero(t *testing.T) {
	// This is the critical test - ensure times are NOT defaulting to zero
	jsonData := `{
		"sessionId": "critical-test",
		"startTime": "2024-03-15T14:30:00Z",
		"viewingHistory": [],
		"timeOfDay": "afternoon",
		"lastPlayedName": ""
	}`

	var cc ClientContext
	err := json.Unmarshal([]byte(jsonData), &cc)
	if err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// THE KEY ASSERTION: StartTime must NOT be zero
	if cc.StartTime.IsZero() {
		t.Fatal("CRITICAL: StartTime is zero! Time unmarshaling failed.")
	}

	// Additional checks
	expectedYear := 2024
	expectedMonth := time.March
	expectedDay := 15

	if cc.StartTime.Year() != expectedYear {
		t.Errorf("year mismatch: expected %d, got %d", expectedYear, cc.StartTime.Year())
	}
	if cc.StartTime.Month() != expectedMonth {
		t.Errorf("month mismatch: expected %v, got %v", expectedMonth, cc.StartTime.Month())
	}
	if cc.StartTime.Day() != expectedDay {
		t.Errorf("day mismatch: expected %d, got %d", expectedDay, cc.StartTime.Day())
	}
}
