package concierge

import (
	"slices"
	"strings"
	"testing"

	"github.com/baalimago/kinoview/internal/agents/slivingdoc"
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
			},
		},
		{
			name: "missing metadata manager",
			opts: []ConciergeOption{
				WithItemGetter(ig),
				WithSuggestionManager(sm),
				WithSubtitleManager(subm),
			},
		},
		{
			name: "missing suggestion manager",
			opts: []ConciergeOption{
				WithItemGetter(ig),
				WithMetadataManager(mm),
				WithSubtitleManager(subm),
			},
		},
		{
			name: "missing subtitle manager",
			opts: []ConciergeOption{
				WithItemGetter(ig),
				WithMetadataManager(mm),
				WithSuggestionManager(sm),
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
	ucm := &mockUserContextManager{}

	c, err := New(
		WithItemGetter(ig),
		WithItemLister(nil), // explicit nil should be OK
		WithMetadataManager(mm),
		WithSuggestionManager(sm),
		WithSubtitleManager(subm),
		WithModel("gpt-5"),
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

	c, err := New(
		WithItemGetter(igl),
		WithMetadataManager(mm),
		WithSuggestionManager(sm),
		WithSubtitleManager(subm),
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
	t.Setenv("OPENAI_API_KEY", "somevalue")
	ig := &mockItemGetter{}
	mm := &mockMetadataManager{}
	sm := &mockSuggestionManager{}
	subm := &mockSubtitleManager{}

	c, err := New(
		WithConfigDir(t.TempDir()),
		WithItemGetter(ig),
		WithMetadataManager(mm),
		WithSuggestionManager(sm),
		WithSubtitleManager(subm),
		WithModel("gpt-5"),
	)
	if err != nil {
		t.Fatalf("failed to create concierge: %v", err)
	}

	ctx := t.Context()

	if err := c.Setup(ctx); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
}

func TestConcierge_Setup_WithUserContextManager(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "somevalue")
	ig := &mockItemGetter{}
	mm := &mockMetadataManager{}
	sm := &mockSuggestionManager{}
	subm := &mockSubtitleManager{}
	ucm := &mockUserContextManager{}

	c, err := New(
		WithConfigDir(t.TempDir()),
		WithItemGetter(ig),
		WithMetadataManager(mm),
		WithSuggestionManager(sm),
		WithSubtitleManager(subm),
		WithUserContextManager(ucm),
		WithModel("gpt-5"),
	)
	if err != nil {
		t.Fatalf("failed to create concierge: %v", err)
	}

	ctx := t.Context()

	if err := c.Setup(ctx); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
}

// With the slivingdoc callsign configured, the concierge applies the shared
// tool globs (callsign + file tools) and the full constructor wires the
// callsign without error.
func TestConcierge_ToolGlobsIncludeSlivingdoc(t *testing.T) {
	server := slivingdoc.Server("slivingdoc", "b", "r", "http://127.0.0.1:8333", "/cache/slivingdoc", "/priv")
	c := concierge{}
	WithSlivingdocServer(server)(&c)
	WithSlivingdocWorkspace("/cache/slivingdoc")(&c)

	if !c.notebookEnabled() {
		t.Fatal("expected notebook enabled with a configured server")
	}
	if got := c.notebookGlobs(); !slices.Equal(got, slivingdoc.ToolGlobs()) {
		t.Errorf("notebookGlobs = %v, want %v", got, slivingdoc.ToolGlobs())
	}

	ig := &mockItemGetter{}
	mm := &mockMetadataManager{}
	sm := &mockSuggestionManager{}
	subm := &mockSubtitleManager{}
	if _, err := New(
		WithItemGetter(ig),
		WithMetadataManager(mm),
		WithSuggestionManager(sm),
		WithSubtitleManager(subm),
		WithModel("gpt-5"),
		WithSlivingdocServer(server),
		WithSlivingdocWorkspace("/cache/slivingdoc"),
	); err != nil {
		t.Fatalf("New with slivingdoc server: %v", err)
	}
}

// With a zero server the notebook is disabled: no globs, no NOTES prompt
// section, and the concierge keeps its explicit file tools (the no-server
// construction path stays byte-for-byte the pre-notebook toolset).
func TestConcierge_NoServerOmitsSlivingdoc(t *testing.T) {
	c := concierge{}
	if c.notebookEnabled() {
		t.Fatal("expected notebook disabled with a zero server")
	}
	if got := c.notebookGlobs(); got != nil {
		t.Errorf("notebookGlobs = %v, want nil", got)
	}
	prompt := c.buildPrompt(false)
	if strings.Contains(prompt, "NOTES") || strings.Contains(prompt, "mcp_slivingdoc") {
		t.Errorf("no-server prompt must omit the notebook section:\n%s", prompt)
	}
	if !strings.Contains(prompt, "rows_between") {
		t.Errorf("no-server prompt must keep the subtitle-validation workflow:\n%s", prompt)
	}
	if !strings.Contains(c.buildPrompt(true), "OPENSUBTITLES FALLBACK") {
		t.Error("expected the OpenSubtitles addendum with the fetch tool live")
	}
}

// The NOTES prompt section names the exact workspace path — from the explicit
// option, or read back from the callsign args when the option is empty — so
// the model is never asked to guess where the notebook lives.
func TestConcierge_PromptNamesWorkspace(t *testing.T) {
	server := slivingdoc.Server("slivingdoc", "b", "r", "http://127.0.0.1:8333", "/cache/slivingdoc", "/priv")

	t.Run("explicit workspace option", func(t *testing.T) {
		c := concierge{slivingdocServer: server, slivingdocWorkspace: "/cache/slivingdoc"}
		prompt := c.buildPrompt(false)
		if !strings.Contains(prompt, "Pull the shared notebook into /cache/slivingdoc before you start.") {
			t.Errorf("prompt must name the workspace:\n%s", prompt)
		}
		if !strings.Contains(prompt, "Commit with mcp_slivingdoc_notes_commit with path /cache/slivingdoc when done.") {
			t.Errorf("prompt must name the commit step with the workspace:\n%s", prompt)
		}
	})

	t.Run("workspace derived from callsign args", func(t *testing.T) {
		c := concierge{slivingdocServer: server}
		if got := c.notebookWorkspace(); got != "/cache/slivingdoc" {
			t.Errorf("notebookWorkspace = %q, want %q", got, "/cache/slivingdoc")
		}
		if !strings.Contains(c.buildPrompt(false), "/cache/slivingdoc") {
			t.Error("derived prompt must name the workspace")
		}
	})
}
