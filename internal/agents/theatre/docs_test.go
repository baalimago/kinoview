package theatre

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/go_away_boilerplate/pkg/testboil"
	"github.com/baalimago/kinoview/internal/model"
)

// Every company document round-trips through its file: write → load → same
// semantics, and the library loads all seven as one unit.
func TestDocs_RoundTrip(t *testing.T) {
	co := Open(t.TempDir())
	want := Library{
		Premises: PremisesDoc{{Theme: "Solaris 1972", Shape: "mousehunt", Date: "2026-08-02"}},
		Repertoire: RepertoireDoc{
			Summaries: []PlaySummary{{Title: "The Test Night", Acts: 2, Beats: 14}},
			Facts:     []string{"the mouse got away"},
		},
		Sets:     SetsDoc{{Backdrop: "garden", Count: 2}},
		Registry: RegistryDoc{{ID: "mouse2", Species: "mouse", Coat: "white", Variants: []string{"field", "white"}}},
		Director: DirectorDoc{{Text: "two stares in a row is dead air"}},
		Bulletin: BulletinDoc{{Author: "director", Kind: "decision", Body: "the mouse gets away"}},
		Audience: AudienceDoc{{StoryID: "stry_ab12", Rating: 1, Comment: "more dog", Date: "2026-08-05"}},
	}
	if err := co.SaveLibrary(want); err != nil {
		t.Fatal(err)
	}
	got := co.LoadLibrary()
	if len(got.Premises) != 1 || got.Premises[0].Theme != "Solaris 1972" || got.Premises[0].Shape != "mousehunt" {
		t.Errorf("premises = %+v", got.Premises)
	}
	if len(got.Repertoire.Summaries) != 1 || got.Repertoire.Summaries[0].Title != "The Test Night" {
		t.Errorf("summaries = %+v", got.Repertoire.Summaries)
	}
	if len(got.Repertoire.Facts) != 1 || got.Repertoire.Facts[0] != "the mouse got away" {
		t.Errorf("facts = %v", got.Repertoire.Facts)
	}
	if len(got.Sets) != 1 || got.Sets[0].Backdrop != "garden" || got.Sets[0].Count != 2 {
		t.Errorf("sets = %+v", got.Sets)
	}
	if len(got.Registry) != 1 || got.Registry[0].ID != "mouse2" || got.Registry[0].Coat != "white" {
		t.Errorf("registry = %+v", got.Registry)
	}
	if len(got.Director) != 1 || got.Director[0].Text != "two stares in a row is dead air" {
		t.Errorf("director = %+v", got.Director)
	}
	if len(got.Bulletin) != 1 || got.Bulletin[0].Body != "the mouse gets away" {
		t.Errorf("bulletin = %+v", got.Bulletin)
	}

	// The audience doc round-trips through its own accessors: SaveLibrary
	// never persists it (decision D-5), so the write path is SaveAudience,
	// not the library save.
	if err := co.SaveAudience(want.Audience); err != nil {
		t.Fatal(err)
	}
	aud := co.LoadAudience()
	if len(aud) != 1 || aud[0].StoryID != "stry_ab12" || aud[0].Rating != 1 || aud[0].Comment != "more dog" || aud[0].Date != "2026-08-05" {
		t.Errorf("audience = %+v", aud)
	}

	// SaveLibrary leaves audience.json untouched: a submit's distillation can
	// never clobber a fresh note with a stale in-memory copy (decision D-5).
	stale := want
	stale.Audience = nil
	if err := co.SaveLibrary(stale); err != nil {
		t.Fatal(err)
	}
	if got := co.LoadAudience(); len(got) != 1 || got[0].Comment != "more dog" {
		t.Errorf("audience after SaveLibrary = %+v, want the note untouched", got)
	}

	// Every file landed in the company dir.
	for _, name := range []string{
		premisesFileName, repertoireFileName, setsFileName, registryFileName,
		directorFileName, bulletinFileName, audienceFileName,
	} {
		if _, err := os.Stat(filepath.Join(co.dir, name)); err != nil {
			t.Errorf("missing doc file %s: %v", name, err)
		}
	}
}

// A corrupt doc file loads as an empty doc with an error log; the server
// starts (acceptance criterion). The other docs still load. The registry
// keeps its canonical defaults when its file is corrupt (error table).
func TestDocs_CorruptFileDegradesToEmpty(t *testing.T) {
	cases := []struct {
		name    string
		file    string
		wantLog string
		check   func(t *testing.T, th *Theatre, lib Library)
	}{
		{
			name: "repertoire", file: repertoireFileName, wantLog: "repertoire unreadable",
			check: func(t *testing.T, th *Theatre, lib Library) {
				if len(lib.Repertoire.Facts) != 0 {
					t.Errorf("repertoire = %+v, want the empty doc", lib.Repertoire)
				}
				if len(lib.Premises) != 1 {
					t.Errorf("premises = %+v, want the healthy doc untouched", lib.Premises)
				}
			},
		},
		{
			name: "registry", file: registryFileName, wantLog: "registry unreadable",
			check: func(t *testing.T, th *Theatre, lib Library) {
				if len(lib.Registry) != 0 {
					t.Errorf("registry = %+v, want the empty doc", lib.Registry)
				}
				// The seeded canonical defaults still answer, whatever the file
				// said.
				if look, ok := th.registry.Lookup("ina"); !ok || look.Coat != "ginger" {
					t.Errorf("ina = %+v, want the canonical default after a corrupt registry file", look)
				}
			},
		},
		{
			name: "audience", file: audienceFileName, wantLog: "audience unreadable",
			check: func(t *testing.T, th *Theatre, lib Library) {
				if len(lib.Audience) != 0 {
					t.Errorf("audience = %+v, want the empty doc", lib.Audience)
				}
				if len(lib.Premises) != 1 {
					t.Errorf("premises = %+v, want the healthy doc untouched", lib.Premises)
				}
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			co := Open(dir)
			path := filepath.Join(dir, CompanyDir, tt.file)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("{garbage"), 0o644); err != nil {
				t.Fatal(err)
			}

			var th *Theatre
			errOut := testboil.CaptureStderr(t, func(t *testing.T) {
				// The server path: a fresh theatre over the same cache dir,
				// with a healthy doc beside the corrupt one. Warm is
				// synchronous, so no save goroutine outlives the test.
				if err := co.SavePremises(PremisesDoc{{Theme: "ok"}}); err != nil {
					t.Fatal(err)
				}
				th = New(models.Configurations{}, dir, time.Hour)
				th.Warm(context.Background())
				if th.Next().Title == "" {
					t.Fatal("Next should still serve")
				}
			})
			if !strings.Contains(errOut, tt.wantLog) {
				t.Errorf("stderr = %q, want the corrupt-doc error logged", errOut)
			}
			tt.check(t, th, co.LoadLibrary())
		})
	}
}

// A doc over its cap is trimmed oldest-first on the next write (acceptance
// criterion): the newest entries survive, the oldest drop.
func TestDocs_TrimmedToCapOldestFirst(t *testing.T) {
	co := Open(t.TempDir())

	premises := make(PremisesDoc, 0, premisesCap+5)
	for i := range premisesCap + 5 {
		premises = append(premises, Premise{Theme: fmt.Sprintf("theme-%02d", i)})
	}
	if err := co.SavePremises(premises); err != nil {
		t.Fatal(err)
	}
	got := co.LoadPremises()
	if len(got) != premisesCap {
		t.Fatalf("premises = %d, want the cap %d", len(got), premisesCap)
	}
	if got[0].Theme != "theme-00" || got[premisesCap-1].Theme != "theme-39" {
		t.Errorf("trim kept the wrong end: first %q, last %q", got[0].Theme, got[premisesCap-1].Theme)
	}

	lessons := make(DirectorDoc, 0, directorCap+3)
	for i := range directorCap + 3 {
		lessons = append(lessons, Lesson{Text: fmt.Sprintf("lesson %d", i)})
	}
	if err := co.SaveDirector(lessons); err != nil {
		t.Fatal(err)
	}
	if got := co.LoadDirector(); len(got) != directorCap || got[0].Text != "lesson 0" {
		t.Errorf("director = %d entries, first %q, want %d starting at lesson 0",
			len(got), got[0].Text, directorCap)
	}

	// Bulletin and repertoire caps hold too.
	notices := make(BulletinDoc, 0, bulletinCap+4)
	for i := range bulletinCap + 4 {
		notices = append(notices, Notice{Author: "director", Kind: "note", Body: fmt.Sprintf("note %d", i)})
	}
	if err := co.SaveBulletin(notices); err != nil {
		t.Fatal(err)
	}
	if got := co.LoadBulletin(); len(got) != bulletinCap {
		t.Errorf("bulletin = %d, want %d", len(got), bulletinCap)
	}

	// The audience cap holds too: trimmed to the cap, newest entries kept (the
	// fixture is newest-first, the order AppendAudience maintains).
	notes := make(AudienceDoc, 0, audienceCap+5)
	for i := audienceCap + 4; i >= 0; i-- {
		notes = append(notes, AudienceNote{StoryID: fmt.Sprintf("stry_ab%02d", i), Rating: 1, Date: "2026-08-05"})
	}
	if err := co.SaveAudience(notes); err != nil {
		t.Fatal(err)
	}
	if got := co.LoadAudience(); len(got) != audienceCap || got[0].StoryID != "stry_ab44" {
		t.Errorf("audience = %d entries, first %q, want %d starting at stry_ab44",
			len(got), got[0].StoryID, audienceCap)
	}
}

// The trim gates repair hostile content: unknown vocabularies are dropped,
// lengths are capped, duplicates collapse.
func TestDocs_TrimGatesRepairHostileContent(t *testing.T) {
	co := Open(t.TempDir())
	lib := Library{
		Premises: PremisesDoc{
			{Theme: strings.Repeat("t", 200), Shape: strings.Repeat("s", 200)},
			{Theme: "Solaris", Shape: "mousehunt"},
			{Theme: "solaris"}, // duplicate after lowercasing? no — themes are not lowercased, keep both
		},
		Sets: SetsDoc{
			{Backdrop: "bogus"}, // unknown backdrop dropped
			{Backdrop: "Night", Cells: []CellPlacement{{Row: "far", Col: 9, Piece: "tree"}}},
		},
		Registry: RegistryDoc{
			{ID: "bad id", Species: "cat", Coat: "ginger"}, // id fails the pattern
			{ID: "dragon", Species: "dragon", Coat: "red"}, // unknown species
			{ID: "mouse2", Species: "mouse", Coat: "white", Variants: []string{"field", "white", "white"}},
		},
		Bulletin: BulletinDoc{
			{Author: "dragon", Kind: "note", Body: "x"},  // unknown author dropped
			{Author: "stage", Kind: "bogus", Body: "x"},  // unknown kind dropped
			{Author: "director", Kind: "note", Body: ""}, // empty body dropped
		},
	}
	if err := co.SaveLibrary(lib); err != nil {
		t.Fatal(err)
	}
	got := co.LoadLibrary()

	if len(got.Premises) != 3 || len(got.Premises[0].Theme) != model.MaxTitleLen || len(got.Premises[0].Shape) != MaxShapeLen {
		t.Errorf("premises = %+v, want capped lengths and 3 entries (exact-match dedupe only)", got.Premises)
	}
	if len(got.Sets) != 1 || got.Sets[0].Backdrop != "night" || got.Sets[0].Cells[0].Col != model.CellCols-1 {
		t.Errorf("sets = %+v, want the valid backdrop with clamped cells", got.Sets)
	}
	if len(got.Registry) != 1 || got.Registry[0].ID != "mouse2" || len(got.Registry[0].Variants) != 2 {
		t.Errorf("registry = %+v, want mouse2 with deduped variants", got.Registry)
	}
	if len(got.Bulletin) != 0 {
		t.Errorf("bulletin = %+v, want empty (all entries hostile)", got.Bulletin)
	}

	// The audience gate: bad story ids and ratings drop, comments truncate
	// (never reject), duplicates collapse.
	aud := AudienceDoc{
		{StoryID: "../x", Rating: 1, Comment: "bad id"},
		{StoryID: "stry_ab12", Rating: 0, Comment: "bad rating"},
		{StoryID: "stry_ab12", Rating: 1, Comment: strings.Repeat("c", 300)},
		{StoryID: "stry_ab12", Rating: 1, Comment: strings.Repeat("c", 300)}, // duplicate
		{StoryID: "stry_ab12", Rating: -1, Comment: "the mouse wins"},
	}
	if err := co.SaveAudience(aud); err != nil {
		t.Fatal(err)
	}
	gotAud := co.LoadAudience()
	if len(gotAud) != 2 || gotAud[0].Rating != 1 || gotAud[0].Comment != strings.Repeat("c", audienceCommentMax) || gotAud[1].Rating != -1 {
		t.Errorf("audience = %+v, want the truncated +1 note and the -1 note", gotAud)
	}
}

// The doc context excerpts reach their roles: each role reads its own doc,
// everyone reads the bulletin, and nobody reads another role's doc.
func TestDocs_ContextExcerptsPerRole(t *testing.T) {
	co := Open(t.TempDir())
	lib := Library{
		Premises:   PremisesDoc{{Theme: "Solaris 1972", Shape: "mousehunt"}},
		Repertoire: RepertoireDoc{Summaries: []PlaySummary{{Title: "The Test Night", Acts: 2}}, Facts: []string{"the mouse got away"}},
		Sets:       SetsDoc{{Backdrop: "garden", Count: 3}},
		Director:   DirectorDoc{{Text: "two stares in a row is dead air"}},
		Bulletin:   BulletinDoc{{Author: "director", Kind: "decision", Body: "the mouse gets away"}},
		Audience:   AudienceDoc{{StoryID: "stry_ab12", Rating: 1, Comment: "more dog", Date: "2026-08-05"}},
	}
	if err := co.SaveLibrary(lib); err != nil {
		t.Fatal(err)
	}
	// SaveLibrary never writes audience.json (decision D-5); the excerpt test
	// needs the note on disk, so it goes through the doc's own write path.
	if err := co.SaveAudience(lib.Audience); err != nil {
		t.Fatal(err)
	}
	stage := OpenStage(co, "stry_ab12")
	silenceFeed(stage)
	runner := NewRunner(co, stage)

	cases := []struct {
		role    string
		have    []string
		notHave []string
	}{
		{"dramaturg", []string{"Premises already used", "Solaris 1972", "Audience feedback from recent shows", "more dog", "[+1]"}, []string{"the mouse got away", "two stares in a row"}},
		{"playwright", []string{"Canon facts from earlier productions", "the mouse got away", "Earlier productions", "The Test Night"}, []string{"Premises already used", "two stares in a row", "Audience feedback"}},
		{"scenographer", []string{"Set recipes already used", "garden (0 cells"}, []string{"the mouse got away", "two stares in a row", "Audience feedback"}},
		{"director", []string{"Directing lessons from earlier productions", "two stares in a row is dead air", "Audience feedback from recent shows", "more dog"}, []string{"the mouse got away", "Premises already used"}},
		{"wardrobe", nil, []string{"Audience feedback", "Premises already used", "the mouse got away"}},
	}
	for _, tt := range cases {
		ctx := runner.withDocsContext("base", tt.role)
		for _, want := range tt.have {
			if !strings.Contains(ctx, want) {
				t.Errorf("%s context lacks %q", tt.role, want)
			}
		}
		for _, nope := range tt.notHave {
			if strings.Contains(ctx, nope) {
				t.Errorf("%s context carries %q — another role's doc leaked", tt.role, nope)
			}
		}
		// The bulletin reaches every role.
		if !strings.Contains(ctx, "the mouse gets away") {
			t.Errorf("%s context lacks the bulletin", tt.role)
		}
	}
}

// AppendAudience is the doc's single write path (decision D-5): notes are
// prepended newest-first, the cap holds, and two concurrent appends lose no
// note — the load-modify-save holds the company's mutex (R2-01).
func TestDocs_AppendAudience(t *testing.T) {
	co := Open(t.TempDir())

	// Seed past the cap, newest first — the order AppendAudience maintains.
	seed := make(AudienceDoc, 0, audienceCap+5)
	for i := audienceCap + 4; i >= 0; i-- {
		seed = append(seed, AudienceNote{StoryID: fmt.Sprintf("stry_ab%02d", i), Rating: 1, Date: "2026-08-05"})
	}
	if err := co.SaveAudience(seed); err != nil {
		t.Fatal(err)
	}

	if err := co.AppendAudience(AudienceNote{StoryID: "stry_new", Rating: -1, Comment: "too slow"}); err != nil {
		t.Fatal(err)
	}
	got := co.LoadAudience()
	if len(got) != audienceCap {
		t.Fatalf("audience = %d, want the cap %d", len(got), audienceCap)
	}
	if got[0].StoryID != "stry_new" || got[0].Rating != -1 || got[0].Comment != "too slow" {
		t.Errorf("newest note = %+v, want the appended note first", got[0])
	}
	for _, n := range got {
		if n.StoryID == "stry_ab00" {
			t.Errorf("oldest seeded note survived the cap")
		}
	}

	// Two concurrent appends lose no note.
	var wg sync.WaitGroup
	for _, id := range []string{"stry_conc1", "stry_conc2"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if err := co.AppendAudience(AudienceNote{StoryID: id, Rating: 1}); err != nil {
				t.Errorf("append %s: %v", id, err)
			}
		}(id)
	}
	wg.Wait()

	got = co.LoadAudience()
	seen := map[string]bool{}
	for _, n := range got {
		seen[n.StoryID] = true
	}
	if !seen["stry_conc1"] || !seen["stry_conc2"] {
		t.Errorf("concurrent append lost a note: %v", seen)
	}
	if len(got) != audienceCap {
		t.Errorf("audience = %d, want the cap %d", len(got), audienceCap)
	}
}
