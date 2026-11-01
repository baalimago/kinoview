package troupe

import (
	"path"
	"slices"
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
		"view_image", "mcp_playwright",
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
		// exactly one materialisation: globs or a dynamic constructor.
		if (len(e.globs) == 0) == (e.build == nil) {
			t.Errorf("registry entry %q must set exactly one of globs/build", e.name)
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
		"view_image":   clai_tools.ViewImage,
	}
	for name, tool := range tools {
		e, ok := lookupTool(name)
		if !ok {
			t.Fatalf("lookupTool(%q) not found", name)
		}
		if got := tool.Specification().Name; got != name {
			t.Errorf("clai tool for %q is named %q — the registry glob would not resolve", name, got)
		}
		if len(e.globs) != 1 || e.globs[0] != name {
			t.Errorf("registry entry %q globs = %v, want the exact tool name", name, e.globs)
		}
		if e.notebook {
			t.Errorf("registry entry %q must not be notebook-gated", name)
		}
		if e.browser {
			t.Errorf("registry entry %q must not be browser-gated", name)
		}
	}
}

// TestRegistry_PerceptionTools pins the perception pair: view_image is a clai
// native tool matched by its exact name, and mcp_playwright enumerates the
// read-only inspection subset of the Playwright MCP server. Only the server
// subset is gated: a clai native tool needs no MCP server. Page mutation and
// code execution stay out of the registry on purpose.
func TestRegistry_PerceptionTools(t *testing.T) {
	view, ok := lookupTool("view_image")
	if !ok {
		t.Fatal("lookupTool(view_image) not found")
	}
	if len(view.globs) != 1 || view.globs[0] != "view_image" || view.browser || view.notebook || view.build != nil {
		t.Errorf("view_image entry = %+v, want the exact, ungated native glob", view)
	}
	if got := clai_tools.ViewImage.Specification().Name; got != view.globs[0] {
		t.Errorf("clai view_image is named %q — the registry glob would not resolve", got)
	}

	pw, ok := lookupTool("mcp_playwright")
	if !ok {
		t.Fatal("lookupTool(mcp_playwright) not found")
	}
	if !pw.browser || pw.notebook || pw.build != nil {
		t.Errorf("mcp_playwright entry = %+v, want a browser-gated subset", pw)
	}
	want := []string{
		"mcp_playwright_browser_navigate",
		"mcp_playwright_browser_wait_for",
		"mcp_playwright_browser_take_screenshot",
		"mcp_playwright_browser_console_messages",
		"mcp_playwright_browser_resize",
		"mcp_playwright_browser_snapshot",
		"mcp_playwright_browser_close",
	}
	if len(pw.globs) != len(want) {
		t.Fatalf("mcp_playwright globs = %v, want %v", pw.globs, want)
	}
	for i, name := range want {
		if pw.globs[i] != name {
			t.Errorf("mcp_playwright globs[%d] = %q, want %q", i, pw.globs[i], name)
		}
	}
	for _, forbidden := range []string{
		"mcp_playwright_browser_click",
		"mcp_playwright_browser_type",
		"mcp_playwright_browser_evaluate",
		"mcp_playwright_browser_run_code_unsafe",
		"mcp_playwright_browser_file_upload",
	} {
		if slices.Contains(pw.globs, forbidden) {
			t.Errorf("mcp_playwright grants the mutating tool %q", forbidden)
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
		if len(e.globs) != 1 || e.globs[0] != name || !e.notebook || e.build != nil {
			t.Errorf("registry entry %q = %+v, want the exact notebook-gated glob", name, e)
		}
		if matched, err := path.Match("mcp_slivingdoc*", e.globs[0]); err != nil || !matched {
			t.Errorf("registry glob %q is not covered by the shared mcp_slivingdoc* glob", e.globs[0])
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
	if len(e.globs) != 0 || e.notebook || e.build == nil {
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
