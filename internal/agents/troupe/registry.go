package troupe

import (
	"github.com/baalimago/clai/pkg/text/models"
)

// toolEntry is one closed registry entry: the canonical name a role note
// selects and how the stage materialises it on a spawned agent. A glob entry
// names a clai built-in or MCP tool by exact name; a build entry constructs
// the dynamic tool (spawn_role) over the swarm seam. Exactly one of glob or
// build is set.
type toolEntry struct {
	name     string
	glob     string
	notebook bool // the tool lives behind the slivingdoc MCP server
	build    func(Swarm) models.LLMTool
}

// toolRegistry is the single source of truth for the name→tool mapping. A
// role note can select from this table, never define: new tools are
// human-gated, and adding one is a change to this table and nothing else.
// The file tools are matched by exact name, so a tool clai adds to its own
// registry later matches no glob here and stays unreachable; the notebook
// tools are enumerated likewise.
var toolRegistry = []toolEntry{
	// filesystem read/write
	{name: "cat", glob: "cat"},
	{name: "rows_between", glob: "rows_between"},
	{name: "ls", glob: "ls"},
	{name: "rg", glob: "rg"},
	{name: "write_file", glob: "write_file"},
	{name: "apply_patch", glob: "apply_patch"},
	{name: "mkdir", glob: "mkdir"},
	// slivingdoc — the shared notebook, live only when the callsign is
	// configured on the spawner.
	{name: "mcp_slivingdoc_notes_pull", glob: "mcp_slivingdoc_notes_pull", notebook: true},
	{name: "mcp_slivingdoc_notes_commit", glob: "mcp_slivingdoc_notes_commit", notebook: true},
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
