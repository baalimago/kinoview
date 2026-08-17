package slivingdoc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// withFakeCLI routes the commit seam through a scripted runner; it is shared
// with the seed tests in slivingdoc_test.go.
func notebookEnvFile(t *testing.T) string {
	t.Helper()
	envFile := filepath.Join(t.TempDir(), "credentials.env")
	if err := os.WriteFile(envFile, []byte("AWS_ACCESS_KEY_ID=k\nAWS_SECRET_ACCESS_KEY=s\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return envFile
}

// TestFeedbackRecorder_AppendsJSONLLine — one post yields one valid JSONL
// line whose decoded fields match (storyId, rating, comment, ts).
func TestFeedbackRecorder_AppendsJSONLLine(t *testing.T) {
	fake := &fakeCLI{}
	withFakeCLI(t, fake)

	workspace := filepath.Join(t.TempDir(), "slivingdoc")
	recorder := NewFeedbackRecorder(NewNotebook(NpxRunner("npx"), workspace, filepath.Join(t.TempDir(), "priv"), notebookEnvFile(t)))

	if err := recorder.Feedback(t.Context(), "stry_abc12345", 1, "more dog"); err != nil {
		t.Fatalf("Feedback: %v", err)
	}

	// Exactly one commit, never a pull.
	if len(fake.calls) != 1 {
		t.Fatalf("expected one commit, got %v", fake.calls)
	}
	if fake.calls[0][2] != "commit" {
		t.Errorf("first call = %v, want commit", fake.calls[0])
	}

	b, err := os.ReadFile(filepath.Join(workspace, feedbackName))
	if err != nil {
		t.Fatalf("feedback.jsonl: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected one JSONL line, got %d: %q", len(lines), b)
	}
	var rec feedbackRecord
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("line is not valid JSON: %v", err)
	}
	if rec.StoryID != "stry_abc12345" || rec.Rating != 1 || rec.Comment != "more dog" {
		t.Errorf("decoded = (%q, %d, %q), want (stry_abc12345, 1, more dog)", rec.StoryID, rec.Rating, rec.Comment)
	}
	ts, err := time.Parse(time.RFC3339, rec.TS)
	if err != nil {
		t.Fatalf("ts %q is not RFC 3339: %v", rec.TS, err)
	}
	if d := time.Since(ts); d > time.Minute || d < -time.Minute {
		t.Errorf("ts %v is not the receive time (now ±1min)", rec.TS)
	}
}

// A thumbs-down with an empty comment still lands the comment field — the
// line is never rewritten or trimmed (decision Q6).
func TestFeedbackRecorder_EmptyCommentIsPreserved(t *testing.T) {
	fake := &fakeCLI{}
	withFakeCLI(t, fake)

	workspace := filepath.Join(t.TempDir(), "slivingdoc")
	recorder := NewFeedbackRecorder(NewNotebook(NpxRunner("npx"), workspace, filepath.Join(t.TempDir(), "priv"), notebookEnvFile(t)))

	if err := recorder.Feedback(t.Context(), "stry_abc12345", -1, ""); err != nil {
		t.Fatalf("Feedback: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(workspace, feedbackName))
	if err != nil {
		t.Fatalf("feedback.jsonl: %v", err)
	}
	if !strings.Contains(string(b), `"comment":""`) {
		t.Errorf("line drops the empty comment: %s", b)
	}
}

// TestFeedbackRecorder_CommitErrorReturnsError — a failing commit propagates,
// so the handler answers 500 instead of silently losing the note.
func TestFeedbackRecorder_CommitErrorReturnsError(t *testing.T) {
	fake := &fakeCLI{failOn: "commit"}
	withFakeCLI(t, fake)

	workspace := filepath.Join(t.TempDir(), "slivingdoc")
	recorder := NewFeedbackRecorder(NewNotebook(NpxRunner("npx"), workspace, filepath.Join(t.TempDir(), "priv"), notebookEnvFile(t)))

	err := recorder.Feedback(t.Context(), "stry_abc12345", 1, "")
	if err == nil {
		t.Fatal("expected error from failed commit")
	}
	if !strings.Contains(err.Error(), "commit") {
		t.Errorf("error does not name the commit step: %v", err)
	}
}

// TestNotebook_AppendJSONLDoesNotPull — appending never clobbers an existing
// line and never runs a pull: the worktree is the shared copy and a pull
// would discard an uncommitted line.
func TestNotebook_AppendJSONLDoesNotPull(t *testing.T) {
	fake := &fakeCLI{}
	withFakeCLI(t, fake)

	workspace := filepath.Join(t.TempDir(), "slivingdoc")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `{"storyId":"stry_old","rating":-1,"comment":"","ts":"2026-08-16T16:07:35Z"}` + "\n"
	if err := os.WriteFile(filepath.Join(workspace, feedbackName), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	notebook := NewNotebook(NpxRunner("npx"), workspace, filepath.Join(t.TempDir(), "priv"), notebookEnvFile(t))
	if err := notebook.AppendJSONL(feedbackName, feedbackRecord{StoryID: "stry_new", Rating: 1, Comment: "hi", TS: "2026-08-16T17:00:00Z"}); err != nil {
		t.Fatalf("AppendJSONL: %v", err)
	}

	for _, call := range fake.calls {
		if len(call) > 2 && call[2] == "pull" {
			t.Fatalf("append ran a pull, clobbering the shared copy: %v", fake.calls)
		}
	}
	b, err := os.ReadFile(filepath.Join(workspace, feedbackName))
	if err != nil {
		t.Fatalf("feedback.jsonl: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (existing preserved + new), got %d: %q", len(lines), b)
	}
	if !strings.Contains(lines[0], "stry_old") || !strings.Contains(lines[1], "stry_new") {
		t.Errorf("lines out of order or clobbered: %q", b)
	}
}

// Concurrent posts must never interleave partial lines or racing commits:
// the mutex serializes append+commit as one unit.
func TestNotebook_AppendJSONL_ConcurrentPosts(t *testing.T) {
	fake := &fakeCLI{}
	withFakeCLI(t, fake)

	workspace := filepath.Join(t.TempDir(), "slivingdoc")
	notebook := NewNotebook(NpxRunner("npx"), workspace, filepath.Join(t.TempDir(), "priv"), notebookEnvFile(t))

	const posts = 20
	var wg sync.WaitGroup
	for range posts {
		wg.Go(func() {
			_ = notebook.AppendJSONL(feedbackName, feedbackRecord{StoryID: "stry_x", Rating: 1, Comment: "x", TS: "2026-08-16T17:00:00Z"})
		})
	}
	wg.Wait()

	b, err := os.ReadFile(filepath.Join(workspace, feedbackName))
	if err != nil {
		t.Fatalf("feedback.jsonl: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if len(lines) != posts {
		t.Fatalf("expected %d whole lines, got %d: %q", posts, len(lines), b)
	}
	for _, line := range lines {
		var rec feedbackRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("interleaved partial line %q: %v", line, err)
		}
	}
	if len(fake.calls) != posts {
		t.Errorf("expected %d commits, got %d", posts, len(fake.calls))
	}
}

// Every append-side failure propagates with the failing step named, so the
// caller can surface precisely what went wrong instead of losing the note
// silently. (The commit failure is pinned separately by
// TestFeedbackRecorder_CommitErrorReturnsError.)
func TestNotebook_AppendJSONL_FailuresPropagate(t *testing.T) {
	withFakeCLI(t, &fakeCLI{})

	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(t.TempDir(), "slivingdoc")
	envFile := filepath.Join(t.TempDir(), "credentials.env")
	if err := os.WriteFile(envFile, []byte("AWS_ACCESS_KEY_ID=k\nAWS_SECRET_ACCESS_KEY=s\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name      string
		notebook  *Notebook
		file      string
		v         any
		wantError string
	}{
		{"worktree unwritable", NewNotebook(NpxRunner("npx"), filepath.Join(blocked, "sub"), filepath.Join(t.TempDir(), "priv"), envFile), feedbackName, feedbackRecord{}, "worktree"},
		{"encode failure", NewNotebook(NpxRunner("npx"), workspace, filepath.Join(t.TempDir(), "priv"), envFile), feedbackName, func() {}, "encode"},
		{"open failure", NewNotebook(NpxRunner("npx"), workspace, filepath.Join(t.TempDir(), "priv"), envFile), "missing/feedback.jsonl", feedbackRecord{}, "open"},
		{"env file missing", NewNotebook(NpxRunner("npx"), workspace, filepath.Join(t.TempDir(), "priv"), filepath.Join(t.TempDir(), "missing.env")), feedbackName, feedbackRecord{}, "env"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.notebook.AppendJSONL(tt.file, tt.v)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Errorf("error does not name the %s step: %v", tt.wantError, err)
			}
		})
	}
}

// The commit child receives the same workspace/private roots as the callsign
// and the credentials env file, so the appended line reaches the bucket.
func TestNotebook_CommitArgsCarryRootsAndEnv(t *testing.T) {
	fake := &fakeCLI{}
	withFakeCLI(t, fake)

	workspace := filepath.Join(t.TempDir(), "slivingdoc")
	envFile := filepath.Join(t.TempDir(), "credentials.env")
	if err := os.WriteFile(envFile, []byte("AWS_ACCESS_KEY_ID=KEY\nAWS_SECRET_ACCESS_KEY=SECRET\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	notebook := NewNotebook(NpxRunner("npx"), workspace, "/priv", envFile)
	if err := notebook.AppendJSONL(feedbackName, feedbackRecord{StoryID: "stry_abc12345", Rating: 1, Comment: "", TS: "2026-08-16T17:00:00Z"}); err != nil {
		t.Fatalf("AppendJSONL: %v", err)
	}

	want := []string{"-y", "slivingdoc", "commit", "--workspace-root", workspace, "--private-root", "/priv", workspace, "-m", "append " + feedbackName}
	if !slices.Equal(fake.calls[0], want) {
		t.Errorf("commit call = %v, want %v", fake.calls[0], want)
	}
	for _, want := range []string{"AWS_ACCESS_KEY_ID=KEY", "AWS_SECRET_ACCESS_KEY=SECRET"} {
		if !slices.Contains(fake.envs[0], want) {
			t.Errorf("child env missing %q: %v", want, fake.envs[0])
		}
	}
}
