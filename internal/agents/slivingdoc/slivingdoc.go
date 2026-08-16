// Package slivingdoc builds the slivingdoc MCP callsign and the shared tool
// globs every agent uses to read and write the shared notebook, plus the
// shared worktree helper.
//
// The callsign mirrors the sakfraga harvest_agent pattern with kinoview
// adaptations: the native slivingdoc binary, the SeaweedFS endpoint and
// path-style addressing. Agents address the notebook through the
// mcp_slivingdoc_notes_pull and mcp_slivingdoc_notes_commit tools (the
// mcp_slivingdoc* glob) and read and edit the materialised files with the
// clai file tools.
package slivingdoc

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/baalimago/clai/pkg/text/models"
)

const (
	// Callsign is the MCP server name agents address the notebook through.
	Callsign = "slivingdoc"

	// timeoutSeconds bounds a single MCP tool call: a pull or commit over a
	// cold S3 gateway must never hang an agent loop forever.
	timeoutSeconds = 300

	// bulletinName is the conventional cross-agent surface seeded into the
	// worktree at setup.
	bulletinName = "bulletin.md"

	// seedMessage is the commit message for the seeded bulletin.
	seedMessage = "seed bulletin.md"
)

// Server builds the slivingdoc MCP server. command is the native slivingdoc
// binary path; endpoint is the SeaweedFS S3 endpoint (empty = real AWS, no
// --path-style). workspaceRoot is the single shared worktree every agent
// materialises the notebook into; privateRoot is slivingdoc's own private
// state root, kept separate so concurrent slivingdoc users on the host
// cannot clash on it.
func Server(command, bucket, region, endpoint, workspaceRoot, privateRoot string) models.McpServer {
	args := []string{
		"serve",
		"--bucket", bucket,
		"--region", region,
	}
	if endpoint != "" {
		args = append(args, "--endpoint", endpoint, "--path-style")
	}
	args = append(args,
		"--workspace-root", workspaceRoot,
		"--private-root", privateRoot,
	)
	return models.McpServer{
		Name:           Callsign,
		Command:        command,
		Args:           args,
		TimeoutSeconds: timeoutSeconds,
	}
}

// ToolGlobs returns the shared tool globs: the slivingdoc callsign plus the
// file tools agents use to read and write notes.
//
// The wildcard mcp_slivingdoc* matches both mcp_slivingdoc_notes_pull and
// mcp_slivingdoc_notes_commit. The file tools are clai built-in names, so
// enumeration is enforcement: a tool clai adds to its registry later matches
// no exact glob and stays unreachable.
func ToolGlobs() []string {
	return []string{
		"mcp_slivingdoc*",
		"cat",
		"rows_between",
		"ls",
		"rg",
		"write_file",
		"apply_patch",
		"mkdir",
	}
}

// NotesPartial is the shared notebook contract every agent prompt carries:
// pull the notebook, read the shared notes, write findings, commit. It is
// byte-identical across every agent with only the workspace path substituted,
// so the model is never asked to guess where the notebook lives. The pull and
// commit argument lists stay in the MCP tool descriptions; the prompt states
// intent and method, and no file name is hardcoded — agents discover the
// layout with the file tools.
func NotesPartial(workspace string) string {
	return fmt.Sprintf(`NOTES
Pull the shared notebook into %s before you start.
Read what others wrote with the file tools.
Write what you learn for the next agent.
Commit with mcp_slivingdoc_notes_commit with path %s when done.
`, workspace, workspace)
}

// WorkspaceRoot reads the shared worktree path back from the callsign args.
// It is the same value the MCP child materialises the notebook into, so a
// prompt can name it without guessing. Empty when the args carry no
// --workspace-root (never for servers built by Server).
func WorkspaceRoot(server models.McpServer) string {
	for i, a := range server.Args {
		if a == "--workspace-root" && i+1 < len(server.Args) {
			return server.Args[i+1]
		}
	}
	return ""
}

// Seed materialises the shared notebook into the worktree and ensures a
// bulletin.md exists for cross-agent notices, committing it when it was
// created. privateRoot is slivingdoc's private state root (the same value
// the MCP server uses), passed on the command line because the CLI's
// workspace and private roots default to the process working directory and
// the host cache — neither is the shared worktree. envFile is the
// credentials env file the McpServer's EnvFile references; the same
// variables reach the seed child so it can talk to the S3 bucket. A missing
// binary or an unreachable bucket surfaces as an error for the caller to
// degrade with.
func Seed(command, workspaceRoot, privateRoot, envFile string) error {
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		return fmt.Errorf("slivingdoc: seed worktree: %w", err)
	}
	env, err := loadEnvFile(envFile)
	if err != nil {
		return fmt.Errorf("slivingdoc: seed env: %w", err)
	}
	roots := []string{"--workspace-root", workspaceRoot, "--private-root", privateRoot}
	pullArgs := append(append([]string{"pull"}, roots...), workspaceRoot)
	if err := runCLI(command, env, pullArgs...); err != nil {
		return fmt.Errorf("slivingdoc: seed pull: %w", err)
	}
	created, err := seedBulletin(workspaceRoot)
	if err != nil {
		return fmt.Errorf("slivingdoc: seed bulletin: %w", err)
	}
	if !created {
		return nil
	}
	commitArgs := append(append([]string{"commit"}, roots...), workspaceRoot, "-m", seedMessage)
	if err := runCLI(command, env, commitArgs...); err != nil {
		return fmt.Errorf("slivingdoc: seed commit: %w", err)
	}
	return nil
}

// seedBulletin writes bulletin.md into the worktree when it does not exist
// yet; it reports whether it created it, so the caller knows when a commit
// is needed.
func seedBulletin(workspaceRoot string) (bool, error) {
	path := filepath.Join(workspaceRoot, bulletinName)
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}
	content := "# Bulletin\n\nCross-agent notices for the kinoview agents. Append dated notes.\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// loadEnvFile parses the credentials env file (KEY=VALUE lines, # comments)
// into a child-process environment slice.
func loadEnvFile(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var env []string
	for line := range strings.SplitSeq(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		env = append(env, k+"="+v)
	}
	return env, nil
}

// runCLI executes the slivingdoc binary with the given environment. The
// child inherits the parent environment so host logging and proxy
// configuration reach it; env carries the S3 credentials on top. Tests
// inject a scripted runner through this seam.
var runCLI = func(command string, env []string, args ...string) error {
	cmd := exec.Command(command, args...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %w\n%s", command, args, err, out)
	}
	return nil
}
