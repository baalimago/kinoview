package storage

import (
	"context"

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
