package concierge

import (
	"context"
	"testing"
	"time"

	"github.com/baalimago/kinoview/internal/model"
)

type mockItemGetter struct{}

func (m *mockItemGetter) GetItemByID(id string) (model.Item, error)     { return model.Item{}, nil }
func (m *mockItemGetter) GetItemByName(name string) (model.Item, error) { return model.Item{}, nil }

type mockItemGetterLister struct {
	mockItemGetter
}

func (m *mockItemGetterLister) Snapshot() []model.Item { return nil }

type mockMetadataManager struct{}

func (m *mockMetadataManager) UpdateMetadata(item model.Item, metadata string) error { return nil }

type mockSuggestionManager struct{}

func (m *mockSuggestionManager) List() ([]model.Suggestion, error) { return nil, nil }
func (m *mockSuggestionManager) Remove(id string) error            { return nil }
func (m *mockSuggestionManager) Add(s model.Suggestion) error      { return nil }

type mockSubtitleManager struct{}

func (m *mockSubtitleManager) Find(item model.Item) (model.MediaInfo, error) {
	return model.MediaInfo{}, nil
}

func (m *mockSubtitleManager) ExtractSubtitles(item model.Item, streamIndex string) (string, error) {
	return "", nil
}
func (m *mockSubtitleManager) Associate(item model.Item, path string) error { return nil }

type mockSubtitleSelector struct{}

func (m *mockSubtitleSelector) Select(ctx context.Context, streams []model.Stream) (int, error) {
	return 0, nil
}

type mockUserContextManager struct{}

func (m *mockUserContextManager) AllClientContexts() []model.ClientContext { return nil }
func (m *mockUserContextManager) StoreClientContext(_ model.ClientContext) error {
	return nil
}

func TestNew_Errors(t *testing.T) {
	ig := &mockItemGetter{}
	mm := &mockMetadataManager{}
	sm := &mockSuggestionManager{}
	subm := &mockSubtitleManager{}
	ss := &mockSubtitleSelector{}

	tests := []struct {
		name string
		opts []ConciergeOption
	}{
		{
			name: "missing item getter",
			opts: []ConciergeOption{
				WithMetadataManager(mm),
				WithSuggestionManager(sm),
				WithSubtitleManager(subm),
				WithSubtitleSelector(ss),
			},
		},
		{
			name: "missing metadata manager",
			opts: []ConciergeOption{
				WithItemGetter(ig),
				WithSuggestionManager(sm),
				WithSubtitleManager(subm),
				WithSubtitleSelector(ss),
			},
		},
		{
			name: "missing suggestion manager",
			opts: []ConciergeOption{
				WithItemGetter(ig),
				WithMetadataManager(mm),
				WithSubtitleManager(subm),
				WithSubtitleSelector(ss),
			},
		},
		{
			name: "missing subtitle manager",
			opts: []ConciergeOption{
				WithItemGetter(ig),
				WithMetadataManager(mm),
				WithSuggestionManager(sm),
				WithSubtitleSelector(ss),
			},
		},
		{
			name: "missing subtitle selector",
			opts: []ConciergeOption{
				WithItemGetter(ig),
				WithMetadataManager(mm),
				WithSuggestionManager(sm),
				WithSubtitleManager(subm),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.opts...)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestNew_OK_OptionsApplied(t *testing.T) {
	ig := &mockItemGetter{}
	mm := &mockMetadataManager{}
	sm := &mockSuggestionManager{}
	subm := &mockSubtitleManager{}
	ss := &mockSubtitleSelector{}
	ucm := &mockUserContextManager{}

	c, err := New(
		WithItemGetter(ig),
		WithItemLister(nil), // explicit nil should be OK
		WithMetadataManager(mm),
		WithSuggestionManager(sm),
		WithSubtitleManager(subm),
		WithSubtitleSelector(ss),
		WithModel("gpt-5"),
		WithInterval(123*time.Second),
		WithStoreDir("/tmp/store"),
		WithConfigDir("/tmp/config"),
		WithCacheDir("/tmp/cache"),
		WithUserContextManager(ucm),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("expected concierge to be non-nil")
	}
}

func TestNew_ItemListerDerivesFromItemGetter(t *testing.T) {
	igl := &mockItemGetterLister{}
	mm := &mockMetadataManager{}
	sm := &mockSuggestionManager{}
	subm := &mockSubtitleManager{}
	ss := &mockSubtitleSelector{}

	c, err := New(
		WithItemGetter(igl),
		WithMetadataManager(mm),
		WithSuggestionManager(sm),
		WithSubtitleManager(subm),
		WithSubtitleSelector(ss),
		WithModel("gpt-5"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("expected concierge to be non-nil")
	}
}

func TestConcierge_Setup_NoUserContextManager(t *testing.T) {
	ig := &mockItemGetter{}
	mm := &mockMetadataManager{}
	sm := &mockSuggestionManager{}
	subm := &mockSubtitleManager{}
	ss := &mockSubtitleSelector{}

	c, err := New(
		WithItemGetter(ig),
		WithMetadataManager(mm),
		WithSuggestionManager(sm),
		WithSubtitleManager(subm),
		WithSubtitleSelector(ss),
		WithModel("gpt-5"),
	)
	if err != nil {
		t.Fatalf("failed to create concierge: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := c.Setup(ctx); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
}

func TestConcierge_Setup_WithUserContextManager(t *testing.T) {
	ig := &mockItemGetter{}
	mm := &mockMetadataManager{}
	sm := &mockSuggestionManager{}
	subm := &mockSubtitleManager{}
	ss := &mockSubtitleSelector{}
	ucm := &mockUserContextManager{}

	c, err := New(
		WithItemGetter(ig),
		WithMetadataManager(mm),
		WithSuggestionManager(sm),
		WithSubtitleManager(subm),
		WithSubtitleSelector(ss),
		WithUserContextManager(ucm),
		WithModel("gpt-5"),
	)
	if err != nil {
		t.Fatalf("failed to create concierge: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := c.Setup(ctx); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
}
