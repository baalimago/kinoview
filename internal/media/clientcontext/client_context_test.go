package clientcontext

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestManagerLoad_FileDoesNotExist(t *testing.T) {
	tmpDir := t.TempDir()
	m := &Manager{logPath: filepath.Join(tmpDir, "kinoview", "client", "context.log")}
	if err := m.load(); err != nil {
		t.Fatalf("load returned error: %v", err)
	}
	if got := m.AllClientContexts(); len(got) != 0 {
		t.Fatalf("expected 0 contexts, got %d", len(got))
	}
}

func TestManagerLoad_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "kinoview", "client", "context.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(logPath, nil, 0o644); err != nil {
		t.Fatalf("writefile: %v", err)
	}

	m := &Manager{logPath: logPath}
	if err := m.load(); err != nil {
		t.Fatalf("load returned error: %v", err)
	}
	if got := m.AllClientContexts(); len(got) != 0 {
		t.Fatalf("expected 0 contexts, got %d", len(got))
	}
}

func TestManagerLoad_InvalidJSONLine(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "kinoview", "client", "context.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// One valid JSON line and one invalid.
	data := []byte("{\"SessionID\":\"s1\",\"ViewingHistory\":[{\"Name\":\"m1\",\"ViewedAt\":\"2020-01-01T00:00:00Z\",\"PlayedForSec\":\"1\"}]}\n{not-json}\n")
	if err := os.WriteFile(logPath, data, 0o644); err != nil {
		t.Fatalf("writefile: %v", err)
	}

	m := &Manager{logPath: logPath}
	err := m.load()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unmarshal user context log entry") {
		t.Fatalf("expected unmarshal context error, got: %v", err)
	}
}

func TestAppendToLogLocked_NoDeltaDoesNotWrite(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "kinoview", "client", "context.log")
	m := &Manager{logPath: logPath}

	now := time.Now().UTC()
	ctx := model.ClientContext{
		SessionID: "s1",
		ViewingHistory: []model.ViewMetadata{
			{Name: "m1", ViewedAt: now, PlayedForSec: "10"},
		},
	}

	// First call: there is no prior history => it WILL write.
	// appendToLogLocked expects StoreClientContext ordering: append ctx to m.contexts after appending.
	if err := m.appendToLogLocked(ctx); err != nil {
		t.Fatalf("appendToLogLocked: %v", err)
	}
	m.contexts = append(m.contexts, ctx)
	b1, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(b1) == 0 {
		t.Fatalf("expected some bytes written")
	}

	// Second call with identical context should not write (no viewing history changes)
	if err := m.appendToLogLocked(ctx); err != nil {
		t.Fatalf("appendToLogLocked 2: %v", err)
	}
	m.contexts = append(m.contexts, ctx)
	b2, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read2: %v", err)
	}
	if string(b2) != string(b1) {
		t.Fatalf("expected log unchanged; before=%q after=%q", string(b1), string(b2))
	}
}

func TestAppendToLogLocked_ErrorMissingSessionID(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "kinoview", "client", "context.log")
	m := &Manager{logPath: logPath}

	ctx := model.ClientContext{SessionID: ""}
	m.contexts = append(m.contexts, ctx)
	err := m.appendToLogLocked(ctx)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to separate") {
		t.Fatalf("expected wrapping error, got: %v", err)
	}
}

func TestAppendToLogLocked_WritesDeltaAndIsJSONL(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "kinoview", "client", "context.log")
	m := &Manager{logPath: logPath}

	now := time.Now().UTC()
	ctx1 := model.ClientContext{
		SessionID:      "s1",
		ViewingHistory: []model.ViewMetadata{{Name: "m1", ViewedAt: now, PlayedForSec: "10"}},
	}
	if err := m.appendToLogLocked(ctx1); err != nil {
		t.Fatalf("append1: %v", err)
	}
	m.contexts = append(m.contexts, ctx1)

	ctx2 := model.ClientContext{
		SessionID:      "s1",
		ViewingHistory: []model.ViewMetadata{{Name: "m1", ViewedAt: now, PlayedForSec: "11"}},
	}
	if err := m.appendToLogLocked(ctx2); err != nil {
		t.Fatalf("append2: %v", err)
	}
	m.contexts = append(m.contexts, ctx2)

	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 jsonl lines, got %d: %q", len(lines), string(b))
	}
	for i, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			t.Fatalf("line %d empty", i)
		}
		var d model.ClientContextDelta
		if err := json.Unmarshal([]byte(ln), &d); err != nil {
			t.Fatalf("line %d not valid json: %v; line=%q", i, err, ln)
		}
		if d.SessionID != "s1" {
			t.Fatalf("line %d: expected session s1, got %q", i, d.SessionID)
		}
		if len(d.ViewingHistory) != 1 {
			t.Fatalf("line %d: expected 1 view, got %d", i, len(d.ViewingHistory))
		}
	}
}

func TestViewMetadataEqual(t *testing.T) {
	now := time.Now()
	base := model.ViewMetadata{Name: "m", PlayedForSec: "1", ViewedAt: now}

	if !viewMetadataEqual(base, base) {
		t.Fatalf("expected equal")
	}

	if viewMetadataEqual(base, model.ViewMetadata{Name: "x", PlayedForSec: "1", ViewedAt: now}) {
		t.Fatalf("expected not equal when Name differs")
	}
	if viewMetadataEqual(base, model.ViewMetadata{Name: "m", PlayedForSec: "2", ViewedAt: now}) {
		t.Fatalf("expected not equal when PlayedForSec differs")
	}
	if viewMetadataEqual(base, model.ViewMetadata{Name: "m", PlayedForSec: "1", ViewedAt: now.Add(time.Second)}) {
		t.Fatalf("expected not equal when ViewedAt differs")
	}
}
