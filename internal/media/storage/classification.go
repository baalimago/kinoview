package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
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

// AddToClassificationQueue by pushing the item to the back of the queue
func (s *store) AddToClassificationQueue(i model.Item) {
	s.classificationRequest <- classificationCandidate{
		correlationID: randString(10),
		item:          i,
	}
}

func (s *store) startClassificationRoutine(ctx context.Context, workerID int, workChan <-chan classificationCandidate, resChan chan<- classificationResult) {
	ancli.Noticef("Classification worker %v started", workerID)
	for {
		select {
		case <-ctx.Done():
			return
		case c := <-workChan:
			ancli.Noticef("[%v] - Worker %v, classifying: %v", c.correlationID, workerID, c.item.Name)
			i, err := s.classifier.Classify(ctx, c.item)
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
	resChan := make(chan classificationResult, 1000)
	workChan := make(chan classificationCandidate, 1000)
	for i := range s.classificationWorkers {
		go s.startClassificationRoutine(ctx, i, workChan, resChan)
	}
	go func() {
		ancli.Noticef("Starting classification delegator")
		amToClassify := 0
		for {
			select {
			case <-ctx.Done():
				return
			case c := <-s.classificationRequest:
				amToClassify++
				ancli.Noticef("[%v] New classification request: %v, total: %v", c.correlationID, c.item.Name, amToClassify)
				workChan <- c
			case r := <-resChan:
				amToClassify--
				ancli.Noticef("[%v] Work done, am in queue: %v", r.correlationID, amToClassify)
				if r.classifierErr == nil {
					s.store(r.item)
				} else {
					s.classifierErrors <- fmt.Errorf("[%v] classification error: %v", r.correlationID, r.classifierErr)
				}
			}
		}
	}()
	return nil
}
