package clientcontext

import (
	"testing"
	"time"

	"github.com/baalimago/kinoview/internal/model"
)

func TestMergeDeltas(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name     string
		deltas   []model.ClientContextDelta
		expected []model.ClientContext
	}{
		{
			name:     "empty deltas",
			deltas:   []model.ClientContextDelta{},
			expected: []model.ClientContext{},
		},
		{
			name: "single delta with single view",
			deltas: []model.ClientContextDelta{
				{
					SessionID: "session1",
					ViewingHistory: []model.ViewMetadata{
						{
							Name:         "movie1",
							ViewedAt:     now,
							PlayedForSec: "3600",
						},
					},
				},
			},
			expected: []model.ClientContext{
				{
					SessionID: "session1",
					ViewingHistory: []model.ViewMetadata{
						{
							Name:         "movie1",
							ViewedAt:     now,
							PlayedForSec: "3600",
						},
					},
					StartTime:      now,
					LastPlayedName: "movie1",
				},
			},
		},
		{
			name: "multiple deltas same session merged",
			deltas: []model.ClientContextDelta{
				{
					SessionID: "session1",
					ViewingHistory: []model.ViewMetadata{
						{
							Name:         "movie1",
							ViewedAt:     now,
							PlayedForSec: "1800",
						},
					},
				},
				{
					SessionID: "session1",
					ViewingHistory: []model.ViewMetadata{
						{
							Name:         "movie2",
							ViewedAt:     now.Add(1 * time.Hour),
							PlayedForSec: "2700",
						},
					},
				},
			},
			expected: []model.ClientContext{
				{
					SessionID: "session1",
					ViewingHistory: []model.ViewMetadata{
						{
							Name:         "movie1",
							ViewedAt:     now,
							PlayedForSec: "1800",
						},
						{
							Name:         "movie2",
							ViewedAt:     now.Add(1 * time.Hour),
							PlayedForSec: "2700",
						},
					},
					StartTime:      now,
					LastPlayedName: "movie2",
				},
			},
		},
		{
			name: "out of order deltas same session",
			deltas: []model.ClientContextDelta{
				{
					SessionID: "session1",
					ViewingHistory: []model.ViewMetadata{
						{
							Name:         "movie2",
							ViewedAt:     now.Add(1 * time.Hour),
							PlayedForSec: "2700",
						},
					},
				},
				{
					SessionID: "session1",
					ViewingHistory: []model.ViewMetadata{
						{
							Name:         "movie1",
							ViewedAt:     now,
							PlayedForSec: "1800",
						},
					},
				},
			},
			expected: []model.ClientContext{
				{
					SessionID: "session1",
					ViewingHistory: []model.ViewMetadata{
						{
							Name:         "movie2",
							ViewedAt:     now.Add(1 * time.Hour),
							PlayedForSec: "2700",
						},
						{
							Name:         "movie1",
							ViewedAt:     now,
							PlayedForSec: "1800",
						},
					},
					StartTime:      now,
					LastPlayedName: "movie2",
				},
			},
		},
		{
			name: "multiple sessions",
			deltas: []model.ClientContextDelta{
				{
					SessionID: "session1",
					ViewingHistory: []model.ViewMetadata{
						{
							Name:         "movie1",
							ViewedAt:     now,
							PlayedForSec: "1800",
						},
					},
				},
				{
					SessionID: "session2",
					ViewingHistory: []model.ViewMetadata{
						{
							Name:         "movie2",
							ViewedAt:     now.Add(2 * time.Hour),
							PlayedForSec: "2700",
						},
					},
				},
			},
			expected: []model.ClientContext{
				{
					SessionID: "session1",
					ViewingHistory: []model.ViewMetadata{
						{
							Name:         "movie1",
							ViewedAt:     now,
							PlayedForSec: "1800",
						},
					},
					StartTime:      now,
					LastPlayedName: "movie1",
				},
				{
					SessionID: "session2",
					ViewingHistory: []model.ViewMetadata{
						{
							Name:         "movie2",
							ViewedAt:     now.Add(2 * time.Hour),
							PlayedForSec: "2700",
						},
					},
					StartTime:      now.Add(2 * time.Hour),
					LastPlayedName: "movie2",
				},
			},
		},
		{
			name: "empty viewing history",
			deltas: []model.ClientContextDelta{
				{
					SessionID:      "session1",
					ViewingHistory: []model.ViewMetadata{},
				},
			},
			expected: []model.ClientContext{
				{
					SessionID:      "session1",
					ViewingHistory: []model.ViewMetadata{},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeDeltas(tt.deltas)

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d contexts, got %d",
					len(tt.expected), len(result))
				return
			}

			for i, ctx := range result {
				exp := tt.expected[i]
				if ctx.SessionID != exp.SessionID {
					t.Errorf("context %d: expected SessionID %s, got %s",
						i, exp.SessionID, ctx.SessionID)
				}
				if len(ctx.ViewingHistory) != len(exp.ViewingHistory) {
					t.Errorf("context %d: expected %d views, got %d",
						i, len(exp.ViewingHistory), len(ctx.ViewingHistory))
				}
				for j, vm := range ctx.ViewingHistory {
					expVM := exp.ViewingHistory[j]
					if vm.Name != expVM.Name {
						t.Errorf("context %d view %d: expected name %s, got %s",
							i, j, expVM.Name, vm.Name)
					}
					if vm.PlayedForSec != expVM.PlayedForSec {
						t.Errorf("context %d view %d: expected playedFor %s, got %s",
							i, j, expVM.PlayedForSec, vm.PlayedForSec)
					}
				}
				if ctx.StartTime != exp.StartTime {
					t.Errorf("context %d: expected StartTime %v, got %v",
						i, exp.StartTime, ctx.StartTime)
				}
				if ctx.LastPlayedName != exp.LastPlayedName {
					t.Errorf("context %d: expected LastPlayedName %s, got %s",
						i, exp.LastPlayedName, ctx.LastPlayedName)
				}
			}
		})
	}
}

func TestStoreClientContextConcurrent(t *testing.T) {
	tmpDir := t.TempDir()
	m, err := New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	numGoroutines := 10
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			ctx := model.ClientContext{
				SessionID: "session1",
				StartTime: time.Now(),
				ViewingHistory: []model.ViewMetadata{
					{
						Name:         "movie" + string(rune(idx)),
						ViewedAt:     time.Now(),
						PlayedForSec: "1800",
					},
				},
				LastPlayedName: "movie" + string(rune(idx)),
			}
			err := m.StoreClientContext(ctx)
			if err != nil {
				t.Errorf("concurrent store failed: %v", err)
			}
			done <- true
		}(i)
	}

	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	all := m.AllClientContexts()
	if len(all) != numGoroutines {
		t.Errorf("expected %d contexts, got %d",
			numGoroutines, len(all))
	}
}
