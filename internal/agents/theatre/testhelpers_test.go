package theatre

import (
	"fmt"
	"testing"

	"github.com/baalimago/kinoview/internal/model"
)

// validStory is a minimal playable story for round-trip tests: it survives
// model.Story.Validate, so a working file built on it is always loadable.
func validStory() model.Story {
	s := model.Story{
		ID:         "stry_test1",
		Title:      "The Test Night",
		Origin:     "composer",
		DurationMs: 4000,
		Scene:      model.Scene{Backdrop: "night"},
		Cast: []model.Cast{
			{ID: "ina", Character: "cat", Lane: 0, X: 0.4},
		},
		Beats: []model.Beat{
			{T: 0, Actor: "ina", Action: "enter", From: "left", Ms: 1200},
			{T: 1600, Actor: "ina", Action: "vocalize"},
		},
	}
	if err := s.Validate(); err != nil {
		panic(err)
	}
	return s
}

// silenceFeed turns a stage's feed into a no-op, for fixtures whose stdout is
// not under test. It must run before the first emit, so the feed goroutine
// sees the swap through the channel's happens-before.
func silenceFeed(s *Stage) {
	s.feed.print = func(string, string) {}
}

// scriptedProduction runs a fixture generation through the stage: 20 events
// (phases, posts, a consult, an answer, a deliver, a warning note, calls and
// a submit) that exercise every transcript kind and the documented feed line
// formats. With loud=false the feed is silenced — the fixture is for the
// files, not the stdout. The feed is left closed by Submit; callers drain it
// with <-stage.feed.done() where they need the output flushed.
func scriptedProduction(t *testing.T, co *Company, gen string, loud bool) *Stage {
	t.Helper()
	stage := OpenStage(co, gen, WithBudgets(50, 200))
	if !loud {
		silenceFeed(stage)
	}
	stage.SetActorBudget("dramaturg", 8)
	stage.SetActorBudget("playwright", 8)
	stage.SetActorBudget("scenographer", 8)
	stage.SetPhase("brief")
	stage.Emit(TranscriptEvent{Kind: "post", From: "director", To: "dramaturg", Body: "brief (mood=standoff, lineup=3)"})
	for i := range 6 {
		stage.Emit(TranscriptEvent{Kind: "post", From: "dramaturg", To: "playwright", Body: fmt.Sprintf("note %d", i)})
	}
	stage.Emit(TranscriptEvent{Kind: "consult", From: "playwright", To: "wardrobe", Body: `"does silver read on night?"`})
	stage.Emit(TranscriptEvent{Kind: "answer", From: "wardrobe", To: "playwright", Body: `"silver reads; keep ina lane 1"`})
	stage.Emit(TranscriptEvent{Kind: "deliver", From: "playwright", To: "draft", Body: `16 beats / 3 acts / "The Long Night"`})
	stage.Emit(TranscriptEvent{Kind: "note", From: "stage", Body: "budget refusal: playwright out of calls", Level: "warning"})
	for range 3 {
		stage.RecordCall("director", "read_story")
	}
	stage.RecordConsult("playwright", 2)
	stage.RecordTokens("playwright", 1234)
	for range 2 {
		stage.RecordCall("scenographer", "dress_set")
	}
	stage.SetPhase("dress")
	for i := range 5 {
		stage.Emit(TranscriptEvent{Kind: "post", From: "scenographer", To: "director", Body: fmt.Sprintf("dressing %d", i)})
	}
	stage.SetPhase("submit")
	stage.Submit("The Long Night")
	return stage
}
