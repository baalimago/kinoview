package butler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/model"
)

// MockFullResponse mocks the text.FullResponse interface
type MockFullResponse struct {
	SetupFunc  func(ctx context.Context) error
	QueryFunc  func(ctx context.Context, chat models.Chat) (models.Chat, error)
	queryCount atomic.Int32
}

func (m *MockFullResponse) Setup(ctx context.Context) error {
	if m.SetupFunc != nil {
		return m.SetupFunc(ctx)
	}
	return nil
}

func (m *MockFullResponse) Query(ctx context.Context, chat models.Chat) (models.Chat, error) {
	m.queryCount.Add(1)
	if m.QueryFunc != nil {
		return m.QueryFunc(ctx, chat)
	}
	return models.Chat{}, nil
}

func (m *MockFullResponse) QueryCount() int32 {
	return m.queryCount.Load()
}

// MockSubtitler mocks the Subtitler interface
type MockSubtitler struct {
	FindFunc    func(item model.Item) (model.MediaInfo, error)
	ExtractFunc func(item model.Item, streamIndex string) (string, error)
}

func (m *MockSubtitler) Find(item model.Item) (model.MediaInfo, error) {
	if m.FindFunc != nil {
		return m.FindFunc(item)
	}
	return model.MediaInfo{}, nil
}

func (m *MockSubtitler) ExtractSubtitles(item model.Item, streamIndex string) (string, error) {
	if m.ExtractFunc != nil {
		return m.ExtractFunc(item, streamIndex)
	}
	return "", nil
}

func (m *MockSubtitler) Associate(item model.Item, path string) error {
	return nil
}

// MockSubtitleSelector mocks the SubtitleSelector interface
type MockSubtitleSelector struct {
	SelectEnglishFunc func(ctx context.Context, streams []model.Stream) (int, error)
}

func (m *MockSubtitleSelector) Select(ctx context.Context, streams []model.Stream) (int, error) {
	if m.SelectEnglishFunc != nil {
		return m.SelectEnglishFunc(ctx, streams)
	}
	return 0, nil
}

func TestNewButler(t *testing.T) {
	c := models.Configurations{}
	subs := &MockSubtitler{}
	b := New(c, subs)
	if b == nil {
		t.Fatal("NewButler returned nil")
	}
}

func TestButler_Setup(t *testing.T) {
	mockLLM := &MockFullResponse{
		SetupFunc: func(ctx context.Context) error {
			return nil
		},
	}
	b := &butler{
		llm: mockLLM,
	}
	err := b.Setup(context.Background())
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	mockLLM.SetupFunc = func(ctx context.Context) error {
		return errors.New("setup error")
	}
	err = b.Setup(context.Background())
	if err == nil {
		t.Fatal("Expected error from Setup, got nil")
	}
}

func TestButler_PrepSuggestions(t *testing.T) {
	ctx := context.Background()
	clientCtx := model.ClientContext{
		ViewingHistory: []model.ViewMetadata{
			{Name: "Item 1", ViewedAt: time.Now(), PlayedForSec: "300"},
		},
	}
	items := []model.Item{
		{Name: "Movie A", MIMEType: "video/mp4"},
		{Name: "Movie B", MIMEType: "video/mp4"},
	}

	mockSubs := &MockSubtitler{
		FindFunc: func(item model.Item) (model.MediaInfo, error) {
			return model.MediaInfo{
				Streams: []model.Stream{{Index: 1, CodecType: "subtitle"}},
			}, nil
		},
		ExtractFunc: func(item model.Item, streamIndex string) (string, error) {
			return "/tmp/subs.srt", nil
		},
	}

	mockSelector := &MockSubtitleSelector{
		SelectEnglishFunc: func(ctx context.Context, streams []model.Stream) (int, error) {
			return 1, nil
		},
	}

	// This mock LLM handles both the main suggestion query and the semantic indexer queries
	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			systemMsg := chat.Messages[0].Content
			// Check if it is the main picker prompt
			if strings.Contains(systemMsg, "You are a media Butler") {
				resp := `[
					{
						"description": "Movie A",
						"motivation": "It is great"
					}
				]`
				return models.Chat{
					Messages: []models.Message{
						{Role: "assistant", Content: resp},
					},
				}, nil
			} else if strings.Contains(systemMsg, "Your job is to pick a media item from a list") {
				// Semantic indexer prompt
				resp := `{"index": 0}`
				return models.Chat{
					Messages: []models.Message{
						{Role: "assistant", Content: resp},
					},
				}, nil
			}
			return models.Chat{}, errors.New("unknown prompt")
		},
	}

	b := &butler{
		llm:      mockLLM,
		subs:     mockSubs,
		selector: mockSelector,
	}

	recs, err := b.PrepSuggestions(ctx, clientCtx, items)
	if err != nil {
		t.Fatalf("PrepSuggestions failed: %v", err)
	}

	if len(recs) != 1 {
		t.Fatalf("Expected 1 recommendation, got %d", len(recs))
	}

	if recs[0].Item.Name != "Movie A" {
		t.Errorf("Expected Movie A, got %s", recs[0].Item.Name)
	}
	if recs[0].Motivation != "It is great" {
		t.Errorf("Expected motivation 'It is great', got %s", recs[0].Motivation)
	}
	if recs[0].SubtitleID != "1" {
		t.Errorf("Expected SubtitleID '1', got %s", recs[0].SubtitleID)
	}
}

func TestButler_PrepSuggestions_LLMErrors(t *testing.T) {
	ctx := context.Background()
	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			return models.Chat{}, errors.New("llm error")
		},
	}

	b := &butler{llm: mockLLM}
	_, err := b.PrepSuggestions(ctx, model.ClientContext{}, nil)
	if err == nil {
		t.Fatal("Expected error from PrepSuggestions when LLM fails")
	}

	// Empty response
	mockLLM.QueryFunc = func(ctx context.Context, chat models.Chat) (models.Chat, error) {
		return models.Chat{Messages: []models.Message{}}, nil
	}
	_, err = b.PrepSuggestions(ctx, model.ClientContext{}, nil)
	if err == nil {
		t.Fatal("Expected error when LLM returns empty response")
	}

	// Invalid JSON
	mockLLM.QueryFunc = func(ctx context.Context, chat models.Chat) (models.Chat, error) {
		return models.Chat{
			Messages: []models.Message{
				{Role: "assistant", Content: "not json"},
			},
		}, nil
	}
	_, err = b.PrepSuggestions(ctx, model.ClientContext{}, nil)
	if err == nil {
		t.Fatal("Expected error when LLM returns invalid JSON")
	}
}

func TestButler_prepSuggestion_SubsErrors(t *testing.T) {
	ctx := context.Background()
	items := []model.Item{{Name: "Movie A"}}

	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			// Semantic indexer prompt
			return models.Chat{
				Messages: []models.Message{
					{Role: "assistant", Content: `{"index": 0}`},
				},
			}, nil
		},
	}

	// Case 1: sub.Find fails
	mockSubs := &MockSubtitler{
		FindFunc: func(item model.Item) (model.MediaInfo, error) {
			return model.MediaInfo{}, errors.New("find error")
		},
	}
	b := &butler{llm: mockLLM, subs: mockSubs}
	_, err := b.prepSuggestion(ctx, suggestionResponse{Description: "Movie A"}, items)
	if err == nil {
		t.Fatal("Expected error when subs.Find fails")
	}
	var psErr *PreloadSubsError
	if !errors.As(err, &psErr) {
		t.Errorf("Expected PreloadSubsError, got %T", err)
	}

	// Case 2: selector fails
	mockSubs.FindFunc = func(item model.Item) (model.MediaInfo, error) {
		return model.MediaInfo{Streams: []model.Stream{}}, nil
	}
	mockSelector := &MockSubtitleSelector{
		SelectEnglishFunc: func(ctx context.Context, streams []model.Stream) (int, error) {
			return 0, errors.New("selector error")
		},
	}
	b.selector = mockSelector
	_, err = b.prepSuggestion(ctx, suggestionResponse{Description: "Movie A"}, items)
	if err == nil {
		t.Fatal("Expected error when selector fails")
	}
	if !errors.As(err, &psErr) {
		t.Errorf("Expected PreloadSubsError, got %T", err)
	}

	// Case 3: extract fails
	mockSelector.SelectEnglishFunc = func(ctx context.Context, streams []model.Stream) (int, error) {
		return 1, nil
	}
	mockSubs.ExtractFunc = func(item model.Item, streamIndex string) (string, error) {
		return "", errors.New("extract error")
	}
	_, err = b.prepSuggestion(ctx, suggestionResponse{Description: "Movie A"}, items)
	if err == nil {
		t.Fatal("Expected error when extract fails")
	}
	if errors.As(err, &psErr) {
		t.Errorf("Did not expect PreloadSubsError for extract failure (based on code logic check)")
	}
}

func TestSelector_SelectEnglish(t *testing.T) {
	ctx := context.Background()

	// Happy path: English subtitle picked deterministically, zero LLM queries
	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			t.Error("unexpected LLM query for deterministic English subtitle selection")
			return models.Chat{}, nil
		},
	}

	s := &selector{llm: mockLLM}

	streams := []model.Stream{
		{Index: 0, CodecType: "video"},
		{Index: 1, CodecType: "subtitle", Tags: model.Tags{Language: "eng", Title: "English"}, CodecName: "subrip"},
	}

	idx, err := s.Select(ctx, streams)
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}
	if idx != 1 {
		t.Errorf("Expected index 1, got %d", idx)
	}
	if mockLLM.QueryCount() != 0 {
		t.Errorf("Expected 0 LLM queries for deterministic English, got %d", mockLLM.QueryCount())
	}

	// No subtitles at all → error before LLM
	_, err = s.Select(ctx, []model.Stream{{Index: 0, CodecType: "video"}})
	if err == nil {
		t.Error("Expected error when no subtitles found")
	}

	// Swedish-only subtitle falls through to LLM (non-English → unusable)
	llmForSwedish := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			return models.Chat{
				Messages: []models.Message{{Role: "assistant", Content: `{"index": 1}`}},
			}, nil
		},
	}
	sSwedish := &selector{llm: llmForSwedish}
	idx, err = sSwedish.Select(ctx, []model.Stream{
		{Index: 1, CodecType: "subtitle", Tags: model.Tags{Language: "swe", Title: "Swedish"}},
	})
	if err != nil {
		t.Fatalf("Select with Swedish fallback failed: %v", err)
	}
	if idx != 1 {
		t.Errorf("Expected index 1 from LLM fallback, got %d", idx)
	}
	if llmForSwedish.QueryCount() != 1 {
		t.Errorf("Expected 1 LLM query for Swedish fallback, got %d", llmForSwedish.QueryCount())
	}

	// LLM error on fallback
	llmError := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			return models.Chat{}, errors.New("llm error")
		},
	}
	sErr := &selector{llm: llmError}
	_, err = sErr.Select(ctx, []model.Stream{
		{Index: 0, CodecType: "subtitle", Tags: model.Tags{Language: "swe"}},
	})
	if err == nil {
		t.Error("Expected error when LLM fails on fallback")
	}

	// LLM returns error JSON
	llmErrJSON := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			return models.Chat{
				Messages: []models.Message{{Role: "assistant", Content: `{"error": "no subs"}`}},
			}, nil
		},
	}
	sErrJSON := &selector{llm: llmErrJSON}
	_, err = sErrJSON.Select(ctx, []model.Stream{
		{Index: 0, CodecType: "subtitle", Tags: model.Tags{Language: "swe"}},
	})
	if err == nil {
		t.Error("Expected error when LLM returns error info")
	}
}

func TestButler_MetadataAndDebug(t *testing.T) {
	os.Setenv("DEBUG", "true")
	defer os.Unsetenv("DEBUG")

	ctx := context.Background()
	rawMeta := json.RawMessage(`{"year": 2023, "season": 1, "episode": 1, "alt_name": "Alt", "name": "Name"}`)
	items := []model.Item{
		{Name: "Item 1", Metadata: &rawMeta},
	}

	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			if strings.Contains(chat.Messages[0].Content, "You are a media Butler") {
				return models.Chat{
					Messages: []models.Message{{Role: "assistant", Content: `[{"description": "Item 1", "motivation": "test"}]`}},
				}, nil
			}
			if strings.Contains(chat.Messages[0].Content, "Your job is to pick") {
				return models.Chat{
					Messages: []models.Message{{Role: "assistant", Content: `{"index": 0}`}},
				}, nil
			}
			if strings.Contains(chat.Messages[0].Content, "subtitle stream") {
				return models.Chat{
					Messages: []models.Message{{Role: "assistant", Content: `{"index": 0}`}},
				}, nil
			}
			return models.Chat{}, nil
		},
	}

	b := &butler{
		llm:      mockLLM,
		subs:     nil,
		selector: NewSelector(models.Configurations{}),
	}
	sel := b.selector.(*selector)
	sel.llm = mockLLM

	_, _ = b.PrepSuggestions(ctx, model.ClientContext{}, items)

	b.selector.Select(ctx, []model.Stream{
		{Index: 0, CodecType: "subtitle"},
	})
}

func TestUnwrap(t *testing.T) {
	err := &PreloadSubsError{Err: errors.New("inner")}
	if err.Unwrap().Error() != "inner" {
		t.Error("Unwrap failed")
	}
	if err.Error() == "" {
		t.Error("Error() should not be empty")
	}
}

func TestParseSuggestionsResponse_NoArray(t *testing.T) {
	ctx := context.Background()
	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			return models.Chat{
				Messages: []models.Message{{Role: "assistant", Content: "no array here"}},
			}, nil
		},
	}
	b := &butler{llm: mockLLM}
	_, err := b.PrepSuggestions(ctx, model.ClientContext{}, nil)
	if err == nil {
		t.Fatal("Expected error when no JSON array")
	}
	if !strings.Contains(err.Error(), "no JSON array found") {
		t.Errorf("Expected 'no JSON array found' error, got %v", err)
	}
}

func TestButler_PrepSuggestions_Fallback(t *testing.T) {
	ctx := context.Background()
	items := []model.Item{{Name: "fallback"}}
	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			msg := chat.Messages[0].Content
			if strings.Contains(msg, "You are a media Butler") {
				return models.Chat{
					Messages: []models.Message{
						{Role: "system", Content: `[{"description":"fallback", "motivation":"test"}]`},
					},
				}, nil
			}
			if strings.Contains(msg, "Your job is to pick") {
				return models.Chat{
					Messages: []models.Message{{Role: "system", Content: `{"index": 0}`}},
				}, nil
			}
			return models.Chat{}, nil
		},
	}
	b := &butler{llm: mockLLM}
	recs, err := b.PrepSuggestions(ctx, model.ClientContext{}, items)
	if err != nil {
		t.Fatalf("Fallback failed: %v", err)
	}
	if len(recs) != 1 {
		t.Errorf("Expected 1 rec, got %d", len(recs))
	}
}

func TestSelector_Fallback(t *testing.T) {
	ctx := context.Background()
	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			return models.Chat{
				Messages: []models.Message{{Role: "system", Content: `{"index": 0}`}},
			}, nil
		},
	}
	s := &selector{llm: mockLLM}
	// Use Swedish so the deterministic path falls through to LLM
	idx, err := s.Select(ctx, []model.Stream{
		{Index: 0, CodecType: "subtitle", Tags: model.Tags{Language: "swe"}},
	})
	if err != nil {
		t.Fatalf("Fallback failed: %v", err)
	}
	if idx != 0 {
		t.Errorf("Expected 0, got %d", idx)
	}
}

func TestButler_PrepSuggestions_PartialError(t *testing.T) {
	ctx := context.Background()
	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			var fullMsg strings.Builder
			for _, m := range chat.Messages {
				fullMsg.WriteString(m.Content + "\n")
			}

			if strings.Contains(fullMsg.String(), "You are a media Butler") {
				return models.Chat{
					Messages: []models.Message{
						{Role: "assistant", Content: `[
                             {"description":"valid", "motivation":"test"},
                             {"description":"invalid", "motivation":"test"}
                         ]`},
					},
				}, nil
			}
			if strings.Contains(fullMsg.String(), "Semantic description: valid") {
				return models.Chat{
					Messages: []models.Message{{Role: "assistant", Content: `{"index": 0}`}},
				}, nil
			}
			return models.Chat{
				Messages: []models.Message{{Role: "assistant", Content: `not json`}},
			}, nil
		},
	}

	items := []model.Item{{Name: "Item 1"}}
	b := &butler{llm: mockLLM}

	recs, err := b.PrepSuggestions(ctx, model.ClientContext{}, items)
	if err != nil {
		t.Errorf("Did not expect error, got %v", err)
	}
	if len(recs) != 1 {
		t.Errorf("Expected 1 valid rec, got %d", len(recs))
	}
}

func TestSelector_EmptyResponse(t *testing.T) {
	ctx := context.Background()
	// Use non-English streams so the deterministic path falls through to LLM
	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			return models.Chat{Messages: []models.Message{}}, nil
		},
	}
	s := &selector{llm: mockLLM}
	streams := []model.Stream{{CodecType: "subtitle", Index: 0, Tags: model.Tags{Language: "swe"}}}
	_, err := s.Select(ctx, streams)
	if err == nil {
		t.Fatal("Expected error on empty response")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Errorf("Expected 'empty response' error, got %v", err)
	}
}

// Phase 2 tests — Butler returns the index

func TestResolveItem_ZeroIndex(t *testing.T) {
	ctx := context.Background()
	items := []model.Item{
		{Name: "Item 0"},
		{Name: "Item 1"},
	}

	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			t.Error("unexpected LLM query when index is valid")
			return models.Chat{}, nil
		},
	}

	b := &butler{llm: mockLLM}
	zero := 0
	result, err := b.resolveItem(ctx, suggestionResponse{Index: &zero, Description: "Item 0"}, items)
	if err != nil {
		t.Fatalf("resolveItem failed: %v", err)
	}
	if result.Name != "Item 0" {
		t.Errorf("Expected Item 0, got %s", result.Name)
	}
	if mockLLM.QueryCount() != 0 {
		t.Errorf("Expected 0 LLM queries, got %d", mockLLM.QueryCount())
	}
}

func TestResolveItem_MissingIndexFallsBack(t *testing.T) {
	ctx := context.Background()
	items := []model.Item{{Name: "Movie A"}}

	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			return models.Chat{
				Messages: []models.Message{{Role: "assistant", Content: `{"index": 0}`}},
			}, nil
		},
	}

	b := &butler{llm: mockLLM}
	result, err := b.resolveItem(ctx, suggestionResponse{Description: "Movie A"}, items)
	if err != nil {
		t.Fatalf("resolveItem failed: %v", err)
	}
	if result.Name != "Movie A" {
		t.Errorf("Expected Movie A, got %s", result.Name)
	}
	if mockLLM.QueryCount() != 1 {
		t.Errorf("Expected 1 fallback query, got %d", mockLLM.QueryCount())
	}
}

func TestResolveItem_OutOfRangeFallsBack(t *testing.T) {
	ctx := context.Background()
	items := []model.Item{{Name: "Movie A"}}

	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			return models.Chat{
				Messages: []models.Message{{Role: "assistant", Content: `{"index": 0}`}},
			}, nil
		},
	}

	b := &butler{llm: mockLLM}
	outOfRange := 9999
	result, err := b.resolveItem(ctx, suggestionResponse{Index: &outOfRange, Description: "Movie A"}, items)
	if err != nil {
		t.Fatalf("resolveItem should fall back, not error: %v", err)
	}
	if result.Name != "Movie A" {
		t.Errorf("Expected Movie A, got %s", result.Name)
	}
	if mockLLM.QueryCount() != 1 {
		t.Errorf("Expected 1 fallback query, got %d", mockLLM.QueryCount())
	}
}

func TestResolveItem_NegativeIndexFallsBack(t *testing.T) {
	ctx := context.Background()
	items := []model.Item{{Name: "Movie A"}}

	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			return models.Chat{
				Messages: []models.Message{{Role: "assistant", Content: `{"index": 0}`}},
			}, nil
		},
	}

	b := &butler{llm: mockLLM}
	neg := -1
	result, err := b.resolveItem(ctx, suggestionResponse{Index: &neg, Description: "Movie A"}, items)
	if err != nil {
		t.Fatalf("resolveItem should fall back, not error: %v", err)
	}
	if result.Name != "Movie A" {
		t.Errorf("Expected Movie A, got %s", result.Name)
	}
	if mockLLM.QueryCount() != 1 {
		t.Errorf("Expected 1 fallback query, got %d", mockLLM.QueryCount())
	}
}

func TestResolveItem_EmptyItems(t *testing.T) {
	ctx := context.Background()
	var items []model.Item

	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			return models.Chat{
				Messages: []models.Message{{Role: "assistant", Content: `{"index": 0}`}},
			}, nil
		},
	}

	b := &butler{llm: mockLLM}
	_, err := b.resolveItem(ctx, suggestionResponse{Description: "something"}, items)
	if err == nil {
		t.Fatal("Expected error when resolving with empty items")
	}
}

func TestResolveItem_DuplicateIndices(t *testing.T) {
	ctx := context.Background()
	items := []model.Item{
		{Name: "Movie A"},
		{Name: "Movie B"},
		{Name: "Movie C"},
	}

	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			t.Error("unexpected LLM query")
			return models.Chat{}, nil
		},
	}

	b := &butler{llm: mockLLM}
	idx := 1
	r1, err := b.resolveItem(ctx, suggestionResponse{Index: &idx, Description: "First"}, items)
	if err != nil {
		t.Fatalf("first resolveItem failed: %v", err)
	}
	r2, err := b.resolveItem(ctx, suggestionResponse{Index: &idx, Description: "Second"}, items)
	if err != nil {
		t.Fatalf("second resolveItem failed: %v", err)
	}
	if r1.Name != "Movie B" || r2.Name != "Movie B" {
		t.Errorf("Expected both to resolve to Movie B, got %s and %s", r1.Name, r2.Name)
	}
}

func TestParseSuggestions_NonIntegerIndex(t *testing.T) {
	ctx := context.Background()
	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			return models.Chat{
				Messages: []models.Message{{Role: "assistant", Content: `[{"index": "42", "description": "test", "motivation": "test"}]`}},
			}, nil
		},
	}
	b := &butler{llm: mockLLM}
	_, err := b.PrepSuggestions(ctx, model.ClientContext{}, []model.Item{{Name: "x"}})
	if err == nil {
		t.Fatal("Expected parse error for non-integer index")
	}
}

func TestPickerSystemPrompt_MentionsIndex(t *testing.T) {
	if !strings.Contains(pickerSystemPrompt, `"index"`) {
		t.Error("System prompt must mention 'index' field in JSON format")
	}
	if !strings.Contains(pickerSystemPrompt, "Copy it verbatim") {
		t.Error("System prompt must instruct verbatim index copying")
	}
	if !strings.Contains(pickerSystemPrompt, "description") {
		t.Error("System prompt must still require 'description' field")
	}
}

func TestPrepSuggestions_NoIndexerQueryOnValidIndex(t *testing.T) {
	ctx := context.Background()
	items := []model.Item{
		{Name: "Movie A"},
		{Name: "Movie B"},
		{Name: "Movie C"},
	}

	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			sys := chat.Messages[0].Content
			if strings.Contains(sys, "You are a media Butler") {
				return models.Chat{
					Messages: []models.Message{{Role: "assistant", Content: `[
						{"index": 0, "description": "Movie A", "motivation": "test"},
						{"index": 1, "description": "Movie B", "motivation": "test"},
						{"index": 2, "description": "Movie C", "motivation": "test"}
					]`}},
				}, nil
			}
			if strings.Contains(sys, "pick a media item") {
				t.Error("semantic indexer should not be called when indices are valid")
				return models.Chat{}, nil
			}
			return models.Chat{}, nil
		},
	}

	b := &butler{llm: mockLLM}
	recs, err := b.PrepSuggestions(ctx, model.ClientContext{}, items)
	if err != nil {
		t.Fatalf("PrepSuggestions failed: %v", err)
	}
	if len(recs) != 3 {
		t.Fatalf("Expected 3 suggestions, got %d", len(recs))
	}
	found := make(map[string]bool)
	for _, rec := range recs {
		found[rec.Item.Name] = true
	}
	for _, it := range items {
		if !found[it.Name] {
			t.Errorf("Missing suggestion for %s", it.Name)
		}
	}
}

func TestPrepSuggestions_QueryCount(t *testing.T) {
	ctx := context.Background()
	items := []model.Item{
		{Name: "Movie A"},
		{Name: "Movie B"},
		{Name: "Movie C"},
	}

	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			sys := chat.Messages[0].Content
			if strings.Contains(sys, "You are a media Butler") {
				return models.Chat{
					Messages: []models.Message{{Role: "assistant", Content: `[
						{"index": 0, "description": "Movie A", "motivation": "test"},
						{"index": 1, "description": "Movie B", "motivation": "test"},
						{"index": 2, "description": "Movie C", "motivation": "test"}
					]`}},
				}, nil
			}
			if strings.Contains(sys, "pick a media item") {
				t.Error("semantic indexer should not be called when indices are valid")
				return models.Chat{}, nil
			}
			return models.Chat{
				Messages: []models.Message{{Role: "assistant", Content: `{"index": 0}`}},
			}, nil
		},
	}

	mockSubs := &MockSubtitler{
		FindFunc: func(item model.Item) (model.MediaInfo, error) {
			return model.MediaInfo{
				Streams: []model.Stream{{Index: 0, CodecType: "subtitle"}},
			}, nil
		},
		ExtractFunc: func(item model.Item, streamIndex string) (string, error) {
			return "/tmp/subs.srt", nil
		},
	}

	sel := NewSelector(models.Configurations{}).(*selector)
	sel.llm = mockLLM

	b := &butler{
		llm:      mockLLM,
		subs:     mockSubs,
		selector: sel,
	}

	recs, err := b.PrepSuggestions(ctx, model.ClientContext{}, items)
	if err != nil {
		t.Fatalf("PrepSuggestions failed: %v", err)
	}
	if len(recs) != 3 {
		t.Fatalf("Expected 3 suggestions, got %d", len(recs))
	}

	// After Phase 3, the selector is deterministic for English subtitles.
	// Only the butler's main picker query remains: 1 query total.
	if mockLLM.QueryCount() != 1 {
		t.Errorf("Expected 1 LLM query (1 butler + 0 selector), got %d", mockLLM.QueryCount())
	}
}

func TestPrepSuggestions_PartialIndexerFailure(t *testing.T) {
	ctx := context.Background()
	items := []model.Item{
		{Name: "Movie A"},
		{Name: "Movie B"},
	}

	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			sys := chat.Messages[0].Content
			if strings.Contains(sys, "You are a media Butler") {
				return models.Chat{
					Messages: []models.Message{{Role: "assistant", Content: `[
						{"index": 0, "description": "Movie A", "motivation": "test"},
						{"description": "Movie B", "motivation": "test"}
					]`}},
				}, nil
			}
			if strings.Contains(sys, "pick a media item") {
				return models.Chat{}, errors.New("indexer failure")
			}
			return models.Chat{}, nil
		},
	}

	b := &butler{llm: mockLLM}
	recs, err := b.PrepSuggestions(ctx, model.ClientContext{}, items)
	if err != nil {
		t.Errorf("PrepSuggestions should not error on partial failure: %v", err)
	}
	if len(recs) != 1 {
		t.Errorf("Expected 1 valid suggestion (Movie A with valid index), got %d", len(recs))
	}
	if len(recs) > 0 && recs[0].Item.Name != "Movie A" {
		t.Errorf("Expected Movie A, got %s", recs[0].Item.Name)
	}
}

func TestPrepSuggestions_AllFailDoesNotReturnNilNil(t *testing.T) {
	ctx := context.Background()
	items := []model.Item{{Name: "Movie A"}}

	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			sys := chat.Messages[0].Content
			if strings.Contains(sys, "You are a media Butler") {
				return models.Chat{
					Messages: []models.Message{{Role: "assistant", Content: `[
						{"description": "Movie A", "motivation": "test"}
					]`}},
				}, nil
			}
			return models.Chat{}, errors.New("indexer failure")
		},
	}

	b := &butler{llm: mockLLM}
	recs, err := b.PrepSuggestions(ctx, model.ClientContext{}, items)
	if err == nil {
		t.Error("expected error on total failure, got nil")
	}
	if recs != nil {
		t.Errorf("expected nil recs on total failure, got %d", len(recs))
	}
}

// Phase 3 tests — Deterministic Subtitle Selection

func TestRankSubtitle_Table(t *testing.T) {
	tests := []struct {
		name    string
		stream  model.Stream
		wantPos bool // true if score >= 0 (usable)
	}{
		{
			name:    "English subrip default",
			stream:  model.Stream{Index: 0, CodecType: "subtitle", CodecName: "subrip", Tags: model.Tags{Language: "eng"}, Disposition: model.Disposition{Default: 1}},
			wantPos: true,
		},
		{
			name:    "English SDH",
			stream:  model.Stream{Index: 1, CodecType: "subtitle", CodecName: "subrip", Tags: model.Tags{Language: "eng"}, Disposition: model.Disposition{HearingImpaired: 1}},
			wantPos: true,
		},
		{
			name:    "English forced",
			stream:  model.Stream{Index: 2, CodecType: "subtitle", CodecName: "subrip", Tags: model.Tags{Language: "en"}, Disposition: model.Disposition{Forced: 1}},
			wantPos: true,
		},
		{
			name:    "English commentary",
			stream:  model.Stream{Index: 3, CodecType: "subtitle", CodecName: "subrip", Tags: model.Tags{Language: "eng", Title: "Commentary"}, Disposition: model.Disposition{Comment: 1}},
			wantPos: false,
		},
		{
			name:    "Swedish only",
			stream:  model.Stream{Index: 4, CodecType: "subtitle", CodecName: "subrip", Tags: model.Tags{Language: "swe"}},
			wantPos: false,
		},
		{
			name:    "untagged language",
			stream:  model.Stream{Index: 5, CodecType: "subtitle", CodecName: "subrip"},
			wantPos: true,
		},
		{
			name:    "English external sidecar",
			stream:  model.Stream{Index: 6, CodecType: "subtitle", CodecName: "subrip", Tags: model.Tags{Language: "eng"}, ExternalPath: "/path/to/sub.srt"},
			wantPos: true,
		},
		{
			name:    "PGS bitmap English",
			stream:  model.Stream{Index: 7, CodecType: "subtitle", CodecName: "hdmv_pgs_subtitle", Tags: model.Tags{Language: "eng"}},
			wantPos: true,
		},
		{
			name:    "English signs track",
			stream:  model.Stream{Index: 8, CodecType: "subtitle", CodecName: "subrip", Tags: model.Tags{Language: "eng", Title: "Signs"}},
			wantPos: true,
		},
		{
			name:    "title commentary no disposition",
			stream:  model.Stream{Index: 9, CodecType: "subtitle", CodecName: "subrip", Tags: model.Tags{Language: "eng", Title: "Director Commentary"}},
			wantPos: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := rankSubtitle(tt.stream)
			isUsable := score >= 0
			if isUsable != tt.wantPos {
				t.Errorf("rankSubtitle score=%d, usable=%v, want usable=%v", score, isUsable, tt.wantPos)
			}
		})
	}
}

func TestSelect_EnglishDispositionPriority(t *testing.T) {
	ctx := context.Background()

	streams := []model.Stream{
		{Index: 0, CodecType: "subtitle", CodecName: "subrip", Tags: model.Tags{Language: "eng"}, Disposition: model.Disposition{HearingImpaired: 1}},
		{Index: 1, CodecType: "subtitle", CodecName: "subrip", Tags: model.Tags{Language: "eng"}, Disposition: model.Disposition{Default: 1}},
		{Index: 2, CodecType: "subtitle", CodecName: "subrip", Tags: model.Tags{Language: "eng"}, Disposition: model.Disposition{Forced: 1}},
	}

	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			t.Error("unexpected LLM query for deterministic selection")
			return models.Chat{}, nil
		},
	}

	s := &selector{llm: mockLLM}
	idx, err := s.Select(ctx, streams)
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}
	if idx != 1 {
		t.Errorf("Expected default stream (index 1), got %d", idx)
	}
	if mockLLM.QueryCount() != 0 {
		t.Errorf("Expected 0 LLM queries, got %d", mockLLM.QueryCount())
	}
}

func TestSelect_FallsBackWhenNoUsableCandidate(t *testing.T) {
	ctx := context.Background()

	t.Run("commentary only", func(t *testing.T) {
		mockLLM := &MockFullResponse{
			QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
				return models.Chat{
					Messages: []models.Message{{Role: "assistant", Content: `{"index": 0}`}},
				}, nil
			},
		}
		s := &selector{llm: mockLLM}
		idx, err := s.Select(ctx, []model.Stream{
			{Index: 0, CodecType: "subtitle", CodecName: "subrip", Tags: model.Tags{Language: "eng"}, Disposition: model.Disposition{Comment: 1}},
		})
		if err != nil {
			t.Fatalf("Select should fall back to LLM, got error: %v", err)
		}
		if idx != 0 {
			t.Errorf("Expected index 0 from LLM fallback, got %d", idx)
		}
		if mockLLM.QueryCount() != 1 {
			t.Errorf("Expected 1 LLM query for fallback, got %d", mockLLM.QueryCount())
		}
	})

	t.Run("non-english only", func(t *testing.T) {
		mockLLM := &MockFullResponse{
			QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
				return models.Chat{
					Messages: []models.Message{{Role: "assistant", Content: `{"index": 1}`}},
				}, nil
			},
		}
		s := &selector{llm: mockLLM}
		idx, err := s.Select(ctx, []model.Stream{
			{Index: 1, CodecType: "subtitle", CodecName: "subrip", Tags: model.Tags{Language: "swe"}},
		})
		if err != nil {
			t.Fatalf("Select should fall back to LLM, got error: %v", err)
		}
		if idx != 1 {
			t.Errorf("Expected index 1 from LLM fallback, got %d", idx)
		}
		if mockLLM.QueryCount() != 1 {
			t.Errorf("Expected 1 LLM query, got %d", mockLLM.QueryCount())
		}
	})
}

func TestSelect_BitmapEnglishBeatsForeignText(t *testing.T) {
	ctx := context.Background()

	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			t.Error("unexpected LLM query — bitmap English should be selectable deterministically")
			return models.Chat{}, nil
		},
	}

	s := &selector{llm: mockLLM}
	idx, err := s.Select(ctx, []model.Stream{
		{Index: 0, CodecType: "subtitle", CodecName: "subrip", Tags: model.Tags{Language: "swe"}},
		{Index: 1, CodecType: "subtitle", CodecName: "hdmv_pgs_subtitle", Tags: model.Tags{Language: "eng"}},
	})
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}
	if idx != 1 {
		t.Errorf("Expected PGS English (index 1) to beat Swedish, got %d", idx)
	}
	if mockLLM.QueryCount() != 0 {
		t.Errorf("Expected 0 LLM queries, got %d", mockLLM.QueryCount())
	}
}

func TestRankBest_Deterministic(t *testing.T) {
	fixture := []model.Stream{
		{Index: 0, CodecType: "subtitle", CodecName: "subrip", Tags: model.Tags{Language: "eng"}, Disposition: model.Disposition{Default: 1}},
		{Index: 1, CodecType: "subtitle", CodecName: "subrip", Tags: model.Tags{Language: "eng"}, Disposition: model.Disposition{HearingImpaired: 1}},
		{Index: 2, CodecType: "subtitle", CodecName: "subrip", Tags: model.Tags{Language: "eng"}, Disposition: model.Disposition{Forced: 1}},
	}

	expected := 0

	for range 100 {
		shifted := make([]model.Stream, len(fixture))
		for i := range fixture {
			shifted[i] = fixture[(i+1)%len(fixture)]
		}

		idx, ok := rankBest(shifted)
		if !ok {
			t.Fatal("rankBest returned no usable candidate")
		}
		if idx != expected {
			t.Errorf("rankBest returned %d, expected %d (input order should not affect result)", idx, expected)
		}
	}
}

func TestRankSubtitle_RegionalLanguageTag(t *testing.T) {
	score := rankSubtitle(model.Stream{
		CodecType: "subtitle", CodecName: "subrip",
		Tags: model.Tags{Language: "eng-US"},
	})
	if score < 0 {
		t.Errorf("eng-US should be treated as English, got score=%d", score)
	}

	score = rankSubtitle(model.Stream{
		CodecType: "subtitle", CodecName: "subrip",
		Tags: model.Tags{Language: "en-GB"},
	})
	if score < 0 {
		t.Errorf("en-GB should be treated as English, got score=%d", score)
	}
}

func TestRankSubtitle_UndefinedLanguage(t *testing.T) {
	score := rankSubtitle(model.Stream{
		CodecType: "subtitle", CodecName: "subrip",
		Tags: model.Tags{Language: "und"},
	})
	if score < 0 {
		t.Errorf("und should be treated as unknown (usable), got score=%d", score)
	}
	if score < 10 {
		t.Errorf("und should score at least 10 (unknown), got %d", score)
	}
}

func TestRankSubtitle_TitleOnlyCommentary(t *testing.T) {
	score := rankSubtitle(model.Stream{
		CodecType: "subtitle", CodecName: "subrip",
		Tags: model.Tags{Title: "Director Commentary"},
	})
	if score >= 0 {
		t.Errorf("title 'commentary' should be unusable, got score=%d", score)
	}
}

func TestRankBest_TieBreakOnIndex(t *testing.T) {
	streams := []model.Stream{
		{Index: 5, CodecType: "subtitle", CodecName: "subrip", Tags: model.Tags{Language: "eng"}},
		{Index: 2, CodecType: "subtitle", CodecName: "subrip", Tags: model.Tags{Language: "eng"}},
		{Index: 8, CodecType: "subtitle", CodecName: "subrip", Tags: model.Tags{Language: "eng"}},
	}

	idx, ok := rankBest(streams)
	if !ok {
		t.Fatal("rankBest returned no usable candidate")
	}
	if idx != 2 {
		t.Errorf("Expected lowest index 2 on tie, got %d", idx)
	}
}

func TestRankBest_AllUnusable(t *testing.T) {
	streams := []model.Stream{
		{Index: 0, CodecType: "subtitle", CodecName: "subrip", Tags: model.Tags{Language: "swe"}},
		{Index: 1, CodecType: "subtitle", CodecName: "subrip", Tags: model.Tags{Language: "spa"}},
	}

	_, ok := rankBest(streams)
	if ok {
		t.Error("rankBest should return false when all candidates are unusable")
	}
}

func TestFilterSubtitleStreams(t *testing.T) {
	streams := []model.Stream{
		{Index: 0, CodecType: "video"},
		{Index: 1, CodecType: "audio"},
		{Index: 2, CodecType: "subtitle"},
		{Index: 3, CodecType: "subtitle"},
	}

	subs := filterSubtitleStreams(streams)
	if len(subs) != 2 {
		t.Fatalf("Expected 2 subtitle streams, got %d", len(subs))
	}
	if subs[0].Index != 2 || subs[1].Index != 3 {
		t.Errorf("Expected indices [2, 3], got [%d, %d]", subs[0].Index, subs[1].Index)
	}
}

func TestRankSubtitle_CodecPenalties(t *testing.T) {
	for _, codec := range []string{"subrip", "ass", "ssa", "webvtt", "mov_text"} {
		t.Run(codec, func(t *testing.T) {
			score := rankSubtitle(model.Stream{
				CodecType: "subtitle", CodecName: codec,
				Tags: model.Tags{Language: "eng"},
			})
			if score < 115 {
				t.Errorf("Expected score >= 115 for text codec %s, got %d", codec, score)
			}
		})
	}

	for _, codec := range []string{"hdmv_pgs_subtitle", "dvd_subtitle"} {
		t.Run(codec, func(t *testing.T) {
			score := rankSubtitle(model.Stream{
				CodecType: "subtitle", CodecName: codec,
				Tags: model.Tags{Language: "eng"},
			})
			if score > 50 || score < 0 {
				t.Errorf("Expected score around 50 for bitmap codec %s, got %d", codec, score)
			}
		})
	}
}

func TestRankSubtitle_KaraokeSongPenalty(t *testing.T) {
	for _, word := range []string{"sign", "song", "lyric", "karaoke"} {
		t.Run(word, func(t *testing.T) {
			score := rankSubtitle(model.Stream{
				CodecType: "subtitle", CodecName: "subrip",
				Tags: model.Tags{Language: "eng", Title: "English " + word + " track"},
			})
			if score > 60 || score < 0 {
				t.Errorf("Expected score around 55 for %s title, got %d", word, score)
			}
		})
	}
}

func TestSelect_ViaLLMParsePaths(t *testing.T) {
	ctx := context.Background()

	t.Run("valid json", func(t *testing.T) {
		mockLLM := &MockFullResponse{
			QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
				return models.Chat{
					Messages: []models.Message{{Role: "assistant", Content: `{"index": 0}`}},
				}, nil
			},
		}
		s := &selector{llm: mockLLM}
		idx, err := s.selectViaLLM(ctx, []model.Stream{{CodecType: "subtitle", Index: 0}})
		if err != nil {
			t.Fatalf("selectViaLLM failed: %v", err)
		}
		if idx != 0 {
			t.Errorf("Expected 0, got %d", idx)
		}
	})

	t.Run("error json", func(t *testing.T) {
		mockLLM := &MockFullResponse{
			QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
				return models.Chat{
					Messages: []models.Message{{Role: "assistant", Content: `{"error": "no english subtitles found"}`}},
				}, nil
			},
		}
		s := &selector{llm: mockLLM}
		_, err := s.selectViaLLM(ctx, []model.Stream{{CodecType: "subtitle", Index: 0}})
		if err == nil {
			t.Error("Expected error when LLM returns error JSON")
		}
	})

	t.Run("no index field", func(t *testing.T) {
		mockLLM := &MockFullResponse{
			QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
				return models.Chat{
					Messages: []models.Message{{Role: "assistant", Content: `{}`}},
				}, nil
			},
		}
		s := &selector{llm: mockLLM}
		_, err := s.selectViaLLM(ctx, []model.Stream{{CodecType: "subtitle", Index: 0}})
		if err == nil {
			t.Error("Expected error when no index in response")
		}
	})

	t.Run("malformed json", func(t *testing.T) {
		mockLLM := &MockFullResponse{
			QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
				return models.Chat{
					Messages: []models.Message{{Role: "assistant", Content: `not json`}},
				}, nil
			},
		}
		s := &selector{llm: mockLLM}
		_, err := s.selectViaLLM(ctx, []model.Stream{{CodecType: "subtitle", Index: 0}})
		if err == nil {
			t.Error("Expected error on malformed JSON")
		}
	})

	t.Run("json in markdown", func(t *testing.T) {
		mockLLM := &MockFullResponse{
			QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
				return models.Chat{
					Messages: []models.Message{{Role: "assistant", Content: "```json\n{\"index\": 1}\n```"}},
				}, nil
			},
		}
		s := &selector{llm: mockLLM}
		idx, err := s.selectViaLLM(ctx, []model.Stream{{CodecType: "subtitle", Index: 0}, {CodecType: "subtitle", Index: 1}})
		if err != nil {
			t.Fatalf("selectViaLLM should handle JSON in markdown: %v", err)
		}
		if idx != 1 {
			t.Errorf("Expected 1, got %d", idx)
		}
	})
}

func TestSelect_NoSubtitleStreams(t *testing.T) {
	ctx := context.Background()
	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			t.Error("unexpected LLM query when no subtitle streams")
			return models.Chat{}, nil
		},
	}
	s := &selector{llm: mockLLM}
	_, err := s.Select(ctx, []model.Stream{
		{Index: 0, CodecType: "video"},
		{Index: 1, CodecType: "audio"},
	})
	if err == nil {
		t.Fatal("Expected error for no subtitle streams")
	}
	if err.Error() != "no subtitle streams found" {
		t.Errorf("Expected 'no subtitle streams found', got %q", err.Error())
	}
}

// Phase 4 tests — Butler Payload Diet

func TestProjectItems_FieldSet(t *testing.T) {
	rawMeta := json.RawMessage(`{"name": "The Movie", "showName": "The Show", "year": 2023, "season": 1, "episode": 4, "genre": "Action", "duration_min": 120}`)
	items := []model.Item{
		{Name: "Movie.mp4", Metadata: &rawMeta},
	}

	views := ProjectItems(items)
	if len(views) != 1 {
		t.Fatalf("Expected 1 view, got %d", len(views))
	}

	b, _ := json.Marshal(views[0])
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("Failed to unmarshal view: %v", err)
	}

	// Every key must be one of the allowed fields.
	allowed := map[string]bool{"i": true, "n": true, "t": true, "y": true, "s": true, "e": true, "g": true, "r": true, "sn": true}
	for k := range out {
		if !allowed[k] {
			t.Errorf("Unexpected key %q in butlerItemView JSON — field not in the projection", k)
		}
	}
	if out["sn"] != "The Show" {
		t.Errorf("showName not projected, got %v", out["sn"])
	}
}

func TestFormatItems_NoProseMetadata(t *testing.T) {
	rawMeta := json.RawMessage(`{"name": "The Movie", "description": "A long plot summary", "actors": ["Actor 1", "Actor 2"], "year": 2023, "plot": "The story begins...", "director": "Someone", "rating": 8.5}`)
	items := []model.Item{
		{Name: "Movie.mp4", Metadata: &rawMeta},
	}

	payload := formatItems(items)
	for _, prohibited := range []string{"description", "plot", "actors", "director", "rating"} {
		if strings.Contains(payload, prohibited) {
			t.Errorf("Payload contains prohibited field %q: %s", prohibited, payload)
		}
	}
	// The title must still appear.
	if !strings.Contains(payload, "The Movie") {
		t.Error("Expected to find title 'The Movie' in payload")
	}
}

func TestFormatItems_SizeBudget(t *testing.T) {
	// 434-item size test: verify projection is significantly smaller than the old format.
	// Generate items with rich metadata to simulate real data.
	var items []model.Item
	for i := range 434 {
		rawMeta := json.RawMessage(fmt.Sprintf(`{"name":"Movie %d","alt_name":"Alt %d","year":%d,"season":%d,"episode":%d,"description":"A long description for movie %d with lots of text that would bulk up the payload","actors":["Actor A","Actor B","Actor C","Actor D"],"duration_min":%d}`, i, i, 2020+(i%5), (i%3)+1, (i%12)+1, i, 90+(i%60)))
		items = append(items, model.Item{
			Name:     fmt.Sprintf("Movie_%d.mp4", i),
			MIMEType: "video/mp4",
			Metadata: &rawMeta,
		})
	}

	// Old format baseline
	oldPayload := oldFormatItems(items)
	newPayload := formatItems(items)

	ratio := float64(len(newPayload)) / float64(len(oldPayload))
	if ratio >= 0.55 {
		t.Errorf("New payload is %.1f%% of old (%d vs %d bytes) — must be under 55%%", ratio*100, len(newPayload), len(oldPayload))
	}
	t.Logf("Old: %d bytes, New: %d bytes, Ratio: %.1f%%", len(oldPayload), len(newPayload), ratio*100)
}

// oldFormatItems replicates the pre-Phase-4 format for baseline measurement.
func oldFormatItems(items []model.Item) string {
	var result []map[string]any
	for idx, it := range items {
		item := map[string]any{
			"index": idx,
			"name":  it.Name,
			"type":  it.MIMEType,
		}
		if it.Metadata != nil {
			var metadata map[string]any
			err := json.Unmarshal(*it.Metadata, &metadata)
			if err == nil {
				item["metadata"] = metadata
			}
		}
		result = append(result, item)
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return string(b)
}

func TestProjectItems_IndexAlignment(t *testing.T) {
	rawMeta := json.RawMessage(`{"name": "Item 0"}`)
	items := []model.Item{
		{Name: "file0.mp4", Metadata: &rawMeta},
		{Name: "file1.mp4"},
		{Name: "file2.mp4"},
	}

	views := ProjectItems(items)
	if len(views) != len(items) {
		t.Fatalf("Expected %d views, got %d", len(items), len(views))
	}
	for i, v := range views {
		if v.Index != i {
			t.Errorf("View[%d] has index %d", i, v.Index)
		}
		if v.Name != items[i].Name {
			t.Errorf("View[%d] name %q != item name %q", i, v.Name, items[i].Name)
		}
	}
}

func TestProjectItems_NilMetadata(t *testing.T) {
	items := []model.Item{
		{Name: "video.mp4", Metadata: nil},
	}

	views := ProjectItems(items)
	if len(views) != 1 {
		t.Fatalf("Expected 1 view, got %d", len(views))
	}

	b, _ := json.Marshal(views[0])
	if !strings.Contains(string(b), `"i":0`) || !strings.Contains(string(b), `"n":"video.mp4"`) {
		t.Errorf("Nil-metadata item should emit only i and n, got: %s", string(b))
	}
	// Must not contain t, y, s, e, g, r
	for _, key := range []string{`"t"`, `"y"`, `"s"`, `"e"`, `"g"`, `"r"`} {
		if strings.Contains(string(b), key) {
			t.Errorf("Nil metadata should not emit %s, got: %s", key, string(b))
		}
	}
}

func TestProjectItems_MalformedMetadata(t *testing.T) {
	badMeta := json.RawMessage(`not valid json`)
	items := []model.Item{
		{Name: "video.mp4", Metadata: &badMeta},
	}

	views := ProjectItems(items)
	if len(views) != 1 {
		t.Fatalf("Expected 1 view, got %d", len(views))
	}
	if views[0].Index != 0 {
		t.Errorf("Expected index 0, got %d", views[0].Index)
	}
	if views[0].Name != "video.mp4" {
		t.Errorf("Expected name 'video.mp4', got %q", views[0].Name)
	}
	// Malformed metadata must not cause a panic or drop the item.
}

func TestFormatItems_Deterministic(t *testing.T) {
	rawMeta := json.RawMessage(`{"name": "Test", "year": 2023}`)
	items := []model.Item{
		{Name: "a.mp4", Metadata: &rawMeta},
		{Name: "b.mp4", Metadata: &rawMeta},
	}

	first := formatItems(items)
	for range 100 {
		if s := formatItems(items); s != first {
			t.Fatal("formatItems is not deterministic across runs")
		}
	}
}

func TestPickerSystemPrompt_HasKeyLegend(t *testing.T) {
	if !strings.Contains(pickerSystemPrompt, "i=index n=filename t=title y=year s=season e=episode g=genre r=runtimeMinutes") {
		t.Error("System prompt must carry the key legend")
	}
}

func TestFormatItems_EmptyItems(t *testing.T) {
	payload := formatItems(nil)
	if payload != "[]" {
		t.Errorf("Expected '[]' for nil items, got %q", payload)
	}

	payload = formatItems([]model.Item{})
	if payload != "[]" {
		t.Errorf("Expected '[]' for empty items, got %q", payload)
	}
}

func TestProjectItems_AllZeroMetadata(t *testing.T) {
	rawMeta := json.RawMessage(`{"name": "", "year": 0, "season": 0, "episode": 0}`)
	items := []model.Item{
		{Name: "video.mp4", Metadata: &rawMeta},
	}

	views := ProjectItems(items)
	b, _ := json.Marshal(views[0])
	// name is empty so t should be omitted (omitempty); zero values also omitted.
	for _, key := range []string{`"t"`, `"y"`, `"s"`, `"e"`, `"g"`, `"r"`} {
		if strings.Contains(string(b), key) {
			t.Errorf("All-zero metadata should not emit %s, got: %s", key, string(b))
		}
	}
}

func TestProjectItems_TitleEqualsFilename(t *testing.T) {
	rawMeta := json.RawMessage(`{"name": "SameAsFile"}`)
	items := []model.Item{
		{Name: "SameAsFile", Metadata: &rawMeta},
	}

	views := ProjectItems(items)
	b, _ := json.Marshal(views[0])
	if strings.Contains(string(b), `"t"`) {
		t.Errorf("t should be omitted when title equals filename, got: %s", string(b))
	}

	// But alt_name that differs should become the title
	rawMetaWithAlt := json.RawMessage(`{"name": "SameAsFile", "alt_name": "Different"}`)
	items[0].Metadata = &rawMetaWithAlt
	views = ProjectItems(items)
	b, _ = json.Marshal(views[0])
	if !strings.Contains(string(b), `"t":"Different"`) {
		t.Errorf("Expected alt_name as title, got: %s", string(b))
	}
}

func TestProjectItems_MovieWithSeason(t *testing.T) {
	// Classifier may tag movies with season/episode — pass through as-is.
	rawMeta := json.RawMessage(`{"name": "Film", "season": 1, "episode": 0}`)
	items := []model.Item{
		{Name: "Film.mp4", Metadata: &rawMeta},
	}

	views := ProjectItems(items)
	if views[0].Season != 1 {
		t.Errorf("Expected season 1, got %d", views[0].Season)
	}
	if views[0].Episode != 0 {
		t.Errorf("Expected episode 0, got %d", views[0].Episode)
	}
	// Episode 0 is zero-valued, so it won't appear in JSON (omitempty). That's fine.
}

func TestProjectItems_LongName(t *testing.T) {
	longName := strings.Repeat("x", 600)
	items := []model.Item{{Name: longName}}

	views := ProjectItems(items)
	if views[0].Name != longName {
		t.Errorf("Long name should pass through untruncated (len %d)", len(views[0].Name))
	}
}

func TestProjectItems_NonUTF8Name(t *testing.T) {
	// json.Marshal escapes invalid UTF-8 — this must not error.
	items := []model.Item{{Name: "valid\xffbroken"}}

	views := ProjectItems(items)
	b, _ := json.Marshal(views[0])
	// The invalid byte must be escaped.
	if !strings.Contains(string(b), `valid`) {
		t.Errorf("Name not found in output: %s", string(b))
	}
}

func TestButlerContextView_ExcludesSessionIdentity(t *testing.T) {
	c := model.ClientContext{
		SessionID:      "session-123",
		StartTime:      time.Now(),
		LastPlayedName: "Movie A",
		ViewingHistory: []model.ViewMetadata{
			{Name: "Episode 1", ViewedAt: time.Now(), PlayedForSec: "300"},
		},
	}

	payload := formatContext(c)
	if strings.Contains(payload, "session-123") {
		t.Error("Context payload must not contain SessionID")
	}
	if strings.Contains(payload, "startTime") {
		t.Error("Context payload must not contain StartTime")
	}
	if strings.Contains(payload, "sessionId") {
		t.Error("Context payload must not contain sessionId")
	}
	if !strings.Contains(payload, "Movie A") {
		t.Error("Context payload must contain LastPlayedName")
	}
	if !strings.Contains(payload, "Episode 1") {
		t.Error("Context payload must contain ViewingHistory")
	}
}

func TestFormatContext_NoIndentation(t *testing.T) {
	c := model.ClientContext{
		LastPlayedName: "Movie",
		ViewingHistory: []model.ViewMetadata{
			{Name: "Ep1", ViewedAt: time.Now(), PlayedForSec: "300"},
		},
	}
	payload := formatContext(c)
	if strings.Contains(payload, "  ") {
		t.Error("Context payload must not be indented")
	}
}

func TestProjectItems_StableOrdering(t *testing.T) {
	// Build a fixture and shuffle it 100 times; the sorted projection
	// (ignoring the Index field, which is input-order-dependent) must be
	// identical every time.
	rawMeta := json.RawMessage(`{"name":"Item"}`)
	items := []model.Item{
		{Path: "/z.mp4", Name: "z.mp4", Metadata: &rawMeta},
		{Path: "/a.mp4", Name: "a.mp4", Metadata: &rawMeta},
		{Path: "/m.mp4", Name: "m.mp4", Metadata: &rawMeta},
		{Path: "/c.mp4", Name: "c.mp4", Metadata: &rawMeta},
		{Path: "/b.mp4", Name: "b.mp4", Metadata: &rawMeta},
	}

	// Fingerprint the projection without Index (what computeLibraryFingerprint does).
	fingerprint := func(views []butlerItemView) string {
		var parts []string
		for _, v := range views {
			parts = append(parts, fmt.Sprintf("%s|%s|%d|%d|%d|%s|%d",
				v.Name, v.Title, v.Year, v.Season, v.Episode, v.Genre, v.Runtime))
		}
		return strings.Join(parts, "\n")
	}

	first := fingerprint(ProjectItems(items))
	for range 100 {
		shifted := make([]model.Item, len(items))
		for i := range items {
			shifted[i] = items[(i+1)%len(items)]
		}
		got := fingerprint(ProjectItems(shifted))
		if got != first {
			t.Fatal("ProjectItems fingerprint is not stable across input orderings")
		}
	}
}

func TestPrepSuggestions_UsesCompactFormat(t *testing.T) {
	ctx := context.Background()
	rawMeta := json.RawMessage(`{"name":"Test Movie","year":2023,"description":"A long plot that should not appear"}`)
	items := []model.Item{
		{Name: "test.mp4", Metadata: &rawMeta},
	}

	var capturedUserMsg string
	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			// Capture the user message for inspection
			for _, m := range chat.Messages {
				if m.Role == "user" {
					capturedUserMsg = m.Content
				}
			}
			if strings.Contains(chat.Messages[0].Content, "You are a media Butler") {
				return models.Chat{
					Messages: []models.Message{{Role: "assistant", Content: `[{"index":0,"description":"Test Movie","motivation":"test"}]`}},
				}, nil
			}
			return models.Chat{}, nil
		},
	}

	b := &butler{llm: mockLLM}
	_, _ = b.PrepSuggestions(ctx, model.ClientContext{}, items)

	// Key checks on the captured payload:
	if strings.Contains(capturedUserMsg, "  ") {
		t.Error("User message must not contain indentation")
	}
	if strings.Contains(capturedUserMsg, `"type"`) {
		t.Error("User message must not contain 'type' field")
	}
	// The prose metadata description field must not leak into the payload.
	// The word "description" in the user message only appears in the system prompt
	// context (which is the preamble), not as a metadata field.
	if strings.Contains(capturedUserMsg, `"A long plot`) {
		t.Error("User message must not contain prose description from metadata")
	}
	if !strings.Contains(capturedUserMsg, `"i":0`) {
		t.Error("User message must use short key 'i' for index")
	}
	if !strings.Contains(capturedUserMsg, `"n":"test.mp4"`) {
		t.Error("User message must use short key 'n' for name")
	}
	if !strings.Contains(capturedUserMsg, `"t":"Test Movie"`) {
		t.Error("User message must use short key 't' for title")
	}
}

// TestPrepSuggestions_FallbackQueryCount proves that when both fast paths miss —
// the butler returns descriptions without a valid index and the subtitle streams
// are non-English — the full fallback path is exercised: 1 butler query + 3
// semantic-indexer queries + 3 subtitle-selector queries = 7 total.
// This is the acceptance criterion for cross-phase check 2.
func TestPrepSuggestions_FallbackQueryCount(t *testing.T) {
	ctx := context.Background()
	items := []model.Item{
		{Name: "Movie A"},
		{Name: "Movie B"},
		{Name: "Movie C"},
	}

	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			sys := chat.Messages[0].Content
			switch {
			case strings.Contains(sys, "You are a media Butler"):
				// Return suggestions WITHOUT indices to force semantic-indexer fallback.
				return models.Chat{
					Messages: []models.Message{{Role: "assistant", Content: `[
						{"description": "Movie A", "motivation": "test"},
						{"description": "Movie B", "motivation": "test"},
						{"description": "Movie C", "motivation": "test"}
					]`}},
				}, nil
			case strings.Contains(sys, "Your job is to pick a media item"):
				// Semantic indexer fallback for each description.
				return models.Chat{
					Messages: []models.Message{{Role: "assistant", Content: `{"index": 0}`}},
				}, nil
			case strings.Contains(sys, "You are a media stream analyzer"):
				// Subtitle selector fallback for non-English streams.
				return models.Chat{
					Messages: []models.Message{{Role: "assistant", Content: `{"index": 0}`}},
				}, nil
			}
			return models.Chat{}, nil
		},
	}

	// Subtitle streams are non-English (Swedish) to force the selector past
	// the deterministic rankBest fast path and into the LLM fallback.
	mockSubs := &MockSubtitler{
		FindFunc: func(item model.Item) (model.MediaInfo, error) {
			return model.MediaInfo{
				Streams: []model.Stream{
					{Index: 0, CodecType: "subtitle", Tags: model.Tags{Language: "swe"}},
				},
			}, nil
		},
		ExtractFunc: func(item model.Item, streamIndex string) (string, error) {
			return "/tmp/subs.srt", nil
		},
	}

	// Use the real selector with the mock LLM so both deterministic and LLM
	// paths are exercised. The deterministic path returns (-1, false) for non-English.
	sel := NewSelector(models.Configurations{}).(*selector)
	sel.llm = mockLLM

	b := &butler{
		llm:      mockLLM,
		subs:     mockSubs,
		selector: sel,
	}

	recs, err := b.PrepSuggestions(ctx, model.ClientContext{}, items)
	if err != nil {
		t.Fatalf("PrepSuggestions failed: %v", err)
	}
	if len(recs) != 3 {
		t.Fatalf("Expected 3 suggestions, got %d", len(recs))
	}

	// Full fallback: 1 butler + 3 semantic indexer + 3 subtitle selector = 7.
	if qc := mockLLM.QueryCount(); qc != 7 {
		t.Errorf("Expected 7 LLM queries in full fallback (1 butler + 3 indexer + 3 selector), got %d", qc)
	}
}
