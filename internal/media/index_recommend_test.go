package media

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/baalimago/kinoview/internal/model"
)

type mockRec struct {
	lastReq   string
	recommend func(
		context.Context,
		string,
		[]model.Item,
	) (model.Item, error)
}

func (m *mockRec) Setup(ctx context.Context) error {
	return nil
}

func (m *mockRec) Recommend(
	ctx context.Context,
	req string,
	items []model.Item,
) (model.Item, error) {
	m.lastReq = req
	if m.recommend != nil {
		return m.recommend(ctx, req, items)
	}
	return model.Item{}, nil
}

func TestRecommendHandler_MethodNotAllowed(t *testing.T) {
	i, _ := NewIndexer()
	i.store = &mockStore{}
	i.recommender = &mockRec{}

	h := i.recomendHandler()
	req := httptest.NewRequest(http.MethodGet, "/recommend", nil)
	rr := httptest.NewRecorder()

	h(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d, want %d",
			rr.Code, http.StatusMethodNotAllowed)
	}
	if got := rr.Header().Get("Allow"); got != http.MethodPost {
		t.Fatalf("Allow header %q, want %q", got, http.MethodPost)
	}
}

func TestRecommendHandler_BadJSON(t *testing.T) {
	i, _ := NewIndexer()
	i.store = &mockStore{}
	i.recommender = &mockRec{}

	h := i.recomendHandler()
	body := bytes.NewBufferString("{")
	req := httptest.NewRequest(http.MethodPost, "/recommend", body)
	rr := httptest.NewRecorder()

	h(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want %d",
			rr.Code, http.StatusBadRequest)
	}
}

func TestRecommendHandler_UnknownFields(t *testing.T) {
	i, _ := NewIndexer()
	i.store = &mockStore{}
	i.recommender = &mockRec{}

	h := i.recomendHandler()
	body := bytes.NewBufferString(
		`{"Request":"a","Context":"b","Extra":1}`,
	)
	req := httptest.NewRequest(http.MethodPost, "/recommend", body)
	rr := httptest.NewRecorder()

	h(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want %d",
			rr.Code, http.StatusBadRequest)
	}
}

func TestRecommendHandler_EmptyRequest(t *testing.T) {
	i, _ := NewIndexer()
	i.store = &mockStore{}
	i.recommender = &mockRec{}

	h := i.recomendHandler()
	body := bytes.NewBufferString(
		`{"Request":"   ","Context":"b"}`,
	)
	req := httptest.NewRequest(http.MethodPost, "/recommend", body)
	rr := httptest.NewRecorder()

	h(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want %d",
			rr.Code, http.StatusBadRequest)
	}
}

func TestRecommendHandler_Success(t *testing.T) {
	items := []model.Item{
		{ID: "1", Name: "A", MIMEType: "video/mp4"},
		{ID: "2", Name: "B", MIMEType: "video/mp4"},
	}
	i, _ := NewIndexer()
	i.store = &mockStore{items: items}
	rec := &mockRec{
		recommend: func(
			ctx context.Context,
			req string,
			it []model.Item,
		) (model.Item, error) {
			return it[1], nil
		},
	}
	i.recommender = rec

	h := i.recomendHandler()
	body := bytes.NewBufferString(
		`{"Request":"watch drama","Context":{}}`,
	)
	req := httptest.NewRequest(http.MethodPost, "/recommend", body)
	rr := httptest.NewRecorder()

	h(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want %d", rr.Code, http.StatusOK)
	}
	ct := rr.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("content-type %q, want application/json", ct)
	}
	var got model.Item
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != "2" {
		t.Fatalf("got id %q, want %q", got.ID, "2")
	}
	wantReq := "{\n \"request\": \"watch drama\",\n \"context\": {\n  \"sessionId\": \"\",\n  \"startTime\": \"0001-01-01T00:00:00Z\",\n  \"viewingHistory\": null,\n  \"timeOfDay\": \"\",\n  \"lastPlayedName\": \"\"\n }\n}"
	if strings.TrimSpace(rec.lastReq) != wantReq {
		t.Fatalf("\ngot:  %q\nwant: %q", rec.lastReq, wantReq)
	}
}
