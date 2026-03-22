package subtitles

import (
	"context"

	"github.com/baalimago/kinoview/internal/model"
)

type Repository interface {
	Save(ctx context.Context, resource model.SubtitleResource) (model.SubtitleResource, error)
	GetByID(ctx context.Context, subtitleID string) (model.SubtitleResource, error)
	ListByItemID(ctx context.Context, itemID string) ([]model.SubtitleResource, error)
	GetBySourceRef(ctx context.Context, itemID, sourceRef string) (model.SubtitleResource, error)
	GetByChecksum(ctx context.Context, itemID, checksum string) ([]model.SubtitleResource, error)
	SetDefault(ctx context.Context, binding model.SubtitleBinding) (model.SubtitleBinding, error)
	GetDefault(ctx context.Context, itemID string) (model.SubtitleBinding, model.SubtitleResource, error)
	DeleteByItemID(ctx context.Context, itemID string) error
}

type FileStore interface {
	WriteCanonical(ctx context.Context, storageKey string, data []byte) error
	WriteOriginal(ctx context.Context, storageKey string, data []byte) error
	ResolvePath(storageKey string) (string, error)
	DeleteItem(ctx context.Context, itemID string) error
}

type Service interface{}

type Resolver interface {
	ResolveForPlayback(ctx context.Context, item model.Item, explicitSubtitleID string) (model.ResolvedSubtitle, error)
}

type Provider interface{}

type Converter interface{}