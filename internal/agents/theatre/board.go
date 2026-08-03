package theatre

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Board is the per-generation shared worklog. Every agent reads it and may
// write to it; it is the shared memory that makes stateless agent spawns
// conversational (decision D3). The board is ephemeral — at submit it is
// distilled into the company docs — and bounded, because it is read back into
// prompts.
type Board struct {
	Generation string  `json:"generation"`
	Theme      string  `json:"theme"`
	Entries    []Entry `json:"entries"`
}

// Entry is one row of the board worklog. Seq is assigned at append time and
// renumbered on load so an excerpt is always contiguous.
type Entry struct {
	Seq    int    `json:"seq"`
	Author string `json:"author"`
	Kind   string `json:"kind"`
	To     string `json:"to,omitempty"`
	Body   string `json:"body"`
}

// Append adds an entry to the board, enforcing the same gate as load: unknown
// kinds and authors are dropped, invalid addressees clear, over-long bodies
// are truncated, and the newest BoardMaxEntries entries are kept. Seq is
// renumbered after every append so the worklog is always contiguous.
func (b *Board) Append(e Entry) {
	e.normalize()
	if !ValidBoardKinds[e.Kind] || !ValidRoles[e.Author] {
		return
	}
	if e.To != "" && !ValidRoles[e.To] {
		e.To = ""
	}
	b.Entries = append(b.Entries, e)
	if len(b.Entries) > BoardMaxEntries {
		b.Entries = b.Entries[len(b.Entries)-BoardMaxEntries:]
	}
	for i := range b.Entries {
		b.Entries[i].Seq = i + 1
	}
}

// Excerpt returns the last n entries, oldest first — the slice AssembleContext
// renders. Board growth beyond the excerpt cap never grows a prompt. The
// result is a copy, so callers may not disturb the board through it.
func (b Board) Excerpt(n int) []Entry {
	if n <= 0 || len(b.Entries) == 0 {
		return nil
	}
	from := 0
	if len(b.Entries) > n {
		from = len(b.Entries) - n
	}
	out := make([]Entry, len(b.Entries)-from)
	copy(out, b.Entries[from:])
	return out
}

// normalize repairs the board in place so it is safe to read into a prompt:
// unknown kinds and roles are dropped, over-long bodies are truncated, seqs
// are renumbered contiguously and the board is capped to its newest entries.
// The board is untrusted data — LLM-written or hand-edited — so append and
// load run the same gate. It never fails: a partially odd board is better
// than no board.
func (b *Board) normalize() {
	out := make([]Entry, 0, len(b.Entries))
	for _, e := range b.Entries {
		e.normalize()
		if !ValidBoardKinds[e.Kind] || !ValidRoles[e.Author] {
			continue
		}
		if e.To != "" && !ValidRoles[e.To] {
			e.To = ""
		}
		out = append(out, e)
	}
	if len(out) > BoardMaxEntries {
		out = out[len(out)-BoardMaxEntries:]
	}
	for i := range out {
		out[i].Seq = i + 1
	}
	b.Entries = out
}

// normalize bounds a single entry in place: roles and kinds are lowercased so
// "Playwright" and "playwright" are one role, and an over-long body is
// truncated. It never errors; unknown values are dropped by the caller.
func (e *Entry) normalize() {
	e.Author = strings.ToLower(strings.TrimSpace(e.Author))
	e.To = strings.ToLower(strings.TrimSpace(e.To))
	e.Kind = strings.ToLower(strings.TrimSpace(e.Kind))
	e.Body = truncateRunes(e.Body, EntryMaxBody)
}

// LoadBoard reads the board, or the empty board if none exists yet. A missing
// file is not an error; a corrupt one is logged and reported, and the empty
// board is the usable fallback.
func (c *Company) LoadBoard() (Board, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var b Board
	if err := readJSON(c.boardPath(), &b); err != nil {
		logLoadFailure("board", err)
		return Board{}, err
	}
	b.normalize()
	return b, nil
}

// appendBoardEntry appends one entry to the board file, atomically. A board
// that cannot be read degrades to the empty board — a corrupt worklog is
// replaced by the fresh entry rather than blocking the production. The error
// is returned unlogged: callers report in their own voice (the post_to_board
// tool tells the model, the broker logs).
func appendBoardEntry(c *Company, e Entry) error {
	board, err := c.LoadBoard()
	if err != nil {
		board = Board{}
	}
	board.Append(e)
	return c.SaveBoard(board)
}

// SaveBoard writes the board atomically, running the same gate as load so a
// board that could not be read back is never written in the first place.
// The copy passed in is normalised on disk; the caller's in-memory board is
// not mutated.
func (c *Company) SaveBoard(b Board) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	b.normalize()
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal board: %w", err)
	}
	return writeFileAtomic(c.boardPath(), data)
}
