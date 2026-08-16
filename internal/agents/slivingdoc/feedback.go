package slivingdoc

import (
	"context"
	"fmt"
	"time"

	"github.com/baalimago/kinoview/internal/agents"
)

// feedbackName is the conventional audience-notes file in the shared
// notebook: one JSON line per note, append-only (decision Q6).
const feedbackName = "feedback.jsonl"

// feedbackRecord is one audience note as it lands on the line. Whatever the
// audience sent is what lands — no field is rewritten or dropped, and the
// comment is always present, empty or not.
type feedbackRecord struct {
	StoryID string `json:"storyId"`
	Rating  int    `json:"rating"` // +1 thumbs up, -1 thumbs down
	Comment string `json:"comment"`
	TS      string `json:"ts"` // receive time, RFC 3339 UTC
}

// FeedbackRecorder implements agents.Feedbacker over the shared notebook:
// each note is appended to feedback.jsonl and committed, so the audience's
// verdict is durable, merged like every other note and readable by the next
// generation's roles. It replaces the theatre facade's old audience doc
// (removed in phase 4) — feedback no longer touches the theatre at all.
type FeedbackRecorder struct {
	notebook *Notebook
}

// NewFeedbackRecorder wires the recorder to the handler-side notebook seam.
func NewFeedbackRecorder(notebook *Notebook) *FeedbackRecorder {
	return &FeedbackRecorder{notebook: notebook}
}

// Feedback records one audience note. Append and commit are one unit: a
// commit that fails returns an error so the handler answers 500 rather than
// silently losing the note.
func (r *FeedbackRecorder) Feedback(_ context.Context, storyID string, rating int, comment string) error {
	rec := feedbackRecord{
		StoryID: storyID,
		Rating:  rating,
		Comment: comment,
		TS:      time.Now().UTC().Format(time.RFC3339),
	}
	if err := r.notebook.AppendJSONL(feedbackName, rec); err != nil {
		return fmt.Errorf("feedback: %w", err)
	}
	return nil
}

// Compile-time proof that the recorder satisfies the indexer's contract.
var _ agents.Feedbacker = (*FeedbackRecorder)(nil)
