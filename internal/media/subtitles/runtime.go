package subtitles

import (
	"fmt"
	"path/filepath"

	"github.com/baalimago/kinoview/internal/agents"
)

type Runtime struct {
	Repository Repository
	FileStore  FileStore
	Importer   EmbeddedImporter
	Resolver   Resolver
}

func NewRuntime(configRoot string, itemGetter agents.ItemGetter, streamMgr agents.StreamManager, selector agents.SubtitleSelector) (*Runtime, error) {
	if configRoot == "" {
		return nil, fmt.Errorf("create subtitle runtime: config root is empty")
	}
	if itemGetter == nil {
		return nil, fmt.Errorf("create subtitle runtime: item getter is nil")
	}
	if streamMgr == nil {
		return nil, fmt.Errorf("create subtitle runtime: stream manager is nil")
	}
	if selector == nil {
		return nil, fmt.Errorf("create subtitle runtime: subtitle selector is nil")
	}

	rootDir := filepath.Join(configRoot, "subtitles_v2")
	repo, err := NewRepository(rootDir)
	if err != nil {
		return nil, fmt.Errorf("create subtitle repository runtime dependency: %w", err)
	}

	fileStore, err := NewFileStore(rootDir)
	if err != nil {
		return nil, fmt.Errorf("create subtitle file store runtime dependency: %w", err)
	}

	importer, err := NewEmbeddedImporter(itemGetter, streamMgr, selector, repo, fileStore)
	if err != nil {
		return nil, fmt.Errorf("create embedded subtitle importer runtime dependency: %w", err)
	}

	resolver, err := NewResolver(repo, fileStore, importer, true)
	if err != nil {
		return nil, fmt.Errorf("create subtitle resolver runtime dependency: %w", err)
	}

	return &Runtime{
		Repository: repo,
		FileStore:  fileStore,
		Importer:   importer,
		Resolver:   resolver,
	}, nil
}