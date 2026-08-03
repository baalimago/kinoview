package theatre

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/baalimago/kinoview/internal/model"
)

// Distillation (phase 6): at submit, one deterministic pass over the
// generation's paperwork writes the company's durable memory. The agents
// write the board and the artifacts; distillation copies the pieces the docs
// keep — the LLM never writes the docs directly (the integration contract).
// The story is already persisted by the time distillation runs, so a failure
// here is logged and never loses the show; the next submit writes again.

// distill folds one generation's work into the company library: premises
// from the brief, repertoire from the draft, sets from the dressed scene,
// director lessons from the submit call's notes, bulletin from the board's
// decisions and role notes, and the registry always (the costumer's book is
// not a log). A missing artifact leaves its doc untouched — only the docs
// whose artifact exists are updated (the error table). Every document is
// written independently, so a failed write never skips the others.
func (p *production) distill() error {
	board, err := p.company.LoadBoard()
	if err != nil {
		board = Board{}
	}
	w, err := p.company.LoadWorking()
	if err != nil {
		return fmt.Errorf("distill: working file unreadable: %w", err)
	}
	now := time.Now()

	lib := p.company.LoadLibrary()
	lib.Registry = p.theatre.registry.Doc()

	if pr, ok := premiseFrom(board, w, now); ok {
		lib.Premises = trimPremises(append(PremisesDoc{pr}, lib.Premises...))
	}
	if sum, facts, ok := repertoireFrom(w, now); ok {
		lib.Repertoire.Summaries = trimSummaries(append([]PlaySummary{sum}, lib.Repertoire.Summaries...))
		lib.Repertoire.Facts = trimFacts(append(append([]string{}, facts...), lib.Repertoire.Facts...))
	}
	if recipe, ok := setsFrom(w, now); ok {
		lib.Sets = trimSets(append(SetsDoc{bumpSetRecipe(recipe, lib.Sets)}, lib.Sets...))
	}
	if lessons := lessonsFrom(p.lessons, now); len(lessons) > 0 {
		lib.Director = trimLessons(append(lessons, lib.Director...))
	}
	if notices := bulletinFrom(board, now); len(notices) > 0 {
		lib.Bulletin = trimNotices(append(notices, lib.Bulletin...))
	}
	return p.company.SaveLibrary(lib)
}

// premiseFrom extracts the generation's premise from the dramaturg's brief:
// the brief's theme (falling back to the board's generation theme) and its
// shape. The brief rides out of band in the working file (captured at
// draft-write time, review 3 R3-02), so a board overflow past BoardMaxEntries
// can never lose the premise; the board scan below is the fallback for older
// working files and briefs posted after the draft. No brief, no premise — the
// premise is the dramaturg's reading of the theme, not the theme alone.
func premiseFrom(board Board, w Working, now time.Time) (Premise, bool) {
	theme := strings.TrimSpace(board.Theme)
	shape := ""
	if raw := strings.TrimSpace(w.Brief); raw != "" {
		var ba BriefArtifact
		if parseArtifact(raw, &ba) {
			if ba.Theme != "" {
				theme = ba.Theme
			}
			shape = ba.Shape
		}
	} else {
		found := false
		for i := len(board.Entries) - 1; i >= 0; i-- {
			e := board.Entries[i]
			if e.Kind != "brief" {
				continue
			}
			found = true
			var ba BriefArtifact
			if parseArtifact(e.Body, &ba) {
				if ba.Theme != "" {
					theme = ba.Theme
				}
				shape = ba.Shape
			}
			break
		}
		if !found {
			return Premise{}, false
		}
	}
	theme = strings.TrimSpace(theme)
	shape = strings.TrimSpace(shape)
	if theme == "" && shape == "" {
		return Premise{}, false
	}
	return Premise{Theme: theme, Shape: shape, Date: dateStamp(now)}, true
}

// repertoireFrom extracts the generation's play summary and canon facts from
// the working file: the playwright's draft report when it delivered one, the
// draft itself otherwise. No title, no entry.
func repertoireFrom(w Working, now time.Time) (PlaySummary, []string, bool) {
	if w.Story.Title == "" {
		return PlaySummary{}, nil, false
	}
	sum := PlaySummary{
		Title: w.Story.Title,
		Acts:  actsOf(w.Story),
		Beats: len(w.Story.Beats),
		Date:  dateStamp(now),
	}
	if w.Report != nil {
		if w.Report.Title != "" {
			sum.Title = w.Report.Title
		}
		if len(w.Report.Acts) > 0 {
			sum.Acts = len(w.Report.Acts)
		}
		if w.Report.BeatsCount > 0 {
			sum.Beats = w.Report.BeatsCount
		}
	}
	return sum, w.Canon, true
}

// setsFrom extracts the generation's set recipe from the working file's
// dressed scene. The scenographer must have run — its dressing is recorded in
// the working file (Dressed, review 3 R3-02), the board's deliverable copy
// being trimmable — because without it the backdrop is the playwright's
// default, not a dressed set. The recipe is the applied dress, which is the
// authoritative state (the working file beats the board's report copy, whose
// body is truncated at the board cap).
func setsFrom(w Working, now time.Time) (SetRecipe, bool) {
	if !model.ValidBackdrops[w.Story.Scene.Backdrop] || !w.Dressed {
		return SetRecipe{}, false
	}
	recipe := SetRecipe{Backdrop: w.Story.Scene.Backdrop, Date: dateStamp(now)}
	for _, c := range w.Story.Scene.Cells {
		recipe.Cells = append(recipe.Cells, CellPlacement{Row: c.Row, Col: c.Col, Piece: c.Piece})
	}
	for _, pr := range w.Story.Props {
		recipe.Props = append(recipe.Props, PropPlacement{ID: pr.ID, X: pr.X, Lane: pr.Lane})
	}
	return recipe, true
}

// bumpSetRecipe carries a recipe's prior usage count forward: the same
// backdrop + dress again is a count bump, not a duplicate entry.
func bumpSetRecipe(recipe SetRecipe, existing SetsDoc) SetRecipe {
	key := setRecipeKey(recipe)
	for _, e := range existing {
		if setRecipeKey(e) == key {
			recipe.Count = e.Count + 1
			return recipe
		}
	}
	recipe.Count = 1
	return recipe
}

// lessonsFrom splits the director's submit-time notes into critique lessons,
// one per line, capped in length.
func lessonsFrom(lines []string, now time.Time) DirectorDoc {
	var out DirectorDoc
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, Lesson{Text: truncateRunes(line, lessonMaxLen), Date: dateStamp(now)})
	}
	return out
}

// splitLessons splits the director's submit-time notes into lines — one
// lesson per line, blanks dropped.
func splitLessons(notes string) []string {
	var out []string
	for line := range strings.SplitSeq(notes, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

// bulletinFrom extracts the generation's durable announcements: board
// entries flagged decision, and notes from the production roles — never the
// stage, whose warnings are per-generation noise.
func bulletinFrom(board Board, now time.Time) BulletinDoc {
	var out BulletinDoc
	// The board is oldest-first; walk it backwards so the newest ideas lead.
	for i := len(board.Entries) - 1; i >= 0; i-- {
		e := board.Entries[i]
		if e.Kind != "decision" && (e.Kind != "note" || e.Author == "stage") {
			continue
		}
		if strings.TrimSpace(e.Body) == "" {
			continue
		}
		out = append(out, Notice{Author: e.Author, Kind: e.Kind, Body: e.Body, Date: dateStamp(now)})
	}
	return out
}

// parseCanonizations parses the submit call's characters input: a JSON array
// of {id, species, coat}. Free text or malformed JSON is an error the
// director sees — a canonization must be explicit and structured.
func parseCanonizations(s string) ([]CharacterEntry, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var out []CharacterEntry
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("characters input is not a JSON array: %v", err)
	}
	return out, nil
}
