package troupe

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ErrPlayNotFound is the library's not-found sentinel: no play at the id, or
// no submitted play at all (the empty stage). The play API maps it to 404;
// anything else is a broken notebook (500).
var ErrPlayNotFound = errors.New("troupe: play not found")

// PlayPageQuery is one paginated play-index query (decision 20): the keyset
// cursor, page size, order and the status/author filters. The index holds
// only submitted plays, so the status filter can only ever match
// "submitted"; it exists to keep the API surface uniform, and a filter
// matching nothing is an honest empty page.
type PlayPageQuery struct {
	Limit  int    // page size; <= 0 defaults to 20, > 100 clamps to 100
	Order  string // "asc" | "desc"; empty defaults to desc (newest first)
	Status string // optional exact match on the entry status
	Author string // optional exact match on the entry author
	Cursor string // keyset boundary: the id of the last entry of the previous page
}

// PlayPage is one paginated response: the page's entries (newest-first by
// default) and the keyset cursor for the next page — the last id of this
// page, empty when this is the last page.
type PlayPage struct {
	Index []PlayIndexEntry `json:"index"`
	Next  string           `json:"next,omitempty"`
}

const (
	// defaultPlayPageSize is the page size when the query carries none.
	defaultPlayPageSize = 20
	// maxPlayPageSize clamps an oversized limit so one request cannot demand
	// the whole history.
	maxPlayPageSize = 100
)

// PlayLibrary is the read-only view of the submitted-play history: the play
// API reads the newest submitted play and pages over plays/index.json, never
// touching an in-memory story (decision 15 — the served play is always read
// from disk). The library shares the Submitter's worktree and index format;
// it writes nothing.
type PlayLibrary struct {
	worktree string
}

// NewPlayLibrary builds the read-only play view over a materialised notebook
// worktree — the same directory the submitter persists plays into and the
// play API reads them from.
func NewPlayLibrary(worktree string) (*PlayLibrary, error) {
	if worktree == "" {
		return nil, errors.New("troupe: play library: worktree can't be empty")
	}
	return &PlayLibrary{worktree: worktree}, nil
}

// Newest returns the newest submitted play: the first entry of the
// newest-first plays/index.json, then the resolved play file under that id.
// An empty index — no play submitted yet — is the empty stage: ErrPlayNotFound.
func (p *PlayLibrary) Newest() (ResolvedPlay, error) {
	idx, err := readIndex(filepath.Join(p.worktree, playIndex))
	if err != nil {
		return ResolvedPlay{}, err
	}
	if len(idx.Index) == 0 {
		return ResolvedPlay{}, fmt.Errorf("troupe: newest play: %w", ErrPlayNotFound)
	}
	return p.Get(idx.Index[0].ID)
}

// Get returns one play by its story_<UTC> datetime id, read from
// plays/<id>.json. A malformed id or a missing file is ErrPlayNotFound; a
// file that is not a resolved play (never written by submit_play) is an
// error — the index and the play files are written atomically in order, so
// an inconsistent shape is a broken notebook, not an absent play.
func (p *PlayLibrary) Get(id string) (ResolvedPlay, error) {
	if !playIDRe.MatchString(id) {
		return ResolvedPlay{}, fmt.Errorf("troupe: play %q: %w", id, ErrPlayNotFound)
	}
	data, err := os.ReadFile(filepath.Join(p.worktree, "plays", id+".json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ResolvedPlay{}, fmt.Errorf("troupe: play %q: %w", id, ErrPlayNotFound)
		}
		return ResolvedPlay{}, fmt.Errorf("troupe: play %q: %w", id, err)
	}
	var rp ResolvedPlay
	if err := json.Unmarshal(data, &rp); err != nil {
		return ResolvedPlay{}, fmt.Errorf("troupe: play %q: %w", id, err)
	}
	return rp, nil
}

// Page returns one page of the play index: the entries are filtered by
// status/author, ordered by datetime id (desc, newest first, by default),
// cut at the keyset cursor — the id of the previous page's last entry — and
// sliced to the page size. The returned Next is the cursor for the next
// page: the last id of this page, or empty on the last page. A missing index
// is an empty history, not an error.
func (p *PlayLibrary) Page(q PlayPageQuery) (PlayPage, error) {
	idx, err := readIndex(filepath.Join(p.worktree, playIndex))
	if err != nil {
		return PlayPage{}, err
	}
	entries := idx.Index
	if q.Status != "" {
		entries = slices.DeleteFunc(entries, func(e PlayIndexEntry) bool { return string(e.Status) != q.Status })
	}
	if q.Author != "" {
		entries = slices.DeleteFunc(entries, func(e PlayIndexEntry) bool { return e.Author != q.Author })
	}

	desc := q.Order != "asc"
	slices.SortFunc(entries, func(a, b PlayIndexEntry) int {
		if desc {
			return strings.Compare(b.ID, a.ID)
		}
		return strings.Compare(a.ID, b.ID)
	})

	if q.Cursor != "" {
		entries = slices.DeleteFunc(entries, func(e PlayIndexEntry) bool {
			if desc {
				return e.ID >= q.Cursor
			}
			return e.ID <= q.Cursor
		})
	}

	limit := q.Limit
	if limit <= 0 {
		limit = defaultPlayPageSize
	}
	if limit > maxPlayPageSize {
		limit = maxPlayPageSize
	}
	page := PlayPage{Index: entries}
	if len(entries) > limit {
		page.Index = entries[:limit]
		page.Next = page.Index[len(page.Index)-1].ID
	}
	return page, nil
}
