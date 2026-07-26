package model

import (
	"strings"
	"testing"
)

func validStory() Story {
	return Story{
		ID:         "stry_abc123",
		Title:      "The Great Mouse Hunt",
		DurationMs: 4000,
		Cast: []Cast{
			{ID: "ina", Character: "cat", Lane: 0, X: 0.3, Scale: 1},
			{ID: "freija", Character: "dog", Lane: 0, X: 0.7, Scale: 1},
		},
		Beats: []Beat{
			{T: 0, Actor: "ina", Action: "enter", From: "left", Ms: 1100},
			{T: 1500, Actor: "ina", Action: "vocalize"},
		},
	}
}

func TestValidate_AcceptsGoodStory(t *testing.T) {
	s := validStory()
	if err := s.Validate(); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
	if len(s.Beats) != 2 || len(s.Cast) != 2 {
		t.Fatalf("validation should not have dropped anything: %+v", s)
	}
}

func TestValidate_DropsUnknownVocabulary(t *testing.T) {
	s := validStory()
	// An LLM inventing characters, props and actions must not reach the player.
	s.Cast = append(s.Cast, Cast{ID: "drag", Character: "dragon", X: 0.5})
	s.Props = []Prop{{ID: "sword", Prop: "sword", X: 0.5}}
	s.Beats = append(
		s.Beats,
		Beat{T: 100, Actor: "ina", Action: "breatheFire"},
		Beat{T: 200, Actor: "drag", Action: "enter"},
	)

	if err := s.Validate(); err != nil {
		t.Fatalf("expected the story to survive with junk removed, got %v", err)
	}
	for _, c := range s.Cast {
		if c.Character == "dragon" {
			t.Error("unknown character survived validation")
		}
	}
	if len(s.Props) != 0 {
		t.Errorf("unknown prop survived validation: %+v", s.Props)
	}
	for _, b := range s.Beats {
		if b.Action == "breatheFire" || b.Actor == "drag" {
			t.Errorf("unknown action/actor survived validation: %+v", b)
		}
	}
}

func TestValidate_ClampsNumbers(t *testing.T) {
	s := validStory()
	s.DurationMs = 999999
	s.Cast[0].Lane = 99
	s.Cast[0].X = 12
	s.Cast[0].Scale = 50
	s.Beats[0].T = -500
	s.Beats = append(s.Beats, Beat{T: 999999, Actor: "ina", Action: "sit", Ms: 999999})

	if err := s.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.DurationMs != MaxStoryDurationMs {
		t.Errorf("duration not clamped: %v", s.DurationMs)
	}
	if s.Cast[0].Lane != MaxLanes-1 {
		t.Errorf("lane not clamped: %v", s.Cast[0].Lane)
	}
	if s.Cast[0].X > 0.95 || s.Cast[0].X < 0.05 {
		t.Errorf("x not clamped: %v", s.Cast[0].X)
	}
	if s.Cast[0].Scale > 1.6 {
		t.Errorf("scale not clamped: %v", s.Cast[0].Scale)
	}
	for _, b := range s.Beats {
		if b.T < 0 || b.T > s.DurationMs {
			t.Errorf("beat t not clamped: %v", b.T)
		}
		if b.Ms > s.DurationMs {
			t.Errorf("beat ms not clamped: %v", b.Ms)
		}
	}
}

func TestValidate_RejectsBadIDs(t *testing.T) {
	s := validStory()
	s.ID = "../../etc/passwd"
	if err := s.Validate(); err == nil {
		t.Fatal("expected a path-ish story ID to be rejected")
	}

	s = validStory()
	s.Cast[0].ID = "Ina Bad ID!"
	// The cast entry is dropped, so beats referencing it go too. "freija" has no
	// beats of its own, leaving nothing playable.
	if err := s.Validate(); err == nil {
		t.Fatal("expected rejection once every beat lost its actor")
	}
}

func TestValidate_TargetRequiredActionsDropped(t *testing.T) {
	s := validStory()
	s.Beats = append(
		s.Beats,
		Beat{T: 2000, Actor: "ina", Action: "pounce"},                  // no target
		Beat{T: 2100, Actor: "ina", Action: "pounce", Target: "ghost"}, // unknown target
		Beat{T: 2200, Actor: "ina", Action: "greet", Target: "freija"}, // fine
	)
	if err := s.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pounces := 0
	greets := 0
	for _, b := range s.Beats {
		switch b.Action {
		case "pounce":
			pounces++
		case "greet":
			greets++
		}
	}
	if pounces != 0 {
		t.Errorf("targetless/unknown-target pounces survived: %d", pounces)
	}
	if greets != 1 {
		t.Errorf("valid greet was dropped, greets=%d", greets)
	}
}

func TestValidate_ActorMustBeCastNotProp(t *testing.T) {
	s := validStory()
	s.Props = []Prop{{ID: "yarn1", Prop: "yarn", X: 0.5}}
	s.Beats = append(s.Beats, Beat{T: 900, Actor: "yarn1", Action: "vocalize"})
	if err := s.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, b := range s.Beats {
		if b.Actor == "yarn1" {
			t.Error("a prop was allowed to act")
		}
	}
}

func TestSanitizeTitle(t *testing.T) {
	cases := map[string]string{
		"  The   Box  ":          "The Box",
		"Ina\x00\x07 & Freija":   "Ina & Freija",
		"line\nbreak":            "line break",
		strings.Repeat("a", 200): strings.Repeat("a", MaxTitleLen),
	}
	for in, want := range cases {
		if got := sanitizeTitle(in); got != want {
			t.Errorf("sanitizeTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidate_EmptyTitleGetsFallback(t *testing.T) {
	s := validStory()
	s.Title = "   "
	if err := s.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Title == "" {
		t.Error("expected a fallback title")
	}
}

func TestValidate_CapsCollectionSizes(t *testing.T) {
	s := validStory()
	for range 50 {
		s.Beats = append(s.Beats, Beat{T: 100, Actor: "ina", Action: "blink"})
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s.Beats) > MaxBeats {
		t.Errorf("beats not capped: %d", len(s.Beats))
	}
}

// Guard against a template regression: every composer scene must validate and
// every beat must reference a character that entered.
func TestValidate_RejectsNoCast(t *testing.T) {
	s := validStory()
	s.Cast = nil
	if err := s.Validate(); err == nil {
		t.Fatal("expected rejection with no cast")
	}
}

func TestValidate_CellsAndSceneBeats(t *testing.T) {
	s := validStory()
	s.Scene = Scene{
		Backdrop: "livingroom",
		Cells: []Cell{
			{ID: "c1", Row: "mid", Col: 2, Piece: "lamp"},
			{ID: "c2", Row: "far", Col: 9, Piece: "tree"},    // col clamped
			{ID: "c3", Row: "orbit", Col: 1, Piece: "tree"},  // bad row → dropped
			{ID: "c4", Row: "near", Col: 1, Piece: "dragon"}, // bad piece → emptied, slot kept
			{ID: "bad id!", Row: "mid", Col: 0},              // bad id → dropped
		},
	}
	s.Beats = append(s.Beats,
		Beat{T: 3000, Action: "setCell", Target: "c1", Piece: "plant"},
		Beat{T: 3200, Action: "setCell", Target: "nope", Piece: "plant"},  // unknown cell
		Beat{T: 3400, Action: "setCell", Target: "c1", Piece: "warpcore"}, // unknown piece
		Beat{T: 3600, Action: "setBackdrop", Piece: "night"},
		Beat{T: 3800, Action: "setBackdrop", Piece: "mordor"}, // unknown backdrop
	)
	if err := s.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(s.Scene.Cells) != 3 {
		t.Fatalf("expected 3 surviving cells, got %d: %+v", len(s.Scene.Cells), s.Scene.Cells)
	}
	byID := map[string]Cell{}
	for _, c := range s.Scene.Cells {
		byID[c.ID] = c
	}
	if byID["c2"].Col != CellCols-1 {
		t.Errorf("col not clamped: %+v", byID["c2"])
	}
	if byID["c4"].Piece != "" {
		t.Errorf("unknown piece should empty the cell, not fill it: %+v", byID["c4"])
	}

	setCells, setBackdrops := 0, 0
	for _, b := range s.Beats {
		switch b.Action {
		case "setCell":
			setCells++
			if b.Piece != "plant" {
				t.Errorf("bad setCell survived: %+v", b)
			}
			if b.Actor != "" {
				t.Errorf("scene beat should carry no actor: %+v", b)
			}
		case "setBackdrop":
			setBackdrops++
			if b.Piece != "night" {
				t.Errorf("bad setBackdrop survived: %+v", b)
			}
		}
	}
	if setCells != 1 {
		t.Errorf("expected 1 valid setCell, got %d", setCells)
	}
	if setBackdrops != 1 {
		t.Errorf("expected 1 valid setBackdrop, got %d", setBackdrops)
	}
}

// A character action must never smuggle a set piece through.
func TestValidate_CharacterBeatsCarryNoPiece(t *testing.T) {
	s := validStory()
	s.Beats = append(s.Beats, Beat{T: 900, Actor: "ina", Action: "sit", Piece: "lamp"})
	if err := s.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, b := range s.Beats {
		if b.Action == "sit" && b.Piece != "" {
			t.Errorf("character beat kept a piece: %+v", b)
		}
	}
}

// Cell ids share the id namespace, so a cell cannot masquerade as an actor.
func TestValidate_CellCannotAct(t *testing.T) {
	s := validStory()
	s.Scene = Scene{Backdrop: "night", Cells: []Cell{{ID: "c1", Row: "mid", Col: 1, Piece: "lamp"}}}
	s.Beats = append(s.Beats, Beat{T: 900, Actor: "c1", Action: "vocalize"})
	if err := s.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, b := range s.Beats {
		if b.Actor == "c1" {
			t.Error("a cell was allowed to act")
		}
	}
}

func TestValidate_BackdropDefaults(t *testing.T) {
	s := validStory()
	s.Scene.Backdrop = "Atlantis"
	if err := s.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Scene.Backdrop != DefaultBackdrop {
		t.Errorf("backdrop = %q, want %q", s.Scene.Backdrop, DefaultBackdrop)
	}
}
