package theatre

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/go_away_boilerplate/pkg/testboil"
	"github.com/baalimago/kinoview/internal/model"
)

// dressedStory is a playable draft with a dressed garden set and a prop, for
// the distillation fixtures.
func dressedStory() model.Story {
	s := validStory()
	s.Scene = model.Scene{
		Backdrop: "garden",
		Cells:    []model.Cell{{ID: "set_a", Row: "far", Col: 0, Piece: "tree"}},
	}
	s.Props = []model.Prop{{ID: "yarn1", Prop: "yarn", Lane: 0, X: 0.5}}
	if err := s.Validate(); err != nil {
		panic(err)
	}
	return s
}

// distillFixtureProduction wires a production whose board and working file
// carry a complete generation: brief, dressed scene, decision and role note,
// draft report with canon facts.
func distillFixtureProduction(t *testing.T, th *Theatre) *production {
	t.Helper()
	p := th.openProduction("")
	t.Cleanup(p.stage.Close)
	silenceFeed(p.stage)
	co := p.company

	board := Board{Generation: p.gen, Theme: "Solaris 1972"}
	board.Append(Entry{Author: "dramaturg", Kind: "brief", To: "director", Body: `{"mood":"standoff","shape":"mousehunt","theme":"Solaris 1972"}`})
	board.Append(Entry{Author: "scenographer", Kind: "deliverable", To: "director", Body: `{"backdrop":"garden","cells":[{"row":"far","col":0,"piece":"tree"}]}`})
	board.Append(Entry{Author: "director", Kind: "decision", Body: "the mouse gets away this time"})
	board.Append(Entry{Author: "playwright", Kind: "note", Body: "keep the chase short"})
	board.Append(Entry{Author: "stage", Kind: "note", Body: "budget warning"}) // stage notes never reach the bulletin
	if err := co.SaveBoard(board); err != nil {
		t.Fatal(err)
	}

	w := Working{
		Story:    dressedStory(),
		Revision: 2,
		Status:   "dressed",
		// The scenographer's out-of-band marker (review 3, R3-02) — the
		// board's deliverable copy is trimmable, this flag is not.
		Dressed: true,
		Canon:   []string{"the mouse got away"},
		Report: &DraftReport{
			Title:      "The Test Night",
			Acts:       []Act{{Name: "a", Beats: 2}},
			BeatsCount: 5,
			Canon:      []string{"the mouse got away"},
		},
	}
	if err := co.SaveWorking(w); err != nil {
		t.Fatal(err)
	}
	return p
}

// Distillation produces all six docs from a fixture generation — board,
// working file and submit inputs — with correct counts and no LLM
// involvement (acceptance criterion).
func TestDistill_ProducesAllSixDocs(t *testing.T) {
	th := newTestTheatre(t)
	p := distillFixtureProduction(t, th)
	p.lessons = splitLessons("two stares in a row is dead air\nnever open on a nap")

	if err := p.distill(); err != nil {
		t.Fatal(err)
	}
	lib := p.company.LoadLibrary()

	if len(lib.Premises) != 1 || lib.Premises[0].Theme != "Solaris 1972" || lib.Premises[0].Shape != "mousehunt" {
		t.Errorf("premises = %+v, want the brief's theme and shape", lib.Premises)
	}
	if len(lib.Repertoire.Summaries) != 1 || lib.Repertoire.Summaries[0].Title != "The Test Night" ||
		lib.Repertoire.Summaries[0].Acts != 1 || lib.Repertoire.Summaries[0].Beats != 5 {
		t.Errorf("summaries = %+v, want the draft report's shape", lib.Repertoire.Summaries)
	}
	if len(lib.Repertoire.Facts) != 1 || lib.Repertoire.Facts[0] != "the mouse got away" {
		t.Errorf("facts = %v, want the canon fact", lib.Repertoire.Facts)
	}
	if len(lib.Sets) != 1 || lib.Sets[0].Backdrop != "garden" || len(lib.Sets[0].Cells) != 1 || lib.Sets[0].Count != 1 {
		t.Errorf("sets = %+v, want the dressed garden recipe", lib.Sets)
	}
	if len(lib.Registry) != 4 {
		t.Errorf("registry = %+v, want the permanent cast", lib.Registry)
	}
	if len(lib.Director) != 2 || lib.Director[0].Text != "two stares in a row is dead air" {
		t.Errorf("director = %+v, want the two lessons", lib.Director)
	}
	if len(lib.Bulletin) != 2 {
		t.Fatalf("bulletin = %+v, want the decision + the playwright's note", lib.Bulletin)
	}
	if lib.Bulletin[0].Body != "keep the chase short" || lib.Bulletin[1].Body != "the mouse gets away this time" {
		t.Errorf("bulletin = %+v, want newest first, stage notes excluded", lib.Bulletin)
	}
}

// The same recipe twice is a count bump, not a duplicate entry: usage counts
// grow and the doc stays deduped.
func TestDistill_SameSetBumpedNotDuplicated(t *testing.T) {
	th := newTestTheatre(t)
	p := distillFixtureProduction(t, th)
	if err := p.distill(); err != nil {
		t.Fatal(err)
	}
	if err := p.distill(); err != nil {
		t.Fatal(err)
	}
	lib := p.company.LoadLibrary()
	if len(lib.Sets) != 1 || lib.Sets[0].Count != 2 {
		t.Errorf("sets = %+v, want one recipe bumped to count 2", lib.Sets)
	}
}

// Distillation with a missing artifact skips that doc and writes the others
// (error table): no brief → premises untouched; no scenographer deliverable
// → sets untouched; the repertoire still comes from the draft itself.
func TestDistill_MissingArtifactSkipsDoc(t *testing.T) {
	th := newTestTheatre(t)
	p := th.openProduction("")
	t.Cleanup(p.stage.Close)
	silenceFeed(p.stage)
	co := p.company

	// A previous generation's docs, which must survive untouched where the
	// artifact is missing.
	if err := co.SaveSets(SetsDoc{{Backdrop: "night", Count: 3}}); err != nil {
		t.Fatal(err)
	}
	if err := co.SavePremises(PremisesDoc{{Theme: "old theme", Shape: "standoff"}}); err != nil {
		t.Fatal(err)
	}

	// This generation: no brief, no scenographer deliverable, no report.
	if err := co.SaveBoard(Board{Generation: p.gen, Theme: "theme"}); err != nil {
		t.Fatal(err)
	}
	if err := co.SaveWorking(Working{Story: validStory(), Status: "draft"}); err != nil {
		t.Fatal(err)
	}

	if err := p.distill(); err != nil {
		t.Fatal(err)
	}
	lib := co.LoadLibrary()
	if len(lib.Premises) != 1 || lib.Premises[0].Theme != "old theme" {
		t.Errorf("premises = %+v, want the previous doc untouched (no brief)", lib.Premises)
	}
	if len(lib.Sets) != 1 || lib.Sets[0].Backdrop != "night" || lib.Sets[0].Count != 3 {
		t.Errorf("sets = %+v, want the previous doc untouched (scenographer never ran)", lib.Sets)
	}
	if len(lib.Repertoire.Summaries) != 1 || lib.Repertoire.Summaries[0].Title != "The Test Night" {
		t.Errorf("summaries = %+v, want the draft itself distilled", lib.Repertoire.Summaries)
	}
	if len(lib.Registry) != 4 {
		t.Errorf("registry = %+v, want the permanent cast", lib.Registry)
	}
}

// R3-02: a board that overflows BoardMaxEntries trims the brief and the
// scenographer's deliverable off the worklog, but the premise and the set
// recipe still distill — the brief and the dressed marker ride in the working
// file, out of the board's reach. The premises doc never silently loses a
// generation.
func TestDistill_PremiseAndSetsSurviveBoardOverflow(t *testing.T) {
	th := newTestTheatre(t)
	p := th.openProduction("")
	t.Cleanup(p.stage.Close)
	silenceFeed(p.stage)
	co := p.company

	// A chatty generation: the brief is entry 1, then the board overflows
	// past BoardMaxEntries and trims it (and the scenographer's deliverable
	// with it).
	board := Board{Generation: p.gen, Theme: "Solaris 1972"}
	board.Append(Entry{Author: "dramaturg", Kind: "brief", To: "director", Body: `{"mood":"standoff","shape":"mousehunt","theme":"Solaris 1972"}`})
	board.Append(Entry{Author: "scenographer", Kind: "deliverable", To: "director", Body: `{"backdrop":"garden","cells":[{"row":"far","col":0,"piece":"tree"}]}`})
	for range BoardMaxEntries {
		board.Append(Entry{Author: "playwright", Kind: "note", To: "director", Body: "spam"})
	}
	if err := co.SaveBoard(board); err != nil {
		t.Fatal(err)
	}
	saved, err := co.LoadBoard()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range saved.Entries {
		if e.Kind == "brief" || (e.Author == "scenographer" && e.Kind == "deliverable") {
			t.Fatalf("fixture: the brief/deliverable should have been trimmed off the board: %+v", saved.Entries)
		}
	}

	// The working file carries both out of band (writeDraft/writeScene set
	// them in the production; here they are set directly).
	if err := co.SaveWorking(Working{
		Story:    dressedStory(),
		Revision: 2,
		Status:   "dressed",
		Dressed:  true,
		Brief:    `{"mood":"standoff","shape":"mousehunt","theme":"Solaris 1972"}`,
	}); err != nil {
		t.Fatal(err)
	}

	if err := p.distill(); err != nil {
		t.Fatal(err)
	}
	lib := co.LoadLibrary()
	if len(lib.Premises) != 1 || lib.Premises[0].Theme != "Solaris 1972" || lib.Premises[0].Shape != "mousehunt" {
		t.Errorf("premises = %+v, want the brief's theme and shape despite the board overflow", lib.Premises)
	}
	if len(lib.Sets) != 1 || lib.Sets[0].Backdrop != "garden" || len(lib.Sets[0].Cells) != 1 {
		t.Errorf("sets = %+v, want the dressed garden recipe despite the board overflow", lib.Sets)
	}
}

// A doc write failure mid-submit never loses the story: the story is already
// persisted, the failure is logged, and the generation completes (error
// table). Each failing doc is skipped; the others still land.
func TestDistill_WriteFailureAfterStoryPersisted(t *testing.T) {
	for _, failing := range []string{registryFileName, bulletinFileName} {
		t.Run(failing, func(t *testing.T) {
			cacheDir := t.TempDir()
			th := New(models.Configurations{Model: "stub", ConfigDir: t.TempDir()}, cacheDir, time.Hour)
			// The doc's path is a directory, so its write fails.
			if err := os.MkdirAll(filepath.Join(cacheDir, CompanyDir, failing), 0o755); err != nil {
				t.Fatal(err)
			}
			th.runLLM = fixtureScript(t)

			errOut := testboil.CaptureStderr(t, func(t *testing.T) {
				if !th.Prepare(context.Background(), "test") {
					t.Fatal("Prepare should run")
				}
			})
			if _, err := os.Stat(filepath.Join(cacheDir, "intro_story.json")); err != nil {
				t.Errorf("story not persisted: %v", err)
			}
			if s := th.Next(); s.Title != "The Test Night" {
				t.Errorf("story = %q, want the submitted draft", s.Title)
			}
			if !strings.Contains(errOut, "distillation failed") {
				t.Errorf("stderr = %q, want the distillation failure logged", errOut)
			}
		})
	}
}
