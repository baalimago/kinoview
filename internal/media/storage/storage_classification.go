package storage

import (
	"context"

	"github.com/baalimago/kinoview/internal/model"
)

// addToClassificationQueue by pushing the item to the back of the queue
func (s *store) addToClassificationQueue(i model.Item) {
	s.classificationMu.Lock()
	defer s.classificationMu.Unlock()
	s.classificationQueue = append(s.classificationQueue, i)
}

// startClassificationStation and return an error if the startup failed, or a
// chan error if the routine successfully started. Closing of chan error indicates
// shutdown of routine
func (s *store) startClassificationStation(ctx context.Context) (error, chan error) {
	return nil, nil
}
