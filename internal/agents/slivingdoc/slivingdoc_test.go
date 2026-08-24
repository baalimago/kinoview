package slivingdoc

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/baalimago/clai/pkg/text/models"
)

func TestServer_Args(t *testing.T) {
	t.Run("endpoint sets path-style", func(t *testing.T) {
		got := Server(NpxRunner("npx"), "slivingdoc", "us-east-1", "http://127.0.0.1:8333", "/ws", "/priv")
		want := []string{
			"-y", "slivingdoc", "serve",
			"--bucket", "slivingdoc",
			"--region", "us-east-1",
			"--endpoint", "http://127.0.0.1:8333", "--path-style",
			"--workspace-root", "/ws",
			"--private-root", "/priv",
		}
		if !slices.Equal(got.Args, want) {
			t.Errorf("Args = %v, want %v", got.Args, want)
		}
		if got.Name != Callsign {
			t.Errorf("Name = %q, want %q", got.Name, Callsign)
		}
		if got.Command != "npx" {
			t.Errorf("Command = %q, want %q", got.Command, "npx")
		}
	})

	t.Run("empty endpoint omits path-style", func(t *testing.T) {
		got := Server(NpxRunner("npx"), "b", "r", "", "/ws", "/priv")
		want := []string{
			"-y", "slivingdoc", "serve",
			"--bucket", "b",
			"--region", "r",
			"--workspace-root", "/ws",
			"--private-root", "/priv",
		}
		if !slices.Equal(got.Args, want) {
			t.Errorf("Args = %v, want %v", got.Args, want)
		}
	})
}

func TestServer_Timeout(t *testing.T) {
	got := Server(NpxRunner("npx"), "b", "r", "", "/ws", "/priv")
	if got.TimeoutSeconds != 300 {
		t.Errorf("TimeoutSeconds = %d, want 300", got.TimeoutSeconds)
	}
}

// A prebuilt binary runner (the production arm path) runs the CLI directly:
// the command is the binary and the args start with the subcommand, not with
// the npx -y slivingdoc prefix.
func TestServer_PrebuiltRunner(t *testing.T) {
	got := Server(BinaryRunner("/opt/slivingdoc"), "b", "r", "http://127.0.0.1:8333", "/ws", "/priv")
	if got.Command != "/opt/slivingdoc" {
		t.Errorf("Command = %q, want %q", got.Command, "/opt/slivingdoc")
	}
	want := []string{
		"serve",
		"--bucket", "b",
		"--region", "r",
		"--endpoint", "http://127.0.0.1:8333", "--path-style",
		"--workspace-root", "/ws",
		"--private-root", "/priv",
	}
	if !slices.Equal(got.Args, want) {
		t.Errorf("Args = %v, want %v", got.Args, want)
	}
}

func TestToolGlobs(t *testing.T) {
	want := []string{
		"mcp_slivingdoc*",
		"cat",
		"rows_between",
		"ls",
		"rg",
		"write_file",
		"apply_patch",
		"mkdir",
	}
	got := ToolGlobs()
	if !slices.Equal(got, want) {
		t.Errorf("ToolGlobs = %v, want %v", got, want)
	}
}

// The NOTES partial is byte-identical across every agent with only the
// workspace path substituted, and it names the commit tool but no file name.
func TestNotesPartial_SubstitutesWorkspace(t *testing.T) {
	got := NotesPartial("/cache/slivingdoc")
	want := "NOTES\n" +
		"Pull the shared notebook into /cache/slivingdoc before you start.\n" +
		"Read what others wrote with the file tools.\n" +
		"Write what you learn for the next agent.\n" +
		"Commit with mcp_slivingdoc_notes_commit with path /cache/slivingdoc when done.\n"
	if got != want {
		t.Errorf("NotesPartial = %q, want %q", got, want)
	}
	if strings.Contains(got, "bulletin") {
		t.Errorf("NotesPartial hardcodes a file name: %q", got)
	}
}

// WorkspaceRoot reads the shared worktree path back from the callsign args,
// so a prompt names the same path the MCP child materialises into.
func TestWorkspaceRoot_FromCallsignArgs(t *testing.T) {
	server := Server(NpxRunner("npx"), "b", "r", "http://127.0.0.1:8333", "/cache/slivingdoc", "/priv")
	if got := WorkspaceRoot(server); got != "/cache/slivingdoc" {
		t.Errorf("WorkspaceRoot = %q, want %q", got, "/cache/slivingdoc")
	}

	t.Run("zero server yields empty", func(t *testing.T) {
		if got := WorkspaceRoot(models.McpServer{}); got != "" {
			t.Errorf("WorkspaceRoot(zero) = %q, want empty", got)
		}
	})
}

// fakeCLI records the invocations routed through the runCLI seam and can
// simulate a pull that materialises the notebook state.
type fakeCLI struct {
	calls     [][]string
	envs      [][]string
	pullFiles map[string]string // files a pull writes into the worktree
	failOn    string            // subcommand whose execution fails
}

func (f *fakeCLI) run(_ string, env []string, args ...string) error {
	f.calls = append(f.calls, append([]string(nil), args...))
	f.envs = append(f.envs, append([]string(nil), env...))
	// The subcommand is index 0 for a prebuilt binary and index 2 for npx
	// (-y slivingdoc <subcommand>).
	sub := ""
	if len(args) > 0 && (args[0] == "pull" || args[0] == "commit") {
		sub = args[0]
	} else if len(args) > 2 {
		sub = args[2]
	}
	if sub == f.failOn {
		return os.ErrNotExist
	}
	if sub == "pull" {
		root := args[len(args)-1] // the positional worktree path
		for name, content := range f.pullFiles {
			if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

func withFakeCLI(t *testing.T, fake *fakeCLI) {
	t.Helper()
	old := runCLI
	runCLI = fake.run
	t.Cleanup(func() { runCLI = old })
}

func TestSeed_PullsAndCommitsBulletin(t *testing.T) {
	fake := &fakeCLI{}
	withFakeCLI(t, fake)

	workspace := filepath.Join(t.TempDir(), "slivingdoc")
	private := filepath.Join(t.TempDir(), "slivingdoc-private")
	envFile := filepath.Join(t.TempDir(), "credentials.env")
	if err := os.WriteFile(envFile, []byte("AWS_ACCESS_KEY_ID=k\nAWS_SECRET_ACCESS_KEY=s\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Seed(NpxRunner("npx"), workspace, private, envFile); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	// pull, then commit — the bulletin was created.
	wantPull := []string{"-y", "slivingdoc", "pull", "--workspace-root", workspace, "--private-root", private, workspace}
	wantCommit := []string{"-y", "slivingdoc", "commit", "--workspace-root", workspace, "--private-root", private, workspace, "-m", seedMessage}
	if len(fake.calls) != 2 {
		t.Fatalf("expected pull + commit, got %v", fake.calls)
	}
	if !slices.Equal(fake.calls[0], wantPull) {
		t.Errorf("pull call = %v, want %v", fake.calls[0], wantPull)
	}
	if !slices.Equal(fake.calls[1], wantCommit) {
		t.Errorf("commit call = %v, want %v", fake.calls[1], wantCommit)
	}

	b, err := os.ReadFile(filepath.Join(workspace, bulletinName))
	if err != nil {
		t.Fatalf("bulletin.md: %v", err)
	}
	if !strings.Contains(string(b), "Bulletin") {
		t.Errorf("bulletin.md = %q, want a bulletin header", b)
	}
}

func TestSeed_ExistingBulletin_NoCommit(t *testing.T) {
	fake := &fakeCLI{pullFiles: map[string]string{bulletinName: "# Bulletin\n\nAlready here.\n"}}
	withFakeCLI(t, fake)

	workspace := filepath.Join(t.TempDir(), "slivingdoc")
	envFile := filepath.Join(t.TempDir(), "credentials.env")
	if err := os.WriteFile(envFile, []byte("AWS_ACCESS_KEY_ID=k\nAWS_SECRET_ACCESS_KEY=s\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Seed(NpxRunner("npx"), workspace, filepath.Join(t.TempDir(), "priv"), envFile); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	if len(fake.calls) != 1 || fake.calls[0][2] != "pull" {
		t.Fatalf("expected a single pull, got %v", fake.calls)
	}
	b, err := os.ReadFile(filepath.Join(workspace, bulletinName))
	if err != nil {
		t.Fatalf("bulletin.md: %v", err)
	}
	if !strings.Contains(string(b), "Already here") {
		t.Errorf("bulletin.md overwritten: %q", b)
	}
}

// A failed seed step propagates with the step named in the error, so the
// caller degrades with a precise warning.
func TestSeed_StepFailure_Propagates(t *testing.T) {
	cases := []struct {
		name   string
		failOn string
		step   string
	}{
		{"pull", "pull", "pull"},
		{"commit", "commit", "commit"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeCLI{failOn: tt.failOn}
			withFakeCLI(t, fake)

			envFile := filepath.Join(t.TempDir(), "credentials.env")
			if err := os.WriteFile(envFile, []byte("AWS_ACCESS_KEY_ID=k\nAWS_SECRET_ACCESS_KEY=s\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			err := Seed(NpxRunner("npx"), filepath.Join(t.TempDir(), "ws"), filepath.Join(t.TempDir(), "priv"), envFile)
			if err == nil {
				t.Fatal("expected error from failed " + tt.step)
			}
			if !strings.Contains(err.Error(), tt.step) {
				t.Errorf("error does not name the %s step: %v", tt.step, err)
			}
		})
	}
}

func TestSeed_MissingEnvFile_Fails(t *testing.T) {
	withFakeCLI(t, &fakeCLI{})

	err := Seed(NpxRunner("npx"), filepath.Join(t.TempDir(), "ws"), filepath.Join(t.TempDir(), "priv"), filepath.Join(t.TempDir(), "missing.env"))
	if err == nil {
		t.Fatal("expected error from missing env file")
	}
}

func TestSeed_EnvReachesChild(t *testing.T) {
	fake := &fakeCLI{}
	withFakeCLI(t, fake)

	envFile := filepath.Join(t.TempDir(), "credentials.env")
	if err := os.WriteFile(envFile, []byte("AWS_ACCESS_KEY_ID=KEY\nAWS_SECRET_ACCESS_KEY=SECRET\nSLIVINGDOC_PATH_STYLE=true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Seed(NpxRunner("npx"), filepath.Join(t.TempDir(), "ws"), filepath.Join(t.TempDir(), "priv"), envFile); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if len(fake.envs) == 0 {
		t.Fatal("no child invocations recorded")
	}
	for _, want := range []string{"AWS_ACCESS_KEY_ID=KEY", "AWS_SECRET_ACCESS_KEY=SECRET", "SLIVINGDOC_PATH_STYLE=true"} {
		if !slices.Contains(fake.envs[0], want) {
			t.Errorf("child env missing %q: %v", want, fake.envs[0])
		}
	}
}

// TestPull_MaterialisesWithoutAuthoring pins the troupe's Warm seam: Pull
// runs the pull only — no bulletin seeding, no commit — and materialises
// the worktree state the pull writes.
func TestPull_MaterialisesWithoutAuthoring(t *testing.T) {
	fake := &fakeCLI{pullFiles: map[string]string{"note.json": `{"kind":"model"}`}}
	withFakeCLI(t, fake)

	workspace := filepath.Join(t.TempDir(), "slivingdoc")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	private := filepath.Join(t.TempDir(), "slivingdoc-private")
	envFile := filepath.Join(t.TempDir(), "credentials.env")
	if err := os.WriteFile(envFile, []byte("AWS_ACCESS_KEY_ID=k\nAWS_SECRET_ACCESS_KEY=s\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Pull(NpxRunner("npx"), workspace, private, envFile); err != nil {
		t.Fatalf("Pull: %v", err)
	}

	// Exactly one pull: no bulletin seeding, no commit — Pull never authors.
	want := []string{"-y", "slivingdoc", "pull", "--workspace-root", workspace, "--private-root", private, workspace}
	if len(fake.calls) != 1 || !slices.Equal(fake.calls[0], want) {
		t.Fatalf("calls = %v, want exactly the pull %v", fake.calls, want)
	}
	if _, err := os.Stat(filepath.Join(workspace, bulletinName)); !os.IsNotExist(err) {
		t.Errorf("Pull must not seed the bulletin (stat err = %v)", err)
	}
	b, err := os.ReadFile(filepath.Join(workspace, "note.json"))
	if err != nil {
		t.Fatalf("pulled file: %v", err)
	}
	if !strings.Contains(string(b), "model") {
		t.Errorf("pulled file = %q, want the materialised note", b)
	}
}

// TestPull_FailurePropagates pins that a failed pull returns its error for
// the caller (the facade's Warm) to degrade with.
func TestPull_FailurePropagates(t *testing.T) {
	fake := &fakeCLI{failOn: "pull"}
	withFakeCLI(t, fake)

	envFile := filepath.Join(t.TempDir(), "credentials.env")
	if err := os.WriteFile(envFile, []byte("AWS_ACCESS_KEY_ID=k\nAWS_SECRET_ACCESS_KEY=s\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := Pull(NpxRunner("npx"), filepath.Join(t.TempDir(), "ws"), filepath.Join(t.TempDir(), "priv"), envFile)
	if err == nil || !strings.Contains(err.Error(), "pull") {
		t.Fatalf("err = %v, want a pull error", err)
	}
}

// Server builds a complete clai McpServer: callsign, command and args are
// all set, so agent.WithMcpServers accepts it as-is.
func TestServer_IsUsableMcpServer(t *testing.T) {
	got := Server(NpxRunner("npx"), "b", "r", "http://127.0.0.1:8333", "/ws", "/priv")
	var _ models.McpServer = got // the exact clai type the agents register
	if got.Name == "" || got.Command == "" || len(got.Args) == 0 {
		t.Errorf("incomplete McpServer: %+v", got)
	}
}
