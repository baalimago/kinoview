package theatre

import (
	"fmt"
	"strings"
	"testing"
)

// AssembleContext renders every section, in the documented order, with the
// working summary in its context-cheap shape.
func TestAssembleContext_OrderAndContent(t *testing.T) {
	t.Parallel()
	var board Board
	board.Generation = "stry_test1"
	for i := range 5 {
		board.Append(Entry{Author: "stage", Kind: "note", Body: fmt.Sprintf("note %d", i)})
	}
	working := Working{Story: validStory(), Revision: 1, Status: "draft"}.Summary()
	out := AssembleContext("stry_test1", "Solaris 1972", board, working,
		"you write the draft", "write the draft now")

	for _, want := range []string{
		"Generation: stry_test1",
		"Theme: Solaris 1972",
		"Board (most recent work):",
		"[1] stage (note) → company: note 0",
		"Working file:",
		"Title: The Test Night",
		"Cast: ina",
		"Beats: 2",
		"Backdrop: night",
		"Status: draft",
		"Your role:",
		"you write the draft",
		"Task:",
		"write the draft now",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	order := []string{"Generation: ", "Theme: ", "Board (most recent work):", "Working file:", "Your role:", "Task:"}
	for i := 1; i < len(order); i++ {
		if strings.Index(out, order[i-1]) > strings.Index(out, order[i]) {
			t.Errorf("sections out of order: %q before %q", order[i], order[i-1])
		}
	}
}

// The excerpt cap is the same regardless of how far past it the board grows:
// a 60-entry board renders the same number of board lines as a 21-entry one.
func TestAssembleContext_BoardExcerptCapped(t *testing.T) {
	t.Parallel()
	boardLines := func(n int) int {
		var board Board
		for i := range n {
			board.Append(Entry{Author: "stage", Kind: "note", Body: fmt.Sprintf("note %d", i)})
		}
		out := AssembleContext("g", "", board, Summary{}, "role", "task")
		from := strings.Index(out, "Board (most recent work):")
		to := strings.Index(out, "Working file:")
		return strings.Count(out[from:to], "note ")
	}
	for _, n := range []int{21, 60} {
		if got := boardLines(n); got != BoardExcerptMax {
			t.Errorf("%d-entry board rendered %d excerpt lines, want %d", n, got, BoardExcerptMax)
		}
	}
}

func TestAssembleContext_EmptyBoard(t *testing.T) {
	t.Parallel()
	out := AssembleContext("g", "", Board{}, Summary{}, "role", "task")
	if !strings.Contains(out, "(empty — nothing posted yet)") {
		t.Errorf("empty board not marked:\n%s", out)
	}
	if strings.Contains(out, "→ company:") {
		t.Errorf("empty board rendered entries:\n%s", out)
	}
}
