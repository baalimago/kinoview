package recommender

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/model"
)

type fakeLLM struct {
	resp models.Chat
	err  error
	got  models.Chat
}

func (f *fakeLLM) Setup(ctx context.Context) error { return nil }

func (f *fakeLLM) Query(
	ctx context.Context,
	chat models.Chat,
) (models.Chat, error) {
	f.got = chat
	return f.resp, f.err
}

func TestRecommend_OK(t *testing.T) {
	f := &fakeLLM{
		resp: models.Chat{
			Messages: []models.Message{
				{
					Role:    "system",
					Content: `{"mediaId":"2"}`,
				},
			},
		},
	}
	r := &recommender{llm: f}
	items := []model.Item{
		{ID: "1", Name: "One", MIMEType: "video/mp4"},
		{ID: "2", Name: "Two", MIMEType: "video/mp4"},
	}
	got, err := r.Recommend(context.Background(), "play", items)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.ID != "2" {
		t.Fatalf("got id %q want %q", got.ID, "2")
	}
	if len(f.got.Messages) == 0 {
		t.Fatalf("no messages sent to llm")
	}
	sys := f.got.Messages[0]
	if sys.Role != "system" {
		t.Fatalf("role %q want %q", sys.Role, "system")
	}
	if !strings.Contains(sys.Content, "Request: 'play'") {
		t.Fatalf("prompt missing request: %q", sys.Content)
	}
	if !strings.Contains(sys.Content, "Media:") {
		t.Fatalf("prompt missing media header")
	}
	if !strings.Contains(sys.Content, "- id: 1") {
		t.Fatalf("prompt missing item 1")
	}
	if !strings.Contains(sys.Content, "- id: 2") {
		t.Fatalf("prompt missing item 2")
	}
}

func TestRecommend_LLMError(t *testing.T) {
	f := &fakeLLM{err: errors.New("boom")}
	r := &recommender{llm: f}
	_, err := r.Recommend(context.Background(), "x", nil)
	if err == nil {
		t.Fatalf("want error, got nil")
	}
}

func TestRecommend_ParseError(t *testing.T) {
	f := &fakeLLM{
		resp: models.Chat{
			Messages: []models.Message{
				{Role: "system", Content: "no json here"},
			},
		},
	}
	r := &recommender{llm: f}
	_, err := r.Recommend(context.Background(), "x", nil)
	if err == nil {
		t.Fatalf("want error, got nil")
	}
}

func TestRecommend_IDNotFound(t *testing.T) {
	f := &fakeLLM{
		resp: models.Chat{
			Messages: []models.Message{
				{Role: "system", Content: `{"mediaId":"404"}`},
			},
		},
	}
	r := &recommender{llm: f}
	items := []model.Item{
		{ID: "a", Name: "A", MIMEType: "video/mp4"},
	}
	_, err := r.Recommend(context.Background(), "x", items)
	if err == nil {
		t.Fatalf("want error, got nil")
	}
}

func TestExtractMediaID(t *testing.T) {
	tests := []struct {
		name string
		s    string
		id   string
		ok   bool
	}{
		{
			name: "simple",
			s:    `{"mediaId":"abc123"}`,
			id:   "abc123",
			ok:   true,
		},
		{
			name: "whitespace_and_newlines",
			s: `Answer:
  {
    "mediaId": "xyz-789"
  }`,
			id: "xyz-789",
			ok: true,
		},
		{
			name: "embedded_text",
			s:    `prefix {"mediaId": "v42"} suffix`,
			id:   "v42",
			ok:   true,
		},
		{
			name: "missing_key",
			s:    `{"id":"abc"}`,
			ok:   false,
		},
		{
			name: "wrong_case_key",
			s:    `{"MediaId":"abc"}`,
			ok:   false,
		},
		{
			name: "unquoted_value",
			s:    `{"mediaId": 123}`,
			ok:   false,
		},
		{
			name: "missing_colon",
			s:    `{"mediaId" "abc"}`,
			ok:   false,
		},
		{
			name: "spaces_around_colon",
			s:    `{"mediaId"   :    "spaced"}`,
			id:   "spaced",
			ok:   true,
		},
		{
			name: "multiple_ids_first_wins",
			s: `{
    "mediaId": "first",
    "x": 1
  }
  and later
  {
    "mediaId": "second"
  }`,
			id: "first",
			ok: true,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			id, err := extractMediaID(tc.s)
			if tc.ok {
				if err != nil {
					t.Fatalf("got err: %v", err)
				}
				if id != tc.id {
					t.Fatalf("got id %q want %q", id, tc.id)
				}
			} else {
				if err == nil {
					t.Fatalf("want error, got nil")
				}
			}
		})
	}
}
