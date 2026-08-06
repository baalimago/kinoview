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

// The wardrobe's tool set is the spec's three — post_to_board, read_board and
// advise — without consult, matching its "You ask: nothing" scope (review 1,
// R1-02). The other three production roles keep consult.
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
	for _, want := range []string{"post_to_board", "read_board", "advise"} {
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
		{"dramaturg", "delivered with write_brief"},
		{"playwright", "final answer is the complete story"},
		{"playwright", "character one of cat, dog, mouse, bird"},
		{"playwright", "prop one of yarn, box, ball, bone, cushion, bowl"},
		{"playwright", "action one of enter, exit, walkTo, vocalize, sit, stretch, blink, pounce, chase, greet, stareoff, nap, bat, yawn, sniff, jump"},
		{"playwright", "one of night, livingroom, garden, theatre, sunset, kitchen, forest, rain"},
		{"playwright", "1-2 canon facts"},
		{"playwright", "cast member must enter with an enter beat"},
		{"scenographer", "delivered with write_scene"},
		{"scenographer", "never put a piece through a performer"},
		{"wardrobe", "grounded in the character registry"},
		{"wardrobe", "You decide: nothing"},
	} {
		if !strings.Contains(RolePrompt(tc.role), tc.needle) {
			t.Errorf("%s prompt dropped %q", tc.role, tc.needle)
		}
	}
}
