package storage

import (
	"encoding/json"
	"os"
	"path"
	"testing"
	"time"

	"github.com/baalimago/kinoview/internal/model"
)

func Test_isExternalClassificationReset(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"name":"Done"}`)
	tests := []struct {
		name   string
		cached model.Item
		disk   model.Item
		want   bool
	}{
		{
			name:   "metadata cleared",
			cached: model.Item{ID: "a", MIMEType: "video/mp4", Metadata: &raw, ClassificationAttempts: 3},
			disk:   model.Item{ID: "a", MIMEType: "video/mp4", ClassificationAttempts: 0},
			want:   true,
		},
		{
			name:   "stop-loss cleared",
			cached: model.Item{ID: "a", MIMEType: "video/mp4", ClassificationAttempts: 5, ClassificationError: "rate limited"},
			disk:   model.Item{ID: "a", MIMEType: "video/mp4", ClassificationAttempts: 0},
			want:   true,
		},
		{
			name:   "identical unclassified",
			cached: model.Item{ID: "a", MIMEType: "video/mp4", ClassificationAttempts: 1},
			disk:   model.Item{ID: "a", MIMEType: "video/mp4", ClassificationAttempts: 1},
			want:   false,
		},
		{
			name:   "disk has metadata",
			cached: model.Item{ID: "a", MIMEType: "video/mp4", ClassificationAttempts: 0},
			disk:   model.Item{ID: "a", MIMEType: "video/mp4", Metadata: &raw, ClassificationAttempts: 0},
			want:   false,
		},
		{
			name:   "not a video",
			cached: model.Item{ID: "a", MIMEType: "image/jpeg", Metadata: &raw},
			disk:   model.Item{ID: "a", MIMEType: "image/jpeg"},
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isExternalClassificationReset(tt.cached, tt.disk); got != tt.want {
				t.Errorf("isExternalClassificationReset = %v, want %v", got, tt.want)
			}
		})
	}
}

// writeStoreItem simulates the CLI writing a store file straight to disk.
func writeStoreItem(t *testing.T, dir, id string, it model.Item) {
	t.Helper()
	data, err := json.Marshal(it)
	if err != nil {
		t.Fatalf("marshal %v: %v", id, err)
	}
	if err := os.WriteFile(path.Join(dir, id), data, 0o644); err != nil {
		t.Fatalf("write %v: %v", id, err)
	}
}

// watchReadyStore marks the store as started with a permissive rate limiter
// and returns a channel that receives the next enqueued item.
func watchReadyStore(t *testing.T, s *store) chan model.Item {
	t.Helper()
	s.started.Store(true)
	s.rateLimiter = newRateLimiter(100, 100)
	got := make(chan model.Item, 1)
	go func() {
		c := <-s.classificationRequest
		got <- c.item
	}()
	return got
}

func Test_requeueExternalReset_picksUpReset(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := NewStore(WithStorePath(dir), WithClassificationStartupCooldown(0))
	raw := json.RawMessage(`{"name":"Done"}`)
	seedItem(t, s, model.Item{
		ID: "id1", Name: "done.mkv", Path: "/media/done.mkv",
		MIMEType: "video/x-matroska", Metadata: &raw, ClassificationAttempts: 3,
	})
	got := watchReadyStore(t, s)

	// Simulate the CLI's ResetClassification: a second store instance writes
	// the reset straight to disk, bypassing this store's cache.
	cli := NewStore(WithStorePath(dir))
	seedItem(t, cli, model.Item{
		ID: "id1", Name: "done.mkv", Path: "/media/done.mkv",
		MIMEType: "video/x-matroska", Metadata: &raw, ClassificationAttempts: 3,
	})
	if _, err := cli.ResetClassification("id1"); err != nil {
		t.Fatalf("cli reset: %v", err)
	}

	if !s.requeueExternalReset("id1") {
		t.Fatal("expected the external reset to be picked up")
	}

	select {
	case item := <-got:
		if item.ID != "id1" {
			t.Errorf("enqueued %q, want id1", item.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reset item was not enqueued")
	}

	it, err := s.GetItemByID("id1")
	if err != nil {
		t.Fatal(err)
	}
	if it.Metadata != nil {
		t.Error("cache metadata was not cleared")
	}
	if it.ClassificationAttempts != 1 {
		t.Errorf("cache attempts = %d, want 1 (one accepted attempt)", it.ClassificationAttempts)
	}
}

func Test_requeueExternalReset_noopWhenCacheMatchesDisk(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := NewStore(WithStorePath(dir), WithClassificationStartupCooldown(0))
	seedItem(t, s, model.Item{ID: "id1", Name: "queued.mkv", Path: "/media/queued.mkv", MIMEType: "video/mp4"})
	got := watchReadyStore(t, s)

	if s.requeueExternalReset("id1") {
		t.Error("expected no-op when disk matches cache")
	}
	select {
	case item := <-got:
		t.Errorf("unexpected enqueue of %q", item.Name)
	default:
	}
}

func Test_requeueExternalReset_stopLossCleared(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := NewStore(WithStorePath(dir), WithClassificationStartupCooldown(0))
	seedItem(t, s, model.Item{
		ID: "id1", Name: "stuck.mkv", Path: "/media/stuck.mkv",
		MIMEType: "video/mp4", ClassificationAttempts: 5, ClassificationError: "rate limited",
	})
	got := watchReadyStore(t, s)

	// reclassify-stale: attempts dropped to 0, metadata still absent.
	writeStoreItem(t, dir, "id1", model.Item{ID: "id1", Name: "stuck.mkv", Path: "/media/stuck.mkv", MIMEType: "video/mp4"})

	if !s.requeueExternalReset("id1") {
		t.Fatal("expected stop-loss clear to be picked up")
	}
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("stop-loss cleared item was not enqueued")
	}
}

func Test_requeueExternalReset_imageIgnored(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := NewStore(WithStorePath(dir))
	seedItem(t, s, model.Item{ID: "img1", Name: "poster.jpg", Path: "/media/poster.jpg", MIMEType: "image/jpeg"})

	// An external "reset" of an image must not touch it.
	writeStoreItem(t, dir, "img1", model.Item{ID: "img1", Name: "poster.jpg", Path: "/media/poster.jpg", MIMEType: "image/jpeg"})

	if s.requeueExternalReset("img1") {
		t.Error("image must not be requeued")
	}
}

// A drop (here: no classification station running) must not consume the
// item's attempt budget, and the item stays pending for a later retry.
func Test_tryRequeue_restoresAttemptsWhenDropped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := NewStore(WithStorePath(dir), WithClassificationStartupCooldown(0))
	raw := json.RawMessage(`{"name":"Done"}`)
	seedItem(t, s, model.Item{ID: "id1", Name: "a.mkv", Path: "/media/a.mkv", MIMEType: "video/mp4", Metadata: &raw, ClassificationAttempts: 1})
	writeStoreItem(t, dir, "id1", model.Item{ID: "id1", Name: "a.mkv", Path: "/media/a.mkv", MIMEType: "video/mp4"})

	if !s.requeueExternalReset("id1") {
		t.Fatal("expected reset to be picked up")
	}

	s.pendingRequeueMu.Lock()
	pending := len(s.pendingRequeue)
	s.pendingRequeueMu.Unlock()
	if pending != 1 {
		t.Errorf("pending = %d, want 1 (dropped item stays queued for retry)", pending)
	}

	it, err := s.GetItemByID("id1")
	if err != nil {
		t.Fatal(err)
	}
	if it.ClassificationAttempts != 0 {
		t.Errorf("attempts = %d, want 0 (drop must not burn the budget)", it.ClassificationAttempts)
	}
}

// The requeue loop retries pending items until the station accepts them.
func Test_requeueLoop_eventuallyEnqueues(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := NewStore(WithStorePath(dir), WithClassificationStartupCooldown(0))
	raw := json.RawMessage(`{"name":"Done"}`)
	seedItem(t, s, model.Item{
		ID: "id1", Name: "done.mkv", Path: "/media/done.mkv",
		MIMEType: "video/x-matroska", Metadata: &raw, ClassificationAttempts: 3,
	})
	s.started.Store(true)
	// Start empty so the first attempt is skipped; a token is earned after 5ms.
	s.rateLimiter = &rateLimiter{interval: 5 * time.Millisecond, burst: 1, tokens: 0, last: time.Now()}
	got := make(chan model.Item, 1)
	go func() {
		c := <-s.classificationRequest
		got <- c.item
	}()

	ctx := t.Context()
	go s.requeueLoop(ctx, 10*time.Millisecond)

	writeStoreItem(t, dir, "id1", model.Item{ID: "id1", Name: "done.mkv", Path: "/media/done.mkv", MIMEType: "video/x-matroska"})
	if !s.requeueExternalReset("id1") {
		t.Fatal("expected reset to be picked up")
	}

	select {
	case item := <-got:
		if item.ID != "id1" {
			t.Errorf("enqueued %q, want id1", item.ID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("requeue loop never got the item accepted")
	}

	waitUntil(t, 2*time.Second, func() bool {
		s.pendingRequeueMu.Lock()
		defer s.pendingRequeueMu.Unlock()
		_, ok := s.pendingRequeue["id1"]
		return !ok
	})
}

// End-to-end: the fsnotify watcher on the store directory picks up a reset
// written by a separate store instance (the CLI) and re-queues the item.
func Test_watchStoreDir_picksUpExternalReset(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := NewStore(WithStorePath(dir), WithClassificationStartupCooldown(0))
	raw := json.RawMessage(`{"name":"Done"}`)
	seedItem(t, s, model.Item{
		ID: "id1", Name: "done.mkv", Path: "/media/done.mkv",
		MIMEType: "video/x-matroska", Metadata: &raw, ClassificationAttempts: 3,
	})
	got := watchReadyStore(t, s)

	ctx := t.Context()
	go s.watchStoreDir(ctx)
	// Give fsnotify time to register the directory before the CLI writes.
	time.Sleep(50 * time.Millisecond)

	cli := NewStore(WithStorePath(dir))
	seedItem(t, cli, model.Item{
		ID: "id1", Name: "done.mkv", Path: "/media/done.mkv",
		MIMEType: "video/x-matroska", Metadata: &raw, ClassificationAttempts: 3,
	})
	if _, err := cli.ResetClassification("id1"); err != nil {
		t.Fatalf("cli reset: %v", err)
	}

	select {
	case item := <-got:
		if item.ID != "id1" {
			t.Errorf("enqueued %q, want id1", item.ID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watcher never picked up the external reset")
	}

	waitUntil(t, 2*time.Second, func() bool {
		it, err := s.GetItemByID("id1")
		return err == nil && it.Metadata == nil
	})
}

// The server's own writes must not be mistaken for external resets.
func Test_watchStoreDir_ignoresServerWrites(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := NewStore(WithStorePath(dir), WithClassificationStartupCooldown(0))
	raw := json.RawMessage(`{"name":"Done"}`)
	seedItem(t, s, model.Item{
		ID: "id1", Name: "done.mkv", Path: "/media/done.mkv",
		MIMEType: "video/x-matroska", Metadata: &raw,
	})
	got := watchReadyStore(t, s)

	ctx := t.Context()
	go s.watchStoreDir(ctx)

	// A server-side metadata update rewrites the file; the watcher must ignore
	// it (metadata present on both sides) and not re-queue.
	it, err := s.GetItemByID("id1")
	if err != nil {
		t.Fatal(err)
	}
	it.Name = "done-renamed.mkv"
	if err := s.store(it); err != nil {
		t.Fatalf("server write: %v", err)
	}

	time.Sleep(200 * time.Millisecond) // give the watcher time to (wrongly) act
	select {
	case item := <-got:
		t.Errorf("self-write was requeued: %q", item.Name)
	default:
	}
}

// A cooldown drop must not silently strand the item: it is marked for the
// requeue loop to retry once the cooldown expires.
func Test_AddToClassificationQueue_cooldownDropMarkedForRequeue(t *testing.T) {
	t.Parallel()
	s := NewStore(WithStorePath(t.TempDir()), WithClassificationStartupCooldown(time.Hour))
	s.classificationStationStartTime = time.Now()
	s.started.Store(true)
	s.rateLimiter = newRateLimiter(100, 100)

	s.AddToClassificationQueue(model.Item{ID: "a", Name: "a.mkv"})

	s.pendingRequeueMu.Lock()
	_, pending := s.pendingRequeue["a"]
	s.pendingRequeueMu.Unlock()
	if !pending {
		t.Error("cooldown-dropped item was not marked for requeue")
	}
}

// A rate-limit drop must be marked for the requeue loop as well.
func Test_AddToClassificationQueue_rateDropMarkedForRequeue(t *testing.T) {
	t.Parallel()
	s := NewStore(WithStorePath(t.TempDir()), WithClassificationStartupCooldown(0))
	s.started.Store(true)
	// Never earns a token: every attempt is dropped.
	s.rateLimiter = &rateLimiter{interval: time.Hour, burst: 0, tokens: 0, last: time.Now()}

	s.AddToClassificationQueue(model.Item{ID: "a", Name: "a.mkv"})

	s.pendingRequeueMu.Lock()
	_, pending := s.pendingRequeue["a"]
	s.pendingRequeueMu.Unlock()
	if !pending {
		t.Error("rate-dropped item was not marked for requeue")
	}
}

// End-to-end: an item dropped by the startup cooldown is re-presented by the
// requeue loop once the cooldown expires, and gets enqueued.
func Test_requeueLoop_recoversCooldownDroppedItem(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := NewStore(WithStorePath(dir), WithClassificationStartupCooldown(50*time.Millisecond))
	s.classificationStationStartTime = time.Now()
	seedItem(t, s, model.Item{ID: "a", Name: "a.mkv", Path: "/media/a.mkv", MIMEType: "video/mp4"})
	s.started.Store(true)
	s.rateLimiter = newRateLimiter(100, 100)

	got := make(chan model.Item, 1)
	go func() {
		c := <-s.classificationRequest
		got <- c.item
	}()

	ctx := t.Context()
	go s.requeueLoop(ctx, 10*time.Millisecond)

	// Presented during the cooldown window → dropped and marked.
	s.AddToClassificationQueue(model.Item{ID: "a", Name: "a.mkv", Path: "/media/a.mkv", MIMEType: "video/mp4"})

	select {
	case item := <-got:
		if item.ID != "a" {
			t.Errorf("enqueued %q, want a", item.ID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cooldown-dropped item was never re-presented")
	}
}
