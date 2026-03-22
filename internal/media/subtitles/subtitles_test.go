package subtitles

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/kinoview/internal/model"
)

func TestNewSubtitleID(t *testing.T) {
	t.Parallel()

	got, err := NewSubtitleID(time.Date(2026, 3, 22, 10, 11, 12, 0, time.UTC))
	if err != nil {
		t.Fatalf("NewSubtitleID failed: %v", err)
	}
	if !strings.HasPrefix(got, "sub_20260322T101112.") {
		t.Fatalf("expected formatted prefix, got %q", got)
	}
}

func TestStorageKeys(t *testing.T) {
	t.Parallel()

	canonical, err := CanonicalStorageKey("item-1", "sub-1")
	if err != nil {
		t.Fatalf("CanonicalStorageKey failed: %v", err)
	}
	if canonical != "item-1/sub-1.vtt" {
		t.Fatalf("unexpected canonical key: %q", canonical)
	}

	original, err := OriginalStorageKey("item-1", "sub-1", ".srt")
	if err != nil {
		t.Fatalf("OriginalStorageKey failed: %v", err)
	}
	if original != "item-1/sub-1.orig.srt" {
		t.Fatalf("unexpected original key: %q", original)
	}
}

func TestResolveStoragePathRejectsTraversal(t *testing.T) {
	t.Parallel()

	if _, err := resolveStoragePath("/tmp/root", "../evil"); err == nil {
		t.Fatal("expected traversal rejection error, got nil")
	}
}

func TestWriteFileAtomic(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "nested", "file.txt")
	if err := writeFileAtomic(target, []byte("hello"), 0o644); err != nil {
		t.Fatalf("writeFileAtomic failed: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("unexpected file contents: %q", string(got))
	}
}

func TestRepositoryLifecycle(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repo, err := NewRepository(root)
	if err != nil {
		t.Fatalf("NewRepository failed: %v", err)
	}

	ctx := context.Background()
	resource := model.SubtitleResource{
		ID:             "sub-1",
		ItemID:         "item-1",
		Source:         model.SubtitleSourceEmbedded,
		Origin:         model.SubtitleOriginEmbedded,
		Format:         model.SubtitleFormatVTT,
		StorageKey:     "item-1/sub-1.vtt",
		ChecksumSHA256: "abc",
		SourceRef:      "embedded:stream:1",
		CreatedAt:      time.Unix(1, 0).UTC(),
		UpdatedAt:      time.Unix(2, 0).UTC(),
	}

	if _, err := repo.Save(ctx, resource); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	gotByID, err := repo.GetByID(ctx, resource.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if gotByID.ID != resource.ID {
		t.Fatalf("unexpected resource id: %q", gotByID.ID)
	}

	gotList, err := repo.ListByItemID(ctx, resource.ItemID)
	if err != nil {
		t.Fatalf("ListByItemID failed: %v", err)
	}
	if len(gotList) != 1 {
		t.Fatalf("expected 1 listed resource, got %d", len(gotList))
	}

	if _, err := repo.SetDefault(ctx, model.SubtitleBinding{
		ItemID:            resource.ItemID,
		DefaultSubtitleID: resource.ID,
		UpdatedAt:         time.Unix(3, 0).UTC(),
	}); err != nil {
		t.Fatalf("SetDefault failed: %v", err)
	}

	binding, boundResource, err := repo.GetDefault(ctx, resource.ItemID)
	if err != nil {
		t.Fatalf("GetDefault failed: %v", err)
	}
	if binding.DefaultSubtitleID != resource.ID || boundResource.ID != resource.ID {
		t.Fatalf("unexpected default binding/resource: %+v %+v", binding, boundResource)
	}

	gotBySourceRef, err := repo.GetBySourceRef(ctx, resource.ItemID, resource.SourceRef)
	if err != nil {
		t.Fatalf("GetBySourceRef failed: %v", err)
	}
	if gotBySourceRef.ID != resource.ID {
		t.Fatalf("unexpected source ref resource: %q", gotBySourceRef.ID)
	}

	gotByChecksum, err := repo.GetByChecksum(ctx, resource.ItemID, resource.ChecksumSHA256)
	if err != nil {
		t.Fatalf("GetByChecksum failed: %v", err)
	}
	if len(gotByChecksum) != 1 || gotByChecksum[0].ID != resource.ID {
		t.Fatalf("unexpected checksum results: %+v", gotByChecksum)
	}

	if err := repo.DeleteByItemID(ctx, resource.ItemID); err != nil {
		t.Fatalf("DeleteByItemID failed: %v", err)
	}

	gotList, err = repo.ListByItemID(ctx, resource.ItemID)
	if err != nil {
		t.Fatalf("ListByItemID after delete failed: %v", err)
	}
	if len(gotList) != 0 {
		t.Fatalf("expected 0 listed resources after delete, got %d", len(gotList))
	}
}

func TestRepositoryRejectsBindingForDifferentItem(t *testing.T) {
	t.Parallel()

	repo, err := NewRepository(t.TempDir())
	if err != nil {
		t.Fatalf("NewRepository failed: %v", err)
	}

	ctx := context.Background()
	_, err = repo.Save(ctx, model.SubtitleResource{
		ID:         "sub-1",
		ItemID:     "item-1",
		Format:     model.SubtitleFormatVTT,
		StorageKey: "item-1/sub-1.vtt",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	_, err = repo.SetDefault(ctx, model.SubtitleBinding{
		ItemID:            "item-2",
		DefaultSubtitleID: "sub-1",
		UpdatedAt:         time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("expected cross-item binding error, got nil")
	}
}

func TestRepositoryRebuildToleratesMalformedFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "resources"), 0o755); err != nil {
		t.Fatalf("MkdirAll resources failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "bindings"), 0o755); err != nil {
		t.Fatalf("MkdirAll bindings failed: %v", err)
	}

	good := model.SubtitleResource{
		ID:         "sub-1",
		ItemID:     "item-1",
		Format:     model.SubtitleFormatVTT,
		StorageKey: "item-1/sub-1.vtt",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	goodBytes, err := json.Marshal(good)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "resources", "good.json"), goodBytes, 0o644); err != nil {
		t.Fatalf("WriteFile good failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "resources", "bad.json"), []byte("{"), 0o644); err != nil {
		t.Fatalf("WriteFile bad failed: %v", err)
	}

	repo, err := NewRepository(root)
	if err != nil {
		t.Fatalf("NewRepository failed: %v", err)
	}

	listed, err := repo.ListByItemID(context.Background(), "item-1")
	if err != nil {
		t.Fatalf("ListByItemID failed: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected 1 listed resource after rebuild, got %d", len(listed))
	}
}

func TestFileStoreLifecycle(t *testing.T) {
	t.Parallel()

	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	if err := store.WriteCanonical(ctx, "item-1/sub-1.vtt", []byte("WEBVTT")); err != nil {
		t.Fatalf("WriteCanonical failed: %v", err)
	}
	if err := store.WriteOriginal(ctx, "item-1/sub-1.orig.srt", []byte("1\n00:00:00,000 --> 00:00:01,000")); err != nil {
		t.Fatalf("WriteOriginal failed: %v", err)
	}

	path, err := store.ResolvePath("item-1/sub-1.vtt")
	if err != nil {
		t.Fatalf("ResolvePath failed: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Stat resolved path failed: %v", err)
	}

	if _, err := store.ResolvePath("../evil"); err == nil {
		t.Fatal("expected path traversal rejection, got nil")
	}

	if err := store.DeleteItem(ctx, "item-1"); err != nil {
		t.Fatalf("DeleteItem failed: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected item dir removal, got err=%v", err)
	}
}

func TestResolverPrecedence(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repo, err := NewRepository(filepath.Join(root, "repo"))
	if err != nil {
		t.Fatalf("NewRepository failed: %v", err)
	}
	store, err := NewFileStore(filepath.Join(root, "files"))
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	explicit := model.SubtitleResource{
		ID:         "sub-explicit",
		ItemID:     "item-1",
		Source:     model.SubtitleSourceEmbedded,
		Origin:     model.SubtitleOriginEmbedded,
		Format:     model.SubtitleFormatVTT,
		StorageKey: "item-1/sub-explicit.vtt",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	defaultRes := model.SubtitleResource{
		ID:         "sub-default",
		ItemID:     "item-1",
		Source:     model.SubtitleSourceEmbedded,
		Origin:     model.SubtitleOriginEmbedded,
		Format:     model.SubtitleFormatVTT,
		StorageKey: "item-1/sub-default.vtt",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	for _, resource := range []model.SubtitleResource{explicit, defaultRes} {
		if err := store.WriteCanonical(ctx, resource.StorageKey, []byte("WEBVTT")); err != nil {
			t.Fatalf("WriteCanonical failed: %v", err)
		}
		if _, err := repo.Save(ctx, resource); err != nil {
			t.Fatalf("Save failed: %v", err)
		}
	}
	if _, err := repo.SetDefault(ctx, model.SubtitleBinding{
		ItemID:            "item-1",
		DefaultSubtitleID: defaultRes.ID,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SetDefault failed: %v", err)
	}

	resolver, err := NewResolver(repo, store, nil, false)
	if err != nil {
		t.Fatalf("NewResolver failed: %v", err)
	}

	resolved, err := resolver.ResolveForPlayback(ctx, model.Item{ID: "item-1"}, explicit.ID)
	if err != nil {
		t.Fatalf("ResolveForPlayback explicit failed: %v", err)
	}
	if resolved.SubtitleID != explicit.ID {
		t.Fatalf("expected explicit subtitle, got %q", resolved.SubtitleID)
	}

	resolved, err = resolver.ResolveForPlayback(ctx, model.Item{ID: "item-1"}, "")
	if err != nil {
		t.Fatalf("ResolveForPlayback default failed: %v", err)
	}
	if resolved.SubtitleID != defaultRes.ID {
		t.Fatalf("expected default subtitle, got %q", resolved.SubtitleID)
	}
}

func TestResolverFallbackAndNoSubtitle(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repo, err := NewRepository(filepath.Join(root, "repo"))
	if err != nil {
		t.Fatalf("NewRepository failed: %v", err)
	}
	store, err := NewFileStore(filepath.Join(root, "files"))
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	fallback := &mockImporter{
		result: ImportEmbeddedResult{
			Resource: model.SubtitleResource{
				ID:         "sub-fallback",
				ItemID:     "item-1",
				Source:     model.SubtitleSourceEmbedded,
				Origin:     model.SubtitleOriginEmbedded,
				Format:     model.SubtitleFormatVTT,
				StorageKey: "item-1/sub-fallback.vtt",
			},
		},
	}
	if err := store.WriteCanonical(context.Background(), "item-1/sub-fallback.vtt", []byte("WEBVTT")); err != nil {
		t.Fatalf("WriteCanonical failed: %v", err)
	}

	resolver, err := NewResolver(repo, store, fallback, true)
	if err != nil {
		t.Fatalf("NewResolver failed: %v", err)
	}

	resolved, err := resolver.ResolveForPlayback(context.Background(), model.Item{ID: "item-1"}, "")
	if err != nil {
		t.Fatalf("ResolveForPlayback fallback failed: %v", err)
	}
	if resolved.SubtitleID != "sub-fallback" {
		t.Fatalf("expected fallback subtitle, got %q", resolved.SubtitleID)
	}

	noFallbackResolver, err := NewResolver(repo, store, nil, false)
	if err != nil {
		t.Fatalf("NewResolver without fallback failed: %v", err)
	}
	_, err = noFallbackResolver.ResolveForPlayback(context.Background(), model.Item{ID: "item-2"}, "")
	if !errors.Is(err, ErrNoResolvedSubtitle) {
		t.Fatalf("expected ErrNoResolvedSubtitle, got %v", err)
	}
}

type mockImporter struct {
	result ImportEmbeddedResult
	err    error
}

func (m *mockImporter) Import(ctx context.Context, req ImportEmbeddedRequest) (ImportEmbeddedResult, error) {
	return m.result, m.err
}

type importerTestItemGetter struct {
	item model.Item
}

func (g *importerTestItemGetter) GetItemByID(ID string) (model.Item, error) {
	if g.item.ID != ID {
		return model.Item{}, errors.New("item not found")
	}
	return g.item, nil
}

func (g *importerTestItemGetter) GetItemByName(name string) (model.Item, error) {
	if g.item.Name != name {
		return model.Item{}, errors.New("item not found")
	}
	return g.item, nil
}

type importerTestStreamManager struct {
	info        model.MediaInfo
	extractedTo string
}

func (m *importerTestStreamManager) Find(item model.Item) (model.MediaInfo, error) {
	return m.info, nil
}

func (m *importerTestStreamManager) ExtractSubtitles(item model.Item, streamIndex string) (string, error) {
	return m.extractedTo, nil
}

type importerTestSelector struct {
	selected int
}

func (s *importerTestSelector) Select(ctx context.Context, streams []model.Stream) (int, error) {
	return s.selected, nil
}

func TestEmbeddedImporter_UsesHumanReadableLabelFromStreamMetadata(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repo, err := NewRepository(filepath.Join(root, "repo"))
	if err != nil {
		t.Fatalf("NewRepository failed: %v", err)
	}
	fileStore, err := NewFileStore(filepath.Join(root, "files"))
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	extractedPath := filepath.Join(root, "imported.vtt")
	if err := os.WriteFile(extractedPath, []byte("WEBVTT"), 0o644); err != nil {
		t.Fatalf("WriteFile extracted subtitle failed: %v", err)
	}

	importer, err := NewEmbeddedImporter(
		&importerTestItemGetter{item: model.Item{ID: "item-1", Name: "Video"}},
		&importerTestStreamManager{
			info: model.MediaInfo{
				Streams: []model.Stream{
					{
						Index: 3,
						Tags: model.Tags{
							Language: "eng",
							Title:    "English SDH",
						},
						Disposition: model.Disposition{
							Default:         1,
							HearingImpaired: 1,
						},
					},
				},
			},
			extractedTo: extractedPath,
		},
		&importerTestSelector{selected: 3},
		repo,
		fileStore,
	)
	if err != nil {
		t.Fatalf("NewEmbeddedImporter failed: %v", err)
	}

	result, err := importer.Import(context.Background(), ImportEmbeddedRequest{
		ItemID:      "item-1",
		MakeDefault: true,
	})
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	if result.Resource.Label != "English (SDH) — stream 3" {
		t.Fatalf("expected human-readable label, got %q", result.Resource.Label)
	}
}