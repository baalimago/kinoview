package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
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
	ancli.Silent = true
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dir := t.TempDir()
	s := NewStore(WithStorePath(dir), WithClassificationWorkers(3))

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

	if err := s.startClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	M := 12
	for i := 0; i < M; i++ {
		it := model.Item{
			ID:       fmt.Sprintf("ok-%d", i),
			Name:     fmt.Sprintf("ok-%d", i),
			MIMEType: "video/mp4",
		}
		s.addToClassificationQueue(it)
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
	ancli.Silent = true
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dir := t.TempDir()
	s := NewStore(WithStorePath(dir), WithClassificationWorkers(2))

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

	if err := s.startClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	M := 10
	K := 4
	badIDs := map[string]struct{}{}
	for i := 0; i < M; i++ {
		id := fmt.Sprintf("id-%d", i)
		name := fmt.Sprintf("ok-%d", i)
		if i < K {
			name = fmt.Sprintf("bad-%d", i)
			badIDs[id] = struct{}{}
		}
		it := model.Item{
			ID:       id,
			Name:     name,
			MIMEType: "video/mp4",
		}
		s.addToClassificationQueue(it)
	}

	waitUntil(t, 2*time.Second, func() bool {
		s.cacheMu.RLock()
		defer s.cacheMu.RUnlock()
		return len(s.cache) == (M - K)
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
		if _, ok := s.cache[id]; ok {
			t.Fatalf("bad item stored: %s", id)
		}
	}
}

func Test_startClassificationStation_concurrency(t *testing.T) {
	ancli.Silent = true
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dir := t.TempDir()
	s := NewStore(WithStorePath(dir), WithClassificationWorkers(4))

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
			time.Sleep(100 * time.Millisecond)
			atomic.AddInt32(&active, -1)
			meta := json.RawMessage(`{"ok":true}`)
			i.Metadata = &meta
			return i, nil
		},
	}

	if err := s.startClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	M := 8
	start := time.Now()
	for i := 0; i < M; i++ {
		it := model.Item{
			ID:       fmt.Sprintf("c-%d", i),
			Name:     fmt.Sprintf("c-%d", i),
			MIMEType: "video/mp4",
		}
		s.addToClassificationQueue(it)
	}

	waitUntil(t, 3*time.Second, func() bool {
		s.cacheMu.RLock()
		defer s.cacheMu.RUnlock()
		return len(s.cache) == M
	})
	dur := time.Since(start)

	if dur >= 300*time.Millisecond {
		t.Fatalf("took too long: %v", dur)
	}
	if atomic.LoadInt32(&maxConc) < 4 {
		t.Fatalf("expected >=4 concurrent, got %d", maxConc)
	}
}

func Test_startClassificationStation_context(t *testing.T) {
	ancli.Silent = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dir := t.TempDir()
	s := NewStore(WithStorePath(dir), WithClassificationWorkers(1))

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

	if err := s.startClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	it := model.Item{
		ID:       "ctx-1",
		Name:     "ctx-1",
		MIMEType: "video/mp4",
	}
	s.addToClassificationQueue(it)

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
	ctx, cancel := context.WithCancel(context.Background())

	dir := t.TempDir()
	s := NewStore(WithStorePath(dir), WithClassificationWorkers(2))

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

	if err := s.startClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	for i := 0; i < 5; i++ {
		it := model.Item{
			ID:       fmt.Sprintf("k-%d", i),
			Name:     fmt.Sprintf("k-%d", i),
			MIMEType: "video/mp4",
		}
		s.addToClassificationQueue(it)
	}

	time.Sleep(50 * time.Millisecond)
	cancel()

	// ensure no further changes after cancel window
	s.cacheMu.RLock()
	before := len(s.cache)
	s.cacheMu.RUnlock()

	time.Sleep(200 * time.Millisecond)

	s.cacheMu.RLock()
	after := len(s.cache)
	s.cacheMu.RUnlock()

	if after != before {
		t.Fatalf("got new stores after cancel, %d -> %d", before, after)
	}
	close(block)
}

func Test_startClassificationStation_backpressure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dir := t.TempDir()
	s := NewStore(WithStorePath(dir), WithClassificationWorkers(3))

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

	if err := s.startClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	M := 40
	for i := 0; i < M; i++ {
		it := model.Item{
			ID:       fmt.Sprintf("bp-%d", i),
			Name:     fmt.Sprintf("bp-%d", i),
			MIMEType: "video/mp4",
		}
		s.addToClassificationQueue(it)
	}

	waitUntil(t, time.Second, func() bool {
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
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dir := t.TempDir()
	s := NewStore(WithStorePath(dir), WithClassificationWorkers(1))

	s.classificationRequest = make(chan classificationCandidate, 10)
	s.classifierErrors = make(chan error, 10)

	s.classifier = &mockClassifier{
		SetupFunc: func(ctx context.Context) error { return nil },
		ClassifyFunc: func(ctx context.Context, i model.Item) (model.Item, error) {
			return i, fmt.Errorf("uh oh")
		},
	}

	if err := s.startClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	it := model.Item{
		ID:       "cid-1",
		Name:     "cid-1",
		MIMEType: "video/mp4",
	}
	s.addToClassificationQueue(it)

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
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	dir := t.TempDir()
	s := NewStore(WithStorePath(dir), WithClassificationWorkers(8))

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

	if err := s.startClassificationStation(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	M := 200
	for i := 0; i < M; i++ {
		it := model.Item{
			ID:       fmt.Sprintf("big-%d", i),
			Name:     fmt.Sprintf("big-%d", i),
			MIMEType: "video/mp4",
		}
		s.addToClassificationQueue(it)
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
	return len(h) >= len(n) && (func() bool {
		return indexOf(h, n) >= 0
	})()
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
