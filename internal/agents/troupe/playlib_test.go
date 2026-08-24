package troupe

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// submitDraft authors one draft play into the seeded worktree and submits it
// through a fresh submitter, leaving a real resolved play + index entry on
// disk — the shape the library reads. author lets a test vary the index
// entries for the author filter.
func submitDraft(t *testing.T, worktree, playID, author string) {
	t.Helper()
	if author == "" {
		author = "director"
	}
	draft := fmt.Sprintf(`{"kind":"play","id":"%s","status":"draft","author":"%s","provenance":"generation g_01j9x","spec":{"instances":[{"id":"cat","model":"cat@1","role":"actor","scale":1,"x":0.1}],"timeline":[{"at":0,"on":"cat","clip":"walk@1"}]}}`, playID, author)
	path := filepath.Join(worktree, "plays", playID+".json")
	if err := os.WriteFile(path, []byte(draft), 0o644); err != nil {
		t.Fatalf("write draft %s: %v", playID, err)
	}
	s, err := NewSubmitter(worktree)
	if err != nil {
		t.Fatalf("NewSubmitter: %v", err)
	}
	if _, err := s.Submit(playID); err != nil {
		t.Fatalf("submit %s: %v", playID, err)
	}
}

// newTestLibrary builds a library over a seeded temp worktree.
func newTestLibrary(t *testing.T) *PlayLibrary {
	t.Helper()
	l, err := NewPlayLibrary(seedWorktree(t))
	if err != nil {
		t.Fatalf("NewPlayLibrary: %v", err)
	}
	return l
}

// TestNewPlayLibrary_Errors pins the required option: a library without a
// worktree is refused before anything runs.
func TestNewPlayLibrary_Errors(t *testing.T) {
	if _, err := NewPlayLibrary(""); err == nil || !strings.Contains(err.Error(), "worktree can't be empty") {
		t.Fatalf("err = %v, want a worktree error", err)
	}
}

// TestPlayLibrary_Newest pins the headline read: the newest submitted play is
// the first plays/index.json entry and its resolved play file on disk — the
// served artifact, never an in-memory story.
func TestPlayLibrary_Newest(t *testing.T) {
	l := newTestLibrary(t)
	submitDraft(t, l.worktree, "story_20260821T090000Z", "")
	submitDraft(t, l.worktree, "story_20260821T093000Z", "")

	rp, err := l.Newest()
	if err != nil {
		t.Fatalf("Newest: %v", err)
	}
	if rp.Play.ID != "story_20260821T093000Z" {
		t.Errorf("newest id = %q, want the later datetime id", rp.Play.ID)
	}
	if rp.Play.Status != StatusSubmitted {
		t.Errorf("newest status = %q, want submitted", rp.Play.Status)
	}
	if len(rp.Assets.Models) == 0 {
		t.Error("the newest play must carry its flattened asset table")
	}
}

// TestPlayLibrary_Newest_EmptyStage pins the empty stage: no submitted play
// means ErrPlayNotFound — the API's 404, never a fabricated play.
func TestPlayLibrary_Newest_EmptyStage(t *testing.T) {
	l := newTestLibrary(t)
	if _, err := l.Newest(); !errors.Is(err, ErrPlayNotFound) {
		t.Fatalf("Newest on an empty index = %v, want ErrPlayNotFound", err)
	}
}

// TestPlayLibrary_Get pins one-play reads: a submitted play returns by its
// datetime id; a malformed id, a missing play and a never-submitted id are
// all ErrPlayNotFound.
func TestPlayLibrary_Get(t *testing.T) {
	l := newTestLibrary(t)
	submitDraft(t, l.worktree, "story_20260821T090000Z", "")

	rp, err := l.Get("story_20260821T090000Z")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rp.Play.ID != "story_20260821T090000Z" {
		t.Errorf("id = %q", rp.Play.ID)
	}

	for _, id := range []string{"story_20260821T090001Z", "cat@1", "resolved", ""} {
		if _, err := l.Get(id); !errors.Is(err, ErrPlayNotFound) {
			t.Errorf("Get(%q) = %v, want ErrPlayNotFound", id, err)
		}
	}
}

// TestPlayLibrary_Page pins the keyset pagination: newest-first by default,
// the page size, the next cursor chaining, both orders and the
// status/author filters.
func TestPlayLibrary_Page(t *testing.T) {
	l := newTestLibrary(t)
	ids := []string{
		"story_20260820T161500Z",
		"story_20260821T090000Z",
		"story_20260821T093000Z",
		"story_20260822T080000Z",
	}
	for i, id := range ids {
		author := "director"
		if i%2 == 1 {
			author = "understudy"
		}
		submitDraft(t, l.worktree, id, author)
	}

	t.Run("newest-first default order", func(t *testing.T) {
		page, err := l.Page(PlayPageQuery{Limit: 2})
		if err != nil {
			t.Fatalf("Page: %v", err)
		}
		got := pageIDs(page)
		if len(got) != 2 || got[0] != "story_20260822T080000Z" || got[1] != "story_20260821T093000Z" {
			t.Errorf("first page = %v, want the two newest", got)
		}
		if page.Next != got[1] {
			t.Errorf("next cursor = %q, want the last id %q", page.Next, got[1])
		}

		page2, err := l.Page(PlayPageQuery{Limit: 2, Cursor: page.Next})
		if err != nil {
			t.Fatalf("Page(cursor): %v", err)
		}
		got2 := pageIDs(page2)
		if len(got2) != 2 || got2[0] != "story_20260821T090000Z" || got2[1] != "story_20260820T161500Z" {
			t.Errorf("second page = %v, want the remaining two", got2)
		}
		if page2.Next != "" {
			t.Errorf("second page next = %q, want empty (last page)", page2.Next)
		}
	})

	t.Run("ascending order", func(t *testing.T) {
		page, err := l.Page(PlayPageQuery{Limit: 100, Order: "asc"})
		if err != nil {
			t.Fatalf("Page: %v", err)
		}
		got := pageIDs(page)
		if got[0] != "story_20260820T161500Z" || got[len(got)-1] != "story_20260822T080000Z" {
			t.Errorf("ascending page = %v, want oldest first", got)
		}
	})

	t.Run("author filter", func(t *testing.T) {
		page, err := l.Page(PlayPageQuery{Author: "understudy"})
		if err != nil {
			t.Fatalf("Page: %v", err)
		}
		got := pageIDs(page)
		if len(got) != 2 {
			t.Errorf("author-filtered page = %v, want the two understudy plays", got)
		}
		for _, id := range got {
			if id != "story_20260821T090000Z" && id != "story_20260822T080000Z" {
				t.Errorf("author filter leaked %q into the page", id)
			}
		}
	})

	t.Run("status filter matches everything or nothing", func(t *testing.T) {
		all, err := l.Page(PlayPageQuery{Status: string(StatusSubmitted)})
		if err != nil {
			t.Fatalf("Page: %v", err)
		}
		if len(pageIDs(all)) != 4 {
			t.Errorf("submitted filter = %d entries, want 4 (the index holds only submitted)", len(pageIDs(all)))
		}
		none, err := l.Page(PlayPageQuery{Status: string(StatusDraft)})
		if err != nil {
			t.Fatalf("Page: %v", err)
		}
		if len(pageIDs(none)) != 0 {
			t.Errorf("draft filter = %v, want an empty page", pageIDs(none))
		}
	})

	t.Run("limit clamps", func(t *testing.T) {
		page, err := l.Page(PlayPageQuery{Limit: 1000})
		if err != nil {
			t.Fatalf("Page: %v", err)
		}
		if len(pageIDs(page)) != 4 {
			t.Errorf("clamped page = %d entries, want 4", len(pageIDs(page)))
		}
	})
}

// TestPlayLibrary_Page_EmptyIndex pins the empty history: a missing index is
// an empty page, not an error — a fresh notebook pages cleanly.
func TestPlayLibrary_Page_EmptyIndex(t *testing.T) {
	l := newTestLibrary(t)
	page, err := l.Page(PlayPageQuery{})
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if len(page.Index) != 0 || page.Next != "" {
		t.Errorf("empty page = %+v, want no entries and no cursor", page)
	}
}

func pageIDs(page PlayPage) []string {
	ids := make([]string, 0, len(page.Index))
	for _, e := range page.Index {
		ids = append(ids, e.ID)
	}
	return ids
}
