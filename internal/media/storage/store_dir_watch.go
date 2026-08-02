package storage

import (
	"context"
	"encoding/json"
	"os"
	"path"
	"strings"
	"time"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/model"
	"github.com/fsnotify/fsnotify"
)

// requeueRetryInterval is how often the requeue loop re-presents externally
// reset items the classification station has not accepted yet.
const requeueRetryInterval = 2 * time.Second

// watchStoreDir reacts to classification resets that the `kinoview media` CLI
// writes straight to the store directory. The CLI bypasses Store on purpose —
// Store copies cached metadata back over re-scanned items, so clearing
// metadata through it is impossible — which leaves the running server blind to
// the reset: nothing re-queued the item until a restart. Watching our own
// store directory closes that gap: self-writes always match the cache and are
// ignored, so only external resets trigger a requeue.
func (s *store) watchStoreDir(ctx context.Context) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		ancli.Errf("store dir watcher: %v", err)
		return
	}
	defer w.Close()
	if err := w.Add(s.storePath); err != nil {
		ancli.Errf("store dir watcher: failed to watch %v: %v", s.storePath, err)
		return
	}
	ancli.Noticef("watching store directory for external classification resets: %v", s.storePath)
	for {
		select {
		case <-ctx.Done():
			return
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			ancli.Errf("store dir watcher error: %v", err)
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			if ev.Has(fsnotify.Write) || ev.Has(fsnotify.Create) {
				s.requeueExternalReset(path.Base(ev.Name))
			}
		}
	}
}

// requeueLoop re-presents externally reset items the classification station
// could not accept yet (startup cooldown, rate limit, full queue) until they
// are either enqueued or permanently skipped.
func (s *store) requeueLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = requeueRetryInterval
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			for _, id := range s.pendingRequeueIDs() {
				s.tryRequeue(id)
			}
		}
	}
}

// requeueExternalReset checks the on-disk copy of id for a classification
// reset the cache does not know about, and if one exists syncs the cache and
// queues the item for reclassification. Reports whether it acted.
func (s *store) requeueExternalReset(id string) bool {
	s.cacheMu.RLock()
	cached, ok := s.cache[id]
	s.cacheMu.RUnlock()
	if !ok {
		return false
	}
	disk, err := readStoreItem(s.storePath, id)
	if err != nil {
		// Partial write mid-update; the next fsnotify event re-reads it.
		return false
	}
	if !isExternalClassificationReset(cached, disk) {
		return false
	}

	cached.Metadata = nil
	cached.ClassificationAttempts = disk.ClassificationAttempts
	cached.ClassificationLastTry = disk.ClassificationLastTry
	cached.ClassificationError = disk.ClassificationError
	s.cacheMu.Lock()
	s.cache[id] = cached
	s.cacheMu.Unlock()

	ancli.Noticef("store dir: picked up external classification reset: %v", cached.Name)
	s.markPendingRequeue(id)
	s.tryRequeue(id)
	return true
}

// tryRequeue makes one attempt to enqueue a pending externally-reset item.
// It applies the same stop-loss and backoff rules as the media watcher path,
// and only consumes an attempt when the station accepts the item: a drop
// (full queue, memory pressure) restores the counter so retries do not march
// the item toward the max-attempts ceiling. Rate-limit and cooldown denials
// are skipped silently before enqueuing, so a slow drain does not spam the
// server log with "rate limit reached" warnings every tick.
func (s *store) tryRequeue(id string) {
	s.cacheMu.RLock()
	cached, ok := s.cache[id]
	s.cacheMu.RUnlock()
	if !ok || cached.Metadata != nil || !strings.Contains(cached.MIMEType, "video") {
		s.unmarkPendingRequeue(id)
		return
	}
	if s.atMaxAttempts(cached) {
		ancli.Warnf("classification permanently skipped for %v: max attempts (%v) reached", cached.Name, s.classificationMaxAttempts)
		s.unmarkPendingRequeue(id)
		return
	}
	if s.inBackoff(cached) {
		return // retry on a later tick
	}
	if s.classificationStartupCooldown > 0 && time.Since(s.classificationStationStartTime) < s.classificationStartupCooldown {
		return // cooldown still active; retry on a later tick
	}
	if s.memoryHigh() {
		return // memory pressure; retry on a later tick
	}
	if s.rateLimiter != nil && !s.rateLimiter.peek() {
		return // no token yet; retry on a later tick
	}

	attemptsBefore := cached.ClassificationAttempts
	lastTryBefore := cached.ClassificationLastTry
	cached.ClassificationAttempts++
	cached.ClassificationLastTry = time.Now()
	cached.ClassificationError = ""
	s.cacheMu.Lock()
	s.cache[id] = cached
	s.cacheMu.Unlock()

	s.AddToClassificationQueue(cached)
	if _, inFlight := s.inFlight.Load(id); inFlight {
		s.unmarkPendingRequeue(id)
		return
	}
	// Dropped (queue full, memory pressure): keep pending and restore the
	// attempt so a later retry still counts as one.
	cached.ClassificationAttempts = attemptsBefore
	cached.ClassificationLastTry = lastTryBefore
	s.cacheMu.Lock()
	s.cache[id] = cached
	s.cacheMu.Unlock()
}

// isExternalClassificationReset reports whether disk holds a classification
// reset relative to cached: metadata cleared, or the attempt counter dropped
// below what the server knows (the CLI's reclassify-stale). Only videos
// classify; images and other types never do.
func isExternalClassificationReset(cached, disk model.Item) bool {
	if !strings.Contains(cached.MIMEType, "video") {
		return false
	}
	if disk.Metadata != nil {
		return false
	}
	if cached.Metadata != nil {
		return true
	}
	return disk.ClassificationAttempts < cached.ClassificationAttempts
}

// readStoreItem loads one item's persisted JSON from the store directory.
func readStoreItem(storePath, id string) (model.Item, error) {
	data, err := os.ReadFile(path.Join(storePath, id))
	if err != nil {
		return model.Item{}, err
	}
	var it model.Item
	if err := json.Unmarshal(data, &it); err != nil {
		return model.Item{}, err
	}
	return it, nil
}

func (s *store) markPendingRequeue(id string) {
	s.pendingRequeueMu.Lock()
	s.pendingRequeue[id] = struct{}{}
	s.pendingRequeueMu.Unlock()
}

func (s *store) unmarkPendingRequeue(id string) {
	s.pendingRequeueMu.Lock()
	delete(s.pendingRequeue, id)
	s.pendingRequeueMu.Unlock()
}

func (s *store) pendingRequeueIDs() []string {
	s.pendingRequeueMu.Lock()
	defer s.pendingRequeueMu.Unlock()
	ids := make([]string, 0, len(s.pendingRequeue))
	for id := range s.pendingRequeue {
		ids = append(ids, id)
	}
	return ids
}
