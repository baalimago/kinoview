package troupe

import (
	"github.com/baalimago/clai/pkg/text/models"
)

// toolEntry is one closed registry entry: the canonical name a role note
// selects and how the stage materialises it on a spawned agent. A glob entry
// names clai built-ins or MCP tools by their exact names; a build entry
// constructs the dynamic tool (spawn_role) over the swarm seam. Exactly one
// of globs or build is set.
type toolEntry struct {
	name     string
	globs    []string
	notebook bool // the tool lives behind the slivingdoc MCP server
	browser  bool // the tool lives behind the Playwright MCP server
	build    func(Swarm) models.LLMTool
}

// toolRegistry is the single source of truth for the name→tool mapping. A
// role note can select from this table, never define: new tools are
// human-gated, and adding one is a change to this table and nothing else.
// Every tool is matched by exact name, so a tool clai or an MCP server adds
// later matches no glob here and stays unreachable. The perception entry
// enumerates the read-only inspection subset on purpose: the troupe needs to
// look at the show, not to click it or to run code in it.
var toolRegistry = []toolEntry{
	// filesystem read/write
	{name: "cat", globs: []string{"cat"}},
	{name: "rows_between", globs: []string{"rows_between"}},
	{name: "ls", globs: []string{"ls"}},
	{name: "rg", globs: []string{"rg"}},
	{name: "write_file", globs: []string{"write_file"}},
	{name: "apply_patch", globs: []string{"apply_patch"}},
	{name: "mkdir", globs: []string{"mkdir"}},
	// perception — the show is a browser render, so the troupe can look at
	// what it authored. view_image is a clai native tool. mcp_playwright is
	// the read-only inspection subset of the Playwright MCP server: look,
	// wait, screenshot, outline, console, resize, close. Page mutation
	// (click, type, run code) is deliberately absent.
	{name: "view_image", globs: []string{"view_image"}},
	{name: "mcp_playwright", globs: []string{
		"mcp_playwright_browser_navigate",
		"mcp_playwright_browser_wait_for",
		"mcp_playwright_browser_take_screenshot",
		"mcp_playwright_browser_console_messages",
		"mcp_playwright_browser_resize",
		"mcp_playwright_browser_snapshot",
		"mcp_playwright_browser_close",
	}, browser: true},
	// slivingdoc — the shared notebook, live only when the callsign is
	// configured on the spawner.
	{name: "mcp_slivingdoc_notes_pull", globs: []string{"mcp_slivingdoc_notes_pull"}, notebook: true},
	{name: "mcp_slivingdoc_notes_commit", globs: []string{"mcp_slivingdoc_notes_commit"}, notebook: true},
	// spawn_role — recursive: a role that selects it may spawn sub-agents
	// through the same runner. The build closes over the swarm seam, so the
	// director's spawn_role tool binds to its own swarm (phase 7).
	{name: "spawn_role", build: newSpawnRoleTool},
}

// registryHas reports whether name is a canonical registry tool name.
func registryHas(name string) bool {
	_, ok := lookupTool(name)
	return ok
}

// lookupTool returns the registry entry for a canonical tool name.
func lookupTool(name string) (toolEntry, bool) {
	for _, e := range toolRegistry {
		if e.name == name {
			return e, true
		}
	}
	return toolEntry{}, false
}
