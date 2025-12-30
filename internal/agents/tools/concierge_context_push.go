package tools

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/baalimago/clai/pkg/text/models"
)

type conciergeContextPushTool struct {
	cfg conciergeContextConfig
}

type conciergeNote struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Note      string    `json:"note"`
}

func NewConciergeContextPush(opts ...ConciergeContextOption) (*conciergeContextPushTool, error) {
	t := &conciergeContextPushTool{}
	for _, o := range opts {
		o(&t.cfg)
	}
	return t, nil
}

func (t *conciergeContextPushTool) Call(input models.Input) (string, error) {
	note, ok := input["note"].(string)
	if !ok {
		return "", fmt.Errorf("note must be a string")
	}
	note = sanitizeNote(note)
	if note == "" {
		return "", errors.New("note can't be empty")
	}

	dir, err := conciergeCacheDir(t.cfg.cacheDir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create cache dir: %w", err)
	}

	n := conciergeNote{
		ID:        newID(),
		CreatedAt: time.Now().UTC(),
		Note:      note,
	}

	b, err := json.MarshalIndent(n, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal note: %w", err)
	}

	p := filepath.Join(dir, fmt.Sprintf("%s.json", n.ID))
	if err := os.WriteFile(p, b, 0o644); err != nil {
		return "", fmt.Errorf("failed to persist note: %w", err)
	}

	return fmt.Sprintf("stored concierge note id=%s at %s", n.ID, p), nil
}

func (t *conciergeContextPushTool) Specification() models.Specification {
	return models.Specification{
		Name:        "concierge_context_push",
		Description: "Persist a concierge note (motivations/thoughts/decisions) for later review and improvement across runs.",
		Inputs: &models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterObject{
				"note": {
					Type:        "string",
					Description: "The concierge's motivations/thoughts/decisions to persist for future runs.",
				},
			},
			Required: []string{"note"},
		},
	}
}

func sanitizeNote(s string) string {
	return strings.TrimSpace(s)
}

func newID() string {
	var b [16]byte
	_, err := rand.Read(b[:])
	if err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
