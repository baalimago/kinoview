package butler

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/model"
)

// MockFullResponse mocks the text.FullResponse interface
type MockFullResponse struct {
	SetupFunc func(ctx context.Context) error
	QueryFunc func(ctx context.Context, chat models.Chat) (models.Chat, error)
}

func (m *MockFullResponse) Setup(ctx context.Context) error {
	if m.SetupFunc != nil {
		return m.SetupFunc(ctx)
	}
	return nil
}

func (m *MockFullResponse) Query(ctx context.Context, chat models.Chat) (models.Chat, error) {
	if m.QueryFunc != nil {
		return m.QueryFunc(ctx, chat)
	}
	return models.Chat{}, nil
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

func (m *MockSubtitler) Extract(item model.Item, streamIndex string) (string, error) {
	if m.ExtractFunc != nil {
		return m.ExtractFunc(item, streamIndex)
	}
	return "", nil
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
	// Expect partial success (recommendation returned without subs) or wrapped error?
	// Looking at code:
	// err = b.preloadSubs(ctx, item, &rec)
	// if err != nil { return rec, fmt.Errorf("failed to preloadSubs: %w", err) }
	// So it returns error.
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
	// Extract error is NOT wrapped in PreloadSubsError in current code?
	// preloadSubs returns fmt.Errorf("failed to extract subs...")
	// prepSuggestion wraps it in "failed to preloadSubs: ..."
	if errors.As(err, &psErr) {
		t.Errorf("Did not expect PreloadSubsError for extract failure (based on code logic check)")
	}
}

func TestSelector_SelectEnglish(t *testing.T) {
	ctx := context.Background()
	// Test the real selector logic using a mock LLM
	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			return models.Chat{
				Messages: []models.Message{
					{Role: "assistant", Content: `{"index": 1}`},
				},
			}, nil
		},
	}

	s := &selector{
		llm: mockLLM,
	}

	streams := []model.Stream{
		{Index: 0, CodecType: "video"},
		{Index: 1, CodecType: "subtitle", Tags: model.Tags{Title: "English"}},
	}

	idx, err := s.Select(ctx, streams)
	if err != nil {
		t.Fatalf("SelectEnglish failed: %v", err)
	}
	if idx != 1 {
		t.Errorf("Expected index 1, got %d", idx)
	}

	// Test no subtitles
	_, err = s.Select(ctx, []model.Stream{{Index: 0, CodecType: "video"}})
	if err == nil {
		t.Error("Expected error when no subtitles found")
	}

	// Test LLM error
	mockLLM.QueryFunc = func(ctx context.Context, chat models.Chat) (models.Chat, error) {
		return models.Chat{}, errors.New("llm error")
	}
	_, err = s.Select(ctx, streams)
	if err == nil {
		t.Error("Expected error when LLM fails")
	}

	// Test LLM returns error JSON
	mockLLM.QueryFunc = func(ctx context.Context, chat models.Chat) (models.Chat, error) {
		return models.Chat{
			Messages: []models.Message{
				{Role: "assistant", Content: `{"error": "no subs"}`},
			},
		}, nil
	}
	_, err = s.Select(ctx, streams)
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
			// Simply return valid JSON for whatever prompt
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
			if strings.Contains(chat.Messages[0].Content, "subtitle stream") { // selector prompt
				return models.Chat{
					Messages: []models.Message{{Role: "assistant", Content: `{"index": 0}`}},
				}, nil
			}

			return models.Chat{}, nil
		},
	}

	b := &butler{
		llm:      mockLLM,
		subs:     nil,                                  // skip subs
		selector: NewSelector(models.Configurations{}), // use real selector structure but injecting mock into it
	}
	// Inject mock into selector
	sel := b.selector.(*selector)
	sel.llm = mockLLM

	// Call PrepSuggestions to trigger the debug print
	_, _ = b.PrepSuggestions(ctx, model.ClientContext{}, items)

	// Call Selector english
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
				// Return NON-assistant role. This tests fallback in PrepSuggestions
				return models.Chat{
					Messages: []models.Message{
						{Role: "system", Content: `[{"description":"fallback", "motivation":"test"}]`},
					},
				}, nil
			}
			if strings.Contains(msg, "Your job is to pick") {
				// Semantic indexer needs valid response.
				// Testing fallback there too: return non-assistant role
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
	idx, err := s.Select(ctx, []model.Stream{
		{Index: 0, CodecType: "subtitle", Tags: model.Tags{Title: "English"}},
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
			var fullMsg string
			for _, m := range chat.Messages {
				fullMsg += m.Content + "\n"
			}

			if strings.Contains(fullMsg, "You are a media Butler") {
				return models.Chat{
					Messages: []models.Message{
						{Role: "assistant", Content: `[
                             {"description":"valid", "motivation":"test"},
                             {"description":"invalid", "motivation":"test"}
                         ]`},
					},
				}, nil
			}
			if strings.Contains(fullMsg, "Semantic description: valid") {
				return models.Chat{
					Messages: []models.Message{{Role: "assistant", Content: `{"index": 0}`}},
				}, nil
			}
			// For "invalid", return garbage JSON for semantic indexer
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
		// One valid, one invalid.
		t.Errorf("Expected 1 valid rec, got %d", len(recs))
	}
}

func TestSelector_EmptyResponse(t *testing.T) {
	ctx := context.Background()
	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
			return models.Chat{Messages: []models.Message{}}, nil
		},
	}
	s := &selector{llm: mockLLM}
	// Need at least one subtitle stream
	streams := []model.Stream{{CodecType: "subtitle", Index: 0}}
	_, err := s.Select(ctx, streams)
	if err == nil {
		t.Fatal("Expected error on empty response")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Errorf("Expected 'empty response' error, got %v", err)
	}
}
