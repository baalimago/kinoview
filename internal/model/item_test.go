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
			validate:  nil,
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
			validate:  nil,
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
	now := time.Now().UTC().Truncate(time.Second)

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
		LastPlayedName: "movie1",
	}

	jsonData, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var unmarshaled ClientContext
	err = json.Unmarshal(jsonData, &unmarshaled)
	if err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

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

	if cc.StartTime.IsZero() {
		t.Fatal("CRITICAL: StartTime is zero! Time unmarshaling failed.")
	}

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

func TestMatchesGlobalSearch(t *testing.T) {
	t.Run("empty needle matches everything", func(t *testing.T) {
		it := Item{Name: "test", Path: "/path"}
		if !MatchesGlobalSearch(it, "") {
			t.Fatal("empty needle should match")
		}
	})

	t.Run("matches name case-insensitive", func(t *testing.T) {
		it := Item{Name: "TheMatrix.mp4", Path: "/movies/"}
		if !MatchesGlobalSearch(it, "matrix") {
			t.Fatal("should match name")
		}
		if !MatchesGlobalSearch(it, "THEMATRIX") {
			t.Fatal("should match name case-insensitively")
		}
	})

	t.Run("matches path case-insensitive", func(t *testing.T) {
		it := Item{Name: "movie", Path: "/Movies/Action/film.mp4"}
		if !MatchesGlobalSearch(it, "action") {
			t.Fatal("should match path")
		}
	})

	t.Run("no match returns false", func(t *testing.T) {
		it := Item{Name: "comedy.mp4", Path: "/videos/"}
		if MatchesGlobalSearch(it, "horror") {
			t.Fatal("should not match")
		}
	})

	t.Run("matches metadata fields", func(t *testing.T) {
		md := json.RawMessage(`{"title":"Inception","director":"Christopher Nolan"}`)
		it := Item{Name: "movie.mp4", Path: "/v/", Metadata: &md}
		if !MatchesGlobalSearch(it, "nolan") {
			t.Fatal("should match metadata director")
		}
		if !MatchesGlobalSearch(it, "inception") {
			t.Fatal("should match metadata title")
		}
	})

	t.Run("matches metadata array values", func(t *testing.T) {
		md := json.RawMessage(`{"tags":["action","sci-fi"]}`)
		it := Item{Name: "f.mp4", Path: "/p/", Metadata: &md}
		if !MatchesGlobalSearch(it, "sci-fi") {
			t.Fatal("should match tag")
		}
	})

	t.Run("handles nil metadata", func(t *testing.T) {
		it := Item{Name: "plain.mp4", Path: "/v/", Metadata: nil}
		if MatchesGlobalSearch(it, "nonexistent") {
			t.Fatal("should not match nil metadata")
		}
		if !MatchesGlobalSearch(it, "plain") {
			t.Fatal("should still match name with nil metadata")
		}
	})

	t.Run("handles invalid JSON metadata gracefully", func(t *testing.T) {
		md := json.RawMessage(`not valid json`)
		it := Item{Name: "broken.mp4", Path: "/v/", Metadata: &md}
		if MatchesGlobalSearch(it, "valid") {
			t.Fatal("should not panic on invalid metadata JSON")
		}
		if !MatchesGlobalSearch(it, "broken") {
			t.Fatal("should match name despite invalid metadata")
		}
	})
}

func TestSearchMetadata(t *testing.T) {
	t.Run("finds string in map", func(t *testing.T) {
		m := map[string]interface{}{"key": "hello world"}
		if !SearchMetadata(m, "hello") {
			t.Fatal("should find substring in map value")
		}
	})

	t.Run("finds string in nested map", func(t *testing.T) {
		m := map[string]interface{}{
			"parent": map[string]interface{}{
				"child": "deep value",
			},
		}
		if !SearchMetadata(m, "deep") {
			t.Fatal("should find substring in nested map")
		}
	})

	t.Run("finds string in array", func(t *testing.T) {
		a := []interface{}{"one", "two three", "four"}
		if !SearchMetadata(a, "three") {
			t.Fatal("should find substring in array element")
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		m := map[string]interface{}{"title": "The Matrix"}
		if !SearchMetadata(m, "matrix") {
			t.Fatal("should match case-insensitively")
		}
	})

	t.Run("no match returns false", func(t *testing.T) {
		m := map[string]interface{}{"key": "abc"}
		if SearchMetadata(m, "xyz") {
			t.Fatal("should not find non-matching string")
		}
	})
}
