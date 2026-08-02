package media

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/baalimago/kinoview/internal/model"
)

func TestDeriveClassificationState(t *testing.T) {
	raw := json.RawMessage(`{"title":"X"}`)
	tests := []struct {
		name        string
		item        model.Item
		maxAttempts int
		want        classifyState
	}{
		{"video with metadata is done", model.Item{MIMEType: "video/mp4", Metadata: &raw}, 5, stateDone},
		{"video over ceiling stops", model.Item{MIMEType: "video/mp4", ClassificationAttempts: 5}, 5, stateStopped},
		{"video with error failed", model.Item{MIMEType: "video/mp4", ClassificationAttempts: 1, ClassificationError: "rate limited"}, 5, stateFailed},
		{"video attempted in progress", model.Item{MIMEType: "video/mp4", ClassificationAttempts: 1}, 5, stateInProgress},
		{"video untouched queued", model.Item{MIMEType: "video/mp4"}, 5, stateQueued},
		{"image not classifiable", model.Item{MIMEType: "image/jpeg"}, 5, stateNotClassifiable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deriveClassificationState(tt.item, tt.maxAttempts); got != tt.want {
				t.Errorf("deriveClassificationState = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTerminal(t *testing.T) {
	terminal := []classifyState{stateDone, stateFailed, stateStopped, stateNotClassifiable}
	for _, s := range terminal {
		if !s.terminal() {
			t.Errorf("expected %v to be terminal", s)
		}
	}
	for _, s := range []classifyState{stateQueued, stateInProgress} {
		if s.terminal() {
			t.Errorf("expected %v to be non-terminal", s)
		}
	}
}

func writeItemFile(t *testing.T, dir string, it model.Item) {
	t.Helper()
	data, err := json.Marshal(it)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path.Join(dir, it.ID), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadItemStates(t *testing.T) {
	dir := t.TempDir()
	raw := json.RawMessage(`{"title":"Done"}`)
	writeItemFile(t, dir, model.Item{ID: "done", Name: "Done.mkv", MIMEType: "video/mp4", Metadata: &raw, ClassificationAttempts: 2})
	writeItemFile(t, dir, model.Item{ID: "queued", Name: "Queued.mkv", MIMEType: "video/mp4"})

	items := []model.Item{
		{ID: "done", Name: "Done.mkv"},
		{ID: "queued", Name: "Queued.mkv"},
		{ID: "missing", Name: "Missing.mkv"},
	}
	states := readItemStates(dir, items, 5, nil)
	if len(states) != 3 {
		t.Fatalf("got %d states, want 3", len(states))
	}
	if states[0].state != stateDone {
		t.Errorf("done item state = %v, want done", states[0].state)
	}
	if states[1].state != stateQueued {
		t.Errorf("queued item state = %v, want queued", states[1].state)
	}
	if states[2].state != stateQueued {
		t.Errorf("missing item state = %v, want queued", states[2].state)
	}

	// Unreadable file keeps the previous state instead of flickering.
	prev := []itemState{
		{name: "Done.mkv", state: stateDone},
		{name: "Queued.mkv", state: stateQueued},
		{name: "Missing.mkv", state: stateInProgress},
	}
	states = readItemStates(dir, items, 5, prev)
	if states[2].state != stateInProgress {
		t.Errorf("missing item with prev state = %v, want in progress kept", states[2].state)
	}
}

func TestStatesChanged(t *testing.T) {
	prev := []itemState{{state: stateQueued}, {state: stateQueued}}
	cur := []itemState{{state: stateQueued}, {state: stateQueued}}
	if statesChanged(prev, cur) {
		t.Error("expected no change")
	}
	cur[1].state = stateInProgress
	if !statesChanged(prev, cur) {
		t.Error("expected change")
	}
}

func TestRenderProgressLine(t *testing.T) {
	t.Run("single item queued", func(t *testing.T) {
		line := renderProgressLine("Show.S01E01.mkv", []itemState{{name: "Show.S01E01.mkv", state: stateQueued}}, time.Second, "", 80)
		if !strings.Contains(line, "Show.S01E01.mkv: … queued") {
			t.Errorf("unexpected line: %q", line)
		}
	})
	t.Run("single item failed shows error", func(t *testing.T) {
		line := renderProgressLine("Show.S01E01.mkv", []itemState{{name: "Show.S01E01.mkv", state: stateFailed, err: "rate limited"}}, time.Second, "", 80)
		if !strings.Contains(line, "✗ failed — rate limited") {
			t.Errorf("unexpected line: %q", line)
		}
	})
	t.Run("group aggregates", func(t *testing.T) {
		states := []itemState{
			{name: "S01E01.mkv", state: stateDone},
			{name: "S01E02.mkv", state: stateDone},
			{name: "S01E03.mkv", state: stateInProgress, attempt: 1},
			{name: "S01E04.mkv", state: stateQueued},
		}
		line := renderProgressLine("Season 1", states, 2*time.Minute, "", 80)
		for _, want := range []string{"2/4 done", "S01E03.mkv (attempt 1)", "1 queued", "2m0s"} {
			if !strings.Contains(line, want) {
				t.Errorf("line %q missing %q", line, want)
			}
		}
	})
	t.Run("hint appended", func(t *testing.T) {
		line := renderProgressLine("Season 1", []itemState{{name: "S01E01.mkv", state: stateQueued}}, time.Second, "no progress", 80)
		if !strings.Contains(line, "no progress") {
			t.Errorf("expected hint in line: %q", line)
		}
	})
	t.Run("truncates to width", func(t *testing.T) {
		line := renderProgressLine("A Very Long Show Name Indeed", []itemState{{name: "S01E01.mkv", state: stateQueued}}, time.Second, "", 30)
		if len([]rune(line)) > 30 {
			t.Errorf("line longer than width: %q", line)
		}
		if !strings.Contains(line, " … ") {
			t.Errorf("expected middle-truncation infix: %q", line)
		}
	})
}

func TestTruncateMiddle(t *testing.T) {
	if got := truncateMiddle("short", 80); got != "short" {
		t.Errorf("truncateMiddle short = %q", got)
	}
	got := truncateMiddle("abcdefghijklmnopqrstuvwxyz", 10)
	if len([]rune(got)) > 10 {
		t.Errorf("truncateMiddle overflow: %q", got)
	}
	if !strings.Contains(got, " … ") {
		t.Errorf("expected infix: %q", got)
	}
}

// runWatch starts the progress watch and returns when it exits or the timeout
// fires, along with everything written to out.
func runWatch(t *testing.T, ctx context.Context, dir, label string, items []model.Item, maxAttempts int, opts progressWatchOptions, timeout time.Duration) string {
	t.Helper()
	var buf bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- watchClassificationProgress(ctx, dir, label, items, maxAttempts, &buf, opts)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("watch error: %v", err)
		}
	case <-time.After(timeout):
		t.Fatal("watch did not exit before timeout")
	}
	return buf.String()
}

func TestWatchClassificationProgress_FinishesWhenAllAttempted(t *testing.T) {
	dir := t.TempDir()
	raw := json.RawMessage(`{"title":"Done"}`)
	a := model.Item{ID: "a", Name: "A.mkv", MIMEType: "video/mp4"}
	b := model.Item{ID: "b", Name: "B.mkv", MIMEType: "video/mp4"}
	writeItemFile(t, dir, a)
	writeItemFile(t, dir, b)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Simulate the server: A classifies cleanly, B fails once.
	go func() {
		time.Sleep(40 * time.Millisecond)
		writeItemFile(t, dir, model.Item{ID: "a", Name: "A.mkv", MIMEType: "video/mp4", Metadata: &raw, ClassificationAttempts: 1})
		time.Sleep(40 * time.Millisecond)
		writeItemFile(t, dir, model.Item{ID: "b", Name: "B.mkv", MIMEType: "video/mp4", ClassificationAttempts: 1, ClassificationError: "rate limited"})
	}()

	out := runWatch(t, ctx, dir, "Season 1", []model.Item{a, b}, 5, progressWatchOptions{
		pollInterval: 10 * time.Millisecond,
		termWidth:    120,
	}, 5*time.Second)

	if !strings.Contains(out, "reclassification finished — 1/2 done") {
		t.Errorf("expected final summary, got: %q", out)
	}
	if !strings.Contains(out, "✗ B.mkv — rate limited") {
		t.Errorf("expected failed item detail, got: %q", out)
	}
}

func TestWatchClassificationProgress_QuitStops(t *testing.T) {
	dir := t.TempDir()
	item := model.Item{ID: "a", Name: "A.mkv", MIMEType: "video/mp4"}
	writeItemFile(t, dir, item)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out := runWatch(t, ctx, dir, "A.mkv", []model.Item{item}, 5, progressWatchOptions{
		pollInterval: 10 * time.Millisecond,
		termWidth:    120,
		quit:         func() bool { return true },
	}, 5*time.Second)

	if !strings.Contains(out, "reclassification stopped") {
		t.Errorf("expected stopped summary, got: %q", out)
	}
}

func TestWatchClassificationProgress_AlreadyDoneExitsImmediately(t *testing.T) {
	dir := t.TempDir()
	raw := json.RawMessage(`{"title":"Done"}`)
	item := model.Item{ID: "a", Name: "A.mkv", MIMEType: "video/mp4", Metadata: &raw}
	writeItemFile(t, dir, item)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out := runWatch(t, ctx, dir, "A.mkv", []model.Item{item}, 5, progressWatchOptions{
		pollInterval: 10 * time.Millisecond,
		termWidth:    120,
	}, 5*time.Second)

	if !strings.Contains(out, "reclassification finished — 1/1 done") {
		t.Errorf("expected immediate done summary, got: %q", out)
	}
}

func TestWatchClassificationProgress_ShowsNoProgressHint(t *testing.T) {
	dir := t.TempDir()
	item := model.Item{ID: "a", Name: "A.mkv", MIMEType: "video/mp4"}
	writeItemFile(t, dir, item)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var buf bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- watchClassificationProgress(ctx, dir, "A.mkv", []model.Item{item}, 5, &buf, progressWatchOptions{
			pollInterval:        10 * time.Millisecond,
			noProgressHintAfter: 30 * time.Millisecond,
			termWidth:           120,
		})
	}()
	// Let the hint render, then stop the watch.
	time.Sleep(80 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("watch error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watch did not exit after cancel")
	}
	if !strings.Contains(buf.String(), "no progress") {
		t.Errorf("expected no-progress hint, got: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "reclassification interrupted") {
		t.Errorf("expected interrupted summary, got: %q", buf.String())
	}
}

// newQuitTest builds a ttyQuit over a raw non-blocking pipe, mirroring how
// openTTYQuit creates its fd. It returns the poller and the write end (a raw
// fd) for feeding input.
func newQuitTest(t *testing.T) (*ttyQuit, int) {
	t.Helper()
	fds := make([]int, 2)
	if err := syscall.Pipe(fds); err != nil {
		t.Fatal(err)
	}
	if err := syscall.SetNonblock(fds[0], true); err != nil {
		syscall.Close(fds[0])
		syscall.Close(fds[1])
		t.Fatal(err)
	}
	return &ttyQuit{fd: fds[0]}, fds[1]
}

func TestTTYQuitPoll(t *testing.T) {
	t.Run("q quits", func(t *testing.T) {
		q, w := newQuitTest(t)
		defer q.close()
		if _, err := syscall.Write(w, []byte("q\n")); err != nil {
			t.Fatal(err)
		}
		syscall.Close(w)
		if !q.poll() {
			t.Error("expected q to quit")
		}
	})
	t.Run("quit quits", func(t *testing.T) {
		q, w := newQuitTest(t)
		defer q.close()
		if _, err := syscall.Write(w, []byte("quit\n")); err != nil {
			t.Fatal(err)
		}
		syscall.Close(w)
		if !q.poll() {
			t.Error("expected quit to quit")
		}
	})
	t.Run("non-quit input ignored", func(t *testing.T) {
		q, w := newQuitTest(t)
		defer q.close()
		if _, err := syscall.Write(w, []byte("n\n")); err != nil {
			t.Fatal(err)
		}
		syscall.Close(w)
		if q.poll() {
			t.Error("expected non-quit input not to quit")
		}
	})
	t.Run("no input does not block", func(t *testing.T) {
		q, w := newQuitTest(t)
		defer q.close()
		defer syscall.Close(w)

		done := make(chan bool, 1)
		go func() { done <- q.poll() }()
		select {
		case got := <-done:
			if got {
				t.Error("expected idle poll not to quit")
			}
		case <-time.After(time.Second):
			t.Fatal("poll blocked on an idle non-blocking fd")
		}
	})
	t.Run("multi-line pending evaluated", func(t *testing.T) {
		q, w := newQuitTest(t)
		defer q.close()
		if _, err := syscall.Write(w, []byte("qu")); err != nil {
			t.Fatal(err)
		}
		if _, err := syscall.Write(w, []byte("it\n")); err != nil {
			t.Fatal(err)
		}
		syscall.Close(w)
		if !q.poll() {
			t.Error("expected accumulated quit to quit")
		}
	})
}

// TestWatchClassificationProgress_ContextCancelWithLivePoller is the regression
// test for "reclassify does not exit on context cancel": the watch's default
// tty quit poller blocked forever on its first idle read, so the loop never
// returned to its select and never observed ctx.Done().
func TestWatchClassificationProgress_ContextCancelWithLivePoller(t *testing.T) {
	dir := t.TempDir()
	item := model.Item{ID: "a", Name: "A.mkv", MIMEType: "video/mp4"}
	writeItemFile(t, dir, item)

	// A real idle, non-blocking fd stands in for /dev/tty: the poll must
	// return EAGAIN immediately so the watch keeps iterating.
	q, w := newQuitTest(t)
	defer q.close()
	defer syscall.Close(w)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- watchClassificationProgress(ctx, dir, "A.mkv", []model.Item{item}, 5, &bytes.Buffer{}, progressWatchOptions{
			pollInterval: 10 * time.Millisecond,
			quit:         q.poll,
			termWidth:    120,
		})
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("watch error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watch did not exit after ctx cancel with a live quit poller")
	}
}

func TestReadItemsFromDisk(t *testing.T) {
	dir := t.TempDir()
	writeItemFile(t, dir, model.Item{ID: "a", Name: "A.mkv", MIMEType: "video/mp4"})
	writeItemFile(t, dir, model.Item{ID: "b", Name: "B.mkv", MIMEType: "video/mp4"})
	if err := os.WriteFile(path.Join(dir, "garbage"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	items := readItemsFromDisk(dir)
	if len(items) != 2 {
		t.Fatalf("readItemsFromDisk returned %d items, want 2", len(items))
	}
	names := map[string]bool{}
	for _, it := range items {
		names[it.Name] = true
	}
	if !names["A.mkv"] || !names["B.mkv"] {
		t.Errorf("unexpected items: %v", names)
	}
}

func TestPrintFinalReport(t *testing.T) {
	var buf bytes.Buffer
	states := []itemState{
		{name: "A.mkv", state: stateDone},
		{name: "B.mkv", state: stateFailed, err: "rate limited"},
		{name: "C.mkv", state: stateQueued},
	}
	printFinalReport(&buf, "Season 1", states, 90*time.Second, "finished")
	out := buf.String()
	for _, want := range []string{"1/3 done", "✗ B.mkv — rate limited", "… C.mkv — not picked up yet"} {
		if !strings.Contains(out, want) {
			t.Errorf("final report missing %q: %q", want, out)
		}
	}
}

func TestPrintGroupSummary(t *testing.T) {
	raw := json.RawMessage(`{"title":"X"}`)
	row := mediaRow{
		kind:     rowGroup,
		groupKey: "/media/Show/Season 1",
		members: []model.Item{
			{Name: "Show.S01E01.mkv", MIMEType: "video/mp4", Metadata: &raw},
			{Name: "Show.S01E02.mkv", MIMEType: "video/mp4"},
		},
	}
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	printGroupSummary(row)
	w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	if !strings.Contains(string(out), "/media/Show/Season 1") || !strings.Contains(string(out), "Show.S01E01.mkv") {
		t.Errorf("unexpected group summary: %q", out)
	}
}
