package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/baalimago/clai/pkg/text/models"
)

type conciergeContextGetTool struct {
	cfg conciergeContextConfig
}

func NewConciergeContextGet(opts ...ConciergeContextOption) (*conciergeContextGetTool, error) {
	t := &conciergeContextGetTool{}
	for _, o := range opts {
		o(&t.cfg)
	}
	return t, nil
}

func (t *conciergeContextGetTool) Call(input models.Input) (string, error) {
	mode, _ := input["mode"].(string)
	if mode == "" {
		mode = "summary"
	}

	limit := 5
	if v, ok := input["limit"].(float64); ok {
		if int(v) > 0 {
			limit = int(v)
		}
	}
	offset := 0
	if v, ok := input["offset"].(float64); ok {
		if int(v) >= 0 {
			offset = int(v)
		}
	}
	idFilter, _ := input["id"].(string)

	dir, err := conciergeCacheDir(t.cfg.cacheDir)
	if err != nil {
		return "", err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "no concierge context has been stored", nil
		}
		return "", fmt.Errorf("failed to read concierge cache dir: %w", err)
	}

	notes := make([]conciergeNote, 0)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var n conciergeNote
		if err := json.Unmarshal(b, &n); err != nil {
			continue
		}
		if n.ID == "" {
			n.ID = strings.TrimSuffix(name, ".json")
		}
		notes = append(notes, n)
	}

	if len(notes) == 0 {
		return "no concierge context has been stored", nil
	}

	sort.SliceStable(notes, func(i, j int) bool {
		return notes[i].CreatedAt.After(notes[j].CreatedAt)
	})

	if idFilter != "" {
		filtered := make([]conciergeNote, 0, len(notes))
		for _, n := range notes {
			if n.ID == idFilter {
				filtered = append(filtered, n)
			}
		}
		notes = filtered
		if len(notes) == 0 {
			return fmt.Sprintf("no concierge note found for id=%q", idFilter), nil
		}
	}

	if offset >= len(notes) {
		return "no results (offset past available notes)", nil
	}
	notes = notes[offset:]
	if limit > len(notes) {
		limit = len(notes)
	}
	notes = notes[:limit]

	switch mode {
	case "most_recent":
		return renderConciergeNotes(notes[:1], true), nil
	case "summary":
		return renderConciergeNotes(notes, false), nil
	case "full":
		return renderConciergeNotes(notes, true), nil
	default:
		return "unknown mode; valid: most_recent, summary, full", nil
	}
}

func (t *conciergeContextGetTool) Specification() models.Specification {
	return models.Specification{
		Name:        "concierge_context_get",
		Description: "Inspect previously persisted concierge notes (motivations/thoughts/decisions) to improve future runs.",
		Inputs: &models.InputSchema{
			Type:     "object",
			Required: make([]string, 0),
			Properties: map[string]models.ParameterObject{
				"mode": {
					Type:        "string",
					Description: "What to return: 'most_recent', 'summary' (default), or 'full'.",
				},
				"limit": {
					Type:        "integer",
					Description: "Max number of notes to return (default 5).",
				},
				"offset": {
					Type:        "integer",
					Description: "Offset into the ordered notes (default 0).",
				},
				"id": {
					Type:        "string",
					Description: "Filter results to a specific note id.",
				},
			},
		},
	}
}

func renderConciergeNotes(notes []conciergeNote, includeBody bool) string {
	var b strings.Builder
	for i, n := range notes {
		if i > 0 {
			b.WriteString("\n")
		}
		at := n.CreatedAt
		if at.IsZero() {
			at = time.Time{}
		}
		b.WriteString(fmt.Sprintf("- id=%s, time=%s\n", safe(n.ID), at.Format(time.RFC3339)))
		if includeBody {
			b.WriteString(fmt.Sprintf("  note: %s\n", n.Note))
		}
	}
	return strings.TrimSpace(b.String())
}
