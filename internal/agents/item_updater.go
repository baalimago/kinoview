package agents

import "github.com/baalimago/kinoview/internal/model"

// ItemUpdater persists an updated item.
//
// Used by tools/agents that derive additional data for an item (e.g. default subtitle
// selection) and needs it to be reflected in later suggestions/playback.
//
// Implemented by the media storage store via Store(ctx, item).
// Note: Prefer to keep this interface small to avoid leaking store details.
type ItemUpdater interface {
	UpdateItem(item model.Item) error
}
