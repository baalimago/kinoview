package media

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baalimago/kinoview/internal/agents/troupe"
)

// seedTroupeWorktree writes a minimal valid notebook (one model, one clip)
// into a fresh temp dir and returns its root.
func seedTroupeWorktree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	assets := map[string]string{
		"models/cat@1.json": `{"kind":"model","id":"cat","version":1,"status":"draft","author":"fixture","provenance":"fixture","spec":{"bones":[{"id":"root","parent":null,"x":0,"y":0,"rot":0,"length":0}]}}`,
		"clips/walk@1.json": `{"kind":"clip","id":"walk","version":1,"status":"draft","author":"fixture","provenance":"fixture","spec":{"duration":1000,"loop":false}}`,
	}
	for rel, data := range assets {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return root
}

// submitTroupePlay authors one draft play into the worktree and submits it
// through a fresh submitter, leaving a resolved play + index entry on disk.
func submitTroupePlay(t *testing.T, worktree, playID string) {
	t.Helper()
	draft := fmt.Sprintf(`{"kind":"play","id":"%s","status":"draft","author":"director","provenance":"generation g_01j9x","spec":{"instances":[{"id":"cat","model":"cat@1","role":"actor","scale":1,"x":0.1}],"timeline":[{"at":0,"on":"cat","clip":"walk@1"}]}}`, playID)
	path := filepath.Join(worktree, "plays", playID+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir plays: %v", err)
	}
	if err := os.WriteFile(path, []byte(draft), 0o644); err != nil {
		t.Fatalf("write draft: %v", err)
	}
	s, err := troupe.NewSubmitter(worktree)
	if err != nil {
		t.Fatalf("NewSubmitter: %v", err)
	}
	if _, err := s.Submit(playID); err != nil {
		t.Fatalf("submit %s: %v", playID, err)
	}
}

// newTroupeIndexer builds an Indexer with the troupe API wired over a seeded
// worktree.
func newTroupeIndexer(t *testing.T) (*Indexer, string) {
	t.Helper()
	worktree := seedTroupeWorktree(t)
	lib, err := troupe.NewPlayLibrary(worktree)
	if err != nil {
		t.Fatalf("NewPlayLibrary: %v", err)
	}
	fb, err := troupe.NewFeedbackWriter(worktree)
	if err != nil {
		t.Fatalf("NewFeedbackWriter: %v", err)
	}
	idx := &Indexer{
		troupeLibrary:  lib,
		troupeFeedback: fb,
	}
	return idx, worktree
}

// doTroupeGet issues one GET against the troupe mux.
func doTroupeGet(t *testing.T, idx *Indexer, path string) *httptest.ResponseRecorder {
	t.Helper()
	h := idx.TroupeHandler()
	if h == nil {
		t.Fatal("TroupeHandler must be non-nil when the troupe is wired")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// TestTroupeHandler_Disabled pins the gating: without the library or the
// feedback writer the troupe mux is nil — serve leaves the surface unmounted
// and the API answers 404.
func TestTroupeHandler_Disabled(t *testing.T) {
	idx := &Indexer{}
	if h := idx.TroupeHandler(); h != nil {
		t.Error("TroupeHandler must be nil when the troupe is not wired")
	}
	idx = &Indexer{troupeLibrary: mustLibrary(t)}
	if h := idx.TroupeHandler(); h != nil {
		t.Error("TroupeHandler must be nil without the feedback writer")
	}
}

func mustLibrary(t *testing.T) *troupe.PlayLibrary {
	t.Helper()
	l, err := troupe.NewPlayLibrary(seedTroupeWorktree(t))
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// TestTroupePlayResolved pins GET /play/resolved: it returns the newest
// submitted play — the later datetime id — and 404s on the empty stage.
func TestTroupePlayResolved(t *testing.T) {
	idx, worktree := newTroupeIndexer(t)
	submitTroupePlay(t, worktree, "story_20260820T161500Z")
	submitTroupePlay(t, worktree, "story_20260821T093000Z")

	rec := doTroupeGet(t, idx, "/api/v1/troupe/play/resolved")
	if rec.Code != http.StatusOK {
		t.Fatalf("resolved = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var rp troupe.ResolvedPlay
	if err := json.Unmarshal(rec.Body.Bytes(), &rp); err != nil {
		t.Fatalf("decode resolved: %v", err)
	}
	if rp.Play.ID != "story_20260821T093000Z" {
		t.Errorf("resolved id = %q, want the newest", rp.Play.ID)
	}

	empty, worktree2 := newTroupeIndexer(t)
	_ = worktree2 // no plays submitted — the empty stage
	rec2 := doTroupeGet(t, empty, "/api/v1/troupe/play/resolved")
	if rec2.Code != http.StatusNotFound {
		t.Errorf("empty stage = %d, want 404", rec2.Code)
	}
}

// TestTroupePlayGet pins GET /play/{id}: one play by id, 404 for a missing
// id, and the reserved literal — resolved never reaches the {id} route.
func TestTroupePlayGet(t *testing.T) {
	idx, worktree := newTroupeIndexer(t)
	submitTroupePlay(t, worktree, "story_20260820T161500Z")

	rec := doTroupeGet(t, idx, "/api/v1/troupe/play/story_20260820T161500Z")
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if rec := doTroupeGet(t, idx, "/api/v1/troupe/play/story_20260821T000000Z"); rec.Code != http.StatusNotFound {
		t.Errorf("missing play = %d, want 404", rec.Code)
	}
	// resolved is a reserved segment: the literal route answers (200 with
	// the newest play), never the {id} 404.
	if rec := doTroupeGet(t, idx, "/api/v1/troupe/play/resolved"); rec.Code != http.StatusOK {
		t.Errorf("reserved literal = %d, want 200 (matched before {id})", rec.Code)
	}
}

// TestTroupePlayIndex pins GET /play: pagination with the keyset cursor and
// the limit/order/author filters.
func TestTroupePlayIndex(t *testing.T) {
	idx, worktree := newTroupeIndexer(t)
	for _, id := range []string{
		"story_20260820T161500Z",
		"story_20260821T090000Z",
		"story_20260821T093000Z",
	} {
		submitTroupePlay(t, worktree, id)
	}

	rec := doTroupeGet(t, idx, "/api/v1/troupe/play?limit=2")
	if rec.Code != http.StatusOK {
		t.Fatalf("index = %d, want 200", rec.Code)
	}
	var page troupe.PlayPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if len(page.Index) != 2 || page.Index[0].ID != "story_20260821T093000Z" {
		t.Errorf("first page = %+v, want the two newest", page.Index)
	}
	rec2 := doTroupeGet(t, idx, "/api/v1/troupe/play?limit=2&cursor="+page.Next)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second page = %d, want 200", rec2.Code)
	}
	var page2 troupe.PlayPage
	if err := json.Unmarshal(rec2.Body.Bytes(), &page2); err != nil {
		t.Fatalf("decode page2: %v", err)
	}
	if len(page2.Index) != 1 || page2.Index[0].ID != "story_20260820T161500Z" || page2.Next != "" {
		t.Errorf("second page = %+v, want the oldest, no next cursor", page2)
	}

	recAsc := doTroupeGet(t, idx, "/api/v1/troupe/play?order=asc")
	var asc troupe.PlayPage
	if err := json.Unmarshal(recAsc.Body.Bytes(), &asc); err != nil {
		t.Fatal(err)
	}
	if asc.Index[0].ID != "story_20260820T161500Z" {
		t.Errorf("ascending first = %q, want the oldest", asc.Index[0].ID)
	}
}

// TestTroupeFeedback pins POST /feedback: a valid note answers 204 and lands
// as one file named <playId>_<type>_<utc>.json; a malformed body is 400 and
// a commit failure is 500.
func TestTroupeFeedback(t *testing.T) {
	idx, worktree := newTroupeIndexer(t)
	submitTroupePlay(t, worktree, "story_20260820T161500Z")

	body := `{"playId":"story_20260820T161500Z","type":"rating","data":{"rating":1,"comment":"more dog"}}`
	rec := httptest.NewRecorder()
	idx.TroupeHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/troupe/feedback", bytes.NewBufferString(body)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("feedback = %d, want 204 (%s)", rec.Code, rec.Body.String())
	}
	entries, err := os.ReadDir(filepath.Join(worktree, "feedback"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("feedback notes = %v, want exactly one file", entries)
	}
	if !strings.HasPrefix(entries[0].Name(), "story_20260820T161500Z_rating_") {
		t.Errorf("note name = %q, want the <playId>_<type>_<utc> prefix", entries[0].Name())
	}

	bad := `{"playId":"story_20260820T161500Z","type":"rating","data":{"rating":0}}`
	recBad := httptest.NewRecorder()
	idx.TroupeHandler().ServeHTTP(recBad, httptest.NewRequest(http.MethodPost, "/api/v1/troupe/feedback", bytes.NewBufferString(bad)))
	if recBad.Code != http.StatusBadRequest {
		t.Errorf("invalid note = %d, want 400", recBad.Code)
	}

	// Commit failure surfaces as 500, never a silent drop.
	fb, err := troupe.NewFeedbackWriter(worktree, troupe.WithFeedbackCommit(func(string) error {
		return fmt.Errorf("commit exploded")
	}))
	if err != nil {
		t.Fatal(err)
	}
	failing := &Indexer{troupeLibrary: idx.troupeLibrary, troupeFeedback: fb}
	rec500 := httptest.NewRecorder()
	failing.TroupeHandler().ServeHTTP(rec500, httptest.NewRequest(http.MethodPost, "/api/v1/troupe/feedback", bytes.NewBufferString(`{"playId":"story_20260820T161500Z","type":"replay","data":{"count":2}}`)))
	if rec500.Code != http.StatusInternalServerError {
		t.Errorf("commit failure = %d, want 500", rec500.Code)
	}
}
