package theatre

import (
	"strings"
	"testing"
)

// AssembleContext renders every section, in the documented order, with the
// working summary in its context-cheap shape.
func TestAssembleContext_OrderAndContent(t *testing.T) {
	t.Parallel()
	working := Working{Story: validStory(), Revision: 1, Status: "draft"}.Summary()
	out := AssembleContext("stry_test1", "Solaris 1972", working,
		"you write the draft", "write the draft now")

	for _, want := range []string{
		"Generation: stry_test1",
		"Theme: Solaris 1972",
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

	order := []string{"Generation: ", "Theme: ", "Working file:", "Your role:", "Task:"}
	for i := 1; i < len(order); i++ {
		if strings.Index(out, order[i-1]) > strings.Index(out, order[i]) {
			t.Errorf("sections out of order: %q before %q", order[i], order[i-1])
		}
	}
}

// The working summary is the context's only state section: it is rendered
// as-is and never grows a prompt.
func TestAssembleContext_WorkingSummaryOnly(t *testing.T) {
	t.Parallel()
	out := AssembleContext("g", "", Working{Story: validStory(), Revision: 1, Status: "draft"}.Summary(), "role", "task")
	if strings.Contains(out, "Board") {
		t.Errorf("output mentions a board:\n%s", out)
	}
}

// A fresh generation has no draft: the working summary is empty and the
// context says so plainly instead of rendering an empty title, a phantom
// "Acts: 1" and a blank backdrop — the previous generation's story must not
// prime the new one (the same-play loop).
func TestAssembleContext_NoDraftYet(t *testing.T) {
	t.Parallel()
	out := AssembleContext("g", "Solaris 1972", Summary{}, "role", "task")
	if !strings.Contains(out, "(no draft yet — this generation starts fresh)") {
		t.Errorf("fresh generation not marked:\n%s", out)
	}
	if strings.Contains(out, "Title: ") {
		t.Errorf("empty working summary rendered a title:\n%s", out)
	}
}
