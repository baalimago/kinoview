package theatre

import (
	"context"
	"strings"
	"testing"
)

// Every production role prompt declares its scope explicitly in the three
// sections the working-context standard relies on: what it decides, what it
// asks and when it stops. A prompt missing a section would let the role drift
// outside its remit or burn budget past its deliverable.
func TestRolePrompts_ThreeSections(t *testing.T) {
	t.Parallel()
	for _, role := range []string{"dramaturg", "playwright", "scenographer", "wardrobe"} {
		prompt := RolePrompt(role)
		if !strings.HasPrefix(prompt, "You are the ") {
			t.Errorf("%s prompt does not open with the role: %q", role, prompt)
		}
		for _, section := range []string{"You decide:", "You ask:", "You stop:"} {
			if !strings.Contains(prompt, section) {
				t.Errorf("%s prompt lacks %q:\n%s", role, section, prompt)
			}
		}
	}
}

// The wardrobe's tool set is just advise — without consult, matching its "You
// ask: nothing" scope (review 1, R1-02). The other three production roles
// keep consult.
func TestRoleTools_WardrobeLacksConsult(t *testing.T) {
	t.Parallel()
	co := Open(t.TempDir())
	stage := OpenStage(co, "stry_ab12")
	silenceFeed(stage)
	runner, _ := stubRunner(t, stage, nil)

	names := func(role string) map[string]bool {
		t.Helper()
		out := map[string]bool{}
		for _, tool := range runner.roleTools(context.Background(), role, 0) {
			out[tool.Specification().Name] = true
		}
		return out
	}

	wardrobe := names("wardrobe")
	if wardrobe["consult"] {
		t.Error("the wardrobe carries consult — its scope says it asks nothing")
	}
	for _, want := range []string{"advise"} {
		if !wardrobe[want] {
			t.Errorf("wardrobe tool set lacks %q; got %v", want, wardrobe)
		}
	}
	for _, role := range []string{"dramaturg", "playwright", "scenographer"} {
		if !names(role)["consult"] {
			t.Errorf("%s lost the consult tool", role)
		}
	}
}

// The role prompts are constants, not vars: the compile-time len checks in
// roles.go prove it (a var would not be usable in a constant expression).
// This test pins the scope text the machinery greps for, so a future rewrite
// cannot silently drop the wardrobe's no-writer contract or the dramaturg's
// ask-nothing clause.
func TestRolePrompts_ScopeTextPinned(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		role, needle string
	}{
		{"dramaturg", "final answer is the brief text"},
		{"playwright", "final answer is the complete story"},
		{"playwright", "character one of cat, dog, mouse, bird"},
		{"playwright", "prop one of yarn, box, ball, bone, cushion, bowl"},
		{"playwright", "action one of enter, exit, walkTo, vocalize, sit, stretch, blink, pounce, chase, greet, stareoff, nap, bat, yawn, sniff, jump"},
		{"playwright", "one of night, livingroom, garden, theatre, sunset, kitchen, forest, rain"},
		{"playwright", "1-2 canon facts"},
		{"playwright", "cast member must enter with an enter beat"},
		{"scenographer", "delivered with write_scene"},
		{"scenographer", "never put a piece through a performer"},
		{"wardrobe", "fixed cast and its canon looks"},
		{"wardrobe", "You decide: nothing"},
	} {
		if !strings.Contains(RolePrompt(tc.role), tc.needle) {
			t.Errorf("%s prompt dropped %q", tc.role, tc.needle)
		}
	}
}

// The theatre board is free-form prose in the shared notebook now: no role
// prompt — director included — may reference the removed structured
// machinery. The list is the phase-4 removal contract plus the phase-5
// notebook direction; a re-introduction of any of these words means the
// prompt drifted back to the old board.
func TestRolePrompt_DropsRegistryAndBoardReferences(t *testing.T) {
	t.Parallel()
	for _, role := range []string{"director", "dramaturg", "playwright", "scenographer", "wardrobe"} {
		prompt := RolePrompt(role)
		for _, gone := range []string{"post_to_board", "read_board", "registry", "pin_identity", "bulletin", "lessons"} {
			if strings.Contains(prompt, gone) {
				t.Errorf("%s prompt still mentions %q:\n%s", role, gone, prompt)
			}
		}
	}
}
