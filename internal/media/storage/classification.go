package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"runtime"
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

// AddToClassificationQueue by pushing the item to the back of the queue.
// Items may be dropped if: already in flight, startup cooldown is active,
// rate limit is exceeded, or the downstream work queue is at capacity.
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
			return
		}
	}
	if s.rateLimiter != nil && !s.rateLimiter.allow() {
		ancli.Warnf("classification rate limit reached, dropping: %v", i.Name)
		s.inFlight.Delete(i.ID)
		return
	}
	s.classificationRequest <- classificationCandidate{
		correlationID: randString(10),
		item:          i,
	}
}

// memoryHigh returns true if the Go runtime heap allocation exceeds
// the configured threshold fraction of total OS memory obtained.
// A threshold <= 0 or >= 1 disables the check (always returns false).
func (s *store) memoryHigh() bool {
	if s.memoryThreshold <= 0 || s.memoryThreshold >= 1 {
		return false
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return float64(m.Alloc) > s.memoryThreshold*float64(m.Sys)
}

func (s *store) startClassificationRoutine(ctx context.Context, workerID int, workChan <-chan classificationCandidate, resChan chan<- classificationResult) {
	workerClassifier := s.classifier.Clone()

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
			i, err := workerClassifier.Classify(ctx, c.item)
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
	if s.classifier == nil {
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
		go s.startClassificationRoutine(ctx, i, workChan, resChan)
	}
	go func() {
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
					continue
				}
				select {
				case workChan <- c:
					amToClassify++
					ancli.Noticef("[%v] New classification request: %v, total: %v", c.correlationID, c.item.Name, amToClassify)
				default:
					s.inFlight.Delete(c.item.ID)
					ancli.Warnf("[%v] classification queue full, dropping: %v", c.correlationID, c.item.Name)
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
	}()
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
