package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/go_away_boilerplate/pkg/testboil"
	"github.com/baalimago/kinoview/internal/model"
)

type mockLLM struct {
	queryFunc func(context.Context, models.Chat) (models.Chat, error)
	setupFunc func(context.Context) error
}

func (m *mockLLM) Query(ctx context.Context, c models.Chat) (models.Chat, error) {
	if m.queryFunc != nil {
		return m.queryFunc(ctx, c)
	}
	return models.Chat{}, nil
}

func (m *mockLLM) Setup(ctx context.Context) error {
	if m.setupFunc != nil {
		return m.setupFunc(ctx)
	}
	return nil
}

func TestClassify(t *testing.T) {
	ctx := context.Background()

	t.Run("successful_classification", func(t *testing.T) {
		expectedJSON := `{"name":"Test Movie","year":2023,"actors":["Actor One"]}`
		mockLLM := &mockLLM{
			queryFunc: func(ctx context.Context, c models.Chat) (models.Chat, error) {
				return models.Chat{
					Messages: []models.Message{
						{Role: "system", Content: expectedJSON},
					},
				}, nil
			},
		}

		c := &classifier{llm: mockLLM}
		input := model.Item{
			ID:   "test_id",
			Path: "/test/path",
			Name: "test_movie.mp4",
		}

		result, err := c.Classify(ctx, input)
		if err != nil {
			t.Fatalf("didnt expect error: %v", err)
		}

		testboil.FailTestIfDiff(t, input.ID, result.ID)
		testboil.FailTestIfDiff(t, input.Path, result.Path)
		testboil.FailTestIfDiff(t, input.Name, result.Name)

		metadata := result.Metadata
		testboil.FailTestIfDiff(t, expectedJSON, string(*metadata))
	})

	t.Run("llm_query_error", func(t *testing.T) {
		mockLLM := &mockLLM{
			queryFunc: func(ctx context.Context, c models.Chat) (models.Chat, error) {
				return models.Chat{}, errors.New("llm error")
			},
		}

		c := &classifier{llm: mockLLM}
		input := model.Item{ID: "test_id"}

		_, err := c.Classify(ctx, input)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
	})

	t.Run("no_system_message", func(t *testing.T) {
		mockLLM := &mockLLM{
			queryFunc: func(ctx context.Context, c models.Chat) (models.Chat, error) {
				return models.Chat{
					Messages: []models.Message{
						{Role: "user", Content: "not system"},
					},
				}, nil
			},
		}

		c := &classifier{llm: mockLLM}
		input := model.Item{ID: "test_id"}

		_, err := c.Classify(ctx, input)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
	})

	t.Run("invalid_braces", func(t *testing.T) {
		mockLLM := &mockLLM{
			queryFunc: func(ctx context.Context, c models.Chat) (models.Chat, error) {
				return models.Chat{
					Messages: []models.Message{
						{Role: "system", Content: "no braces here"},
					},
				}, nil
			},
		}

		c := &classifier{llm: mockLLM}
		input := model.Item{ID: "test_id"}

		_, err := c.Classify(ctx, input)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
	})

	t.Run("mismatched_braces", func(t *testing.T) {
		mockLLM := &mockLLM{
			queryFunc: func(ctx context.Context, c models.Chat) (models.Chat, error) {
				return models.Chat{
					Messages: []models.Message{
						{Role: "system", Content: "{{missing close"},
					},
				}, nil
			},
		}

		c := &classifier{llm: mockLLM}
		input := model.Item{ID: "test_id"}

		_, err := c.Classify(ctx, input)

		if err == nil {
			t.Fatalf("expected error, got nil")
		}
	})

	t.Run("invalid_json", func(t *testing.T) {
		mockLLM := &mockLLM{
			queryFunc: func(ctx context.Context, c models.Chat) (models.Chat, error) {
				return models.Chat{
					Messages: []models.Message{
						{Role: "system", Content: "{invalid: json,}"},
					},
				}, nil
			},
		}

		c := &classifier{llm: mockLLM}
		input := model.Item{ID: "test_id"}

		_, err := c.Classify(ctx, input)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
	})

	t.Run("complex_json_extraction", func(t *testing.T) {
		content := `Here is the classification: {"name":"Complex Movie","year":2024} and some extra text`
		mockLLM := &mockLLM{
			queryFunc: func(ctx context.Context, c models.Chat) (models.Chat, error) {
				return models.Chat{
					Messages: []models.Message{
						{Role: "system", Content: content},
					},
				}, nil
			},
		}

		c := &classifier{llm: mockLLM}
		input := model.Item{ID: "test_id"}

		result, err := c.Classify(ctx, input)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		metadata := result.Metadata
		expectedJSON := `{"name":"Complex Movie","year":2024}`
		testboil.FailTestIfDiff(t, expectedJSON, string(*metadata))
	})
}
