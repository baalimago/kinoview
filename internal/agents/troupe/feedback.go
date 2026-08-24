package troupe

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// The audience note types: the viewer-facing feedback/ notes (decision 21).
// criticism is the sixth type, written server-side by the critic (critic.go);
// the audience never sends it.
const (
	NoteTypeRating     = "rating"
	NoteTypeDismissal  = "dismissal"
	NoteTypeCompletion = "completion"
	NoteTypeReplay     = "replay"
	NoteTypeContinuity = "continuity"
)

// audienceNoteTypes is the closed set of notes the audience may post. An
// unknown type is refused with an exact error listing the set.
var audienceNoteTypes = map[string]bool{
	NoteTypeRating:     true,
	NoteTypeDismissal:  true,
	NoteTypeCompletion: true,
	NoteTypeReplay:     true,
	NoteTypeContinuity: true,
}

// ErrFeedbackInvalid is the writer's validation sentinel: the note is
// well-formed JSON but violates the feedback contract — a malformed play id,
// a play not on disk, an unknown type, invalid data, or a note the
// append-only rule refuses. The play API maps it to 400; anything else — a
// persistence or commit failure — is 500.
var ErrFeedbackInvalid = errors.New("troupe: feedback: invalid note")

// FeedbackRequest is the audience's POST body: playId, type and data. The
// server stamps ts — the client never sends it (decision 21) — and derives
// the filename from it.
type FeedbackRequest struct {
	PlayID string          `json:"playId"`
	Type   string          `json:"type"`
	Data   json.RawMessage `json:"data"`
}

// FeedbackNote is one feedback/ note as persisted: the uniform envelope
// (playId/type/ts/data, decision 21). The data stays raw, byte-preserving
// whatever the audience sent.
type FeedbackNote struct {
	PlayID string          `json:"playId"`
	Type   string          `json:"type"`
	TS     string          `json:"ts"`
	Data   json.RawMessage `json:"data"`
}

// The kind-specific data shapes. Each is validated strictly — the closed
// envelope rule applies to notes as it does to assets: an unknown field is a
// refusal, and a required field that is missing or out of range is a
// refusal.
type (
	// RatingData is a thumbs vote: +1 up, -1 down, an optional comment.
	RatingData struct {
		Rating  int    `json:"rating"`
		Comment string `json:"comment,omitempty"`
	}
	// DismissalData is where the viewer stopped watching, in ms.
	DismissalData struct {
		AtMs int64 `json:"atMs"`
	}
	// CompletionData is how long the play ran to completion, in ms.
	CompletionData struct {
		DurationMs int64 `json:"durationMs"`
	}
	// ReplayData is how many times the play was replayed.
	ReplayData struct {
		Count int `json:"count"`
	}
	// ContinuityData is the viewing history the play ran against.
	ContinuityData struct {
		History []json.RawMessage `json:"history"`
	}
)

// FeedbackWriter is the audience's append-only gate into feedback/: the
// handler-side counterpart of the critic's CriticismWriter, sharing the
// uniform envelope, the server-side ts stamp and the filename derivation.
// It validates the note, stamps ts, derives <playId>_<type>_<utc>.json and
// persists the note atomically; the commit half of the write+commit unit is
// the WithFeedbackCommit seam, wired by phase 9's serve setup. A note is
// append-only: an existing file at the derived name is refused, never
// overwritten.
type FeedbackWriter struct {
	mu       sync.Mutex                  // appends must serialize: the exists-check and the atomic rename
	worktree string                      // the materialised notebook worktree
	commit   func(filename string) error // nil: no commit (tests)
	now      func() time.Time            // test seam; production stamps time.Now
}

// FeedbackWriterOption configures one FeedbackWriter.
type FeedbackWriterOption func(*FeedbackWriter)

// WithFeedbackCommit sets the commit half of the write+commit unit: a
// function committing the worktree through the shared notebook after a note
// is written. Production wires the slivingdoc commit; tests leave it unset.
// A commit failure surfaces from Write, so the handler answers 500 — a note
// is never silently dropped.
func WithFeedbackCommit(fn func(filename string) error) FeedbackWriterOption {
	return func(w *FeedbackWriter) { w.commit = fn }
}

// WithFeedbackClock overrides the writer's clock — a test seam only, never
// an operator surface: the note's ts is always the server time in
// production, and the filename derives from it.
func WithFeedbackClock(now func() time.Time) FeedbackWriterOption {
	return func(w *FeedbackWriter) { w.now = now }
}

// NewFeedbackWriter builds the audience gate over a materialised notebook
// worktree — the same directory the play API serves plays from and the
// critic writes criticism into.
func NewFeedbackWriter(worktree string, opts ...FeedbackWriterOption) (*FeedbackWriter, error) {
	if worktree == "" {
		return nil, errors.New("troupe: feedback writer: worktree can't be empty")
	}
	w := &FeedbackWriter{worktree: worktree}
	for _, o := range opts {
		o(w)
	}
	return w, nil
}

// Write validates and persists one audience note. Exact errors return
// wherever the note violates the contract — a malformed play id, a play not
// on disk, an unknown type, invalid data, an existing filename — and nothing
// is written. The note's ts is stamped here, server-side, and the filename
// derives from it, so the filename and the body never drift. When a commit
// seam is wired, the commit is part of the same unit: a commit failure
// returns an error and the note is never silently dropped.
func (w *FeedbackWriter) Write(req FeedbackRequest) (FeedbackNote, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	e := &errs{}
	if !playIDRe.MatchString(req.PlayID) {
		e.addf("playId: %q must match story_YYYYMMDDTHHMMSSZ", req.PlayID)
	} else if _, err := os.Stat(filepath.Join(w.worktree, "plays", req.PlayID+".json")); err != nil {
		e.addf("playId: %s is not on disk in plays/ — never note a play that does not exist", req.PlayID)
	}
	if !audienceNoteTypes[req.Type] {
		e.addf("type: %q is not an audience note type (rating/dismissal/completion/replay/continuity)", req.Type)
	} else if err := validateFeedbackData(req.Type, req.Data); err != nil {
		e.addf("%v", err)
	}
	if err := e.err(); err != nil {
		return FeedbackNote{}, fmt.Errorf("%w: %v", ErrFeedbackInvalid, err)
	}

	now := time.Now().UTC()
	if w.now != nil {
		now = w.now().UTC()
	}
	note := FeedbackNote{
		PlayID: req.PlayID,
		Type:   req.Type,
		TS:     now.Format(time.RFC3339),
		Data:   req.Data,
	}
	filename := feedbackFilename(req.PlayID, req.Type, now)
	target := filepath.Join(w.worktree, "feedback", filename)
	if _, err := os.Stat(target); err == nil {
		return FeedbackNote{}, fmt.Errorf("%w: %s already exists — notes are append-only", ErrFeedbackInvalid, filename)
	}
	data, err := json.Marshal(note)
	if err != nil {
		return FeedbackNote{}, fmt.Errorf("troupe: feedback: marshal: %w", err)
	}
	if err := writeFileAtomic(target, data); err != nil {
		return FeedbackNote{}, fmt.Errorf("troupe: feedback: %w", err)
	}
	if w.commit != nil {
		if err := w.commit(filename); err != nil {
			return FeedbackNote{}, fmt.Errorf("troupe: feedback: %s: commit: %w", filename, err)
		}
	}
	return note, nil
}

// validateFeedbackData checks one audience note's data against its type's
// closed shape: strict decode (an unknown field is a refusal) plus the
// range/required rules — rating ±1, non-negative positions, a positive
// replay count, a non-empty history.
func validateFeedbackData(noteType string, data []byte) error {
	switch noteType {
	case NoteTypeRating:
		var d RatingData
		if err := decodeStrict(data, &d, "data"); err != nil {
			return err
		}
		if d.Rating != 1 && d.Rating != -1 {
			return errors.New("data: rating must be +1 or -1")
		}
	case NoteTypeDismissal:
		var d DismissalData
		if err := decodeStrict(data, &d, "data"); err != nil {
			return err
		}
		if d.AtMs < 0 {
			return errors.New("data: atMs must not be negative")
		}
	case NoteTypeCompletion:
		var d CompletionData
		if err := decodeStrict(data, &d, "data"); err != nil {
			return err
		}
		if d.DurationMs < 0 {
			return errors.New("data: durationMs must not be negative")
		}
	case NoteTypeReplay:
		var d ReplayData
		if err := decodeStrict(data, &d, "data"); err != nil {
			return err
		}
		if d.Count < 1 {
			return errors.New("data: count must be a positive number of replays")
		}
	case NoteTypeContinuity:
		var d ContinuityData
		if err := decodeStrict(data, &d, "data"); err != nil {
			return err
		}
		if len(d.History) == 0 {
			return errors.New("data: history must not be empty")
		}
	}
	return nil
}

// feedbackFilename derives the feedback/ filename of one note:
// <playId>_<type>_<utc>.json, or <type>_<utc>.json when the note carries no
// play id (the critic's empty-stage criticism). The <utc> is the compact
// form of the stamped ts, so the filename and the body never drift.
func feedbackFilename(playID, noteType string, ts time.Time) string {
	utc := ts.UTC().Format("20060102T150405Z")
	if playID == "" {
		return noteType + "_" + utc + ".json"
	}
	return playID + "_" + noteType + "_" + utc + ".json"
}
