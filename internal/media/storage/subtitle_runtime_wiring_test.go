package storage

import (
	"context"
	"strings"
	"testing"

	"github.com/baalimago/kinoview/internal/media/subtitles"
	"github.com/baalimago/kinoview/internal/model"
)

type testSubtitleSelector struct{}

func (s *testSubtitleSelector) Select(ctx context.Context, streams []model.Stream) (int, error) {
	return 0, nil
}

type testSubtitleStreamManager struct{}

func (m *testSubtitleStreamManager) Find(item model.Item) (model.MediaInfo, error) {
	return model.MediaInfo{
		Streams: []model.Stream{{Index: 7}},
	}, nil
}

func (m *testSubtitleStreamManager) ExtractSubtitles(item model.Item, streamIndex string) (string, error) {
	return "", assertErr("forced extraction failure")
}

func TestStore_SetSubtitleRuntime_WiresImporterToSameStoreInstance(t *testing.T) {
	ctx := context.Background()
	configDir := t.TempDir()
	storeDir := t.TempDir()

	store := NewStore(
		WithStorePath(storeDir),
	)

	if err := store.Store(ctx, model.Item{
		ID:       "item-1",
		Name:     "video",
		Path:     t.TempDir(),
		MIMEType: "application/octet-stream",
	}); err != nil {
		t.Fatalf("store item: %v", err)
	}

	runtime, err := subtitles.NewRuntime(
		configDir,
		store,
		&testSubtitleStreamManager{},
		&testSubtitleSelector{},
	)
	if err != nil {
		t.Fatalf("create subtitle runtime: %v", err)
	}

	if store.subtitleImporter != nil {
		t.Fatal("expected subtitle importer to be nil before runtime injection")
	}

	store.SetSubtitleRuntime(runtime)

	if store.subtitleImporter == nil {
		t.Fatal("expected subtitle importer to be wired after runtime injection")
	}

	_, err = store.subtitleImporter.Import(ctx, subtitles.ImportEmbeddedRequest{
		ItemID: "item-1",
	})
	if err == nil {
		t.Fatal("expected import to fail later due to subtitle extraction, got nil")
	}
	if got, want := err.Error(), "extract subtitles for item"; got == "" || !strings.Contains(got, want) {
		t.Fatalf("expected error to contain %q, got %q", want, got)
	}
	if strings.Contains(err.Error(), "get item") {
		t.Fatalf("expected importer to resolve item from same store instance, got err: %v", err)
	}
}

type assertErr string

func (e assertErr) Error() string {
	return string(e)
}