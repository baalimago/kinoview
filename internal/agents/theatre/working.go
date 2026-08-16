package theatre

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/model"
)

// Working is the draft story mid-production: the story itself plus the
// bookkeeping that sits around it — a revision counter, the pipeline stage
// the draft has reached, the canon facts the playwright has appended (soft
// continuity, D6) and the playwright's draft report (its author's act
// structure supersedes the derived count). The stage manager owns this file;
// subagents deliver into it through their writer tools.
type Working struct {
	Story    model.Story `json:"story"`
	Revision int         `json:"revision"`
	Status   string      `json:"status"`

	// Validated records that the draft has passed the playability gate via
	// the director's validate_story tool. The exhaustion path ships only a
	// validated draft (review 7, R7-01): a playable file with status "draft"
	// was never blessed, so it falls through to the composer floor. Every
	// writer that rewrites the draft clears the flag — the blessing belongs
	// to the exact content that passed the gate, not to the working file.
	Validated bool `json:"validated,omitempty"`

	// Canon holds this generation's canon facts: short past-tense outcome
	// statements the playwright riffed on. They ride in the working summary
	// so every role in the generation reads them.
	Canon []string `json:"canon,omitempty"`

	// Report is the playwright's draft report, when it delivered one: the
	// author's own act structure and the canon facts it kept (phase 5).
	Report *DraftReport `json:"report,omitempty"`
}

// Summary is the compact shape of the working file that every agent context
// shows: enough for a role to know where the draft stands without shipping the
// whole story into the prompt.
type Summary struct {
	Title    string
	Cast     []string
	Beats    int
	Acts     int
	Backdrop string
	Status   string
	Canon    []string
}

// Summary renders the working file in its context-cheap shape. The act count
// comes from the playwright's own draft report when it delivered one; the
// derived count (set changes) is the fallback.
func (w Working) Summary() Summary {
	acts := actsOf(w.Story)
	if w.Report != nil && len(w.Report.Acts) > 0 {
		acts = len(w.Report.Acts)
	}
	out := Summary{
		Title:    w.Story.Title,
		Beats:    len(w.Story.Beats),
		Acts:     acts,
		Backdrop: w.Story.Scene.Backdrop,
		Status:   w.Status,
		Canon:    w.Canon,
	}
	for _, c := range w.Story.Cast {
		out.Cast = append(out.Cast, c.ID)
	}
	return out
}

// actsOf derives the act count from the story. The only act boundary the model
// knows is a set change, so the count is one plus the number of setBackdrop
// beats: a single backdrop is a one-act piece. The playwright's draft report
// carries the author's own act structure, which supersedes this (phase 5).
func actsOf(s model.Story) int {
	acts := 1
	for _, b := range s.Beats {
		if b.Action == "setBackdrop" {
			acts++
		}
	}
	return acts
}

// normalize repairs the working file in place. The story runs through
// model.Story.Validate — the same trust boundary as a fresh LLM reply — and an
// unplayable story rejects the whole file: a draft that cannot be staged is
// worse than no draft, and the caller answers with the composer floor.
func (w *Working) normalize() error {
	if w.Revision < 0 {
		w.Revision = 0
	}
	w.Status = strings.ToLower(strings.TrimSpace(w.Status))
	if !ValidWorkingStatuses[w.Status] {
		// An unknown status means a newer or hand-edited file; the draft
		// itself may be perfectly good, so default rather than reject.
		w.Status = "draft"
	}
	// Canon facts are untrusted LLM text: capped in count and length, trimmed
	// of whitespace, deduped, and dropped when empty.
	canon := make([]string, 0, len(w.Canon))
	seen := map[string]bool{}
	for _, f := range w.Canon {
		f = strings.TrimSpace(f)
		if f == "" || len(canon) >= CanonMaxFacts || seen[f] {
			continue
		}
		seen[f] = true
		canon = append(canon, truncateRunes(f, CanonMaxFact))
	}
	w.Canon = canon
	// The draft report is untrusted LLM text too: same repair, and an empty
	// report is dropped rather than stored.
	if w.Report != nil {
		normalizeDraftReport(w.Report)
		if w.Report.isEmpty() {
			w.Report = nil
		}
	}
	return w.Story.Validate()
}

// LoadWorking reads the draft. It returns an error whenever no usable draft
// exists — the file is missing, corrupt, or the story fails validation — and
// the caller answers with the composer floor. A missing file is not logged:
// "no draft yet" is a normal state early in a generation.
func (c *Company) LoadWorking() (Working, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, err := os.ReadFile(c.workingPath())
	if err != nil {
		if os.IsNotExist(err) {
			return Working{}, err
		}
		ancli.Errf("theatre: working file unreadable: %v", err)
		return Working{}, err
	}
	var w Working
	if err := json.Unmarshal(b, &w); err != nil {
		ancli.Errf("theatre: working file corrupt: %v", err)
		return Working{}, err
	}
	if err := w.normalize(); err != nil {
		ancli.Errf("theatre: working draft rejected: %v", err)
		return Working{}, err
	}
	return w, nil
}

// SaveWorking writes the draft atomically, rejecting a story that cannot be
// staged: an invalid draft is refused at the wrapper boundary, before it ever
// reaches the working file.
func (c *Company) SaveWorking(w Working) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := w.normalize(); err != nil {
		return fmt.Errorf("working draft rejected: %w", err)
	}
	data, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal working file: %w", err)
	}
	return writeFileAtomic(c.workingPath(), data)
}
