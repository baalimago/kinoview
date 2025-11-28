package butler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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

type fakeSubtitler struct {
	findInfo     model.MediaInfo
	findErr      error
	extractPath  string
	extractErr   error
	extractedIdx string
}

func (f *fakeSubtitler) Find(item model.Item) (model.MediaInfo, error) {
	return f.findInfo, f.findErr
}

func (f *fakeSubtitler) Extract(item model.Item, streamIndex string) (string, error) {
	f.extractedIdx = streamIndex
	return f.extractPath, f.extractErr
}

type fakeSelector struct {
	idx int
	err error
}

func (f *fakeSelector) SelectEnglish(ctx context.Context, streams []model.Stream) (int, error) {
	return f.idx, f.err
}

func TestPrepSuggestions_OK(t *testing.T) {
	f := &fakeLLM{
		resp: models.Chat{
			Messages: []models.Message{
				{
					Role:    "assistant",
					Content: `[{"index":0, "motivation": "Because you like it"}, {"index":1, "motivation": "Trending"}]`,
				},
			},
		},
	}
	b := &butler{llm: f}

	items := []model.Item{
		{ID: "1", Name: "One"},
		{ID: "2", Name: "Two"},
		{ID: "3", Name: "Three"},
	}
	ctxCtx := model.ClientContext{
		TimeOfDay: "Evening",
		ViewingHistory: []model.ViewMetadata{
			{Name: "One", ViewedAt: time.Now().Add(-time.Hour)},
		},
	}

	recs, err := b.PrepSuggestions(context.Background(), ctxCtx, items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(recs) != 2 {
		t.Fatalf("expected 2 recommendations, got %d", len(recs))
	}

	if recs[0].ID != "1" || recs[0].Motivation != "Because you like it" {
		t.Errorf("unexpected first recommendation: %+v", recs[0])
	}
	if recs[1].ID != "2" || recs[1].Motivation != "Trending" {
		t.Errorf("unexpected second recommendation: %+v", recs[1])
	}

	// Verify prompt content
	if len(f.got.Messages) < 2 {
		t.Fatal("expected at least 2 messages in chat")
	}
	sys := f.got.Messages[0]
	if sys.Role != "system" {
		t.Errorf("expected first message to be system, got %s", sys.Role)
	}
	user := f.got.Messages[1]
	if user.Role != "user" {
		t.Errorf("expected second message to be user, got %s", user.Role)
	}
	if !strings.Contains(user.Content, "Context:") {
		t.Error("prompt missing context header")
	}
	if !strings.Contains(user.Content, "Available Media:") {
		t.Error("prompt missing available media header")
	}
	if !strings.Contains(user.Content, "Evening") {
		t.Error("prompt missing time of day")
	}
	if !strings.Contains(user.Content, "- index: 0") {
		t.Error("prompt missing item index 0")
	}
}

func TestPrepSuggestions_WithSubtitles(t *testing.T) {
	f := &fakeLLM{
		resp: models.Chat{
			Messages: []models.Message{
				{Role: "assistant", Content: `[{"index":0, "motivation": "Good movie"}]`},
			},
		},
	}

	s := &fakeSubtitler{
		findInfo: model.MediaInfo{
			Streams: []model.Stream{
				{Index: 0, CodecType: "video"},
				{Index: 1, CodecType: "audio"},
				{Index: 2, CodecType: "subtitle"},
				{Index: 3, CodecType: "subtitle"}, // Another one
			},
		},
		extractPath: "/tmp/subs.vtt",
	}

	// Case 1: With Selector
	sel := &fakeSelector{idx: 3}
	b := &butler{llm: f, subs: s, selector: sel}

	items := []model.Item{{ID: "1", Name: "Movie"}}
	recs, err := b.PrepSuggestions(context.Background(), model.ClientContext{}, items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(recs) != 1 {
		t.Fatalf("expected 1 recommendation, got %d", len(recs))
	}

	if recs[0].SubtitleID != "3" {
		t.Errorf("expected SubtitleID 3, got %q", recs[0].SubtitleID)
	}

	// Case 2: Selector Fails
	sel.err = errors.New("no subs")
	// Reset subtitle mock state if needed, but basic struct reused is fine as it's just return values

	// We need to query again.
	// Since fakeLLM returns static response, it works.
	// But we need to reset fakeSubtitler extractedIdx
	s.extractedIdx = ""

	recs2, err := b.PrepSuggestions(context.Background(), model.ClientContext{}, items)
	if err != nil {
		t.Fatalf("unexpected error case 2: %v", err)
	}

	// If selector fails, we drop the recommendation
	if len(recs2) != 0 {
		t.Errorf("expected 0 recommendations, got %d", len(recs2))
	}
}

func TestPrepSuggestions_LLMError(t *testing.T) {
	f := &fakeLLM{err: errors.New("connection failed")}
	b := &butler{llm: f}

	_, err := b.PrepSuggestions(context.Background(), model.ClientContext{}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to query llm") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestPrepSuggestions_ParseError(t *testing.T) {
	f := &fakeLLM{
		resp: models.Chat{
			Messages: []models.Message{
				{Role: "assistant", Content: "I cannot do that"},
			},
		},
	}
	b := &butler{llm: f}

	_, err := b.PrepSuggestions(context.Background(), model.ClientContext{}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no JSON array found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestPrepSuggestions_UnknownItem(t *testing.T) {
	f := &fakeLLM{
		resp: models.Chat{
			Messages: []models.Message{
				{
					Role:    "assistant",
					Content: `[{"index":999, "motivation": "Does not exist"}]`,
				},
			},
		},
	}
	b := &butler{llm: f}
	items := []model.Item{{ID: "1", Name: "One"}}

	recs, err := b.PrepSuggestions(context.Background(), model.ClientContext{}, items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recs) != 0 {
		t.Errorf("expected 0 recommendations for unknown item, got %d", len(recs))
	}
}

func TestParseSuggestions(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantIDs []int
		wantErr bool
	}{
		{
			name:    "simple json",
			content: `[{"index": 1, "motivation": "a"}]`,
			wantIDs: []int{1},
			wantErr: false,
		},
		{
			name:    "markdown json",
			content: "Here is the list:\n```json\n[\n  {\"index\": " + "2, \"motivation\": \"b\"}\n]\n```",
			wantIDs: []int{2},
			wantErr: false,
		},
		{
			name:    "surrounding text",
			content: "Sure! [{\"index\":3,\"motivation\":\"c\"}] Hope this helps.",
			wantIDs: []int{3},
			wantErr: false,
		},
		{
			name:    "invalid json",
			content: `[{"index": 1, "motivation": "a"`,
			wantErr: true,
		},
		{
			name:    "no array",
			content: `{"index": 1}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSuggestions(tc.content)
			if (err != nil) != tc.wantErr {
				t.Errorf("wantErr %v, got error %v", tc.wantErr, err)
				return
			}
			if !tc.wantErr {
				if len(got) != len(tc.wantIDs) {
					t.Errorf("expected %d items, got %d", len(tc.wantIDs), len(got))
				}
				for i, id := range tc.wantIDs {
					if got[i].IndexInList != id {
						t.Errorf("expected index %d, got %d", id, got[i].IndexInList)
					}
				}
			}
		})
	}
}

// ... other tests kept as is
func TestPreloadSubs_Success(t *testing.T) {
	s := &fakeSubtitler{
		findInfo: model.MediaInfo{
			Streams: []model.Stream{
				{Index: 0, CodecType: "subtitle"},
				{Index: 1, CodecType: "subtitle"},
			},
		},
		extractPath: "/tmp/subs.vtt",
	}

	sel := &fakeSelector{idx: 1}
	b := &butler{subs: s, selector: sel}

	item := model.Item{ID: "1", Name: "Test"}
	rec := &model.Recommendation{}

	err := b.preloadSubs(context.Background(), item, rec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rec.SubtitleID != "1" {
		t.Errorf("expected SubtitleID 1, got %q", rec.SubtitleID)
	}

	if s.extractedIdx != "1" {
		t.Errorf("expected extracted idx 1, got %q", s.extractedIdx)
	}
}

func TestPreloadSubs_NoSelector(t *testing.T) {
	s := &fakeSubtitler{
		findInfo: model.MediaInfo{
			Streams: []model.Stream{
				{Index: 0, CodecType: "subtitle"},
			},
		},
		extractPath: "/tmp/subs.vtt",
	}

	b := &butler{subs: s}

	item := model.Item{ID: "1", Name: "Test"}
	rec := &model.Recommendation{}

	err := b.preloadSubs(context.Background(), item, rec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rec.SubtitleID != "" {
		t.Errorf("expected empty SubtitleID, got %q", rec.SubtitleID)
	}

	if s.extractedIdx != "" {
		t.Errorf("expected empty extracted idx, got %q", s.extractedIdx)
	}
}

func TestPreloadSubs_FindError(t *testing.T) {
	s := &fakeSubtitler{
		findErr: errors.New("find failed"),
	}

	b := &butler{subs: s}

	item := model.Item{ID: "1", Name: "TestMovie"}
	rec := &model.Recommendation{}

	err := b.preloadSubs(context.Background(), item, rec)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to find subtitles") {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "TestMovie") {
		t.Errorf("error should contain item name")
	}
}

func TestPreloadSubs_SelectorError(t *testing.T) {
	s := &fakeSubtitler{
		findInfo: model.MediaInfo{
			Streams: []model.Stream{
				{Index: 0, CodecType: "subtitle"},
			},
		},
	}

	sel := &fakeSelector{
		err: errors.New("selection failed"),
	}
	b := &butler{subs: s, selector: sel}

	item := model.Item{ID: "1", Name: "TestMovie"}
	rec := &model.Recommendation{}

	err := b.preloadSubs(context.Background(), item, rec)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to select english") {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "TestMovie") {
		t.Errorf("error should contain item name")
	}
}

func TestPreloadSubs_ExtractError(t *testing.T) {
	s := &fakeSubtitler{
		findInfo: model.MediaInfo{
			Streams: []model.Stream{
				{Index: 0, CodecType: "subtitle"},
			},
		},
		extractErr: errors.New("extract failed"),
	}

	sel := &fakeSelector{idx: 0}
	b := &butler{subs: s, selector: sel}

	item := model.Item{ID: "1", Name: "Test"}
	rec := &model.Recommendation{}

	err := b.preloadSubs(context.Background(), item, rec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rec.SubtitleID != "0" {
		t.Errorf("expected SubtitleID 0, got %q", rec.SubtitleID)
	}
}

func TestPrepSuggestion_Success(t *testing.T) {
	item := model.Item{
		ID:       "1",
		Name:     "Movie One",
		MIMEType: "video/mp4",
	}
	items := []model.Item{item}
	sug := suggestionResponse{
		IndexInList: 0,
		Motivation:  "Good choice",
	}

	b := &butler{}
	rec, err := b.prepSuggestion(
		context.Background(),
		sug,
		items,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rec.Item.ID != "1" {
		t.Errorf("expected ID 1, got %q", rec.Item.ID)
	}
	if rec.Motivation != "Good choice" {
		t.Errorf("expected motivation, got %q", rec.Motivation)
	}
}

func TestPrepSuggestion_WithSubtitles(t *testing.T) {
	item := model.Item{ID: "1", Name: "Movie"}
	items := []model.Item{item}
	sug := suggestionResponse{
		IndexInList: 0,
		Motivation:  "Nice",
	}

	s := &fakeSubtitler{
		findInfo: model.MediaInfo{
			Streams: []model.Stream{
				{Index: 0, CodecType: "subtitle"},
			},
		},
		extractPath: "/tmp/subs.vtt",
	}

	sel := &fakeSelector{idx: 0}
	b := &butler{subs: s, selector: sel}

	rec, err := b.prepSuggestion(
		context.Background(),
		sug,
		items,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rec.SubtitleID != "0" {
		t.Errorf("expected SubtitleID 0, got %q",
			rec.SubtitleID)
	}
}

func TestPrepSuggestion_NoSubtitler(t *testing.T) {
	item := model.Item{ID: "1", Name: "Movie"}
	items := []model.Item{item}
	sug := suggestionResponse{
		IndexInList: 0,
		Motivation:  "Nice",
	}

	b := &butler{subs: nil}

	rec, err := b.prepSuggestion(
		context.Background(),
		sug,
		items,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rec.SubtitleID != "" {
		t.Errorf("expected empty SubtitleID, got %q",
			rec.SubtitleID)
	}
}

func TestPrepSuggestion_IndexOutOfBounds(t *testing.T) {
	items := []model.Item{
		{ID: "1", Name: "One"},
		{ID: "2", Name: "Two"},
	}
	sug := suggestionResponse{
		IndexInList: 5,
		Motivation:  "Bad index",
	}

	b := &butler{}
	_, err := b.prepSuggestion(
		context.Background(),
		sug,
		items,
	)
	if err == nil {
		t.Fatal("expected error for out of bounds index")
	}
	if !strings.Contains(err.Error(),
		"index which isn't in list") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPrepSuggestion_NegativeIndex(t *testing.T) {
	items := []model.Item{
		{ID: "1", Name: "One"},
	}
	sug := suggestionResponse{
		IndexInList: -1,
		Motivation:  "Bad index",
	}

	b := &butler{}
	_, err := b.prepSuggestion(
		context.Background(),
		sug,
		items,
	)
	if err == nil {
		t.Fatal("expected error for negative index")
	}
	if !strings.Contains(err.Error(),
		"index which isn't in list") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPrepSuggestion_SubtitlePreloadError(t *testing.T) {
	item := model.Item{ID: "1", Name: "Movie"}
	items := []model.Item{item}
	sug := suggestionResponse{
		IndexInList: 0,
		Motivation:  "Nice",
	}

	s := &fakeSubtitler{
		findErr: errors.New("find failed"),
	}

	b := &butler{subs: s}

	_, err := b.prepSuggestion(
		context.Background(),
		sug,
		items,
	)
	if err == nil {
		t.Fatal("expected error from preloadSubs")
	}
	if !strings.Contains(err.Error(),
		"failed to find subtitles") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPrepSuggestion_MultipleItems(t *testing.T) {
	items := []model.Item{
		{ID: "1", Name: "One"},
		{ID: "2", Name: "Two"},
		{ID: "3", Name: "Three"},
	}
	// Index 2 is "Three"
	sug := suggestionResponse{
		IndexInList: 2,
		Motivation:  "Pick three",
	}

	b := &butler{}
	rec, err := b.prepSuggestion(
		context.Background(),
		sug,
		items,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rec.Item.ID != "3" {
		t.Errorf("expected ID 3, got %q", rec.Item.ID)
	}
	if rec.Item.Name != "Three" {
		t.Errorf("expected name Three, got %q",
			rec.Item.Name)
	}
}
