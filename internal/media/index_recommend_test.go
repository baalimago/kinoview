package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/baalimago/kinoview/internal/model"
)

type storeWithItems struct {
	items []model.Item
}

func (s *storeWithItems) Setup(
	ctx context.Context,
) (<-chan error, error) {
	return nil, nil
}

func (s *storeWithItems) Start(ctx context.Context) {}

func (s *storeWithItems) Store(
	ctx context.Context,
	i model.Item,
) error {
	return nil
}

func (s *storeWithItems) ListHandlerFunc() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {}
}

func (s *storeWithItems) VideoHandlerFunc() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {}
}

func (s *storeWithItems) SubsListHandlerFunc() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {}
}

func (s *storeWithItems) SubsHandlerFunc() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {}
}

func (s *storeWithItems) Items() []model.Item {
	return s.items
}

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
	i.store = &storeWithItems{}
	i.recommender = &mockRec{}

	h := i.recomendHandler()
	req := httptest.NewRequest(http.MethodPost, "/recommend", nil)
	rr := httptest.NewRecorder()

	h(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d, want %d",
			rr.Code, http.StatusMethodNotAllowed)
	}
	if got := rr.Header().Get("Allow"); got != http.MethodGet {
		t.Fatalf("Allow header %q, want %q", got, http.MethodGet)
	}
}

func TestRecommendHandler_BadJSON(t *testing.T) {
	i, _ := NewIndexer()
	i.store = &storeWithItems{}
	i.recommender = &mockRec{}

	h := i.recomendHandler()
	body := bytes.NewBufferString("{")
	req := httptest.NewRequest(http.MethodGet, "/recommend", body)
	rr := httptest.NewRecorder()

	h(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want %d",
			rr.Code, http.StatusBadRequest)
	}
}

func TestRecommendHandler_UnknownFields(t *testing.T) {
	i, _ := NewIndexer()
	i.store = &storeWithItems{}
	i.recommender = &mockRec{}

	h := i.recomendHandler()
	body := bytes.NewBufferString(
		`{"Request":"a","Context":"b","Extra":1}`,
	)
	req := httptest.NewRequest(http.MethodGet, "/recommend", body)
	rr := httptest.NewRecorder()

	h(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want %d",
			rr.Code, http.StatusBadRequest)
	}
}

func TestRecommendHandler_EmptyRequest(t *testing.T) {
	i, _ := NewIndexer()
	i.store = &storeWithItems{}
	i.recommender = &mockRec{}

	h := i.recomendHandler()
	body := bytes.NewBufferString(
		`{"Request":"   ","Context":"b"}`,
	)
	req := httptest.NewRequest(http.MethodGet, "/recommend", body)
	rr := httptest.NewRecorder()

	h(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want %d",
			rr.Code, http.StatusBadRequest)
	}
}

func TestRecommendHandler_RecommenderError(t *testing.T) {
	items := []model.Item{
		{ID: "1", Name: "A", MIMEType: "video/mp4"},
	}
	i, _ := NewIndexer()
	i.store = &storeWithItems{items: items}
	rec := &mockRec{
		recommend: func(
			ctx context.Context,
			req string,
			it []model.Item,
		) (model.Item, error) {
			return model.Item{}, errors.New("boom")
		},
	}
	i.recommender = rec

	h := i.recomendHandler()
	body := bytes.NewBufferString(
		`{"Request":"play","Context":"now"}`,
	)
	req := httptest.NewRequest(http.MethodGet, "/recommend", body)
	rr := httptest.NewRecorder()

	h(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want %d",
			rr.Code, http.StatusInternalServerError)
	}
}

func TestRecommendHandler_Success(t *testing.T) {
	items := []model.Item{
		{ID: "1", Name: "A", MIMEType: "video/mp4"},
		{ID: "2", Name: "B", MIMEType: "video/mp4"},
	}
	i, _ := NewIndexer()
	i.store = &storeWithItems{items: items}
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
		`{"Request":"watch drama","Context":"evening"}`,
	)
	req := httptest.NewRequest(http.MethodGet, "/recommend", body)
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
	wantReq := "watch drama evening"
	if strings.TrimSpace(rec.lastReq) != wantReq {
		t.Fatalf("combined %q, want %q", rec.lastReq, wantReq)
	}
}

func TestRecommendHandler_ContextCancel(t *testing.T) {
	items := []model.Item{
		{ID: "1", Name: "A", MIMEType: "video/mp4"},
	}
	i, _ := NewIndexer()
	i.store = &storeWithItems{items: items}
	rec := &mockRec{
		recommend: func(
			ctx context.Context,
			req string,
			it []model.Item,
		) (model.Item, error) {
			<-ctx.Done()
			return model.Item{}, errors.New("ctx canceled")
		},
	}
	i.recommender = rec

	h := i.recomendHandler()
	body := bytes.NewBufferString(
		`{"Request":"watch","Context":"later"}`,
	)
	req := httptest.NewRequest(http.MethodGet, "/recommend", body)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want %d",
			rr.Code, http.StatusInternalServerError)
	}
}
