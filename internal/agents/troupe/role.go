package troupe

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Role is a role note: a note, not Go code. The director authors a role by
// writing a file in roles/; the stage reads and executes it by name. The
// note is a flat envelope — id/prompt/tools/budget — not a grammar asset:
// roles are executed by the spawn runner, never resolved into plays. The
// envelope is closed; an unknown field is an error.
type Role struct {
	ID     string   `json:"id"`
	Prompt string   `json:"prompt"`
	Tools  []string `json:"tools"`
	Budget int      `json:"budget"`
}

const (
	// defaultRoleBudget is the per-spawn tool-call budget a role gets when
	// its note carries none.
	defaultRoleBudget = 8
	// maxRoleBudget clamps a role note's budget. Each spawn is a bounded
	// agent; a role can never declare itself unbounded.
	maxRoleBudget = 64
	// maxRolePromptLen caps the prompt a role note may carry.
	maxRolePromptLen = 8000
)

// ParseRole validates one role note: roles/<id>.json with the flat
// id/prompt/tools/budget envelope. The filename is the identity authority —
// the envelope id must match the stem — and the role id shares the asset id
// charset because it becomes a filename. Every tools entry must be in the
// closed registry: a role can select, never define, and a note naming
// anything outside the registry is refused with an exact error. The budget
// is clamped into [defaultRoleBudget, maxRoleBudget]: absent or negative
// becomes the default, oversized is capped.
func ParseRole(filename string, data []byte) (Role, error) {
	wantID, err := roleFilenameID(filename)
	if err != nil {
		return Role{}, fmt.Errorf("troupe: %w", err)
	}
	var r Role
	if err := decodeStrict(data, &r, filename); err != nil {
		return Role{}, fmt.Errorf("troupe: %w", err)
	}

	e := &errs{}
	if r.ID != wantID {
		e.addf("id %q does not match the filename id %q", r.ID, wantID)
	}
	if r.Prompt == "" {
		e.addf("prompt: must not be empty")
	}
	if n := utf8.RuneCountInString(r.Prompt); n > maxRolePromptLen {
		e.addf("prompt: %d runes exceeds the %d cap", n, maxRolePromptLen)
	}
	if len(r.Tools) == 0 {
		e.addf("tools: must select at least one tool from the closed registry")
	}
	seen := make(map[string]bool, len(r.Tools))
	for _, name := range r.Tools {
		if !registryHas(name) {
			e.addf("tools: %q is not in the closed registry", name)
			continue
		}
		if seen[name] {
			e.addf("tools: %q selected twice", name)
		}
		seen[name] = true
	}
	switch {
	case r.Budget <= 0:
		r.Budget = defaultRoleBudget
	case r.Budget > maxRoleBudget:
		r.Budget = maxRoleBudget
	}
	if err := e.err(); err != nil {
		return Role{}, fmt.Errorf("troupe: %s: %w", filename, err)
	}
	return r, nil
}

// roleFilenameID derives the role id from a roles/<id>.json filename.
func roleFilenameID(filename string) (string, error) {
	dir, base := filepath.Split(filepath.Clean(filename))
	if strings.TrimSuffix(dir, string(filepath.Separator)) != "roles" {
		return "", fmt.Errorf("filename %s: role notes live in roles/", filename)
	}
	stem, found := strings.CutSuffix(base, ".json")
	if !found {
		return "", fmt.Errorf("filename %s: must end in .json", filename)
	}
	if !idRe.MatchString(stem) {
		return "", fmt.Errorf("filename %s: role id %q must match %s", filename, stem, idRe)
	}
	return stem, nil
}

// RoleSource reads a role note by id. The stage hands the spawner the
// materialised notebook; the spawn_role tool reads through the same seam, so
// roles are notes everywhere — there is no privileged in-Go role path. A
// source returns ParseRole-validated roles.
type RoleSource interface {
	Role(id string) (Role, error)
}

// snapshotRoleSource reads role notes from a resolver snapshot.
type snapshotRoleSource Snapshot

// NewRoleSource returns a RoleSource reading roles/<id>.json notes from a
// resolver snapshot — the shape the phase-4 tests hand the spawner.
func NewRoleSource(snap Snapshot) RoleSource {
	return snapshotRoleSource(snap)
}

func (s snapshotRoleSource) Role(id string) (Role, error) {
	data, ok := s["roles/"+id+".json"]
	if !ok {
		return Role{}, fmt.Errorf("no such role %s in roles/", id)
	}
	return ParseRole("roles/"+id+".json", data)
}

// worktreeRoleSource reads role notes from the materialised notebook
// worktree at call time — the live copy the director authors into, so a role
// written during the generation is visible to the swarm immediately. The
// serve wiring hands the spawner this source; a snapshot would freeze the
// roles at setup and refuse every role the director authors mid-generation.
type worktreeRoleSource struct {
	worktree string
}

// NewWorktreeRoleSource returns a RoleSource reading live roles/<id>.json
// notes from the materialised worktree — the production shape the serve
// wiring hands the spawner (phase 9).
func NewWorktreeRoleSource(worktree string) RoleSource {
	return worktreeRoleSource{worktree: worktree}
}

func (s worktreeRoleSource) Role(id string) (Role, error) {
	data, err := os.ReadFile(filepath.Join(s.worktree, "roles", id+".json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Role{}, fmt.Errorf("no such role %s in roles/", id)
		}
		return Role{}, fmt.Errorf("troupe: role %s: %w", id, err)
	}
	return ParseRole("roles/"+id+".json", data)
}
