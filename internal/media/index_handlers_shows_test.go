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

	t.Run("metadata season+episode, show name from filename", func(t *testing.T) {
		raw := json.RawMessage(`{"name":"Ethon","season":9,"episode":15}`)
		it := model.Item{
			Name:     "Stargate.SG-1.S09E15.Ethon.mkv",
			Path:     "/media/Stargate.SG-1.S09E15.Ethon.mkv",
			MIMEType: "video/x-matroska",
			Metadata: &raw,
		}
		show, season, ep, ok := extractShowMetadata(it)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if show != "Stargate SG-1" {
			t.Fatalf("expected 'Stargate SG-1', got '%s'", show)
		}
		if season != 9 {
			t.Fatalf("expected season 9, got %d", season)
		}
		if ep != 15 {
			t.Fatalf("expected episode 15, got %d", ep)
		}
	})

	t.Run("metadata from From series example", func(t *testing.T) {
		raw := json.RawMessage(`{"name":"From (2022) - S02E06 - Pas de Deux","season":2,"episode":6}`)
		it := model.Item{
			Name:     "From (2022) - S02E06 - Pas de Deux (1080p AMZN WEB-DL x265 t3nzin).mkv",
			Path:     "/media/From (2022) - S02E06 - Pas de Deux (1080p AMZN WEB-DL x265 t3nzin).mkv",
			MIMEType: "video/x-matroska",
			Metadata: &raw,
		}
		show, season, ep, ok := extractShowMetadata(it)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if show != "From (2022)" {
			t.Fatalf("expected 'From (2022)', got '%s'", show)
		}
		if season != 2 {
			t.Fatalf("expected season 2, got %d", season)
		}
		if ep != 6 {
			t.Fatalf("expected episode 6, got %d", ep)
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
		raw := json.RawMessage(`{"name":"Irrelevant Episode Title","season":4.0,"episode":5.0}`)
		it := model.Item{
			Name:     "Test.Show.S04E05.mkv",
			Path:     "/media/Test.Show.S04E05.mkv",
			MIMEType: "video/x-matroska",
			Metadata: &raw,
		}
		show, season, ep, ok := extractShowMetadata(it)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if show != "Test Show" {
			t.Fatalf("expected 'Test Show', got '%s'", show)
		}
		if season != 4 {
			t.Fatalf("expected season 4, got %d", season)
		}
		if ep != 5 {
			t.Fatalf("expected episode 5, got %d", ep)
		}
	})

	t.Run("metadata string season/episode", func(t *testing.T) {
		raw := json.RawMessage(`{"name":"Ep Title","season":"3","episode":"7"}`)
		it := model.Item{
			Name:     "Another.Show.S03E07.mkv",
			Path:     "/media/Another.Show.S03E07.mkv",
			MIMEType: "video/x-matroska",
			Metadata: &raw,
		}
		show, season, ep, ok := extractShowMetadata(it)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if show != "Another Show" {
			t.Fatalf("expected 'Another Show', got '%s'", show)
		}
		if season != 3 {
			t.Fatalf("expected season 3, got %d", season)
		}
		if ep != 7 {
			t.Fatalf("expected episode 7, got %d", ep)
		}
	})

	t.Run("metadata with season but no show name in path", func(t *testing.T) {
		raw := json.RawMessage(`{"season":1,"episode":5}`)
		it := model.Item{
			Name:     "00105.mkv",
			Path:     "/media/00105.mkv",
			MIMEType: "video/x-matroska",
			Metadata: &raw,
		}
		_, _, _, ok := extractShowMetadata(it)
		if ok {
			t.Fatal("expected ok=false when path has no show name and metadata name is absent")
		}
	})

	t.Run("metadata with name but no season/episode in filename, fallback to metadata name as show", func(t *testing.T) {
		// Has metadata name, but no season/episode in metadata
		raw := json.RawMessage(`{"name":"Breaking Bad","season":1,"episode":1}`)
		it := model.Item{
			Name:     "Breaking.Bad.S01E01.Pilot.mkv",
			Path:     "/media/Breaking.Bad.S01E01.Pilot.mkv",
			MIMEType: "video/x-matroska",
			Metadata: &raw,
		}
		show, season, ep, ok := extractShowMetadata(it)
		if !ok {
			t.Fatal("expected ok=true")
		}
		// Show name from path, not from metadata 'name'
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

	t.Run("space race episode with metadata", func(t *testing.T) {
		raw := json.RawMessage(`{"name":"Space Race","season":7,"episode":8}`)
		it := model.Item{
			Name:     "Stargate.SG-1.S07E08.Space.Race.1080p.BluRay.mkv",
			Path:     "/media/Stargate.SG-1.S07E08.Space.Race.1080p.BluRay.mkv",
			MIMEType: "video/x-matroska",
			Metadata: &raw,
		}
		show, season, ep, ok := extractShowMetadata(it)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if show != "Stargate SG-1" {
			t.Fatalf("expected 'Stargate SG-1', got '%s'", show)
		}
		if season != 7 {
			t.Fatalf("expected season 7, got %d", season)
		}
		if ep != 8 {
			t.Fatalf("expected episode 8, got %d", ep)
		}
	})
}

func Test_showsHandler(t *testing.T) {
	t.Parallel()

	t.Run("groups shows correctly with metadata", func(t *testing.T) {
		raw1 := json.RawMessage(`{"name":"Ethon","season":9,"episode":15}`)
		raw2 := json.RawMessage(`{"name":"Space Race","season":7,"episode":8}`)
		raw3 := json.RawMessage(`{"name":"Another S9 Episode","season":9,"episode":16}`)

		store := &mockStore{
			items: []model.Item{
				{
					ID:       "1",
					Name:     "Stargate.SG-1.S09E15.Ethon.mkv",
					Path:     "/media/Stargate.SG-1.S09E15.Ethon.mkv",
					MIMEType: "video/x-matroska",
					Metadata: &raw1,
				},
				{
					ID:       "2",
					Name:     "Stargate.SG-1.S07E08.Space.Race.mkv",
					Path:     "/media/Stargate.SG-1.S07E08.Space.Race.mkv",
					MIMEType: "video/x-matroska",
					Metadata: &raw2,
				},
				{
					ID:       "3",
					Name:     "Stargate.SG-1.S09E16.Ep16.mkv",
					Path:     "/media/Stargate.SG-1.S09E16.Ep16.mkv",
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
		if show.Name != "Stargate SG-1" {
			t.Fatalf("expected 'Stargate SG-1', got '%s'", show.Name)
		}
		if len(show.Seasons) != 2 {
			t.Fatalf("expected 2 seasons, got %d", len(show.Seasons))
		}

		// Season 7
		if show.Seasons[0].Season != 7 {
			t.Fatalf("expected season 7, got %d", show.Seasons[0].Season)
		}
		if len(show.Seasons[0].Episodes) != 1 {
			t.Fatalf("expected 1 episode in season 7, got %d", len(show.Seasons[0].Episodes))
		}

		// Season 9
		if show.Seasons[1].Season != 9 {
			t.Fatalf("expected season 9, got %d", show.Seasons[1].Season)
		}
		if len(show.Seasons[1].Episodes) != 2 {
			t.Fatalf("expected 2 episodes in season 9, got %d", len(show.Seasons[1].Episodes))
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
					Path:     "/media/image.jpg",
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

	t.Run("multiple shows from From and Stargate", func(t *testing.T) {
		rawFrom := json.RawMessage(`{"name":"Pas de Deux","season":2,"episode":6}`)
		rawSG := json.RawMessage(`{"name":"Ethon","season":9,"episode":15}`)
		store := &mockStore{
			items: []model.Item{
				{
					ID:       "from1",
					Name:     "From (2022) - S02E06 - Pas de Deux.mkv",
					Path:     "/media/From (2022) - S02E06 - Pas de Deux.mkv",
					MIMEType: "video/x-matroska",
					Metadata: &rawFrom,
				},
				{
					ID:       "sg1",
					Name:     "Stargate.SG-1.S09E15.Ethon.mkv",
					Path:     "/media/Stargate.SG-1.S09E15.Ethon.mkv",
					MIMEType: "video/x-matroska",
					Metadata: &rawSG,
				},
			},
		}
		idx := &Indexer{store: store}
		handler := idx.showsHandler()

		req := httptest.NewRequest(http.MethodGet, "/shows", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		var resp model.ShowsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if len(resp.Shows) != 2 {
			t.Fatalf("expected 2 shows, got %d", len(resp.Shows))
		}

		// Shows should be alphabetically sorted
		if resp.Shows[0].Name != "From (2022)" {
			t.Fatalf("expected first show 'From (2022)', got '%s'", resp.Shows[0].Name)
		}
		if resp.Shows[1].Name != "Stargate SG-1" {
			t.Fatalf("expected second show 'Stargate SG-1', got '%s'", resp.Shows[1].Name)
		}
	})
}
