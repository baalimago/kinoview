package troupe

import "context"

// Swarm spawns sub-agents by role note and gathers their final messages.
// The Spawner is the production swarm — every spawned agent runs through the
// phase-4 runner under the phase-5 termination authority, and each spawn
// returns the sub-agent's final message for the spawning agent to gather.
// The director's spawn_role tool spawns through this seam, so a generation
// test injects a fake swarm and runs the machinery without an LLM: one
// commission in, the sub-agent's final message out.
type Swarm interface {
	Spawn(ctx context.Context, roleID, task string) (string, error)
}

// compile-time proof: the spawner is the production swarm.
var _ Swarm = (*Spawner)(nil)
