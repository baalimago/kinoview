package subtitles

import (
	"context"
	"errors"
	"fmt"

	"github.com/baalimago/kinoview/internal/model"
)

var ErrNoResolvedSubtitle = errors.New("no resolved subtitle")

type importFallback interface {
	Import(ctx context.Context, req ImportEmbeddedRequest) (ImportEmbeddedResult, error)
}

type playbackResolver struct {
	repo            Repository
	fileStore       FileStore
	importer        importFallback
	enableFallback  bool
}

func NewResolver(repo Repository, fileStore FileStore, importer EmbeddedImporter, enableFallback bool) (Resolver, error) {
	if repo == nil {
		return nil, fmt.Errorf("create subtitle resolver: repository is nil")
	}
	if fileStore == nil {
		return nil, fmt.Errorf("create subtitle resolver: file store is nil")
	}

	var fallback importFallback
	if importer != nil {
		fallback = importer
	}

	return &playbackResolver{
		repo:           repo,
		fileStore:      fileStore,
		importer:       fallback,
		enableFallback: enableFallback,
	}, nil
}

func (r *playbackResolver) ResolveForPlayback(ctx context.Context, item model.Item, explicitSubtitleID string) (model.ResolvedSubtitle, error) {
	if explicitSubtitleID != "" {
		resource, err := r.repo.GetByID(ctx, explicitSubtitleID)
		if err != nil {
			return model.ResolvedSubtitle{}, fmt.Errorf("resolve explicit subtitle %q for item %q: %w", explicitSubtitleID, item.ID, err)
		}
		if resource.ItemID != item.ID {
			return model.ResolvedSubtitle{}, fmt.Errorf("resolve explicit subtitle %q for item %q: subtitle belongs to item %q", explicitSubtitleID, item.ID, resource.ItemID)
		}
		return r.resolvedSubtitleFromResource(resource)
	}

	if _, resource, err := r.repo.GetDefault(ctx, item.ID); err == nil {
		return r.resolvedSubtitleFromResource(resource)
	}

	if r.enableFallback && r.importer != nil {
		result, err := r.importer.Import(ctx, ImportEmbeddedRequest{ItemID: item.ID})
		if err != nil {
			return model.ResolvedSubtitle{}, fmt.Errorf("fallback import embedded subtitle for item %q: %w", item.ID, err)
		}
		return r.resolvedSubtitleFromResource(result.Resource)
	}

	return model.ResolvedSubtitle{}, fmt.Errorf("resolve subtitle for item %q: %w", item.ID, ErrNoResolvedSubtitle)
}

func (r *playbackResolver) resolvedSubtitleFromResource(resource model.SubtitleResource) (model.ResolvedSubtitle, error) {
	path, err := r.fileStore.ResolvePath(resource.StorageKey)
	if err != nil {
		return model.ResolvedSubtitle{}, fmt.Errorf("resolve subtitle file path for resource %q: %w", resource.ID, err)
	}

	return model.ResolvedSubtitle{
		SubtitleID: resource.ID,
		ItemID:     resource.ItemID,
		Path:       path,
		Format:     resource.Format,
		Language:   resource.Language,
		Source:     resource.Source,
		Origin:     resource.Origin,
	}, nil
}