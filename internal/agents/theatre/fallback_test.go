package theatre

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/baalimago/kinoview/internal/model"
)

// freshCompany wipes one company dir so every seed of a fallback sweep starts
// from empty paperwork — the playwright fallback writes the working file, the
// scenographer fallback dresses it.
func freshCompany(t *testing.T, dir string) *Company {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(dir, CompanyDir)); err != nil {
		t.Fatal(err)
	}
	return Open(dir)
}

// Each role's fallback produces an artifact that passes its schema validation
// across 150 seeds (the composer path is randomized): the dramaturg's brief
// is a valid artifact naming the permanent cast, the playwright's report has
// a title and a readable draft behind it, the scenographer's report has a
// valid backdrop and dresses the working file, and the wardrobe answers from
// the fixed cast. Each seed exercises the on-disk paperwork, so the sweep is
// file-I/O bound: 150 seeds still draw every composer template ~12 times
// while keeping the package inside the house 30s -race -count=3 gate under
// load.
func TestFallback_AllRolesProduceValidArtifactsAcrossSeeds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for seed := range int64(150) {
		co := freshCompany(t, dir)
		stage := OpenStage(co, fmt.Sprintf("stry_seed%d", seed))
		silenceFeed(stage)
		runner := NewRunner(co, stage, WithTheme("Solaris 1972"))
		runner.rnd = rand.New(rand.NewSource(seed))

		briefText, err := runner.roleFallback("dramaturg", "t", 0)
		if err != nil {
			t.Fatalf("seed %d: dramaturg fallback: %v", seed, err)
		}
		var brief BriefArtifact
		if !parseArtifact(briefText, &brief) || brief.Mood == "" {
			t.Fatalf("seed %d: brief is not a valid artifact: %q", seed, briefText)
		}
		if brief.Theme != "Solaris 1972" {
			t.Errorf("seed %d: brief theme = %q, want the generation theme", seed, brief.Theme)
		}
		for _, id := range brief.Lineup {
			if !slices.Contains(permanentCastIDs(), id) {
				t.Errorf("seed %d: lineup names non-permanent %q", seed, id)
			}
		}

		draftText, err := runner.roleFallback("playwright", "t", 0)
		if err != nil {
			t.Fatalf("seed %d: playwright fallback: %v", seed, err)
		}
		var rep DraftReport
		if !parseArtifact(draftText, &rep) || rep.Title == "" {
			t.Fatalf("seed %d: draft report is not a valid artifact: %q", seed, draftText)
		}
		w, err := co.LoadWorking()
		if err != nil || w.Story.ID != stage.gen {
			t.Errorf("seed %d: working draft not saved (%v)", seed, err)
		}
		if rep.BeatsCount != len(w.Story.Beats) {
			t.Errorf("seed %d: report beatsCount %d, working beats %d", seed, rep.BeatsCount, len(w.Story.Beats))
		}

		sceneText, err := runner.roleFallback("scenographer", "t", 0)
		if err != nil {
			t.Fatalf("seed %d: scenographer fallback: %v", seed, err)
		}
		var sr SceneReport
		if !parseArtifact(sceneText, &sr) || !model.ValidBackdrops[sr.Backdrop] {
			t.Fatalf("seed %d: scene report is not a valid artifact: %q", seed, sceneText)
		}
		w, err = co.LoadWorking()
		if err != nil || w.Status != "dressed" || w.Story.Scene.Backdrop != sr.Backdrop {
			t.Errorf("seed %d: working draft not dressed (%v, status %q)", seed, err, w.Status)
		}

		answer, err := runner.roleFallback("wardrobe", "what does ina look like on night?", 0)
		if err != nil || !strings.Contains(answer, "canon look for ina") {
			t.Errorf("seed %d: wardrobe answer = %q, want a fixed-cast-grounded one", seed, answer)
		}
	}
}

// Drafts produced by the playwright fallback keep the composer's invariants:
// every actor enters before acting, and no beat lands past the duration — the
// acceptance criterion's enter-before-act and beats-within-duration checks
// through the fallback path, not just the composer's own tests. 80 seeds
// still draw every template ~7 times; the sweep is file-I/O bound and sized
// for the house 30s -race -count=3 gate under load.
func TestFallback_PlaywrightDraftKeepsComposerInvariants(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for seed := range int64(80) {
		co := freshCompany(t, dir)
		stage := OpenStage(co, "stry_seed")
		silenceFeed(stage)
		runner := NewRunner(co, stage)
		runner.rnd = rand.New(rand.NewSource(seed))
		if _, err := runner.roleFallback("playwright", "t", 0); err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		w, err := co.LoadWorking()
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		s := w.Story
		entered := map[string]int{}
		for _, b := range s.Beats {
			if b.Action == "enter" {
				if _, ok := entered[b.Actor]; !ok {
					entered[b.Actor] = b.T
				}
			}
		}
		for _, b := range s.Beats {
			if b.Action == "enter" || b.Actor == "" {
				continue
			}
			at, ok := entered[b.Actor]
			if !ok || b.T < at {
				t.Fatalf("seed %d: %q acts (%s) without entering first", seed, b.Actor, b.Action)
			}
			if b.T > s.DurationMs {
				t.Fatalf("seed %d: beat at %d exceeds duration %d", seed, b.T, s.DurationMs)
			}
		}
	}
}

// A wardrobe-consult Q&A about a known character against a known backdrop
// returns a fixed-cast-grounded answer in fallback mode: the canonical look
// plus the backdrop note — the floor-lane advice for ground characters, the
// perch note for the bird (a bird reads by species, not by lane, phase 8).
func TestFallback_WardrobeGroundedAnswer(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, question, wantLook, wantNote string
	}{
		{"cat on night", "does ginger read on night for ina?", "canon look for ina is ginger", "lane ≥ 1"},
		{"bird on rain", "does chaffinch read on rain for pip?", "canon look for pip is chaffinch", "perched"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			co := Open(t.TempDir())
			stage := OpenStage(co, "stry_ab12")
			silenceFeed(stage)
			runner := NewRunner(co, stage)

			answer, err := runner.roleFallback("wardrobe", tt.question, 0)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(answer, tt.wantLook) {
				t.Errorf("answer = %q, want the canonical look", answer)
			}
			if !strings.Contains(answer, tt.wantNote) {
				t.Errorf("answer = %q, want the %q backdrop note", answer, tt.wantNote)
			}
		})
	}
}

// The wardrobe consulted for an unknown character refuses with a clear "no
// known character" answer — it never invents a look.
func TestFallback_WardrobeUnknownCharacterNoEntry(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	stage := OpenStage(co, "stry_ab12")
	silenceFeed(stage)
	runner := NewRunner(co, stage)

	answer, err := runner.roleFallback("wardrobe", "what does the dragon wear on night?", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer, "no known character") {
		t.Errorf("answer = %q, want the no-character refusal", answer)
	}
}

// A bird-on-backdrop consult is fixed-cast-grounded too: the canonical look,
// and a perch note instead of the floor-lane advice — a bird reads by
// species, not by lane (phase 8).
func TestFallback_WardrobeBirdPerchAnswer(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	stage := OpenStage(co, "stry_ab12")
	silenceFeed(stage)
	runner := NewRunner(co, stage)

	answer, err := runner.roleFallback("wardrobe", "does chaffinch read on rain for pip?", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer, "canon look for pip is chaffinch") {
		t.Errorf("answer = %q, want the canonical bird look", answer)
	}
	if !strings.Contains(answer, "perched") || !strings.Contains(answer, "rain") {
		t.Errorf("answer = %q, want the perch note on the backdrop", answer)
	}
}

// The dramaturg's fallback brief carries the generation's theme.
func TestFallback_DramaturgBriefCarriesTheme(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	stage := OpenStage(co, "stry_ab12")
	silenceFeed(stage)
	runner := NewRunner(co, stage, WithTheme("Solaris 1972"))

	text, err := runner.roleFallback("dramaturg", "t", 0)
	if err != nil {
		t.Fatal(err)
	}
	var brief BriefArtifact
	if !parseArtifact(text, &brief) || brief.Theme != "Solaris 1972" {
		t.Errorf("brief theme = %q, want the generation theme", brief.Theme)
	}
}

// The scenographer's fallback dresses the working draft: the draft's backdrop
// is kept, cells are laid around the cast and the working file moves to
// "dressed".
func TestFallback_ScenographerDressesDraft(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	stage := OpenStage(co, "stry_ab12")
	silenceFeed(stage)
	runner := NewRunner(co, stage)
	runner.rnd = rand.New(rand.NewSource(7))
	if _, err := runner.roleFallback("playwright", "t", 0); err != nil {
		t.Fatal(err)
	}
	w, err := co.LoadWorking()
	if err != nil {
		t.Fatal(err)
	}
	wantBackdrop := w.Story.Scene.Backdrop

	text, err := runner.roleFallback("scenographer", "t", 0)
	if err != nil {
		t.Fatal(err)
	}
	var sr SceneReport
	if !parseArtifact(text, &sr) || !model.ValidBackdrops[sr.Backdrop] {
		t.Fatalf("scene report is not a valid artifact: %q", text)
	}
	if sr.Backdrop != wantBackdrop {
		t.Errorf("backdrop = %q, want the draft's %q kept", sr.Backdrop, wantBackdrop)
	}
	w, err = co.LoadWorking()
	if err != nil || w.Status != "dressed" {
		t.Fatalf("working file not dressed: %v (status %q)", err, w.Status)
	}
	if len(w.Story.Scene.Cells) == 0 {
		t.Error("fallback dressed nothing — want cells around the cast")
	}
}

// A consulted role answers in place: the fallback never runs its production
// side effects over the working file at a consult depth. The playwright's
// in-place answer describes the draft without revising it.
func TestFallback_ConsultedRolesAnswerInPlace(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	stage := OpenStage(co, "stry_ab12")
	silenceFeed(stage)
	runner := NewRunner(co, stage)
	if _, err := runner.roleFallback("playwright", "t", 0); err != nil {
		t.Fatal(err)
	}
	before, err := co.LoadWorking()
	if err != nil {
		t.Fatal(err)
	}

	answer, err := runner.roleFallback("playwright", "how many beats does the draft have?", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer, "beats") {
		t.Errorf("answer = %q, want the draft's shape", answer)
	}
	after, err := co.LoadWorking()
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision || after.Story.Title != before.Story.Title {
		t.Errorf("a consulted playwright revised the draft: revision %d → %d",
			before.Revision, after.Revision)
	}

	// The scenographer answers in place the same way.
	sceneAnswer, err := runner.roleFallback("scenographer", "what is the set?", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sceneAnswer, "cells") {
		t.Errorf("scene answer = %q, want the set's shape", sceneAnswer)
	}

	// The dramaturg answers in place: without a board, its brief lives only
	// in the final answer it delivered to the director.
	briefAnswer, err := runner.roleFallback("dramaturg", "what is the brief?", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(briefAnswer, "no brief on file") {
		t.Errorf("brief answer = %q, want the no-brief-on-file refusal", briefAnswer)
	}
}

// A playwright fallback never clobbers the playwright's own draft: a failed
// revision (the working file holds this generation's draft) reports the draft
// instead of replacing it with a composer scene. A stale file from an earlier
// generation is overwritten like any other missing draft.
func TestFallback_PlaywrightFallbackPreservesThisGenerationsDraft(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	stage := OpenStage(co, "stry_ab12")
	silenceFeed(stage)
	story := validStory()
	story.ID = stage.gen // the playwright's write_draft stamps the generation id
	if err := co.SaveWorking(Working{Story: story, Revision: 3, Status: "draft"}); err != nil {
		t.Fatal(err)
	}

	runner := NewRunner(co, stage)
	text, err := runner.roleFallback("playwright", "revise the draft", 0)
	if err != nil {
		t.Fatal(err)
	}
	var rep DraftReport
	if !parseArtifact(text, &rep) || rep.Title != "The Test Night" {
		t.Errorf("report = %q, want the existing draft's report", text)
	}
	w, err := co.LoadWorking()
	if err != nil {
		t.Fatal(err)
	}
	if w.Revision != 3 || w.Story.Title != "The Test Night" {
		t.Errorf("working = rev %d, title %q — the playwright's draft was clobbered", w.Revision, w.Story.Title)
	}

	// A stale draft (a different id) is not preserved: the floor composes.
	stale := validStory()
	stale.ID = "stry_old1"
	if err := co.SaveWorking(Working{Story: stale, Revision: 9, Status: "submitted"}); err != nil {
		t.Fatal(err)
	}
	text, err = runner.roleFallback("playwright", "write the draft", 0)
	if err != nil {
		t.Fatal(err)
	}
	w, err = co.LoadWorking()
	if err != nil {
		t.Fatal(err)
	}
	if w.Story.ID != stage.gen || w.Story.Origin != "composer" {
		t.Errorf("working = id %q, origin %q — want a fresh composer draft under the generation id", w.Story.ID, w.Story.Origin)
	}
	if !parseArtifact(text, &rep) || rep.Title != w.Story.Title {
		t.Errorf("report = %q, want the fresh draft's report", text)
	}
}
