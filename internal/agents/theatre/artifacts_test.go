package theatre

import (
	"strings"
	"testing"

	"github.com/baalimago/kinoview/internal/model"
)

// The draft report is repaired at the wrapper boundary: text is capped,
// ids are pattern-checked, counters are clamped and canon facts are bounded
// like the working file's.
func TestArtifacts_DraftReportNormalized(t *testing.T) {
	t.Parallel()
	rep := DraftReport{
		Title: strings.Repeat("x", 100),
		Acts: []Act{
			{Name: "n", Beats: 999, OneLine: strings.Repeat("y", 500)},
			{Name: "m", Beats: 1, OneLine: "ok"},
		},
		Cast:       []string{"ina", "Bad ID!"},
		Props:      []string{"yarn1", "box1", "Bad id", "extra1"},
		BeatsCount: -5,
		Canon:      []string{"ok", strings.Repeat("z", 300)},
	}
	normalizeDraftReport(&rep)
	if len(rep.Title) != model.MaxTitleLen {
		t.Errorf("title = %d runes, want %d", len(rep.Title), model.MaxTitleLen)
	}
	if len(rep.Acts) != 2 || rep.Acts[0].Beats != model.MaxBeats || len(rep.Acts[0].OneLine) != MaxActOneLine {
		t.Errorf("acts = %+v, want 2 with clamped beats and oneLine", rep.Acts)
	}
	if len(rep.Cast) != 1 || rep.Cast[0] != "ina" {
		t.Errorf("cast = %v, want [ina]", rep.Cast)
	}
	if len(rep.Props) != 3 {
		t.Errorf("props = %v, want 3 (BAD dropped, capped at MaxProps)", rep.Props)
	}
	if rep.BeatsCount != 0 {
		t.Errorf("beatsCount = %d, want clamped to 0", rep.BeatsCount)
	}
	if len(rep.Canon) != 2 || len(rep.Canon[1]) != CanonMaxFact {
		t.Errorf("canon = %v, want 2 facts with the long one capped at %d", rep.Canon, CanonMaxFact)
	}
}

// An empty report carries nothing worth storing beside the draft.
func TestArtifacts_EmptyReportDropped(t *testing.T) {
	t.Parallel()
	if _, ok := parseDraftReport(`{}`); ok {
		t.Error("an empty report parsed as a usable artifact")
	}
	if _, ok := parseDraftReport(`{"title":"x"}`); !ok {
		t.Error("a titled report was dropped")
	}
	// A free-text report is not an artifact at all.
	if _, ok := parseDraftReport("16 beats / 3 acts"); ok {
		t.Error("a free-text report parsed as an artifact")
	}
}

// The scene report is repaired at the wrapper boundary: rows and pieces are
// checked against the model vocabulary, columns are clamped into the grid and
// prop placements are bounded. An x written as a 0-100 mark (the playwright
// and scenographer habit, 2026-08-04) normalises to the player's 0-1 space
// before the clamp.
func TestArtifacts_SceneReportNormalized(t *testing.T) {
	t.Parallel()
	sr := SceneReport{
		Backdrop: " Night ",
		Cells: []CellPlacement{
			{Row: "far", Col: 9, Piece: "tree"},
			{Row: "bogus", Col: 1, Piece: "bush"},  // unknown row dropped
			{Row: "near", Col: 2, Piece: "dragon"}, // unknown piece dropped
			{Row: "mid", Col: 3, Piece: "sofa"},    // valid
		},
		Props:  []PropPlacement{{ID: "yarn1", X: 50, Lane: 7}},
		Reason: strings.Repeat("r", 500),
	}
	normalizeSceneReport(&sr)
	if sr.Backdrop != "night" {
		t.Errorf("backdrop = %q, want lowercased night", sr.Backdrop)
	}
	if len(sr.Cells) != 2 {
		t.Fatalf("cells = %+v, want 2 (unknown row and piece dropped)", sr.Cells)
	}
	if sr.Cells[0].Col != model.CellCols-1 {
		t.Errorf("col = %d, want clamped to %d", sr.Cells[0].Col, model.CellCols-1)
	}
	if len(sr.Props) != 1 || sr.Props[0].X != 0.5 || sr.Props[0].Lane != model.MaxLanes-1 {
		t.Errorf("props = %+v, want x 50%% normalised to 0.5 and lane clamped", sr.Props)
	}
	if len(sr.Reason) != MaxReasonLen {
		t.Errorf("reason = %d runes, want capped at %d", len(sr.Reason), MaxReasonLen)
	}
}
