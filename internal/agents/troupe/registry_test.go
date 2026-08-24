package troupe

import (
	"path"
	"testing"

	"github.com/baalimago/clai/pkg/text/models"
	clai_tools "github.com/baalimago/clai/pkg/tools"
)

// TestRegistry_ClosedSet pins the closed registry: exactly the canonical
// tool names, one entry per name. Adding a tool is a human-gated change to
// this table — and to this test.
func TestRegistry_ClosedSet(t *testing.T) {
	want := []string{
		"cat", "rows_between", "ls", "rg", "write_file", "apply_patch", "mkdir",
		"mcp_slivingdoc_notes_pull", "mcp_slivingdoc_notes_commit",
		"spawn_role",
	}
	if len(toolRegistry) != len(want) {
		t.Fatalf("registry has %d entries, want %d", len(toolRegistry), len(want))
	}
	seen := map[string]bool{}
	for _, e := range toolRegistry {
		if e.name == "" {
			t.Error("registry entry with an empty name")
		}
		if seen[e.name] {
			t.Errorf("registry name %q appears more than once", e.name)
		}
		seen[e.name] = true
		// exactly one materialisation: a glob or a dynamic constructor.
		if (e.glob == "") == (e.build == nil) {
			t.Errorf("registry entry %q must set exactly one of glob/build", e.name)
		}
	}
	for _, name := range want {
		if !seen[name] {
			t.Errorf("registry is missing the canonical tool %q", name)
		}
	}
}

// TestRegistry_FileToolsResolve pins every filesystem tool name to the clai
// tool it materialises: the glob matches the tool's own specification name,
// so the mapping is the single source of truth and an agent selecting the
// name gets that tool.
func TestRegistry_FileToolsResolve(t *testing.T) {
	tools := map[string]models.LLMTool{
		"cat":          clai_tools.Cat,
		"rows_between": clai_tools.RowsBetween,
		"ls":           clai_tools.LS,
		"rg":           clai_tools.RipGrep,
		"write_file":   clai_tools.WriteFile,
		"apply_patch":  clai_tools.ApplyPatch,
		"mkdir":        clai_tools.Mkdir,
	}
	for name, tool := range tools {
		e, ok := lookupTool(name)
		if !ok {
			t.Fatalf("lookupTool(%q) not found", name)
		}
		if got := tool.Specification().Name; got != name {
			t.Errorf("clai tool for %q is named %q — the registry glob would not resolve", name, got)
		}
		if e.glob != name {
			t.Errorf("registry entry %q glob = %q, want the exact tool name", name, e.glob)
		}
		if e.notebook {
			t.Errorf("registry entry %q must not be notebook-gated", name)
		}
	}
}

// TestRegistry_NotebookTools pins the slivingdoc tools: enumerated exactly,
// notebook-gated, and covered by the shared mcp_slivingdoc* glob the
// slivingdoc package applies — the two stay in sync.
func TestRegistry_NotebookTools(t *testing.T) {
	for _, name := range []string{"mcp_slivingdoc_notes_pull", "mcp_slivingdoc_notes_commit"} {
		e, ok := lookupTool(name)
		if !ok {
			t.Fatalf("lookupTool(%q) not found", name)
		}
		if e.glob != name || !e.notebook || e.build != nil {
			t.Errorf("registry entry %q = %+v, want the exact notebook-gated glob", name, e)
		}
		if matched, err := path.Match("mcp_slivingdoc*", e.glob); err != nil || !matched {
			t.Errorf("registry glob %q is not covered by the shared mcp_slivingdoc* glob", e.glob)
		}
	}
}

// TestRegistry_SpawnRole pins spawn_role: a dynamic per-spawner tool, never
// a glob, so the recursion always runs through the same runner.
func TestRegistry_SpawnRole(t *testing.T) {
	e, ok := lookupTool("spawn_role")
	if !ok {
		t.Fatal("lookupTool(spawn_role) not found")
	}
	if e.glob != "" || e.notebook || e.build == nil {
		t.Fatalf("spawn_role entry = %+v, want a dynamic constructor", e)
	}

	s := &Spawner{}
	tool := e.build(s)
	spec := tool.Specification()
	if spec.Name != "spawn_role" {
		t.Errorf("tool name = %q, want spawn_role", spec.Name)
	}
	if spec.Inputs == nil || spec.Inputs.Type != "object" {
		t.Fatalf("spawn_role inputs = %+v, want an object schema", spec.Inputs)
	}
	for _, field := range []string{"role", "task"} {
		if _, ok := spec.Inputs.Properties[field]; !ok {
			t.Errorf("spawn_role inputs lack the %q property", field)
		}
	}
	if len(spec.Inputs.Required) != 2 || spec.Inputs.Required[0] != "role" || spec.Inputs.Required[1] != "task" {
		t.Errorf("spawn_role required = %v, want [role task]", spec.Inputs.Required)
	}
}
