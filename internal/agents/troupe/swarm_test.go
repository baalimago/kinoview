package troupe

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// fakeSwarm is an in-memory Swarm for generation tests: it records the
// commissions it received and returns scripted final messages, so the
// director's spawn_role tool runs without an LLM.
type fakeSwarm struct {
	mu     sync.Mutex
	calls  []swarmCall
	script map[string]string // roleID → the sub-agent's final message
	err    error             // optional: every spawn fails with this
}

// swarmCall is one recorded commission.
type swarmCall struct {
	role string
	task string
}

func (f *fakeSwarm) Spawn(_ context.Context, roleID, task string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, swarmCall{role: roleID, task: task})
	if f.err != nil {
		return "", f.err
	}
	return f.script[roleID], nil
}

// calls returns a copy of the recorded commissions.
func (f *fakeSwarm) callsSnapshot() []swarmCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]swarmCall(nil), f.calls...)
}

// TestSpawnerImplementsSwarm pins the production swarm contract: the
// Spawner satisfies Swarm, so the director's spawn_role tool over the swarm
// seam runs every spawn through the phase-4 runner under the phase-5 budget.
func TestSpawnerImplementsSwarm(t *testing.T) {
	s := newTestSpawner(t, fakeRoles{})
	var _ Swarm = s
}

// TestSpawnRoleTool_OverFakeSwarm pins the director's seam: the tool spawns
// through the Swarm interface, so a generation test injects a fake swarm and
// the tool gathers the sub-agent's final message.
func TestSpawnRoleTool_OverFakeSwarm(t *testing.T) {
	swarm := &fakeSwarm{script: map[string]string{"clown": "the gag landed"}}
	tool := newSpawnRoleTool(swarm)

	out, err := tool.Call(map[string]any{"role": "clown", "task": "invent a pounce gag"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if out != "the gag landed" {
		t.Errorf("out = %q, want the swarm's final message", out)
	}
	calls := swarm.callsSnapshot()
	if len(calls) != 1 || calls[0].role != "clown" || calls[0].task != "invent a pounce gag" {
		t.Errorf("commissions = %+v, want the one clown commission", calls)
	}
	if spec := tool.Specification(); spec.Name != "spawn_role" {
		t.Errorf("Specification = %q, want spawn_role", spec.Name)
	}

	t.Run("spawn failure returns to the spawning agent", func(t *testing.T) {
		swarm := &fakeSwarm{err: errors.New("the swarm produced nothing")}
		tool := newSpawnRoleTool(swarm)
		if _, err := tool.Call(map[string]any{"role": "clown", "task": "x"}); err == nil || !strings.Contains(err.Error(), "produced nothing") {
			t.Error("Call must surface the swarm's error")
		}
	})
}
