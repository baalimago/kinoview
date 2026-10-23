package slivingdoc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Notebook is the non-MCP seam into the shared notebook for server-side
// writers (the feedback handler). Agents use the mcp_slivingdoc_* tools; the
// handler appends directly to the shared worktree and commits through the
// slivingdoc CLI, so a note is durable, merged like every other note and
// readable by the next generation's roles.
type Notebook struct {
	cfg Config // command, roots and credentials env file shared with the agents

	// mu serializes append+commit so concurrent posts cannot interleave
	// partial lines or racing commits.
	mu sync.Mutex
}

// NewNotebook builds the handler-side seam over the same worktree the
// callsign materialises: cfg carries the binary, the roots and the S3
// credentials the commit child needs.
func NewNotebook(cfg Config) *Notebook {
	return &Notebook{cfg: cfg}
}

// AppendJSONL encodes v as one JSON line, appends it to name in the shared
// worktree and commits through the slivingdoc binary. Append and commit are
// one unit: a commit that fails returns an error so the caller surfaces the
// loss instead of silently dropping the note. The append never runs a pull
// first — the worktree is the shared copy and a pull would clobber an
// uncommitted line.
func (n *Notebook) AppendJSONL(name string, v any) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	line, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("slivingdoc: append %s: encode: %w", name, err)
	}
	line = append(line, '\n')

	if err := os.MkdirAll(n.cfg.WorkspaceRoot, 0o755); err != nil {
		return fmt.Errorf("slivingdoc: append %s: worktree: %w", name, err)
	}
	f, err := os.OpenFile(filepath.Join(n.cfg.WorkspaceRoot, name), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("slivingdoc: append %s: open: %w", name, err)
	}
	if _, err := f.Write(line); err != nil {
		f.Close()
		return fmt.Errorf("slivingdoc: append %s: write: %w", name, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("slivingdoc: append %s: close: %w", name, err)
	}

	if err := n.Commit("append " + name); err != nil {
		return fmt.Errorf("slivingdoc: append %s: commit: %w", name, err)
	}
	return nil
}

// Commit commits the shared worktree through the slivingdoc CLI: the
// write+commit unit's second half, reusable by any server-side writer (the
// troupe's feedback and criticism notes commit through this seam). The caller
// holds the serialization contract — AppendJSONL wraps it in the Notebook's
// lock; the troupe writers serialize whole write+commit units themselves.
func (n *Notebook) Commit(message string) error {
	env, err := loadEnvFile(n.cfg.EnvFile)
	if err != nil {
		return fmt.Errorf("slivingdoc: commit: env: %w", err)
	}
	args := n.cfg.args("commit", n.cfg.WorkspaceRoot, "-m", message)
	if err := runCLI(n.cfg.Command, env, args...); err != nil {
		return fmt.Errorf("slivingdoc: commit: %w", err)
	}
	return nil
}
