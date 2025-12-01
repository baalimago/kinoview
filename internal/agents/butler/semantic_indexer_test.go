package butler

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/model"
)

func TestSemanticIndexerSelect_ValidResponse(t *testing.T) {
	ctx := context.Background()
	items := []model.Item{
		{Name: "Movie A"},
		{Name: "Movie B"},
		{Name: "Movie C"},
	}

	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context,
			chat models.Chat,
		) (models.Chat, error) {
			return models.Chat{
				Messages: []models.Message{
					{
						Role:    "assistant",
						Content: `{"index": 1}`,
					},
				},
			}, nil
		},
	}

	b := &butler{llm: mockLLM}
	result, err := b.semanticIndexerSelect(ctx,
		suggestionResponse{Description: "Movie B"},
		items)
	if err != nil {
		t.Fatalf("semanticIndexerSelect failed: %v",
			err)
	}
	if result.Name != "Movie B" {
		t.Errorf("Expected Movie B, got %s",
			result.Name)
	}
}

func TestSemanticIndexerSelect_LLMError(t *testing.T) {
	ctx := context.Background()
	items := []model.Item{{Name: "Movie A"}}

	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context,
			chat models.Chat,
		) (models.Chat, error) {
			return models.Chat{},
				errors.New("llm query failed")
		},
	}

	b := &butler{llm: mockLLM}
	_, err := b.semanticIndexerSelect(ctx,
		suggestionResponse{}, items)

	if err == nil {
		t.Fatal("Expected error from LLM failure")
	}
	if !errors.Is(err, errors.New("llm query failed")) {
		if err.Error() != "failed to query llm: llm query failed" {
			t.Errorf("Unexpected error message: %v", err)
		}
	}
}

func TestSemanticIndexerSelect_EmptyResponse(t *testing.T) {
	ctx := context.Background()
	items := []model.Item{{Name: "Movie A"}}

	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context,
			chat models.Chat,
		) (models.Chat, error) {
			return models.Chat{
				Messages: []models.Message{},
			}, nil
		},
	}

	b := &butler{llm: mockLLM}
	_, err := b.semanticIndexerSelect(ctx,
		suggestionResponse{}, items)

	if err == nil {
		t.Fatal("Expected error on empty response")
	}
	if !errors.Is(err,
		errors.New("received empty response from llm")) {
		if err.Error() !=
			"received empty response from llm" {
			t.Errorf("Unexpected error: %v", err)
		}
	}
}

func TestSemanticIndexerSelect_MalformedJSON(t *testing.T) {
	ctx := context.Background()
	items := []model.Item{{Name: "Movie A"}}

	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context,
			chat models.Chat,
		) (models.Chat, error) {
			return models.Chat{
				Messages: []models.Message{
					{
						Role:    "assistant",
						Content: "not json at all",
					},
				},
			}, nil
		},
	}

	b := &butler{llm: mockLLM}
	_, err := b.semanticIndexerSelect(ctx,
		suggestionResponse{}, items)

	if err == nil {
		t.Fatal("Expected error on malformed JSON")
	}
}

func TestSemanticIndexerSelect_InvalidIndex(t *testing.T) {
	ctx := context.Background()
	items := []model.Item{{Name: "Movie A"}}

	tests := []struct {
		name  string
		index int
	}{
		{"negative index", -1},
		{"index out of bounds", 10},
		{"index equals length", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockLLM := &MockFullResponse{
				QueryFunc: func(ctx context.Context,
					chat models.Chat) (
					models.Chat, error,
				) {
					resp := `{"index": ` +
						string(rune(tt.index+48)) + `}`
					return models.Chat{
						Messages: []models.Message{
							{
								Role:    "assistant",
								Content: resp,
							},
						},
					}, nil
				},
			}

			b := &butler{llm: mockLLM}
			_, err := b.semanticIndexerSelect(ctx,
				suggestionResponse{}, items)

			if err == nil {
				t.Fatal("Expected error for " +
					tt.name)
			}
		})
	}
}

func TestSemanticIndexerSelect_JSONExtraction(t *testing.T) {
	ctx := context.Background()
	items := []model.Item{
		{Name: "Movie A"},
		{Name: "Movie B"},
	}

	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context,
			chat models.Chat,
		) (models.Chat, error) {
			// JSON embedded in text
			resp := "The best match is: " +
				`{"index": 1}` +
				" as shown in the analysis"
			return models.Chat{
				Messages: []models.Message{
					{
						Role:    "assistant",
						Content: resp,
					},
				},
			}, nil
		},
	}

	b := &butler{llm: mockLLM}
	result, err := b.semanticIndexerSelect(ctx,
		suggestionResponse{}, items)
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}
	if result.Name != "Movie B" {
		t.Errorf("Expected Movie B, got %s",
			result.Name)
	}
}

func TestSemanticIndexerSelect_MetadataFormatting(t *testing.T) {
	ctx := context.Background()
	rawMeta := json.RawMessage(
		`{"name":"Show Name","alt_name":"Alt Name",
		"year":2024,"season":2,"episode":6}`)

	items := []model.Item{
		{Name: "file1.mkv", Metadata: &rawMeta},
		{Name: "file2.mkv"},
	}

	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context,
			chat models.Chat,
		) (models.Chat, error) {
			// Verify formatted items in message
			userMsg := chat.Messages[1].Content
			if !contains(userMsg, "Show Name") {
				t.Error("Metadata name not in message")
			}
			if !contains(userMsg, "2024") {
				t.Error("Year not in message")
			}

			return models.Chat{
				Messages: []models.Message{
					{
						Role:    "assistant",
						Content: `{"index": 0}`,
					},
				},
			}, nil
		},
	}

	b := &butler{llm: mockLLM}
	result, err := b.semanticIndexerSelect(ctx,
		suggestionResponse{Description: "Show Name"},
		items)
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}
	if result.Name != "file1.mkv" {
		t.Errorf("Expected file1.mkv, got %s",
			result.Name)
	}
}

func TestSemanticIndexerSelect_InvalidMetadata(t *testing.T) {
	ctx := context.Background()
	badMeta := json.RawMessage(`{"invalid": json}`)
	items := []model.Item{
		{Name: "file.mkv", Metadata: &badMeta},
	}

	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context,
			chat models.Chat,
		) (models.Chat, error) {
			return models.Chat{
				Messages: []models.Message{
					{
						Role:    "assistant",
						Content: `{"index": 0}`,
					},
				},
			}, nil
		},
	}

	b := &butler{llm: mockLLM}
	result, err := b.semanticIndexerSelect(ctx,
		suggestionResponse{}, items)
	// Should still work, just skip bad metadata
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}
	if result.Name != "file.mkv" {
		t.Errorf("Expected file.mkv, got %s",
			result.Name)
	}
}

func TestSemanticIndexerSelect_SingleItem(t *testing.T) {
	ctx := context.Background()
	items := []model.Item{{Name: "Only Movie"}}

	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context,
			chat models.Chat,
		) (models.Chat, error) {
			return models.Chat{
				Messages: []models.Message{
					{
						Role:    "assistant",
						Content: `{"index": 0}`,
					},
				},
			}, nil
		},
	}

	b := &butler{llm: mockLLM}
	result, err := b.semanticIndexerSelect(ctx,
		suggestionResponse{}, items)
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}
	if result.Name != "Only Movie" {
		t.Errorf("Expected Only Movie, got %s",
			result.Name)
	}
}

func TestSemanticIndexerSelect_LargeList(t *testing.T) {
	ctx := context.Background()
	items := make([]model.Item, 100)
	for i := 0; i < 100; i++ {
		items[i] = model.Item{Name: "Movie " + string(rune(i))}
	}

	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context,
			chat models.Chat,
		) (models.Chat, error) {
			return models.Chat{
				Messages: []models.Message{
					{
						Role:    "assistant",
						Content: `{"index": 99}`,
					},
				},
			}, nil
		},
	}

	b := &butler{llm: mockLLM}
	result, err := b.semanticIndexerSelect(ctx,
		suggestionResponse{}, items)
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}
	if result.Name != "Movie c" {
		t.Errorf("Expected Movie c, got %s",
			result.Name)
	}
}

func TestSemanticIndexerSelect_NestedJSON(t *testing.T) {
	ctx := context.Background()
	items := []model.Item{{Name: "Movie A"}}

	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context,
			chat models.Chat,
		) (models.Chat, error) {
			// Response with nested braces
			resp := `{"outer": {"nested": true}, ` +
				`"index": 0}`
			return models.Chat{
				Messages: []models.Message{
					{
						Role:    "assistant",
						Content: resp,
					},
				},
			}, nil
		},
	}

	b := &butler{llm: mockLLM}
	result, err := b.semanticIndexerSelect(ctx,
		suggestionResponse{}, items)
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}
	if result.Name != "Movie A" {
		t.Errorf("Expected Movie A, got %s",
			result.Name)
	}
}

func TestSemanticIndexerSelect_SystemPromptInChat(t *testing.T) {
	ctx := context.Background()
	items := []model.Item{{Name: "Movie A"}}

	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context,
			chat models.Chat,
		) (models.Chat, error) {
			// Verify system prompt is set
			if len(chat.Messages) < 1 {
				t.Error("No messages in chat")
			}
			if chat.Messages[0].Role != "system" {
				t.Error("First message not system")
			}
			if !contains(chat.Messages[0].Content,
				"pick a media item") {
				t.Error("System prompt not set")
			}

			return models.Chat{
				Messages: []models.Message{
					{
						Role:    "assistant",
						Content: `{"index": 0}`,
					},
				},
			}, nil
		},
	}

	b := &butler{llm: mockLLM}
	_, err := b.semanticIndexerSelect(ctx,
		suggestionResponse{}, items)
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}
}

func TestSemanticIndexerSelect_UserMessageFormat(t *testing.T) {
	ctx := context.Background()
	items := []model.Item{{Name: "Movie A"}}
	desc := "Action movie from 2024"

	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context,
			chat models.Chat,
		) (models.Chat, error) {
			// Verify user message format
			if len(chat.Messages) < 2 {
				t.Error("Missing user message")
			}
			userMsg := chat.Messages[1].Content
			if !contains(userMsg,
				"Semantic description:") {
				t.Error("Missing description label")
			}
			if !contains(userMsg, desc) {
				t.Error("Description not in message")
			}
			if !contains(userMsg, "Media list:") {
				t.Error("Missing media list label")
			}

			return models.Chat{
				Messages: []models.Message{
					{
						Role:    "assistant",
						Content: `{"index": 0}`,
					},
				},
			}, nil
		},
	}

	b := &butler{llm: mockLLM}
	_, err := b.semanticIndexerSelect(ctx,
		suggestionResponse{Description: desc},
		items)
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}
}

func TestSemanticIndexerSelect_FallbackToLastMessage(t *testing.T) {
	ctx := context.Background()
	items := []model.Item{{Name: "Movie A"}}

	mockLLM := &MockFullResponse{
		QueryFunc: func(ctx context.Context,
			chat models.Chat,
		) (models.Chat, error) {
			// Return non-assistant role, should fallback
			return models.Chat{
				Messages: []models.Message{
					{
						Role:    "user",
						Content: `{"index": 0}`,
					},
					{
						Role:    "system",
						Content: `{"index": 0}`,
					},
				},
			}, nil
		},
	}

	b := &butler{llm: mockLLM}
	result, err := b.semanticIndexerSelect(ctx,
		suggestionResponse{}, items)
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}
	if result.Name != "Movie A" {
		t.Errorf("Expected Movie A, got %s",
			result.Name)
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestSemanticIndexerSelect_Errors(t *testing.T) {
	ctx := context.Background()
	items := []model.Item{{Name: "Movie A"}}
	b := &butler{
		llm: &MockFullResponse{
			QueryFunc: func(ctx context.Context, chat models.Chat) (models.Chat, error) {
				return models.Chat{}, errors.New("llm fail")
			},
		},
	}
	_, err := b.semanticIndexerSelect(ctx, suggestionResponse{}, items)
	if err == nil {
		t.Error("Expected error when LLM fails")
	}

	// Empty response
	b.llm.(*MockFullResponse).QueryFunc = func(ctx context.Context, chat models.Chat) (models.Chat, error) {
		return models.Chat{Messages: []models.Message{}}, nil
	}
	_, err = b.semanticIndexerSelect(ctx, suggestionResponse{}, items)
	if err == nil {
		t.Error("Expected error on empty response")
	}

	// Invalid JSON
	b.llm.(*MockFullResponse).QueryFunc = func(ctx context.Context, chat models.Chat) (models.Chat, error) {
		return models.Chat{Messages: []models.Message{{Role: "assistant", Content: "bad"}}}, nil
	}
	_, err = b.semanticIndexerSelect(ctx, suggestionResponse{}, items)
	if err == nil {
		t.Error("Expected error on invalid json")
	}

	// Invalid Index
	b.llm.(*MockFullResponse).QueryFunc = func(ctx context.Context, chat models.Chat) (models.Chat, error) {
		return models.Chat{Messages: []models.Message{{Role: "assistant", Content: `{"index": 10}`}}}, nil
	}
	_, err = b.semanticIndexerSelect(ctx, suggestionResponse{}, items)
	if err == nil {
		t.Error("Expected error on invalid index")
	}
}
