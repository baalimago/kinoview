package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/agents"
	"github.com/baalimago/kinoview/internal/model"
	"golang.org/x/exp/rand"
)

type ClassificationStation interface {
	StartClassificationStation(ctx context.Context) error
	Ready() <-chan struct{}
	AddToClassificationQueue(i model.Item)
}

type classificationCandidate struct {
	correlationID string
	item          model.Item
}

type classificationResult struct {
	correlationID string
	classifierErr error
	item          model.Item
}

// randString for ID, deterministic length, not crypto-rand.
func randString(n int) string {
	letters := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	out := make([]rune, n)
	rand.Seed(uint64(time.Now().UnixNano()))
	for i := range out {
		out[i] = letters[rand.Intn(len(letters))]
	}
	return string(out)
}

// rateLimiter is a simple token-bucket rate limiter using only the standard library.
// A nil *rateLimiter means unlimited — callers must nil-check before calling allow().
type rateLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	burst    int
	tokens   int
	last     time.Time
}

func newRateLimiter(ratePerSec float64, burst int) *rateLimiter {
	if ratePerSec <= 0 || burst <= 0 {
		return nil
	}
	return &rateLimiter{
		interval: time.Duration(float64(time.Second) / ratePerSec),
		burst:    burst,
		tokens:   burst,
		last:     time.Now(),
	}
}

func (r *rateLimiter) allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(r.last)
	earned := int(elapsed / r.interval)
	if earned > 0 {
		r.tokens = min(r.tokens+earned, r.burst)
		r.last = now
	}
	if r.tokens > 0 {
		r.tokens--
		return true
	}
	return false
}

// peek reports whether a token would be available right now without
// consuming one. The requeue loop uses it to skip items the rate limiter
// would drop anyway, avoiding attempt burns and per-tick log spam.
func (r *rateLimiter) peek() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(r.last)
	earned := int(elapsed / r.interval)
	return r.tokens+earned > 0
}

// AddToClassificationQueue by pushing the item to the back of the queue.
// Items may be dropped if: already in flight, startup cooldown is active,
// rate limit is exceeded, or the downstream work queue is at capacity.
// Items dropped by cooldown, rate limit, a full queue, or memory pressure are
// marked for retry by the requeue loop, so "deferring" actually defers.
func (s *store) AddToClassificationQueue(i model.Item) {
	// classificationRequest is unbuffered and only drained by the classification
	// station, which Start spawns. Anything using the store WITHOUT starting it —
	// the `kinoview media` CLI, for instance — would otherwise block here for
	// ever on the send. That was the "reclassify just hangs" bug: the CLI shares
	// this write path but has no station to consume the queue.
	if !s.started.Load() {
		ancli.Noticef("classification station not running, not queueing: %v", i.Name)
		return
	}
	if _, loaded := s.inFlight.LoadOrStore(i.ID, struct{}{}); loaded {
		ancli.Noticef("classification dedup: %v already in flight, skipping", i.Name)
		return
	}
	if s.classificationStartupCooldown > 0 {
		if time.Since(s.classificationStationStartTime) < s.classificationStartupCooldown {
			ancli.Noticef("classification cooldown active (%v remaining), deferring: %v",
				s.classificationStartupCooldown-time.Since(s.classificationStationStartTime),
				i.Name)
			s.inFlight.Delete(i.ID)
			s.markPendingRequeue(i.ID)
			return
		}
	}
	if s.rateLimiter != nil && !s.rateLimiter.allow() {
		ancli.Warnf("classification rate limit reached, dropping: %v", i.Name)
		s.inFlight.Delete(i.ID)
		s.markPendingRequeue(i.ID)
		return
	}
	s.classificationRequest <- classificationCandidate{
		correlationID: randString(10),
		item:          i,
	}
}

// totalSystemMemory returns the machine's total RAM in bytes, or 0 if it
// cannot be determined. Kept as a variable so tests can stub it.
var totalSystemMemory = func() uint64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return kb * 1024
	}
	return 0
}

// memoryHigh returns true if the Go runtime's total memory footprint exceeds
// the configured threshold fraction of the machine's total RAM. Comparing
// against total system memory (rather than runtime.MemStats.Sys, which only
// grows and never shrinks) is what actually predicts OOM pressure.
// A threshold <= 0 or >= 1 disables the check (always returns false).
func (s *store) memoryHigh() bool {
	if s.memoryThreshold <= 0 || s.memoryThreshold >= 1 {
		return false
	}
	total := s.totalMemory()
	if total == 0 {
		return false
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return float64(m.Sys) > s.memoryThreshold*float64(total)
}

func (s *store) startClassificationRoutine(ctx context.Context, workerID int, workChan <-chan classificationCandidate, resChan chan<- classificationResult) {
	s.classifierMu.RLock()
	workerClassifier := s.classifier.Clone()
	s.classifierMu.RUnlock()

	outSetter, ok := workerClassifier.(agents.OutputSetter)
	if ok {
		f, err := os.Create(path.Join(s.classificationLogsOutdir, fmt.Sprintf("w%v.txt", workerID)))
		if err != nil {
			s.classifierErrors <- fmt.Errorf("worker %v: failed to create log file: %w", workerID, err)
		} else {
			if err := outSetter.SetOutput(f); err != nil {
				s.classifierErrors <- fmt.Errorf("worker %v: failed to set output: %w", workerID, err)
			}
		}
	}

	if err := workerClassifier.Setup(ctx); err != nil {
		s.classifierErrors <- fmt.Errorf("worker %v: failed to setup classifier: %w", workerID, err)
		return
	}

	ancli.Noticef("Classification worker %v started", workerID)
	for {
		select {
		case <-ctx.Done():
			return
		case c := <-workChan:
			ancli.Noticef("[%v] - Worker %v, classifying: %v", c.correlationID, workerID, c.item.Name)
			// Bound each call so a classifier stuck on a looping model (endless
			// reasoning stream, the 2026-08-11 OOM root cause) cannot hold the
			// worker forever. The timeout ctx derives from the station ctx, so
			// shutdown still cancels promptly; a timeout surfaces as a
			// classification error and counts as an attempt.
			classifyCtx := ctx
			cancel := func() {}
			if s.classificationTimeout > 0 {
				classifyCtx, cancel = context.WithTimeout(ctx, s.classificationTimeout)
			}
			i, err := workerClassifier.Classify(classifyCtx, c.item)
			cancel()
			resChan <- classificationResult{
				correlationID: c.correlationID,
				classifierErr: err,
				item:          i,
			}
		}
	}
}

// StartClassificationStation and return an error if the startup failed, or a
// chan error if the routine successfully started. Closing of chan error indicates
// shutdown of routine
func (s *store) StartClassificationStation(ctx context.Context) error {
	s.classifierMu.RLock()
	nilClassifier := s.classifier == nil
	s.classifierMu.RUnlock()
	if nilClassifier {
		return errors.New("classifier is nil, nothing to start")
	}
	// Only initialize if not already set by Start() to avoid races.
	if s.rateLimiter == nil {
		s.classificationStationStartTime = time.Now()
		s.rateLimiter = newRateLimiter(s.classificationRate, s.classificationBurst)
	}

	// The queue is unbuffered, so enqueuing is only safe while the delegator
	// below is draining it. Flag it here rather than only in Start() so that
	// anything running the station directly — tests included — is covered.
	// StartClassificationStation returns once the workers are spawned, so the
	// flag is cleared by the delegator on its way out, not by a defer here.
	s.started.Store(true)

	if s.classificationWorkers > 3 {
		ancli.Warnf("classification workers set to %v (>3); high worker counts increase memory pressure and may cause OOM", s.classificationWorkers)
	}

	queueCap := s.classificationWorkers * 2
	resChan := make(chan classificationResult, queueCap)
	workChan := make(chan classificationCandidate, queueCap)
	for i := range s.classificationWorkers {
		s.wg.Go(func() {
			s.startClassificationRoutine(ctx, i, workChan, resChan)
		})
	}
	s.wg.Go(func() {
		// Once the delegator stops there is no consumer, so enqueuing must go
		// back to being a no-op rather than a deadlock.
		defer s.started.Store(false)
		ancli.Noticef("Starting classification delegator (queue cap: %v)", queueCap)
		amToClassify := 0
		for {
			select {
			case <-ctx.Done():
				return
			case c := <-s.classificationRequest:
				if s.memoryHigh() {
					s.inFlight.Delete(c.item.ID)
					ancli.Warnf("[%v] memory pressure high, dropping: %v", c.correlationID, c.item.Name)
					s.markPendingRequeue(c.item.ID)
					continue
				}
				select {
				case workChan <- c:
					amToClassify++
					ancli.Noticef("[%v] New classification request: %v, total: %v", c.correlationID, c.item.Name, amToClassify)
				default:
					s.inFlight.Delete(c.item.ID)
					ancli.Warnf("[%v] classification queue full, dropping: %v", c.correlationID, c.item.Name)
					s.markPendingRequeue(c.item.ID)
				}
			case r := <-resChan:
				amToClassify--
				s.inFlight.Delete(r.item.ID)
				ancli.Noticef("[%v] Work done, am in queue: %v", r.correlationID, amToClassify)
				if r.classifierErr == nil {
					r.item.ClassificationAttempts = 0
					r.item.ClassificationLastTry = time.Time{}
					r.item.ClassificationError = ""
					s.store(r.item)
				} else {
					r.item.ClassificationError = r.classifierErr.Error()
					s.store(r.item)
					s.classifierErrors <- fmt.Errorf("[%v] classification error: %v", r.correlationID, r.classifierErr)
				}
			}
		}
	})
	return nil
}

// classificationBackoff returns the backoff duration for a given number of
// failed attempts. Starts at 30s and doubles each attempt, capped at 24h.
//
//	attempts=1 → 30s
//	attempts=2 → 60s
//	attempts=3 → 2min
//	attempts=4 → 4min
//	…
func classificationBackoff(attempts int) time.Duration {
	if attempts <= 0 {
		return 0
	}
	const maxBackoff = 24 * time.Hour
	d := 30 * time.Second
	for i := 1; i < attempts; i++ {
		if d > maxBackoff/2 {
			return maxBackoff
		}
		d *= 2
	}
	if d > maxBackoff {
		d = maxBackoff
	}
	return d
}

// atMaxAttempts reports whether the item has hit the stop-loss ceiling.
func (s *store) atMaxAttempts(i model.Item) bool {
	return i.ClassificationAttempts >= s.classificationMaxAttempts
}

// inBackoff reports whether the item is still inside its retry backoff.
func (s *store) inBackoff(i model.Item) bool {
	if i.ClassificationAttempts <= 0 {
		return false
	}
	backoff := classificationBackoff(i.ClassificationAttempts)
	return time.Since(i.ClassificationLastTry) < backoff
}
