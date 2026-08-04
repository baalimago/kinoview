package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/baalimago/kinoview/internal/agents"
	"github.com/baalimago/kinoview/internal/model"
)

func waitUntil(t *testing.T, d time.Duration, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for condition")
}

func Test_startClassificationStation_success(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dir := t.TempDir()
	s := NewStore(WithStorePath(dir), WithClassificationWorkers(6), WithClassificationRate(0), WithClassificationStartupCooldown(0))

	s.classificationRequest = make(chan classificationCandidate, 100)
	s.classifierErrors = make(chan error, 100)

	wantMeta := json.RawMessage(`{"ok":true}`)
	s.classifier = &mockClassifier{
		SetupFunc: func(ctx context.Context) error { return nil },
		ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
			i.Metadata = &wantMeta
			return i, nil
		},
	}

	if err := s.StartClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	M := 12
	for i := range M {
		it := model.Item{
			ID:       fmt.Sprintf("ok-%d", i),
			Name:     fmt.Sprintf("ok-%d", i),
			MIMEType: "video/mp4",
		}
		s.AddToClassificationQueue(it)
	}

	waitUntil(t, 2*time.Second, func() bool {
		s.cacheMu.RLock()
		defer s.cacheMu.RUnlock()
		return len(s.cache) == M
	})

	select {
	case e := <-s.classifierErrors:
		t.Fatalf("unexpected error: %v", e)
	default:
	}

	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	for _, v := range s.cache {
		if v.Metadata == nil {
			t.Fatalf("missing metadata on %s", v.ID)
		}
		if string(*v.Metadata) != string(wantMeta) {
			t.Fatalf("bad metadata for %s", v.ID)
		}
	}
}

func Test_startClassificationStation_error(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dir := t.TempDir()
	s := NewStore(WithStorePath(dir), WithClassificationWorkers(5), WithClassificationRate(0), WithClassificationStartupCooldown(0))

	s.classificationRequest = make(chan classificationCandidate, 100)
	s.classifierErrors = make(chan error, 100)

	s.classifier = &mockClassifier{
		SetupFunc: func(ctx context.Context) error { return nil },
		ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
			if len(i.Name) >= 3 && i.Name[:3] == "bad" {
				return i, fmt.Errorf("boom on %s", i.ID)
			}
			meta := json.RawMessage(`{"ok":true}`)
			i.Metadata = &meta
			return i, nil
		},
	}

	if err := s.StartClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	M := 10
	K := 4
	badIDs := map[string]struct{}{}
	for i := range M {
		id := fmt.Sprintf("id-%d", i)
		name := fmt.Sprintf("ok-%d", i)
		if i < K {
			name = fmt.Sprintf("bad-%d", i)
			badIDs[id] = struct{}{}
		}
		it := model.Item{
			ID:                     id,
			Name:                   name,
			MIMEType:               "video/mp4",
			ClassificationAttempts: 1, // simulate handleVideoItem increment
		}
		s.AddToClassificationQueue(it)
	}

	// All items are now persisted (including failed ones with ClassificationError set)
	waitUntil(t, 2*time.Second, func() bool {
		s.cacheMu.RLock()
		defer s.cacheMu.RUnlock()
		return len(s.cache) == M
	})

	errs := []error{}
	for {
		select {
		case e := <-s.classifierErrors:
			errs = append(errs, e)
		default:
			goto DONE
		}
	}
DONE:

	if len(errs) != K {
		t.Fatalf("expected %d errors, got %d", K, len(errs))
	}
	for _, e := range errs {
		es := e.Error()
		if len(es) == 0 {
			t.Fatalf("empty error")
		}
		if es[0] != '[' {
			t.Fatalf("missing [ prefix: %s", es)
		}
		if !contains(es, "classification error:") {
			t.Fatalf("missing msg: %s", es)
		}
	}

	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	for id := range badIDs {
		item, ok := s.cache[id]
		if !ok {
			t.Fatalf("bad item should be persisted (with error tracking): %s", id)
		}
		if item.Metadata != nil {
			t.Fatalf("bad item should have nil metadata: %s", id)
		}
		if item.ClassificationError == "" {
			t.Fatalf("bad item should have ClassificationError set: %s", id)
		}
		if item.ClassificationAttempts != 1 {
			t.Fatalf("bad item should have ClassificationAttempts=1, got %d: %s", item.ClassificationAttempts, id)
		}
	}
}

func Test_startClassificationStation_concurrency(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dir := t.TempDir()
	s := NewStore(WithStorePath(dir), WithClassificationWorkers(4), WithClassificationRate(0), WithClassificationStartupCooldown(0))

	s.classificationRequest = make(chan classificationCandidate, 1000)
	s.classifierErrors = make(chan error, 1000)

	var active int32
	var maxConc int32
	s.classifier = &mockClassifier{
		SetupFunc: func(ctx context.Context) error { return nil },
		ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
			cur := atomic.AddInt32(&active, 1)
			for {
				m := atomic.LoadInt32(&maxConc)
				if cur <= m {
					break
				}
				if atomic.CompareAndSwapInt32(&maxConc, m, cur) {
					break
				}
			}
			// Barrier: hold every worker until all 4 are inside the
			// classifier. Without it, the peak-concurrency observation races
			// the scheduler under parallel load — a straggler worker can
			// arrive after the first wave already finished its sleep, capping
			// maxConc at 3 despite the station being fully concurrent.
			deadline := time.Now().Add(2 * time.Second)
			for atomic.LoadInt32(&active) < 4 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			time.Sleep(30 * time.Millisecond)
			atomic.AddInt32(&active, -1)
			meta := json.RawMessage(`{"ok":true}`)
			i.Metadata = &meta
			return i, nil
		},
	}

	if err := s.StartClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	M := 8
	for i := range M {
		it := model.Item{
			ID:       fmt.Sprintf("c-%d", i),
			Name:     fmt.Sprintf("c-%d", i),
			MIMEType: "video/mp4",
		}
		s.AddToClassificationQueue(it)
	}

	waitUntil(t, 3*time.Second, func() bool {
		s.cacheMu.RLock()
		defer s.cacheMu.RUnlock()
		return len(s.cache) == M
	})

	// The maxConc check is the concurrency proof: with 4 workers and 8 items
	// the station must run at least 4 at once. No wall-clock bound here —
	// under parallel package load the 8 classifications can take longer than
	// any fixed budget without the station being any less concurrent.
	if atomic.LoadInt32(&maxConc) < 4 {
		t.Fatalf("expected >=4 concurrent, got %d", maxConc)
	}
}

func Test_startClassificationStation_context(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	dir := t.TempDir()
	s := NewStore(WithStorePath(dir), WithClassificationWorkers(1), WithClassificationRate(0), WithClassificationStartupCooldown(0))

	s.classificationRequest = make(chan classificationCandidate, 10)
	s.classifierErrors = make(chan error, 10)

	var gotCtx context.Context
	s.classifier = &mockClassifier{
		SetupFunc: func(ctx context.Context) error { return nil },
		ClassifyFunc: func(c context.Context, i model.Item) (model.Item, error) {
			gotCtx = c
			meta := json.RawMessage(`{"ok":true}`)
			i.Metadata = &meta
			return i, nil
		},
	}

	if err := s.StartClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	it := model.Item{
		ID:       "ctx-1",
		Name:     "ctx-1",
		MIMEType: "video/mp4",
	}
	s.AddToClassificationQueue(it)

	waitUntil(t, 2*time.Second, func() bool {
		s.cacheMu.RLock()
		defer s.cacheMu.RUnlock()
		_, ok := s.cache[it.ID]
		return ok
	})

	if gotCtx != ctx {
		t.Fatalf("ctx not propagated to Classify")
	}
}

func Test_startClassificationStation_cancel_shutdown(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())

	dir := t.TempDir()
	s := NewStore(WithStorePath(dir), WithClassificationWorkers(2), WithClassificationRate(0), WithClassificationStartupCooldown(0))

	s.classificationRequest = make(chan classificationCandidate, 100)
	s.classifierErrors = make(chan error, 100)

	block := make(chan struct{})
	s.classifier = &mockClassifier{
		SetupFunc: func(ctx context.Context) error { return nil },
		ClassifyFunc: func(c context.Context, i model.Item) (model.Item, error) {
			select {
			case <-c.Done():
			case <-block:
			}
			meta := json.RawMessage(`{"ok":true}`)
			i.Metadata = &meta
			return i, nil
		},
	}

	if err := s.StartClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	for i := range 5 {
		it := model.Item{
			ID:       fmt.Sprintf("k-%d", i),
			Name:     fmt.Sprintf("k-%d", i),
			MIMEType: "video/mp4",
		}
		s.AddToClassificationQueue(it)
	}

	time.Sleep(20 * time.Millisecond)
	cancel()

	// Cancel must stop the station: the delegator clears the started flag on
	// the way out. A fixed sleep window would race the worker drain under
	// load — in-flight results land asynchronously, and the delegator's
	// select is random between ctx.Done and a ready result — so poll for the
	// pipeline's own shutdown signal instead.
	waitUntil(t, 2*time.Second, func() bool { return !s.started.Load() })

	// With the station down the cache count is final: the delegator that
	// stores results has exited. Record it and verify it holds.
	s.cacheMu.RLock()
	settled := len(s.cache)
	s.cacheMu.RUnlock()

	time.Sleep(100 * time.Millisecond)

	s.cacheMu.RLock()
	after := len(s.cache)
	s.cacheMu.RUnlock()

	if after != settled {
		t.Fatalf("cache grew after station shutdown: %d -> %d", settled, after)
	}
	close(block)
}

func Test_startClassificationStation_backpressure(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dir := t.TempDir()
	s := NewStore(WithStorePath(dir), WithClassificationWorkers(20), WithClassificationRate(0), WithClassificationStartupCooldown(0))

	s.classificationRequest = make(chan classificationCandidate, 50)
	s.classifierErrors = make(chan error, 50)

	s.classifier = &mockClassifier{
		SetupFunc: func(ctx context.Context) error { return nil },
		ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
			time.Sleep(30 * time.Millisecond)
			meta := json.RawMessage(`{"ok":true}`)
			i.Metadata = &meta
			return i, nil
		},
	}

	if err := s.StartClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	M := 40
	for i := range M {
		it := model.Item{
			ID:       fmt.Sprintf("bp-%d", i),
			Name:     fmt.Sprintf("bp-%d", i),
			MIMEType: "video/mp4",
		}
		s.AddToClassificationQueue(it)
	}

	// 40 items through 20 workers at 30ms each is ~60ms of pure work, but the
	// pipeline (queue, delegator, results) stretches under parallel load —
	// poll with a budget that tolerates it instead of a tight window.
	waitUntil(t, 3*time.Second, func() bool {
		s.cacheMu.RLock()
		defer s.cacheMu.RUnlock()
		return len(s.cache) == M
	})

	select {
	case e := <-s.classifierErrors:
		t.Fatalf("unexpected error: %v", e)
	default:
	}
}

func Test_startClassificationStation_corr_id_in_error(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dir := t.TempDir()
	s := NewStore(WithStorePath(dir), WithClassificationWorkers(1), WithClassificationRate(0), WithClassificationStartupCooldown(0))

	s.classificationRequest = make(chan classificationCandidate, 10)
	s.classifierErrors = make(chan error, 10)

	s.classifier = &mockClassifier{
		SetupFunc: func(ctx context.Context) error { return nil },
		ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
			return i, fmt.Errorf("uh oh")
		},
	}

	if err := s.StartClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	it := model.Item{
		ID:       "cid-1",
		Name:     "cid-1",
		MIMEType: "video/mp4",
	}
	s.AddToClassificationQueue(it)

	var got string
	waitUntil(t, 2*time.Second, func() bool {
		select {
		case e := <-s.classifierErrors:
			got = e.Error()
			return true
		default:
			return false
		}
	})

	if len(got) == 0 || got[0] != '[' {
		t.Fatalf("missing corr prefix: %s", got)
	}
	if !contains(got, "classification error:") {
		t.Fatalf("missing error text: %s", got)
	}
}

func Test_startClassificationStation_large_volume(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	dir := t.TempDir()
	s := NewStore(WithStorePath(dir), WithClassificationWorkers(100), WithClassificationRate(0), WithClassificationStartupCooldown(0))

	s.classificationRequest = make(chan classificationCandidate, 2000)
	s.classifierErrors = make(chan error, 2000)

	s.classifier = &mockClassifier{
		SetupFunc: func(ctx context.Context) error { return nil },
		ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
			meta := json.RawMessage(`{}`)
			i.Metadata = &meta
			return i, nil
		},
	}

	if err := s.StartClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	M := 200
	for i := range M {
		it := model.Item{
			ID:       fmt.Sprintf("big-%d", i),
			Name:     fmt.Sprintf("big-%d", i),
			MIMEType: "video/mp4",
		}
		s.AddToClassificationQueue(it)
	}

	waitUntil(t, 5*time.Second, func() bool {
		s.cacheMu.RLock()
		defer s.cacheMu.RUnlock()
		return len(s.cache) == M
	})

	select {
	case e := <-s.classifierErrors:
		t.Fatalf("unexpected error: %v", e)
	default:
	}
}

func contains(h, n string) bool {
	return len(h) >= len(n) && func() bool {
		return indexOf(h, n) >= 0
	}()
}

func indexOf(h, n string) int {
	// naive search, fine for tests
	N := len(h)
	M := len(n)
	if M == 0 {
		return 0
	}
	if M > N {
		return -1
	}
	for i := 0; i <= N-M; i++ {
		if h[i:i+M] == n {
			return i
		}
	}
	return -1
}

// --- rate limiter unit tests ---

func Test_newRateLimiter_zero_rate(t *testing.T) {
	t.Parallel()
	if rl := newRateLimiter(0, 5); rl != nil {
		t.Fatal("expected nil for zero rate")
	}
	if rl := newRateLimiter(-0.5, 5); rl != nil {
		t.Fatal("expected nil for negative rate")
	}
}

func Test_newRateLimiter_zero_burst(t *testing.T) {
	t.Parallel()
	if rl := newRateLimiter(1.0, 0); rl != nil {
		t.Fatal("expected nil for zero burst")
	}
}

func Test_rateLimiter_allow_burst(t *testing.T) {
	t.Parallel()
	rl := newRateLimiter(100, 3) // very high rate, burst 3
	if rl == nil {
		t.Fatal("expected non-nil rate limiter")
	}
	// First 3 should be allowed immediately
	for i := range 3 {
		if !rl.allow() {
			t.Fatalf("burst token %d should be allowed", i)
		}
	}
	// 4th should be denied (empty bucket)
	if rl.allow() {
		t.Fatal("4th call should be denied after burst exhausted")
	}
}

func Test_rateLimiter_allow_refill(t *testing.T) {
	t.Parallel()
	// rate=100/s, burst=2: each token takes 10ms to refill
	rl := newRateLimiter(100, 2)
	if rl == nil {
		t.Fatal("expected non-nil")
	}
	// Consume both tokens
	if !rl.allow() {
		t.Fatal("first token")
	}
	if !rl.allow() {
		t.Fatal("second token")
	}
	// Third denied
	if rl.allow() {
		t.Fatal("third should be denied")
	}
	// Wait for refill
	time.Sleep(15 * time.Millisecond)
	if !rl.allow() {
		t.Fatal("should have refilled one token after 15ms")
	}
}

func Test_rateLimiter_allow_refill_caps_at_burst(t *testing.T) {
	t.Parallel()
	rl := newRateLimiter(1000, 1) // 1ms per token, burst 1
	if rl == nil {
		t.Fatal("expected non-nil")
	}
	// Consume the only token
	if !rl.allow() {
		t.Fatal("first should be allowed")
	}
	// Wait enough for multiple tokens to refill
	time.Sleep(15 * time.Millisecond)
	// Should get at most 1 (burst cap)
	if !rl.allow() {
		t.Fatal("should get one refilled token")
	}
	if rl.allow() {
		t.Fatal("should not exceed burst cap")
	}
}

func Test_rateLimiter_concurrent(t *testing.T) {
	t.Parallel()
	// 1000/s, burst 50 → essentially unlimited for this test
	rl := newRateLimiter(1000, 50)
	if rl == nil {
		t.Fatal("expected non-nil")
	}
	allowed := make(chan bool, 100)
	for range 50 {
		go func() {
			allowed <- rl.allow()
		}()
	}
	for range 50 {
		if !<-allowed {
			t.Fatal("all concurrent calls within burst should be allowed")
		}
	}
}

// --- classification queue gating tests ---

func Test_AddToClassificationQueue_cooldown(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	dir := t.TempDir()
	s := NewStore(
		WithStorePath(dir),
		WithClassificationWorkers(1),
		WithClassificationRate(0),
		WithClassificationStartupCooldown(100*time.Millisecond),
	)
	s.classificationRequest = make(chan classificationCandidate, 10)
	s.classifierErrors = make(chan error, 10)

	s.classifier = &mockClassifier{
		SetupFunc: func(ctx context.Context) error { return nil },
		ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
			meta := json.RawMessage(`{}`)
			i.Metadata = &meta
			return i, nil
		},
	}

	if err := s.StartClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// During cooldown: items should be dropped (deferred)
	it := model.Item{ID: "cool-1", Name: "cool-1", MIMEType: "video/mp4"}
	s.AddToClassificationQueue(it)

	// Verify not queued
	s.cacheMu.RLock()
	if _, ok := s.cache[it.ID]; ok {
		s.cacheMu.RUnlock()
		t.Fatal("item should not be stored during cooldown")
	}
	s.cacheMu.RUnlock()

	// Wait for cooldown to expire
	time.Sleep(200 * time.Millisecond)

	// Now items flow through
	it2 := model.Item{ID: "cool-2", Name: "cool-2", MIMEType: "video/mp4"}
	s.AddToClassificationQueue(it2)

	waitUntil(t, time.Second, func() bool {
		s.cacheMu.RLock()
		defer s.cacheMu.RUnlock()
		_, ok := s.cache[it2.ID]
		return ok
	})
}

func Test_AddToClassificationQueue_rateLimit(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dir := t.TempDir()
	// 10/s, burst 2, no cooldown
	s := NewStore(
		WithStorePath(dir),
		WithClassificationWorkers(10),
		WithClassificationRate(10),
		WithClassificationBurst(2),
		WithClassificationStartupCooldown(0),
	)
	s.classificationRequest = make(chan classificationCandidate, 100)
	s.classifierErrors = make(chan error, 100)

	var stored atomic.Int32
	s.classifier = &mockClassifier{
		SetupFunc: func(ctx context.Context) error { return nil },
		ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
			stored.Add(1)
			meta := json.RawMessage(`{}`)
			i.Metadata = &meta
			return i, nil
		},
	}

	if err := s.StartClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// Send 10 rapid items — rate 10/s, burst 2
	// First 2 admitted immediately, rest dropped until tokens refill
	M := 10
	for i := range M {
		it := model.Item{ID: fmt.Sprintf("rate-%d", i), Name: fmt.Sprintf("rate-%d", i), MIMEType: "video/mp4"}
		s.AddToClassificationQueue(it)
	}

	// Only ~2 should be admitted (burst)
	time.Sleep(100 * time.Millisecond)
	s.cacheMu.RLock()
	cached := len(s.cache)
	s.cacheMu.RUnlock()
	if cached > 3 {
		t.Fatalf("expected at most 3 items from burst, got %d", cached)
	}
	if cached < 2 {
		t.Fatalf("expected at least 2 items from burst, got %d", cached)
	}
}

func Test_AddToClassificationQueue_queueCap(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dir := t.TempDir()
	// 1 worker → queue cap = 2
	s := NewStore(
		WithStorePath(dir),
		WithClassificationWorkers(1),
		WithClassificationRate(0),
		WithClassificationStartupCooldown(0),
	)
	// Use unbuffered classificationRequest so sends block until delegator processes
	s.classificationRequest = make(chan classificationCandidate)
	s.classifierErrors = make(chan error, 100)

	// Block the single worker so workChan fills up
	workerBlock := make(chan struct{})
	var processed atomic.Int32
	s.classifier = &mockClassifier{
		SetupFunc: func(ctx context.Context) error { return nil },
		ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
			<-workerBlock
			processed.Add(1)
			meta := json.RawMessage(`{}`)
			i.Metadata = &meta
			return i, nil
		},
	}

	if err := s.StartClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// Send items from goroutine since classificationRequest is unbuffered
	M := 10
	go func() {
		for i := range M {
			it := model.Item{ID: fmt.Sprintf("qc-%d", i), Name: fmt.Sprintf("qc-%d", i), MIMEType: "video/mp4"}
			s.AddToClassificationQueue(it)
		}
	}()

	// Wait until the delegator has finished draining the queue. With the
	// worker blocked and workChan (cap workers*2) full, every item beyond
	// worker+cap is dropped and marked pending — so the drop count reaching
	// M-(workers+cap) means the pipeline is quiescent. "All sends done" is
	// not a barrier here: the unbuffered request sends complete via handoff
	// before the delegator processes the item, so the last item can still be
	// dispatched after the senders have finished.
	waitUntil(t, 2*time.Second, func() bool {
		s.pendingRequeueMu.Lock()
		defer s.pendingRequeueMu.Unlock()
		return len(s.pendingRequeue) == M-3
	})

	// Unblock worker
	close(workerBlock)

	// All items in workChan (cap=2) should be processed
	waitUntil(t, 2*time.Second, func() bool {
		return processed.Load() >= 1
	})

	// At most 3 items processed: 1 in-progress + 2 in buffer (cap = workers*2 = 2)
	// When worker picks up an item and blocks, workChan slot frees, delegator refills.
	final := int(processed.Load())
	if final > 3 {
		t.Fatalf("expected at most 3 items (workers + cap), got %d", final)
	}
	if final == 0 {
		t.Fatal("expected at least 1 item processed")
	}

	// Stop the station and drain its goroutines before the temp-dir cleanup
	// runs: the delegator may still be writing a result to disk, and a write
	// racing the RemoveAll fails the test in the cleanup phase.
	cancel()
	s.Wait()
}

func Test_AddToClassificationQueue_nilRateLimiter(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dir := t.TempDir()
	// rate=0 → nil rate limiter, no cooldown
	s := NewStore(
		WithStorePath(dir),
		WithClassificationWorkers(5),
		WithClassificationRate(0),
		WithClassificationStartupCooldown(0),
	)
	s.classificationRequest = make(chan classificationCandidate, 100)
	s.classifierErrors = make(chan error, 100)

	s.classifier = &mockClassifier{
		SetupFunc: func(ctx context.Context) error { return nil },
		ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
			meta := json.RawMessage(`{}`)
			i.Metadata = &meta
			return i, nil
		},
	}

	if err := s.StartClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// All items should flow through (no rate limit)
	M := 10
	for i := range M {
		it := model.Item{ID: fmt.Sprintf("nl-%d", i), Name: fmt.Sprintf("nl-%d", i), MIMEType: "video/mp4"}
		s.AddToClassificationQueue(it)
	}

	waitUntil(t, 2*time.Second, func() bool {
		s.cacheMu.RLock()
		defer s.cacheMu.RUnlock()
		return len(s.cache) == M
	})
}

func Test_AddToClassificationQueue_dedup_sameItem(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dir := t.TempDir()
	s := NewStore(
		WithStorePath(dir),
		WithClassificationWorkers(1),
		WithClassificationRate(0),
		WithClassificationStartupCooldown(0),
	)
	s.classificationRequest = make(chan classificationCandidate, 10)
	s.classifierErrors = make(chan error, 10)

	var stored atomic.Int32
	s.classifier = &mockClassifier{
		SetupFunc: func(ctx context.Context) error { return nil },
		ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
			stored.Add(1)
			time.Sleep(20 * time.Millisecond) // give the second queuer time to try
			meta := json.RawMessage(`{}`)
			i.Metadata = &meta
			return i, nil
		},
	}

	if err := s.StartClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	it := model.Item{ID: "dedup-1", Name: "dedup-1", MIMEType: "video/mp4"}
	s.AddToClassificationQueue(it)
	s.AddToClassificationQueue(it) // second call should be no-op

	waitUntil(t, 2*time.Second, func() bool {
		s.cacheMu.RLock()
		defer s.cacheMu.RUnlock()
		return len(s.cache) == 1
	})

	if n := stored.Load(); n != 1 {
		t.Fatalf("expected 1 classification, got %d", n)
	}
}

func Test_AddToClassificationQueue_dedup_cleanupOnSuccess(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dir := t.TempDir()
	s := NewStore(
		WithStorePath(dir),
		WithClassificationWorkers(1),
		WithClassificationRate(0),
		WithClassificationStartupCooldown(0),
	)
	s.classificationRequest = make(chan classificationCandidate, 10)
	s.classifierErrors = make(chan error, 10)

	var stored atomic.Int32
	s.classifier = &mockClassifier{
		SetupFunc: func(ctx context.Context) error { return nil },
		ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
			stored.Add(1)
			meta := json.RawMessage(`{}`)
			i.Metadata = &meta
			return i, nil
		},
	}

	if err := s.StartClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	it := model.Item{ID: "cleanup-ok", Name: "cleanup-ok", MIMEType: "video/mp4"}
	s.AddToClassificationQueue(it)

	// Wait for the classification to be fully processed: the item lands in
	// the cache only after the delegator cleared the in-flight marker, which
	// is what re-queueing gates on. Syncing on the worker's attempt counter
	// alone races the delegator under load.
	waitUntil(t, 2*time.Second, func() bool {
		s.cacheMu.RLock()
		defer s.cacheMu.RUnlock()
		_, ok := s.cache[it.ID]
		return ok
	})

	// After completion, same item should be queueable again
	s.AddToClassificationQueue(it)
	waitUntil(t, 2*time.Second, func() bool {
		return stored.Load() == 2
	})

	if n := stored.Load(); n != 2 {
		t.Fatalf("expected 2 classifications, got %d", n)
	}
}

func Test_AddToClassificationQueue_dedup_cleanupOnError(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dir := t.TempDir()
	s := NewStore(
		WithStorePath(dir),
		WithClassificationWorkers(1),
		WithClassificationRate(0),
		WithClassificationStartupCooldown(0),
	)
	s.classificationRequest = make(chan classificationCandidate, 10)
	s.classifierErrors = make(chan error, 10)

	var attempts atomic.Int32
	s.classifier = &mockClassifier{
		SetupFunc: func(ctx context.Context) error { return nil },
		ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
			attempts.Add(1)
			return i, fmt.Errorf("transient error")
		},
	}

	if err := s.StartClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// Drain error channel in background
	go func() {
		for range s.classifierErrors {
		}
	}()

	it := model.Item{ID: "cleanup-err", Name: "cleanup-err", MIMEType: "video/mp4"}
	s.AddToClassificationQueue(it)

	// Wait for the first attempt to be fully processed: the item lands in the
	// cache (the error path stores it too) only after the delegator cleared
	// the in-flight marker, which is what re-queueing gates on. Syncing on the
	// worker's attempt counter alone races the delegator under load — the
	// marker can still be set when the counter has already moved.
	waitUntil(t, 2*time.Second, func() bool {
		s.cacheMu.RLock()
		defer s.cacheMu.RUnlock()
		_, ok := s.cache[it.ID]
		return ok
	})

	// After error, in-flight should be cleared → item can be re-queued
	s.AddToClassificationQueue(it)
	waitUntil(t, 2*time.Second, func() bool {
		return attempts.Load() == 2
	})
}

func Test_AddToClassificationQueue_dedup_concurrent(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dir := t.TempDir()
	s := NewStore(
		WithStorePath(dir),
		WithClassificationWorkers(2),
		WithClassificationRate(0),
		WithClassificationStartupCooldown(0),
	)
	s.classificationRequest = make(chan classificationCandidate, 100)
	s.classifierErrors = make(chan error, 100)

	// Block classification until all concurrent queuers have fired.
	// This ensures LoadOrStore dedup is tested, not timing of completion.
	blockClassify := make(chan struct{})
	var stored atomic.Int32
	s.classifier = &mockClassifier{
		SetupFunc: func(ctx context.Context) error { return nil },
		ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
			stored.Add(1)
			<-blockClassify
			meta := json.RawMessage(`{}`)
			i.Metadata = &meta
			return i, nil
		},
	}

	if err := s.StartClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// Fire many concurrent attempts with the same ID — only one should get through
	var wg sync.WaitGroup
	N := 50
	wg.Add(N)
	for range N {
		go func() {
			defer wg.Done()
			it := model.Item{ID: "concurrent-dedup", Name: "concurrent-dedup", MIMEType: "video/mp4"}
			s.AddToClassificationQueue(it)
		}()
	}
	wg.Wait()

	// Only one classification should have been admitted.
	// Brief sleep to let the delegator+worker pick up the item.
	time.Sleep(50 * time.Millisecond)
	if n := stored.Load(); n != 1 {
		t.Fatalf("expected exactly 1 classification admitted, got %d", n)
	}

	// Unblock and let it complete
	close(blockClassify)
	waitUntil(t, 2*time.Second, func() bool {
		s.cacheMu.RLock()
		defer s.cacheMu.RUnlock()
		return len(s.cache) == 1
	})

	select {
	case e := <-s.classifierErrors:
		t.Fatalf("unexpected error: %v", e)
	default:
	}
}

func Test_AddToClassificationQueue_dedup_differentItems(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dir := t.TempDir()
	// Use enough workers so queue cap (workers*2) > item count — no drops
	s := NewStore(
		WithStorePath(dir),
		WithClassificationWorkers(10),
		WithClassificationRate(0),
		WithClassificationStartupCooldown(0),
	)
	s.classificationRequest = make(chan classificationCandidate, 100)
	s.classifierErrors = make(chan error, 100)

	var stored atomic.Int32
	s.classifier = &mockClassifier{
		SetupFunc: func(ctx context.Context) error { return nil },
		ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
			stored.Add(1)
			meta := json.RawMessage(`{}`)
			i.Metadata = &meta
			return i, nil
		},
	}

	if err := s.StartClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	M := 10
	for i := range M {
		it := model.Item{ID: fmt.Sprintf("diff-%d", i), Name: fmt.Sprintf("diff-%d", i), MIMEType: "video/mp4"}
		s.AddToClassificationQueue(it)
	}

	waitUntil(t, 2*time.Second, func() bool {
		return stored.Load() == int32(M)
	})

	if n := stored.Load(); n != int32(M) {
		t.Fatalf("expected %d classifications, got %d", M, n)
	}

	select {
	case e := <-s.classifierErrors:
		t.Fatalf("unexpected error: %v", e)
	default:
	}
}

func Test_AddToClassificationQueue_dedup_cooldownCleanup(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dir := t.TempDir()
	s := NewStore(
		WithStorePath(dir),
		WithClassificationWorkers(1),
		WithClassificationRate(0),
		WithClassificationStartupCooldown(100*time.Millisecond),
	)
	s.classificationRequest = make(chan classificationCandidate, 10)
	s.classifierErrors = make(chan error, 10)

	var stored atomic.Int32
	s.classifier = &mockClassifier{
		SetupFunc: func(ctx context.Context) error { return nil },
		ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
			stored.Add(1)
			meta := json.RawMessage(`{}`)
			i.Metadata = &meta
			return i, nil
		},
	}

	if err := s.StartClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// During cooldown: item is dropped but in-flight entry should be cleaned up
	it := model.Item{ID: "cool-dup", Name: "cool-dup", MIMEType: "video/mp4"}
	s.AddToClassificationQueue(it)

	// Wait for cooldown to expire
	time.Sleep(200 * time.Millisecond)

	// Same item should be queueable after cooldown (since in-flight was cleaned)
	s.AddToClassificationQueue(it)

	waitUntil(t, 2*time.Second, func() bool {
		return stored.Load() == 1
	})

	if n := stored.Load(); n != 1 {
		t.Fatalf("expected 1 classification, got %d", n)
	}
}

func Test_AddToClassificationQueue_dedup_limitCleanup(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dir := t.TempDir()
	// rate=100/s, burst=1 → first item admitted, second dropped
	s := NewStore(
		WithStorePath(dir),
		WithClassificationWorkers(1),
		WithClassificationRate(100),
		WithClassificationBurst(1),
		WithClassificationStartupCooldown(0),
	)
	s.classificationRequest = make(chan classificationCandidate, 10)
	s.classifierErrors = make(chan error, 10)

	var stored atomic.Int32
	s.classifier = &mockClassifier{
		SetupFunc: func(ctx context.Context) error { return nil },
		ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
			stored.Add(1)
			meta := json.RawMessage(`{}`)
			i.Metadata = &meta
			return i, nil
		},
	}

	if err := s.StartClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// First item admitted, second dropped by rate limiter
	it := model.Item{ID: "limit-dup", Name: "limit-dup", MIMEType: "video/mp4"}
	s.AddToClassificationQueue(it)
	s.AddToClassificationQueue(it) // burst exhausted → dropped

	// Wait for the first item to be fully processed (delegator cleared the
	// in-flight marker), then refill and retry.
	waitUntil(t, 2*time.Second, func() bool {
		s.cacheMu.RLock()
		defer s.cacheMu.RUnlock()
		_, ok := s.cache[it.ID]
		return ok
	})

	// Now the item completed. Wait for a token to refill (rate=100/s → 10ms) and retry
	time.Sleep(50 * time.Millisecond)
	s.AddToClassificationQueue(it)

	waitUntil(t, 2*time.Second, func() bool {
		return stored.Load() == 2
	})

	if n := stored.Load(); n != 2 {
		t.Fatalf("expected 2 classifications, got %d", n)
	}
}

func Test_AddToClassificationQueue_noStationDoesNotBlock(t *testing.T) {
	t.Parallel()
	// This test used to assert the opposite — that enqueuing with nothing draining
	// the channel BLOCKS. That behaviour was a bug: the `kinoview media` CLI shares
	// this write path without running a station, so reclassifying from the CLI hung
	// for ever. The contract is now that enqueuing without a consumer is a no-op.

	dir := t.TempDir()
	s := NewStore(
		WithStorePath(dir),
		WithClassificationWorkers(1),
		WithClassificationRate(0),
		WithClassificationStartupCooldown(0),
	)
	// Deliberately tiny, and deliberately never drained.
	s.classificationRequest = make(chan classificationCandidate, 2)
	s.classifierErrors = make(chan error, 10)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 5 {
			it := model.Item{ID: fmt.Sprintf("b-%d", i), MIMEType: "video/mp4"}
			s.AddToClassificationQueue(it)
		}
	}()

	select {
	case <-done:
		// Expected: every call returns promptly instead of deadlocking.
	case <-time.After(2 * time.Second):
		t.Fatal("AddToClassificationQueue blocked with no classification station running")
	}

	// Nothing should have been queued, since there was nobody to receive it.
	if got := len(s.classificationRequest); got != 0 {
		t.Errorf("queued %v item(s) with no station running, want 0", got)
	}
}

func Test_StartClassificationStation_cooldownDefault(t *testing.T) {
	t.Parallel()
	// Default store has cooldown=10s, which we override to a tiny value for test speed
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dir := t.TempDir()
	s := NewStore(
		WithStorePath(dir),
		WithClassificationWorkers(1),
		WithClassificationRate(0),
		WithClassificationStartupCooldown(50*time.Millisecond),
	)
	s.classificationRequest = make(chan classificationCandidate, 10)
	s.classifierErrors = make(chan error, 10)

	s.classifier = &mockClassifier{
		SetupFunc: func(ctx context.Context) error { return nil },
		ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
			meta := json.RawMessage(`{}`)
			i.Metadata = &meta
			return i, nil
		},
	}

	if err := s.StartClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// Immediate: dropped
	it := model.Item{ID: "cd-1", Name: "cd-1", MIMEType: "video/mp4"}
	s.AddToClassificationQueue(it)

	time.Sleep(100 * time.Millisecond)

	// After cooldown: flows through
	it2 := model.Item{ID: "cd-2", Name: "cd-2", MIMEType: "video/mp4"}
	s.AddToClassificationQueue(it2)

	waitUntil(t, time.Second, func() bool {
		s.cacheMu.RLock()
		defer s.cacheMu.RUnlock()
		_, ok := s.cache[it2.ID]
		return ok
	})

	// it should NOT be in cache
	s.cacheMu.RLock()
	_, exists := s.cache[it.ID]
	s.cacheMu.RUnlock()
	if exists {
		t.Fatal("first item should not be stored (cooldown)")
	}
}

func Test_StartClassificationStation_negativeCooldown(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	dir := t.TempDir()
	// Negative cooldown = immediate admission
	s := NewStore(
		WithStorePath(dir),
		WithClassificationWorkers(1),
		WithClassificationRate(0),
		WithClassificationStartupCooldown(-1*time.Second),
	)
	s.classificationRequest = make(chan classificationCandidate, 10)
	s.classifierErrors = make(chan error, 10)

	s.classifier = &mockClassifier{
		SetupFunc: func(ctx context.Context) error { return nil },
		ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
			meta := json.RawMessage(`{}`)
			i.Metadata = &meta
			return i, nil
		},
	}

	if err := s.StartClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	it := model.Item{ID: "nc-1", Name: "nc-1", MIMEType: "video/mp4"}
	s.AddToClassificationQueue(it)

	waitUntil(t, time.Second, func() bool {
		s.cacheMu.RLock()
		defer s.cacheMu.RUnlock()
		_, ok := s.cache[it.ID]
		return ok
	})
}

func Test_startClassificationStation_workersUseClones(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dir := t.TempDir()
	numWorkers := 4
	s := NewStore(
		WithStorePath(dir),
		WithClassificationWorkers(numWorkers),
		WithClassificationRate(0),
		WithClassificationStartupCooldown(0),
	)
	s.classificationRequest = make(chan classificationCandidate, 100)
	s.classifierErrors = make(chan error, 100)

	var cloneCount atomic.Int32
	// Track which worker handles which item via a per-worker state marker
	workerActivity := make(map[int]int32)
	var activityMu sync.Mutex

	s.classifier = &mockClassifier{
		CloneFunc: func() agents.Classifier {
			activityMu.Lock()
			id := int(cloneCount.Add(1))
			workerActivity[id] = 0
			activityMu.Unlock()
			return &mockClassifier{
				SetupFunc: func(ctx context.Context) error { return nil },
				ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
					activityMu.Lock()
					workerActivity[id]++
					activityMu.Unlock()
					meta := json.RawMessage(`{}`)
					i.Metadata = &meta
					return i, nil
				},
			}
		},
	}

	if err := s.StartClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// Send items — each worker should process some
	M := 8
	for i := range M {
		it := model.Item{ID: fmt.Sprintf("wc-%d", i), Name: fmt.Sprintf("wc-%d", i), MIMEType: "video/mp4"}
		s.AddToClassificationQueue(it)
	}

	waitUntil(t, 3*time.Second, func() bool {
		s.cacheMu.RLock()
		defer s.cacheMu.RUnlock()
		return len(s.cache) == M
	})

	// Verify: each worker got its own clone (Clone was called numWorkers times)
	if n := cloneCount.Load(); n != int32(numWorkers) {
		t.Fatalf("expected %d clones, got %d", numWorkers, n)
	}

	// Verify: at least one worker was active (items were processed)
	activityMu.Lock()
	totalProcessed := int32(0)
	for _, count := range workerActivity {
		totalProcessed += count
	}
	activityMu.Unlock()
	if totalProcessed != int32(M) {
		t.Fatalf("expected %d total processed items, got %d", M, totalProcessed)
	}

	// Verify no errors
	select {
	case e := <-s.classifierErrors:
		t.Fatalf("unexpected error: %v", e)
	default:
	}
}

func Test_memoryHigh_disabled(t *testing.T) {
	t.Parallel()
	t.Run("threshold 0 disables", func(t *testing.T) {
		s := NewStore(WithMemoryThreshold(0))
		if s.memoryHigh() {
			t.Fatal("memoryHigh should return false when threshold is 0")
		}
	})

	t.Run("threshold 1 disables", func(t *testing.T) {
		s := NewStore(WithMemoryThreshold(1))
		if s.memoryHigh() {
			t.Fatal("memoryHigh should return false when threshold is 1")
		}
	})

	t.Run("negative threshold disables", func(t *testing.T) {
		s := NewStore(WithMemoryThreshold(-1))
		if s.memoryHigh() {
			t.Fatal("memoryHigh should return false when threshold is negative")
		}
	})

	t.Run("default threshold 0.8: passes under normal conditions", func(t *testing.T) {
		// Pin total RAM at 1 TiB so the assertion is deterministic: the test
		// binary's footprint can never reach 80% of a terabyte, regardless of
		// how much the suite allocated earlier in the process.
		s := NewStore()
		s.totalMemory = func() uint64 { return 1 << 40 }
		// Default is 0.8 (80%). In test conditions, Alloc is tiny vs Sys.
		if s.memoryHigh() {
			t.Fatal("memoryHigh should return false in normal test conditions")
		}
	})
}

func Test_memoryHigh_enabled(t *testing.T) {
	t.Parallel()
	t.Run("threshold 0.01 triggers with any allocation", func(t *testing.T) {
		// Pin total RAM at 1 MiB. Any running Go process has a footprint well
		// above 10 KiB (1% of 1 MiB), so the assertion is deterministic.
		s := NewStore(WithMemoryThreshold(0.01))
		s.totalMemory = func() uint64 { return 1 << 20 }
		// Alloc should be > 1% of Sys in any running Go process.
		if !s.memoryHigh() {
			t.Fatal("memoryHigh should return true when threshold is 1%")
		}
	})
}

func Test_startClassificationStation_memoryGuard(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dir := t.TempDir()
	s := NewStore(
		WithStorePath(dir),
		WithClassificationWorkers(1),
		WithClassificationRate(0),
		WithClassificationStartupCooldown(0),
		WithMemoryThreshold(0.01),
	)
	// Pin total RAM at 1 MiB: every enqueue trips the memory guard and is
	// dropped, independent of how much memory earlier tests allocated.
	s.totalMemory = func() uint64 { return 1 << 20 }

	s.classificationRequest = make(chan classificationCandidate, 100)
	s.classifierErrors = make(chan error, 100)

	var classified int32
	s.classifier = &mockClassifier{
		SetupFunc: func(ctx context.Context) error { return nil },
		ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
			atomic.AddInt32(&classified, 1)
			meta := json.RawMessage(`{"ok":true}`)
			i.Metadata = &meta
			return i, nil
		},
	}

	if err := s.StartClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// Queue several items — all should be dropped by memory guard
	for i := range 5 {
		it := model.Item{
			ID:       fmt.Sprintf("mem-%d", i),
			Name:     fmt.Sprintf("mem-%d", i),
			MIMEType: "video/mp4",
		}
		s.AddToClassificationQueue(it)
	}

	// Give the delegator time to process (it should drop everything). The
	// delegator cleans up the in-flight entry when it drops, so poll for that
	// instead of sleeping a fixed window.
	waitUntil(t, 2*time.Second, func() bool {
		_, loaded := s.inFlight.Load("mem-0")
		return !loaded
	})

	if n := atomic.LoadInt32(&classified); n != 0 {
		t.Fatalf("expected 0 classified items with memory guard active, got %d", n)
	}

	// Verify in-flight entries were cleaned up (items should be re-queueable)
	it := model.Item{ID: "mem-0", Name: "mem-0", MIMEType: "video/mp4"}
	_, loaded := s.inFlight.Load(it.ID)
	if loaded {
		t.Fatal("in-flight entry should be cleaned up after memory-guard drop")
	}
}

func Test_StartClassificationStation_workersWarning(t *testing.T) {
	t.Parallel()
	t.Run("workers <= 3: no warning", func(t *testing.T) {
		s := NewStore(
			WithClassificationWorkers(3),
			WithClassificationRate(0),
			WithClassificationStartupCooldown(0),
		)
		s.classificationRequest = make(chan classificationCandidate, 10)
		s.classifierErrors = make(chan error, 10)
		s.classifier = &mockClassifier{
			SetupFunc:    func(ctx context.Context) error { return nil },
			ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) { return i, nil },
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		// Should not panic or error — just verify it starts
		err := s.StartClassificationStation(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("workers > 3: starts successfully", func(t *testing.T) {
		s := NewStore(
			WithClassificationWorkers(5),
			WithClassificationRate(0),
			WithClassificationStartupCooldown(0),
		)
		s.classificationRequest = make(chan classificationCandidate, 10)
		s.classifierErrors = make(chan error, 10)
		s.classifier = &mockClassifier{
			SetupFunc:    func(ctx context.Context) error { return nil },
			ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) { return i, nil },
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		// Should start despite warning (warning is logged, not returned as error)
		err := s.StartClassificationStation(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// --- Phase 9: classification failure resilience ---

func Test_classificationBackoff(t *testing.T) {
	t.Parallel()
	t.Run("attempts=0 returns 0", func(t *testing.T) {
		if d := classificationBackoff(0); d != 0 {
			t.Fatalf("expected 0, got %v", d)
		}
	})
	t.Run("attempts negative returns 0", func(t *testing.T) {
		if d := classificationBackoff(-1); d != 0 {
			t.Fatalf("expected 0, got %v", d)
		}
	})
	t.Run("attempts=1 returns 30s", func(t *testing.T) {
		if d := classificationBackoff(1); d != 30*time.Second {
			t.Fatalf("expected 30s, got %v", d)
		}
	})
	t.Run("attempts=2 returns 60s", func(t *testing.T) {
		if d := classificationBackoff(2); d != 60*time.Second {
			t.Fatalf("expected 60s, got %v", d)
		}
	})
	t.Run("attempts=3 returns 2min", func(t *testing.T) {
		if d := classificationBackoff(3); d != 2*time.Minute {
			t.Fatalf("expected 2min, got %v", d)
		}
	})
	t.Run("attempts=4 returns 4min", func(t *testing.T) {
		if d := classificationBackoff(4); d != 4*time.Minute {
			t.Fatalf("expected 4min, got %v", d)
		}
	})
	t.Run("attempts=5 returns 8min", func(t *testing.T) {
		if d := classificationBackoff(5); d != 8*time.Minute {
			t.Fatalf("expected 8min, got %v", d)
		}
	})
	t.Run("large attempts caps at 24h", func(t *testing.T) {
		if d := classificationBackoff(100); d != 24*time.Hour {
			t.Fatalf("expected 24h cap, got %v", d)
		}
	})
}

func Test_handleVideoItem_maxAttempts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := NewStore(WithStorePath(dir), WithClassificationMaxAttempts(3))

	i := model.Item{
		ID:                     "maxed-out",
		Name:                   "maxed-out",
		MIMEType:               "video/mp4",
		ClassificationAttempts: 3,
	}

	err := s.handleVideoItem(&i)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Attempts should not have changed (item is permanently skipped)
	if i.ClassificationAttempts != 3 {
		t.Fatalf("expected attempts to stay at 3, got %d", i.ClassificationAttempts)
	}
}

func Test_handleVideoItem_backoffActive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := NewStore(WithStorePath(dir))

	i := model.Item{
		ID:                     "backoff-active",
		Name:                   "backoff-active",
		MIMEType:               "video/mp4",
		ClassificationAttempts: 2,
		ClassificationLastTry:  time.Now(), // just now
	}

	err := s.handleVideoItem(&i)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Attempts should not have changed (backoff not expired)
	if i.ClassificationAttempts != 2 {
		t.Fatalf("expected attempts to stay at 2, got %d", i.ClassificationAttempts)
	}
}

func Test_handleVideoItem_backoffExpired(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := NewStore(
		WithStorePath(dir),
		WithClassificationWorkers(1),
		WithClassificationRate(0),
		WithClassificationStartupCooldown(0),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	s.classificationRequest = make(chan classificationCandidate, 10)
	s.classifierErrors = make(chan error, 10)

	s.classifier = &mockClassifier{
		SetupFunc: func(ctx context.Context) error { return nil },
		ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
			meta := json.RawMessage(`{}`)
			i.Metadata = &meta
			return i, nil
		},
	}

	if err := s.StartClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	i := model.Item{
		ID:                     "backoff-expired",
		Name:                   "backoff-expired",
		MIMEType:               "video/mp4",
		ClassificationAttempts: 1,
		ClassificationLastTry:  time.Now().Add(-31 * time.Second), // 31s ago, backoff is 30s
	}

	err := s.handleVideoItem(&i)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Attempts should be incremented
	if i.ClassificationAttempts != 2 {
		t.Fatalf("expected attempts=2, got %d", i.ClassificationAttempts)
	}

	// The item should eventually be classified (via the queued item)
	waitUntil(t, 2*time.Second, func() bool {
		s.cacheMu.RLock()
		defer s.cacheMu.RUnlock()
		item, ok := s.cache[i.ID]
		return ok && item.Metadata != nil
	})
}

func Test_handleVideoItem_incrementsAttempts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := NewStore(WithStorePath(dir))
	s.classificationRequest = make(chan classificationCandidate, 1) // buffered to avoid blocking

	i := model.Item{
		ID:       "increment-test",
		Name:     "increment-test",
		MIMEType: "video/mp4",
	}

	err := s.handleVideoItem(&i)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if i.ClassificationAttempts != 1 {
		t.Fatalf("expected attempts=1, got %d", i.ClassificationAttempts)
	}
	if i.ClassificationLastTry.IsZero() {
		t.Fatal("expected ClassificationLastTry to be set")
	}
	if i.ClassificationError != "" {
		t.Fatalf("expected ClassificationError to be empty, got %q", i.ClassificationError)
	}
}

func Test_handleVideoItem_legacyItem(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := NewStore(WithStorePath(dir))
	s.classificationRequest = make(chan classificationCandidate, 1) // buffered to avoid blocking

	// Legacy item: zero-value fields (no classificationAttempts in JSON)
	i := model.Item{
		ID:       "legacy",
		Name:     "legacy",
		MIMEType: "video/mp4",
	}

	err := s.handleVideoItem(&i)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should be treated as ClassificationAttempts=0 → queued normally
	if i.ClassificationAttempts != 1 {
		t.Fatalf("expected attempts=1 after first try, got %d", i.ClassificationAttempts)
	}
}

func Test_startClassificationStation_successClearsAttempts(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dir := t.TempDir()
	s := NewStore(
		WithStorePath(dir),
		WithClassificationWorkers(1),
		WithClassificationRate(0),
		WithClassificationStartupCooldown(0),
	)

	s.classificationRequest = make(chan classificationCandidate, 10)
	s.classifierErrors = make(chan error, 10)

	s.classifier = &mockClassifier{
		SetupFunc: func(ctx context.Context) error { return nil },
		ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
			meta := json.RawMessage(`{"ok":true}`)
			i.Metadata = &meta
			return i, nil
		},
	}

	if err := s.StartClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	it := model.Item{
		ID:                     "clear-test",
		Name:                   "clear-test",
		MIMEType:               "video/mp4",
		ClassificationAttempts: 3,
		ClassificationLastTry:  time.Now(),
		ClassificationError:    "previous error",
	}
	s.AddToClassificationQueue(it)

	waitUntil(t, 2*time.Second, func() bool {
		s.cacheMu.RLock()
		defer s.cacheMu.RUnlock()
		item, ok := s.cache[it.ID]
		return ok && item.Metadata != nil
	})

	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	item := s.cache[it.ID]
	if item.ClassificationAttempts != 0 {
		t.Fatalf("expected attempts cleared to 0, got %d", item.ClassificationAttempts)
	}
	if !item.ClassificationLastTry.IsZero() {
		t.Fatalf("expected ClassificationLastTry to be zero, got %v", item.ClassificationLastTry)
	}
	if item.ClassificationError != "" {
		t.Fatalf("expected ClassificationError to be empty, got %q", item.ClassificationError)
	}
}

func Test_startClassificationStation_errorPersistsAttempts(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dir := t.TempDir()
	s := NewStore(
		WithStorePath(dir),
		WithClassificationWorkers(1),
		WithClassificationRate(0),
		WithClassificationStartupCooldown(0),
	)

	s.classificationRequest = make(chan classificationCandidate, 10)
	s.classifierErrors = make(chan error, 10)

	s.classifier = &mockClassifier{
		SetupFunc: func(ctx context.Context) error { return nil },
		ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
			return i, fmt.Errorf("transient failure")
		},
	}

	if err := s.StartClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// Drain error channel
	go func() {
		for range s.classifierErrors {
		}
	}()

	it := model.Item{
		ID:                     "err-persist",
		Name:                   "err-persist",
		MIMEType:               "video/mp4",
		ClassificationAttempts: 1, // simulate handleVideoItem increment
	}
	s.AddToClassificationQueue(it)

	waitUntil(t, 2*time.Second, func() bool {
		s.cacheMu.RLock()
		defer s.cacheMu.RUnlock()
		_, ok := s.cache[it.ID]
		return ok
	})

	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	item := s.cache[it.ID]
	if item.Metadata != nil {
		t.Fatal("expected nil metadata")
	}
	if item.ClassificationAttempts != 1 {
		t.Fatalf("expected attempts=1, got %d", item.ClassificationAttempts)
	}
	if item.ClassificationError == "" {
		t.Fatal("expected ClassificationError to be set")
	}
}
