package classifier

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/go_away_boilerplate/pkg/testboil"
	"github.com/baalimago/kinoview/internal/agents"
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

func TestClone_nonAgent(t *testing.T) {
	// Create a non-agent classifier via New()
	orig := New(models.Configurations{
		Model:     "gpt-5",
		ConfigDir: "/tmp/test",
	}).(*classifier)

	clone := orig.Clone().(*classifier)

	// Fields should match
	if clone.model != orig.model {
		t.Fatalf("model mismatch: %q vs %q", clone.model, orig.model)
	}
	if clone.configDir != orig.configDir {
		t.Fatalf("configDir mismatch: %q vs %q", clone.configDir, orig.configDir)
	}
	if clone.usesAgent != orig.usesAgent {
		t.Fatalf("usesAgent mismatch: %v vs %v", clone.usesAgent, orig.usesAgent)
	}

	// Clone should have independent conf (not shared pointer)
	if clone.conf == orig.conf {
		t.Fatal("clone.conf should not be same pointer as orig.conf")
	}
	if clone.conf.Out != nil {
		t.Fatal("clone.conf.Out should be nil")
	}

	// Clone should have independent llm (not shared pointer)
	if clone.llm == orig.llm {
		t.Fatal("clone.llm should not be same pointer as orig.llm")
	}
}

func TestClone_agentPreservesTools(t *testing.T) {
	orig := NewWithTools(models.Configurations{
		Model:     "gpt-5",
		ConfigDir: "/tmp/test",
		InternalTools: []models.ToolName{
			models.CatTool,
			models.FindTool,
		},
	}, []models.LLMTool{}).(*classifier)

	clone := orig.Clone().(*classifier)

	if clone.model != orig.model {
		t.Fatalf("model mismatch: %q vs %q", clone.model, orig.model)
	}
	if !clone.usesAgent {
		t.Fatal("clone should be agent-type")
	}
	if clone.conf == orig.conf {
		t.Fatal("clone.conf should not be same pointer")
	}
	if len(clone.conf.InternalTools) != len(orig.conf.InternalTools) {
		t.Fatalf("InternalTools length mismatch: %d vs %d", len(clone.conf.InternalTools), len(orig.conf.InternalTools))
	}
	// Verify internal tools were copied
	for i, tool := range orig.conf.InternalTools {
		if clone.conf.InternalTools[i] != tool {
			t.Fatalf("InternalTools[%d] mismatch: %q vs %q", i, clone.conf.InternalTools[i], tool)
		}
	}
}

func TestClone_independentState(t *testing.T) {
	orig := New(models.Configurations{
		Model:     "gpt-5",
		ConfigDir: "/tmp/test",
	}).(*classifier)

	clone := orig.Clone().(*classifier)

	// Mutate orig's conf — clone should be unaffected
	orig.conf.Out = &bytes.Buffer{}
	if clone.conf.Out != nil {
		t.Fatal("clone.conf.Out was mutated by orig.conf.Out change")
	}

	// Mutate clone's conf — orig should be unaffected
	buf := &bytes.Buffer{}
	clone.conf.Out = buf
	if orig.conf.Out != nil && orig.conf.Out == clone.conf.Out {
		t.Fatal("orig.conf.Out was mutated by clone.conf.Out change")
	}
}

func TestClone_returnsClassifier(t *testing.T) {
	orig := New(models.Configurations{
		Model:     "gpt-5",
		ConfigDir: "/tmp/test",
	})

	clone := orig.Clone()
	if clone == nil {
		t.Fatal("Clone returned nil")
	}

	// Verify it implements the interface (compile-time check)
	var _ agents.Classifier = clone

	// Verify it can classify independently
	ctx := context.Background()
	item := model.Item{ID: "test", Name: "test"}
	_, err := clone.Classify(ctx, item)
	// Will error because llm isn't set up, but shouldn't panic
	if err == nil {
		t.Log("Classify succeeded (unexpected but not a failure)")
	}
}

func TestClone_nonAgent_setOutputDoesNotAffectOriginal(t *testing.T) {
	orig := New(models.Configurations{
		Model:     "gpt-5",
		ConfigDir: "/tmp/test",
	})

	clone := orig.Clone()

	// Set output on clone
	cloneBuf := &bytes.Buffer{}
	if err := clone.(*classifier).SetOutput(cloneBuf); err != nil {
		t.Fatalf("SetOutput on clone failed: %v", err)
	}

	// Original should be unaffected
	origCls := orig.(*classifier)
	if origCls.conf.Out != nil {
		t.Fatal("orig.conf.Out was mutated by clone.SetOutput")
	}
	if origCls.llm == clone.(*classifier).llm {
		t.Fatal("orig.llm should differ from clone.llm after SetOutput")
	}
}

func TestClassify(t *testing.T) {
	ctx := context.Background()

	t.Run("successful classification", func(t *testing.T) {
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

	t.Run("LLM query error", func(t *testing.T) {
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

	t.Run("no system message", func(t *testing.T) {
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

	t.Run("invalid braces", func(t *testing.T) {
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

	t.Run("mismatched braces", func(t *testing.T) {
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

	t.Run("invalid JSON", func(t *testing.T) {
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

	t.Run("complex JSON extraction", func(t *testing.T) {
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
