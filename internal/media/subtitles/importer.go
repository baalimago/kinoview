package subtitles

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/baalimago/kinoview/internal/agents"
	"github.com/baalimago/kinoview/internal/model"
)

type ImportEmbeddedRequest struct {
	ItemID         string
	MakeDefault    bool
}

type ImportEmbeddedResult struct {
	Resource      model.SubtitleResource
	AlreadyExists bool
	BecameDefault bool
}

type EmbeddedImporter interface {
	Import(ctx context.Context, req ImportEmbeddedRequest) (ImportEmbeddedResult, error)
}

type embeddedImporter struct {
	itemGetter agents.ItemGetter
	streamMgr  agents.StreamManager
	selector   agents.SubtitleSelector
	repo       Repository
	fileStore  FileStore
	now        func() time.Time
}

func NewEmbeddedImporter(itemGetter agents.ItemGetter, streamMgr agents.StreamManager, selector agents.SubtitleSelector, repo Repository, fileStore FileStore) (EmbeddedImporter, error) {
	if itemGetter == nil {
		return nil, fmt.Errorf("create embedded importer: item getter is nil")
	}
	if streamMgr == nil {
		return nil, fmt.Errorf("create embedded importer: stream manager is nil")
	}
	if selector == nil {
		return nil, fmt.Errorf("create embedded importer: subtitle selector is nil")
	}
	if repo == nil {
		return nil, fmt.Errorf("create embedded importer: repository is nil")
	}
	if fileStore == nil {
		return nil, fmt.Errorf("create embedded importer: file store is nil")
	}

	return &embeddedImporter{
		itemGetter: itemGetter,
		streamMgr:  streamMgr,
		selector:   selector,
		repo:       repo,
		fileStore:  fileStore,
		now:        func() time.Time { return time.Now().UTC() },
	}, nil
}

func (i *embeddedImporter) Import(ctx context.Context, req ImportEmbeddedRequest) (ImportEmbeddedResult, error) {
	item, err := i.itemGetter.GetItemByID(req.ItemID)
	if err != nil {
		return ImportEmbeddedResult{}, fmt.Errorf("get item %q for embedded subtitle import: %w", req.ItemID, err)
	}

	mediaInfo, err := i.streamMgr.Find(item)
	if err != nil {
		return ImportEmbeddedResult{}, fmt.Errorf("find streams for item %q: %w", item.ID, err)
	}

	streamIdx, err := i.selector.Select(ctx, mediaInfo.Streams)
	if err != nil {
		return ImportEmbeddedResult{}, fmt.Errorf("select subtitle stream for item %q: %w", item.ID, err)
	}

	sourceRef := "embedded:stream:" + strconv.Itoa(streamIdx)
	resource, err := i.repo.GetBySourceRef(ctx, item.ID, sourceRef)
	if err == nil {
		becameDefault, err := i.maybeSetDefault(ctx, item.ID, resource.ID, req.MakeDefault)
		if err != nil {
			return ImportEmbeddedResult{}, fmt.Errorf("set default for existing subtitle %q on item %q: %w", resource.ID, item.ID, err)
		}
		return ImportEmbeddedResult{Resource: resource, AlreadyExists: true, BecameDefault: becameDefault}, nil
	}

	extractedPath, err := i.streamMgr.ExtractSubtitles(item, strconv.Itoa(streamIdx))
	if err != nil {
		return ImportEmbeddedResult{}, fmt.Errorf("extract subtitles for item %q stream %d: %w", item.ID, streamIdx, err)
	}

	subtitleBytes, err := os.ReadFile(extractedPath)
	if err != nil {
		return ImportEmbeddedResult{}, fmt.Errorf("read extracted subtitle file %q for item %q: %w", extractedPath, item.ID, err)
	}

	subtitleID, err := NewSubtitleID(i.now())
	if err != nil {
		return ImportEmbeddedResult{}, fmt.Errorf("generate subtitle id for item %q: %w", item.ID, err)
	}
	canonicalKey, err := CanonicalStorageKey(item.ID, subtitleID)
	if err != nil {
		return ImportEmbeddedResult{}, fmt.Errorf("build canonical storage key for subtitle %q item %q: %w", subtitleID, item.ID, err)
	}
	if err := i.fileStore.WriteCanonical(ctx, canonicalKey, subtitleBytes); err != nil {
		return ImportEmbeddedResult{}, fmt.Errorf("write canonical subtitle for item %q subtitle %q: %w", item.ID, subtitleID, err)
	}

	now := i.now()
	resource = model.SubtitleResource{
		ID:         subtitleID,
		ItemID:     item.ID,
		Source:     model.SubtitleSourceEmbedded,
		Origin:     model.SubtitleOriginEmbedded,
		Format:     model.SubtitleFormatVTT,
		Label:      fmt.Sprintf("Embedded subtitle stream %d", streamIdx),
		StorageKey: canonicalKey,
		SizeBytes:  int64(len(subtitleBytes)),
		SourceRef:  sourceRef,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(extractedPath)), ".")
	if ext != "" && ext != "vtt" {
		originalKey, err := OriginalStorageKey(item.ID, subtitleID, ext)
		if err != nil {
			return ImportEmbeddedResult{}, fmt.Errorf("build original storage key for subtitle %q item %q: %w", subtitleID, item.ID, err)
		}
		resource.OriginalStorageKey = originalKey
	}

	resource, err = i.repo.Save(ctx, resource)
	if err != nil {
		return ImportEmbeddedResult{}, fmt.Errorf("save subtitle resource %q for item %q: %w", subtitleID, item.ID, err)
	}

	becameDefault, err := i.maybeSetDefault(ctx, item.ID, resource.ID, req.MakeDefault)
	if err != nil {
		return ImportEmbeddedResult{}, fmt.Errorf("set default for subtitle %q on item %q: %w", resource.ID, item.ID, err)
	}

	return ImportEmbeddedResult{Resource: resource, BecameDefault: becameDefault}, nil
}

func (i *embeddedImporter) maybeSetDefault(ctx context.Context, itemID, subtitleID string, makeDefault bool) (bool, error) {
	if !makeDefault {
		return false, nil
	}

	if _, _, err := i.repo.GetDefault(ctx, itemID); err == nil {
		binding, _, getErr := i.repo.GetDefault(ctx, itemID)
		if getErr != nil {
			return false, fmt.Errorf("re-read default binding for item %q: %w", itemID, getErr)
		}
		if binding.DefaultSubtitleID == subtitleID {
			return false, nil
		}
	}

	_, err := i.repo.SetDefault(ctx, model.SubtitleBinding{
		ItemID:            itemID,
		DefaultSubtitleID: subtitleID,
		UpdatedAt:         i.now(),
	})
	if err != nil {
		return false, fmt.Errorf("persist default subtitle binding for item %q: %w", itemID, err)
	}
	return true, nil
}