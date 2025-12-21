package tools

import (
	"context"
	"errors"

	"github.com/baalimago/kinoview/internal/model"
)

type mockMetadataManager struct {
	updatedItem     model.Item
	updatedMetadata string
	err             error
}

func (m *mockMetadataManager) UpdateMetadata(item model.Item, metadata string) error {
	m.updatedItem = item
	m.updatedMetadata = metadata
	return m.err
}

type mockItemGetter struct {
	item model.Item
	err  error
}

func (m *mockItemGetter) GetItemByID(id string) (model.Item, error) {
	if m.item.ID == id {
		return m.item, nil
	}
	return model.Item{}, errors.New("not found")
}

func (m *mockItemGetter) GetItemByName(name string) (model.Item, error) {
	return m.item, m.err
}

type mockItemLister struct {
	items []model.Item
}

func (m *mockItemLister) Snapshot() []model.Item {
	return m.items
}

type mockSubtitleManager struct {
	mediaInfo     model.MediaInfo
	extractedPath string
	err           error
	associated    bool
}

func (m *mockSubtitleManager) Find(item model.Item) (model.MediaInfo, error) {
	return m.mediaInfo, m.err
}

func (m *mockSubtitleManager) Extract(item model.Item, streamIndex string) (string, error) {
	return m.extractedPath, m.err
}

func (m *mockSubtitleManager) Associate(item model.Item, subtitlePath string) error {
	m.associated = true
	return m.err
}

type mockSubtitleSelector struct {
	selectedIdx int
	err         error
}

func (m *mockSubtitleSelector) Select(ctx context.Context, streams []model.Stream) (int, error) {
	return m.selectedIdx, m.err
}

type mockSuggestionManager struct {
	suggestions []model.Suggestion
	removedID   string
	added       model.Suggestion
	err         error
}

func (m *mockSuggestionManager) List() ([]model.Suggestion, error) {
	return m.suggestions, m.err
}

func (m *mockSuggestionManager) Remove(id string) error {
	m.removedID = id
	return m.err
}

func (m *mockSuggestionManager) Add(s model.Suggestion) error {
	m.added = s
	return m.err
}
