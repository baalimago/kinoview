package media

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/baalimago/kinoview/internal/model"
)

func Test_extractShowMetadata(t *testing.T) {
	t.Parallel()

	t.Run("metadata populated", func(t *testing.T) {
		raw := json.RawMessage(`{"name":"Breaking Bad","season":1,"episode":1}`)
		it := model.Item{
			Name:     "Breaking.Bad.S01E01.mkv",
			MIMEType: "video/x-matroska",
			Metadata: &raw,
		}
		show, season, ep, ok := extractShowMetadata(it)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if show != "Breaking Bad" {
			t.Fatalf("expected 'Breaking Bad', got '%s'", show)
		}
		if season != 1 {
			t.Fatalf("expected season 1, got %d", season)
		}
		if ep != 1 {
			t.Fatalf("expected episode 1, got %d", ep)
		}
	})

	t.Run("metadata alt_name", func(t *testing.T) {
		raw := json.RawMessage(`{"alt_name":"Better Call Saul","season":2,"episode":3}`)
		it := model.Item{
			Name:     "somefile.mkv",
			MIMEType: "video/x-matroska",
			Metadata: &raw,
		}
		show, season, ep, ok := extractShowMetadata(it)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if show != "Better Call Saul" {
			t.Fatalf("expected 'Better Call Saul', got '%s'", show)
		}
		if season != 2 {
			t.Fatalf("expected season 2, got %d", season)
		}
		if ep != 3 {
			t.Fatalf("expected episode 3, got %d", ep)
		}
	})

	t.Run("fallback S01E02 pattern", func(t *testing.T) {
		it := model.Item{
			Name:     "The.Office.S03E05.mkv",
			Path:     "/media/The.Office.S03E05.mkv",
			MIMEType: "video/x-matroska",
		}
		show, season, ep, ok := extractShowMetadata(it)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if show != "The Office" {
			t.Fatalf("expected 'The Office', got '%s'", show)
		}
		if season != 3 {
			t.Fatalf("expected season 3, got %d", season)
		}
		if ep != 5 {
			t.Fatalf("expected episode 5, got %d", ep)
		}
	})

	t.Run("fallback 1x02 pattern", func(t *testing.T) {
		it := model.Item{
			Name:     "Fargo.2x04.mkv",
			Path:     "/media/Fargo.2x04.mkv",
			MIMEType: "video/x-matroska",
		}
		show, season, ep, ok := extractShowMetadata(it)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if show != "Fargo" {
			t.Fatalf("expected 'Fargo', got '%s'", show)
		}
		if season != 2 {
			t.Fatalf("expected season 2, got %d", season)
		}
		if ep != 4 {
			t.Fatalf("expected episode 4, got %d", ep)
		}
	})

	t.Run("fallback with underscores", func(t *testing.T) {
		it := model.Item{
			Name:     "Westworld_S01E02.mkv",
			Path:     "/media/Westworld_S01E02.mkv",
			MIMEType: "video/x-matroska",
		}
		show, season, ep, ok := extractShowMetadata(it)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if show != "Westworld" {
			t.Fatalf("expected 'Westworld', got '%s'", show)
		}
		if season != 1 {
			t.Fatalf("expected season 1, got %d", season)
		}
		if ep != 2 {
			t.Fatalf("expected episode 2, got %d", ep)
		}
	})

	t.Run("fallback with spaces", func(t *testing.T) {
		it := model.Item{
			Name:     "The Wire S02E03.mkv",
			Path:     "/media/The Wire S02E03.mkv",
			MIMEType: "video/x-matroska",
		}
		show, season, ep, ok := extractShowMetadata(it)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if show != "The Wire" {
			t.Fatalf("expected 'The Wire', got '%s'", show)
		}
		if season != 2 {
			t.Fatalf("expected season 2, got %d", season)
		}
		if ep != 3 {
			t.Fatalf("expected episode 3, got %d", ep)
		}
	})

	t.Run("no match returns false", func(t *testing.T) {
		it := model.Item{
			Name:     "random_movie.mkv",
			Path:     "/media/random_movie.mkv",
			MIMEType: "video/x-matroska",
		}
		_, _, _, ok := extractShowMetadata(it)
		if ok {
			t.Fatal("expected ok=false for random movie")
		}
	})

	t.Run("metadata floats as ints", func(t *testing.T) {
		raw := json.RawMessage(`{"name":"Test Show","season":4.0,"episode":5.0}`)
		it := model.Item{
			Name:     "test.mkv",
			MIMEType: "video/x-matroska",
			Metadata: &raw,
		}
		_, season, ep, ok := extractShowMetadata(it)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if season != 4 {
			t.Fatalf("expected season 4, got %d", season)
		}
		if ep != 5 {
			t.Fatalf("expected episode 5, got %d", ep)
		}
	})

	t.Run("metadata string season/episode", func(t *testing.T) {
		raw := json.RawMessage(`{"name":"Test Show 2","season":"3","episode":"7"}`)
		it := model.Item{
			Name:     "test2.mkv",
			MIMEType: "video/x-matroska",
			Metadata: &raw,
		}
		_, season, ep, ok := extractShowMetadata(it)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if season != 3 {
			t.Fatalf("expected season 3, got %d", season)
		}
		if ep != 7 {
			t.Fatalf("expected episode 7, got %d", ep)
		}
	})
}

func Test_showsHandler(t *testing.T) {
	t.Parallel()

	t.Run("groups shows correctly", func(t *testing.T) {
		raw1 := json.RawMessage(`{"name":"Breaking Bad","season":1,"episode":1}`)
		raw2 := json.RawMessage(`{"name":"Breaking Bad","season":1,"episode":2}`)
		raw3 := json.RawMessage(`{"name":"Breaking Bad","season":2,"episode":1}`)

		store := &mockStore{
			items: []model.Item{
				{
					ID:       "1",
					Name:     "bb_s01e01.mkv",
					MIMEType: "video/x-matroska",
					Metadata: &raw1,
				},
				{
					ID:       "2",
					Name:     "bb_s01e02.mkv",
					MIMEType: "video/x-matroska",
					Metadata: &raw2,
				},
				{
					ID:       "3",
					Name:     "bb_s02e01.mkv",
					MIMEType: "video/x-matroska",
					Metadata: &raw3,
				},
			},
		}

		idx := &Indexer{store: store}
		handler := idx.showsHandler()

		req := httptest.NewRequest(http.MethodGet, "/shows", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp model.ShowsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if len(resp.Shows) != 1 {
			t.Fatalf("expected 1 show, got %d", len(resp.Shows))
		}

		show := resp.Shows[0]
		if show.Name != "Breaking Bad" {
			t.Fatalf("expected 'Breaking Bad', got '%s'", show.Name)
		}
		if len(show.Seasons) != 2 {
			t.Fatalf("expected 2 seasons, got %d", len(show.Seasons))
		}

		// Season 1
		if show.Seasons[0].Season != 1 {
			t.Fatalf("expected season 1, got %d", show.Seasons[0].Season)
		}
		if len(show.Seasons[0].Episodes) != 2 {
			t.Fatalf("expected 2 episodes in season 1, got %d", len(show.Seasons[0].Episodes))
		}

		// Season 2
		if show.Seasons[1].Season != 2 {
			t.Fatalf("expected season 2, got %d", show.Seasons[1].Season)
		}
		if len(show.Seasons[1].Episodes) != 1 {
			t.Fatalf("expected 1 episode in season 2, got %d", len(show.Seasons[1].Episodes))
		}
	})

	t.Run("empty store returns empty shows", func(t *testing.T) {
		store := &mockStore{items: []model.Item{}}
		idx := &Indexer{store: store}
		handler := idx.showsHandler()

		req := httptest.NewRequest(http.MethodGet, "/shows", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var resp model.ShowsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if len(resp.Shows) != 0 {
			t.Fatalf("expected 0 shows, got %d", len(resp.Shows))
		}
	})

	t.Run("non-video items skipped", func(t *testing.T) {
		raw := json.RawMessage(`{"name":"Show","season":1,"episode":1}`)
		store := &mockStore{
			items: []model.Item{
				{
					ID:       "img1",
					Name:     "image.jpg",
					MIMEType: "image/jpeg",
					Metadata: &raw,
				},
			},
		}
		idx := &Indexer{store: store}
		handler := idx.showsHandler()

		req := httptest.NewRequest(http.MethodGet, "/shows", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		var resp model.ShowsResponse
		json.Unmarshal(rec.Body.Bytes(), &resp)
		if len(resp.Shows) != 0 {
			t.Fatalf("expected 0 shows for non-video, got %d", len(resp.Shows))
		}
	})

	t.Run("method not allowed", func(t *testing.T) {
		store := &mockStore{items: []model.Item{}}
		idx := &Indexer{store: store}
		handler := idx.showsHandler()

		req := httptest.NewRequest(http.MethodPost, "/shows", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}
	})

	t.Run("fallback filename parsing", func(t *testing.T) {
		store := &mockStore{
			items: []model.Item{
				{
					ID:       "f1",
					Name:     "Game.of.Thrones.S01E01.mkv",
					Path:     "/media/Game.of.Thrones.S01E01.mkv",
					MIMEType: "video/x-matroska",
				},
				{
					ID:       "f2",
					Name:     "Game.of.Thrones.S01E02.mkv",
					Path:     "/media/Game.of.Thrones.S01E02.mkv",
					MIMEType: "video/x-matroska",
				},
			},
		}
		idx := &Indexer{store: store}
		handler := idx.showsHandler()

		req := httptest.NewRequest(http.MethodGet, "/shows", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var resp model.ShowsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if len(resp.Shows) != 1 {
			t.Fatalf("expected 1 show, got %d", len(resp.Shows))
		}
		if resp.Shows[0].Name != "Game of Thrones" {
			t.Fatalf("expected 'Game of Thrones', got '%s'", resp.Shows[0].Name)
		}
	})
}
