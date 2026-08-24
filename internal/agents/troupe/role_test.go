package troupe

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// roleNote wraps a role spec in the flat role envelope.
func roleNote(id, prompt string, tools []string, budget int) string {
	ver := ""
	if budget != 0 {
		ver = fmt.Sprintf(`,"budget":%d`, budget)
	}
	return `{"id":"` + id + `","prompt":"` + prompt + `","tools":[` + joinQuoted(tools) + `]` + ver + `}`
}

func joinQuoted(ss []string) string {
	quoted := make([]string, len(ss))
	for i, s := range ss {
		quoted[i] = `"` + s + `"`
	}
	return strings.Join(quoted, ",")
}

const (
	clownID     = "clown"
	clownPrompt = "You are the clown. You decide: gags. You stop: when the gag lands."
	clownTools  = `["cat","write_file","spawn_role"]`
)

// TestParseRole_Valid pins the positive control: a well-formed role note
// parses clean with the filename as the identity authority.
func TestParseRole_Valid(t *testing.T) {
	r, err := ParseRole("roles/clown.json", []byte(roleNote(clownID, clownPrompt, []string{"cat", "write_file", "spawn_role"}, 8)))
	if err != nil {
		t.Fatalf("ParseRole: %v", err)
	}
	if r.ID != clownID || r.Prompt != clownPrompt || r.Budget != 8 {
		t.Errorf("role = %+v, want id %q, the prompt, budget 8", r, clownID)
	}
	if len(r.Tools) != 3 || r.Tools[0] != "cat" || r.Tools[2] != "spawn_role" {
		t.Errorf("tools = %v, want [cat write_file spawn_role]", r.Tools)
	}
}

// TestParseRole_BudgetClamped pins the clamp: absent or negative budgets
// become the default, oversized budgets are capped, in-range budgets pass
// through untouched.
func TestParseRole_BudgetClamped(t *testing.T) {
	cases := []struct {
		name   string
		budget int
		want   int
	}{
		{"absent", 0, defaultRoleBudget},
		{"negative", -3, defaultRoleBudget},
		{"oversized", 1000, maxRoleBudget},
		{"in range", 32, 32},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			r, err := ParseRole("roles/clown.json", []byte(roleNote(clownID, clownPrompt, []string{"cat"}, tt.budget)))
			if err != nil {
				t.Fatalf("ParseRole: %v", err)
			}
			if r.Budget != tt.want {
				t.Errorf("budget = %d, want %d", r.Budget, tt.want)
			}
		})
	}
}

// TestParseRole_Refused pins the refusals: a role whose tool list names
// anything outside the closed registry is refused with an exact error, and
// the id/prompt/tools shapes are pattern- and length-checked.
func TestParseRole_Refused(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		doc      string
		want     string
	}{
		{
			"unregistered tool",
			"roles/clown.json",
			roleNote(clownID, clownPrompt, []string{"cat", "website_text"}, 8),
			`tools: "website_text" is not in the closed registry`,
		},
		{
			"unregistered tool only",
			"roles/clown.json",
			roleNote(clownID, clownPrompt, []string{"mcp_playwright_navigate"}, 8),
			`tools: "mcp_playwright_navigate" is not in the closed registry`,
		},
		{
			"no tools",
			"roles/clown.json",
			roleNote(clownID, clownPrompt, nil, 8),
			"tools: must select at least one tool from the closed registry",
		},
		{
			"duplicate tool",
			"roles/clown.json",
			roleNote(clownID, clownPrompt, []string{"cat", "cat"}, 8),
			`tools: "cat" selected twice`,
		},
		{
			"empty prompt",
			"roles/clown.json",
			roleNote(clownID, "", []string{"cat"}, 8),
			"prompt: must not be empty",
		},
		{
			"id mismatch with filename",
			"roles/clown.json",
			roleNote("jester", clownPrompt, []string{"cat"}, 8),
			`id "jester" does not match the filename id "clown"`,
		},
		{
			"wrong directory",
			"models/clown.json",
			roleNote(clownID, clownPrompt, []string{"cat"}, 8),
			"role notes live in roles/",
		},
		{
			"bad id charset",
			"roles/Clown.json",
			roleNote(clownID, clownPrompt, []string{"cat"}, 8),
			`role id "Clown" must match`,
		},
		{
			"unknown field",
			"roles/clown.json",
			`{"id":"clown","prompt":"p","tools":["cat"],"budget":8,"mood":"sad"}`,
			"unknown field",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseRole(tt.filename, []byte(tt.doc))
			if err == nil {
				t.Fatalf("ParseRole accepted %s", tt.filename)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %q, want it to contain %q", err, tt.want)
			}
		})
	}
}

// TestParseRole_PromptCapped pins the prompt length cap.
func TestParseRole_PromptCapped(t *testing.T) {
	long := strings.Repeat("x", maxRolePromptLen+1)
	_, err := ParseRole("roles/clown.json", []byte(roleNote(clownID, long, []string{"cat"}, 8)))
	if err == nil || !strings.Contains(err.Error(), "exceeds the") {
		t.Fatalf("err = %v, want a prompt-length error", err)
	}
}

// TestNewRoleSource pins the snapshot-backed role source: role notes are
// read and validated through the same seam the spawner uses.
// TestNewWorktreeRoleSource pins the live role source: it reads roles/ from
// the materialised worktree at call time, so a role written during the
// generation is visible to the swarm immediately — the production shape
// serve hands the spawner (phase 9).
func TestNewWorktreeRoleSource(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "roles"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "roles", "clown.json")
	if err := os.WriteFile(path, []byte(roleNote(clownID, clownPrompt, []string{"cat"}, 0)), 0o644); err != nil {
		t.Fatal(err)
	}

	src := NewWorktreeRoleSource(dir)
	r, err := src.Role("clown")
	if err != nil {
		t.Fatalf("Role(clown): %v", err)
	}
	if r.ID != clownID || r.Budget != defaultRoleBudget {
		t.Errorf("role = %+v, want id %q with the default budget", r, clownID)
	}

	// A role authored after the source was built is visible: the source
	// reads the live worktree, never a setup-time snapshot.
	if err := os.WriteFile(filepath.Join(dir, "roles", "late.json"), []byte(roleNote("late", "late", []string{"cat"}, 4)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := src.Role("late"); err != nil {
		t.Errorf("Role(late) after authoring: %v — the source must see live roles", err)
	}

	if _, err := src.Role("ghost"); err == nil || !strings.Contains(err.Error(), "no such role ghost in roles/") {
		t.Errorf("Role(ghost) err = %v, want a missing-role error", err)
	}
}

// TestNewRoleSource pins the snapshot-backed role source: role notes are
// read and validated through the same seam the spawner uses.
func TestNewRoleSource(t *testing.T) {
	snap := Snapshot{
		"roles/clown.json": []byte(roleNote(clownID, clownPrompt, []string{"cat", "spawn_role"}, 0)),
	}
	src := NewRoleSource(snap)

	r, err := src.Role("clown")
	if err != nil {
		t.Fatalf("Role(clown): %v", err)
	}
	if r.ID != clownID || r.Budget != defaultRoleBudget {
		t.Errorf("role = %+v, want id %q with the default budget", r, clownID)
	}

	if _, err := src.Role("ghost"); err == nil || !strings.Contains(err.Error(), "no such role ghost in roles/") {
		t.Errorf("Role(ghost) err = %v, want a missing-role error", err)
	}

	snap["roles/clown.json"] = []byte(roleNote(clownID, clownPrompt, []string{"website_text"}, 8))
	if _, err := src.Role("clown"); err == nil || !strings.Contains(err.Error(), "not in the closed registry") {
		t.Errorf("Role(clown) with an unregistered tool: err = %v, want a registry refusal", err)
	}
}
