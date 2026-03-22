package tools

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/agents"
	"github.com/baalimago/kinoview/internal/media/subtitles"
)

type preloadSubtitlesTool struct {
	importer subtitles.EmbeddedImporter
}

func NewPreloadSubtitlesTool(ig agents.ItemGetter, sm agents.StreamManager, ss agents.SubtitleSelector) (*preloadSubtitlesTool, error) {
	importer, err := newDefaultEmbeddedImporter(ig, sm, ss)
	if err != nil {
		return nil, fmt.Errorf("create default embedded importer for preload subtitles tool: %w", err)
	}

	return NewPreloadSubtitlesToolWithImporter(importer)
}

func NewPreloadSubtitlesToolWithImporter(importer subtitles.EmbeddedImporter) (*preloadSubtitlesTool, error) {
	if importer == nil {
		return nil, errors.New("subtitle importer can't be nil")
	}

	return &preloadSubtitlesTool{importer: importer}, nil
}

func (pst *preloadSubtitlesTool) Call(input models.Input) (string, error) {
	ID, ok := input["ID"].(string)
	if !ok {
		return "", fmt.Errorf("ID must be a string")
	}

	result, err := pst.importer.Import(context.Background(), subtitles.ImportEmbeddedRequest{
		ItemID:      ID,
		MakeDefault: true,
	})
	if err != nil {
		return "", fmt.Errorf("import embedded subtitles for item %q: %w", ID, err)
	}

	return fmt.Sprintf("preloaded subtitle resource %s for item %s (already_existed=%t default_set=%t)", result.Resource.ID, result.Resource.ItemID, result.AlreadyExists, result.BecameDefault), nil
}

func (pst *preloadSubtitlesTool) Specification() models.Specification {
	return models.Specification{
		Name:        "preload_subtitles",
		Description: "Find, extract, persist and associate the best matching subtitles for a given media item.",
		Inputs: &models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterObject{
				"ID": {
					Type:        "string",
					Description: "ID of the item to preload subtitles for",
				},
			},
			Required: []string{"ID"},
		},
	}
}

func newDefaultEmbeddedImporter(ig agents.ItemGetter, sm agents.StreamManager, ss agents.SubtitleSelector) (subtitles.EmbeddedImporter, error) {
	if ig == nil {
		return nil, fmt.Errorf("validate item getter for preload subtitles tool: %w", errors.New("item getter can't be nil"))
	}
	if sm == nil {
		return nil, fmt.Errorf("validate subtitle manager for preload subtitles tool: %w", errors.New("subtitle manager can't be nil"))
	}
	if ss == nil {
		return nil, fmt.Errorf("validate subtitle selector for preload subtitles tool: %w", errors.New("subtitle selector can't be nil"))
	}

	repoRoot, err := os.MkdirTemp("", "kinoview-subtitles-repo-*")
	if err != nil {
		return nil, fmt.Errorf("create temporary subtitle repository root: %w", err)
	}
	repo, err := subtitles.NewRepository(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("create subtitle repository: %w", err)
	}

	fileStoreRoot, err := os.MkdirTemp("", "kinoview-subtitles-files-*")
	if err != nil {
		return nil, fmt.Errorf("create temporary subtitle file store root: %w", err)
	}
	fileStore, err := subtitles.NewFileStore(fileStoreRoot)
	if err != nil {
		return nil, fmt.Errorf("create subtitle file store: %w", err)
	}

	importer, err := subtitles.NewEmbeddedImporter(ig, sm, ss, repo, fileStore)
	if err != nil {
		return nil, fmt.Errorf("create subtitle embedded importer: %w", err)
	}
	return importer, nil
}
