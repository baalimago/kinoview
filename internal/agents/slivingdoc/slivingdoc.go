// Package slivingdoc builds the slivingdoc MCP callsign and the shared tool
// globs every agent uses to read and write the shared notebook, plus the
// shared worktree helper.
//
// The callsign mirrors the sakfraga harvest_agent pattern with kinoview
// adaptations: the slivingdoc release binary, the SeaweedFS endpoint and
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

// Config is everything the slivingdoc CLI needs from kinoview: the release
// binary to run, the S3 target of the notebook and the roots it materialises
// into. One value builds the MCP callsign, the startup seed, the troupe's
// pull and the handler-side writers, so their paths cannot drift apart.
type Config struct {
	Command       string // slivingdoc release binary: -slivingdocCommand, else slivingdoc on PATH
	Bucket        string // S3 bucket backing the notebook
	Region        string // AWS region label
	Endpoint      string // SeaweedFS S3 endpoint (empty = real AWS, no --path-style)
	WorkspaceRoot string // single shared worktree every agent materialises the notebook into
	PrivateRoot   string // slivingdoc's own private state root, kept apart so concurrent users cannot clash on it
	EnvFile       string // credentials env file every CLI child sources
}

// args returns the argv for one subcommand followed by the two roots every
// invocation shares.
func (c Config) args(subcommand string, rest ...string) []string {
	args := []string{subcommand, "--workspace-root", c.WorkspaceRoot, "--private-root", c.PrivateRoot}
	return append(args, rest...)
}

// Server builds the slivingdoc MCP server from cfg: the release binary as the
// command, the S3 target as flags and the credentials env file the child
// sources.
func Server(cfg Config) models.McpServer {
	args := cfg.args("serve", "--bucket", cfg.Bucket, "--region", cfg.Region)
	if cfg.Endpoint != "" {
		args = append(args, "--endpoint", cfg.Endpoint, "--path-style")
	}
	return models.McpServer{
		Name:           Callsign,
		Command:        cfg.Command,
		Args:           args,
		EnvFile:        cfg.EnvFile,
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
// created. The CLI's own roots default to the process working directory and
// the host cache — neither is the shared worktree — so cfg names both. A
// missing binary or an unreachable bucket surfaces as an error for the caller
// to degrade with.
func Seed(cfg Config) error {
	if err := os.MkdirAll(cfg.WorkspaceRoot, 0o755); err != nil {
		return fmt.Errorf("slivingdoc: seed worktree: %w", err)
	}
	if err := Pull(cfg); err != nil {
		return err
	}
	created, err := seedBulletin(cfg.WorkspaceRoot)
	if err != nil {
		return fmt.Errorf("slivingdoc: seed bulletin: %w", err)
	}
	if !created {
		return nil
	}
	env, err := loadEnvFile(cfg.EnvFile)
	if err != nil {
		return fmt.Errorf("slivingdoc: seed env: %w", err)
	}
	if err := runCLI(cfg.Command, env, cfg.args("commit", cfg.WorkspaceRoot, "-m", seedMessage)...); err != nil {
		return fmt.Errorf("slivingdoc: seed commit: %w", err)
	}
	return nil
}

// Pull materialises the shared notebook into the worktree: the same pull
// every agent's mcp_slivingdoc_notes_pull runs, without authoring anything —
// no bulletin seeding, no commit. The troupe's Warm uses it so the stage
// starts from the repertoire the last generation left, never from a seed.
func Pull(cfg Config) error {
	env, err := loadEnvFile(cfg.EnvFile)
	if err != nil {
		return fmt.Errorf("slivingdoc: pull env: %w", err)
	}
	if err := runCLI(cfg.Command, env, cfg.args("pull", cfg.WorkspaceRoot)...); err != nil {
		return fmt.Errorf("slivingdoc: pull: %w", err)
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

// WriteEnvFile writes the credentials env file the slivingdoc CLI sources for
// its S3 calls: the standard AWS SDK variables plus the notebook bucket and
// the path-style flag. kinoview writes it once at setup from the operator's
// credentials and the -slivingdocEndpoint/-slivingdocBucket/-slivingdocRegion
// flags; the MCP server (EnvFile) and every Seed/Pull/Commit read it back
// through loadEnvFile.
func WriteEnvFile(path, endpoint, bucket, region, accessKey, secretKey string) error {
	content := strings.Join([]string{
		"# kinoview slivingdoc notebook S3 credentials",
		"AWS_ACCESS_KEY_ID=" + accessKey,
		"AWS_SECRET_ACCESS_KEY=" + secretKey,
		"AWS_REGION=" + region,
		"AWS_ENDPOINT_URL_S3=" + endpoint,
		"SLIVINGDOC_BUCKET=" + bucket,
		"SLIVINGDOC_PATH_STYLE=true",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("slivingdoc: write credentials env: %w", err)
	}
	return nil
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
