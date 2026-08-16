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
// slivingdoc binary, so a note is durable, merged like every other note and
// readable by the next generation's roles.
type Notebook struct {
	command     string // slivingdoc binary
	workspace   string // shared worktree every agent materialises into
	privateRoot string // slivingdoc's private state root
	envFile     string // credentials env file (S3 access keys)

	// mu serializes append+commit so concurrent posts cannot interleave
	// partial lines or racing commits.
	mu sync.Mutex
}

// NewNotebook builds the handler-side seam over the same worktree the
// callsign materialises. command is the slivingdoc binary path; envFile
// carries the S3 credentials the commit child needs.
func NewNotebook(command, workspace, privateRoot, envFile string) *Notebook {
	return &Notebook{
		command:     command,
		workspace:   workspace,
		privateRoot: privateRoot,
		envFile:     envFile,
	}
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

	if err := os.MkdirAll(n.workspace, 0o755); err != nil {
		return fmt.Errorf("slivingdoc: append %s: worktree: %w", name, err)
	}
	f, err := os.OpenFile(filepath.Join(n.workspace, name), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
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

	env, err := loadEnvFile(n.envFile)
	if err != nil {
		return fmt.Errorf("slivingdoc: append %s: env: %w", name, err)
	}
	commitArgs := []string{
		"commit",
		"--workspace-root", n.workspace,
		"--private-root", n.privateRoot,
		n.workspace,
		"-m", "append " + name,
	}
	if err := runCLI(n.command, env, commitArgs...); err != nil {
		return fmt.Errorf("slivingdoc: append %s: commit: %w", name, err)
	}
	return nil
}
