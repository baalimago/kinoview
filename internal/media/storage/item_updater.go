package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/baalimago/kinoview/internal/model"
)

// UpdateItem persists an updated item.
//
// This is a small shim so storage.store can satisfy agents.ItemUpdater without
// exporting internal store details.
func (s *store) UpdateItem(item model.Item) error {
	// Store will merge/persist and update cache.
	return s.Store(context.Background(), item)
}

// ResetClassification clears an item's classification so the running server
// picks it up again, and reports whether anything changed.
//
// This does NOT go through Store: Store deliberately copies the existing
// metadata back over the incoming item (so a filesystem re-scan cannot wipe
// classifications), which means clearing metadata through it is impossible. That
// is why the CLI's reclassify appeared to do nothing for items that were already
// classified — the reset was silently undone on the way to disk.
func (s *store) ResetClassification(id string) (bool, error) {
	s.cacheMu.RLock()
	item, ok := s.cache[id]
	s.cacheMu.RUnlock()
	if !ok {
		return false, fmt.Errorf("no item with ID %q", id)
	}

	item.Metadata = nil
	item.ClassificationAttempts = 0
	item.ClassificationLastTry = time.Time{}
	item.ClassificationError = ""

	if err := s.store(item); err != nil {
		return false, fmt.Errorf("persist reset for %q: %w", item.Name, err)
	}
	return true, nil
}

// ClearClassificationStopLoss re-opens classification for an item that hit the
// max-attempts ceiling, WITHOUT discarding whatever metadata it managed to get.
//
// Reporting false when nothing was blocked lets callers say how many items they
// actually freed rather than claiming to have fixed everything they looked at.
func (s *store) ClearClassificationStopLoss(id string) (bool, error) {
	s.cacheMu.RLock()
	item, ok := s.cache[id]
	s.cacheMu.RUnlock()
	if !ok {
		return false, fmt.Errorf("no item with ID %q", id)
	}
	if item.ClassificationAttempts < s.classificationMaxAttempts {
		return false, nil
	}

	item.ClassificationAttempts = 0
	item.ClassificationLastTry = time.Time{}
	item.ClassificationError = ""

	if err := s.store(item); err != nil {
		return false, fmt.Errorf("persist stop-loss clear for %q: %w", item.Name, err)
	}
	return true, nil
}

// ClassificationMaxAttempts is the ceiling after which an item is permanently
// skipped. Exposed so callers can report the threshold they are acting on.
func (s *store) ClassificationMaxAttempts() int {
	return s.classificationMaxAttempts
}
