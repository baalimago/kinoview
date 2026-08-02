package storage

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/baalimago/kinoview/internal/model"
)

// The classification queue is unbuffered and only drained by the station that
// Start spawns. Anything using the store without starting it — the
// `kinoview media` CLI — must not block for ever on the send. This was the
// "reclassify just hangs" bug.
func TestAddToClassificationQueue_DoesNotBlockWhenNotStarted(t *testing.T) {
	s := NewStore(WithStorePath(t.TempDir()), WithClassificationStartupCooldown(0))

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.AddToClassificationQueue(model.Item{ID: "abc", Name: "unstarted.mkv"})
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("AddToClassificationQueue blocked with no classification station running")
	}
}

// The same call must still enqueue normally once the station is up.
func TestAddToClassificationQueue_EnqueuesWhenStarted(t *testing.T) {
	s := NewStore(WithStorePath(t.TempDir()), WithClassificationStartupCooldown(0))
	ctx := t.Context()

	// Stand in for the station: mark started and drain the queue ourselves.
	s.started.Store(true)
	s.rateLimiter = newRateLimiter(100, 100)
	got := make(chan model.Item, 1)
	go func() {
		select {
		case c := <-s.classificationRequest:
			got <- c.item
		case <-ctx.Done():
		}
	}()

	s.AddToClassificationQueue(model.Item{ID: "abc", Name: "started.mkv"})

	select {
	case item := <-got:
		if item.Name != "started.mkv" {
			t.Errorf("enqueued %q", item.Name)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("item was not enqueued despite the station running")
	}
}

func seedItem(t *testing.T, s *store, i model.Item) {
	t.Helper()
	if err := s.store(i); err != nil {
		t.Fatalf("seed %q: %v", i.Name, err)
	}
}

// Reclassify has to clear metadata. Going through Store cannot do it: Store
// deliberately copies existing metadata back over the incoming item, so the
// reset was silently undone and reclassify appeared to do nothing.
func TestResetClassification_ClearsMetadataAndAttempts(t *testing.T) {
	s := NewStore(WithStorePath(t.TempDir()))
	raw := json.RawMessage(`{"name":"Done","genre":"drama"}`)
	seedItem(t, s, model.Item{
		ID: "id1", Name: "done.mkv", Path: "/media/done.mkv",
		MIMEType: "video/x-matroska", Metadata: &raw,
		ClassificationAttempts: 3, ClassificationError: "boom",
		ClassificationLastTry: time.Now(),
	})

	changed, err := s.ResetClassification("id1")
	if err != nil {
		t.Fatalf("ResetClassification: %v", err)
	}
	if !changed {
		t.Error("expected a change to be reported")
	}

	got, err := s.GetItemByID("id1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Metadata != nil {
		t.Errorf("metadata survived the reset: %s", *got.Metadata)
	}
	if got.ClassificationAttempts != 0 {
		t.Errorf("attempts = %d, want 0", got.ClassificationAttempts)
	}
	if got.ClassificationError != "" {
		t.Errorf("error = %q, want empty", got.ClassificationError)
	}
	if !got.ClassificationLastTry.IsZero() {
		t.Errorf("lastTry = %v, want zero", got.ClassificationLastTry)
	}
}

func TestResetClassification_UnknownID(t *testing.T) {
	s := NewStore(WithStorePath(t.TempDir()))
	if _, err := s.ResetClassification("nope"); err == nil {
		t.Error("expected an error for an unknown ID")
	}
}

// Clearing the stop-loss must re-open only the items that actually hit the
// ceiling, and must not throw away metadata they already have.
func TestClearClassificationStopLoss(t *testing.T) {
	s := NewStore(WithStorePath(t.TempDir()), WithClassificationMaxAttempts(5))
	raw := json.RawMessage(`{"name":"Partial"}`)
	seedItem(t, s, model.Item{ID: "blocked", Name: "blocked.mkv", ClassificationAttempts: 5, ClassificationError: "rate limited", Metadata: &raw})
	seedItem(t, s, model.Item{ID: "over", Name: "over.mkv", ClassificationAttempts: 9, ClassificationError: "no key"})
	seedItem(t, s, model.Item{ID: "retrying", Name: "retrying.mkv", ClassificationAttempts: 2, ClassificationError: "timeout"})

	for _, id := range []string{"blocked", "over"} {
		changed, err := s.ClearClassificationStopLoss(id)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if !changed {
			t.Errorf("%s: expected it to be reported as freed", id)
		}
		got, err := s.GetItemByID(id)
		if err != nil {
			t.Fatal(err)
		}
		if got.ClassificationAttempts != 0 || got.ClassificationError != "" {
			t.Errorf("%s not reset: %+v", id, got)
		}
	}

	// Metadata must be preserved: this re-opens classification, it does not
	// discard what an item already managed to get.
	blocked, err := s.GetItemByID("blocked")
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Metadata == nil {
		t.Error("stop-loss clear discarded existing metadata")
	}

	// An item inside its retry budget is the server's business, not ours.
	changed, err := s.ClearClassificationStopLoss("retrying")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("an item below the ceiling was reported as freed")
	}
	retrying, err := s.GetItemByID("retrying")
	if err != nil {
		t.Fatal(err)
	}
	if retrying.ClassificationAttempts != 2 || retrying.ClassificationError != "timeout" {
		t.Errorf("an item below the ceiling was modified: %+v", retrying)
	}
}

func TestClassificationMaxAttempts_ReportsConfigured(t *testing.T) {
	s := NewStore(WithStorePath(t.TempDir()), WithClassificationMaxAttempts(7))
	if got := s.ClassificationMaxAttempts(); got != 7 {
		t.Errorf("ClassificationMaxAttempts = %d, want 7", got)
	}
}
