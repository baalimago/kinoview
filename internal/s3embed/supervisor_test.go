package s3embed

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSupervisor_Endpoint(t *testing.T) {
	s := New(WithS3Port(9999))
	if got, want := s.Endpoint(), "http://127.0.0.1:9999"; got != want {
		t.Errorf("Endpoint() = %v, want %v", got, want)
	}
}

func TestSupervisor_Defaults(t *testing.T) {
	s := New()
	if s.s3Port != DefaultS3Port {
		t.Errorf("s3Port = %d, want %d", s.s3Port, DefaultS3Port)
	}
	if s.masterPort != DefaultMasterPort {
		t.Errorf("masterPort = %d, want %d", s.masterPort, DefaultMasterPort)
	}
	if s.volumePort != DefaultVolumePort {
		t.Errorf("volumePort = %d, want %d", s.volumePort, DefaultVolumePort)
	}
	if s.filerPort != DefaultFilerPort {
		t.Errorf("filerPort = %d, want %d", s.filerPort, DefaultFilerPort)
	}
	if s.bucket != DefaultBucket {
		t.Errorf("bucket = %q, want %q", s.bucket, DefaultBucket)
	}
	if s.dataDir == "" {
		t.Error("dataDir empty, want the user-config-dir default")
	}
	if s.region != DefaultRegion {
		t.Errorf("region = %q, want %q", s.region, DefaultRegion)
	}
}

func TestSupervisor_EnvPath(t *testing.T) {
	s := New(WithDataDir("/tmp/kinoview-s3"))
	if got, want := s.EnvPath(), filepath.Join("/tmp/kinoview-s3", "credentials.env"); got != want {
		t.Errorf("EnvPath() = %v, want %v", got, want)
	}
}

// TestSupervisor_MissingBinary returns ErrBinaryNotFound without spawning when
// no weed binary resolves: no explicit path, none next to the executable, none
// on PATH.
func TestSupervisor_MissingBinary(t *testing.T) {
	t.Run("nothing to resolve", func(t *testing.T) {
		t.Setenv("PATH", "")
		s := New()
		err := s.Start(context.Background())
		if !errors.Is(err, ErrBinaryNotFound) {
			t.Fatalf("Start() error = %v, want ErrBinaryNotFound", err)
		}
	})

	t.Run("explicit path missing names the path", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "does-not-exist")
		s := New(WithBinary(missing))
		err := s.Start(context.Background())
		if err == nil {
			t.Fatal("Start() = nil, want error")
		}
		if !strings.Contains(err.Error(), missing) {
			t.Errorf("Start() error = %v, want it to name %q", err, missing)
		}
	})
}

// TestCredentials_PersistAcrossRestarts verifies the credentials contract:
// explicit options win, then the persisted env file, then a generated pair.
func TestCredentials_PersistAcrossRestarts(t *testing.T) {
	dir := t.TempDir()

	s := New(WithDataDir(dir))
	if err := s.prepareCredentials(); err != nil {
		t.Fatalf("prepareCredentials: %v", err)
	}
	firstKey, firstSecret := s.accessKey, s.secretKey
	if firstKey == "" || firstSecret == "" {
		t.Fatal("expected generated credentials")
	}
	if err := s.writeEnvFile(); err != nil {
		t.Fatalf("writeEnvFile: %v", err)
	}

	restarted := New(WithDataDir(dir))
	if err := restarted.prepareCredentials(); err != nil {
		t.Fatalf("prepareCredentials on restart: %v", err)
	}
	if restarted.accessKey != firstKey || restarted.secretKey != firstSecret {
		t.Errorf("restart reused %q/%q, want persisted %q/%q",
			restarted.accessKey, restarted.secretKey, firstKey, firstSecret)
	}

	explicit := New(WithDataDir(dir), WithAccessKey("k", "v"))
	if err := explicit.prepareCredentials(); err != nil {
		t.Fatalf("prepareCredentials with explicit keys: %v", err)
	}
	if explicit.accessKey != "k" || explicit.secretKey != "v" {
		t.Errorf("explicit keys not honoured: %q/%q", explicit.accessKey, explicit.secretKey)
	}
}

// TestCredentials_EnvFile verifies the env file the slivingdoc MCP server
// configuration sources: AWS SDK variables plus the notebook bucket.
func TestCredentials_EnvFile(t *testing.T) {
	dir := t.TempDir()
	s := New(WithDataDir(dir), WithAccessKey("k", "v"))
	if err := s.writeEnvFile(); err != nil {
		t.Fatalf("writeEnvFile: %v", err)
	}
	b, err := os.ReadFile(s.EnvPath())
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	content := string(b)
	for _, want := range []string{
		"AWS_ACCESS_KEY_ID=k",
		"AWS_SECRET_ACCESS_KEY=v",
		"AWS_REGION=" + DefaultRegion,
		"AWS_ENDPOINT_URL_S3=" + s.Endpoint(),
		"SLIVINGDOC_BUCKET=" + DefaultBucket,
		"SLIVINGDOC_PATH_STYLE=true",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("env file missing %q:\n%s", want, content)
		}
	}
}

// TestCredentials_IAMConfig verifies the SeaweedFS -s3.config schema: one
// identity carrying the generated credentials and the gateway action set.
func TestCredentials_IAMConfig(t *testing.T) {
	dir := t.TempDir()
	s := New(WithDataDir(dir), WithAccessKey("k", "v"))
	if err := s.writeIAMConfig(); err != nil {
		t.Fatalf("writeIAMConfig: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, iamFileName))
	if err != nil {
		t.Fatalf("read iam.json: %v", err)
	}
	var cfg struct {
		Identities []struct {
			Name        string `json:"name"`
			Credentials []struct {
				AccessKey string `json:"accessKey"`
				SecretKey string `json:"secretKey"`
			} `json:"credentials"`
			Actions []string `json:"actions"`
		} `json:"identities"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatalf("unmarshal iam.json: %v", err)
	}
	if len(cfg.Identities) != 1 {
		t.Fatalf("identities = %d, want 1", len(cfg.Identities))
	}
	id := cfg.Identities[0]
	if id.Name != "kinoview" {
		t.Errorf("identity name = %q, want kinoview", id.Name)
	}
	if len(id.Credentials) != 1 || id.Credentials[0].AccessKey != "k" || id.Credentials[0].SecretKey != "v" {
		t.Errorf("credentials = %+v, want the generated pair", id.Credentials)
	}
	for _, action := range []string{"Admin", "Read", "Write", "List", "Tagging"} {
		if !contains(id.Actions, action) {
			t.Errorf("actions %v missing %q", id.Actions, action)
		}
	}
}

// TestCredentials_EnvFile_RegionOption verifies the region knob flows into
// the env file, so signing, the env file and the MCP server's --region
// argument stay in agreement (the -slivingdocRegion flag on serve).
func TestCredentials_EnvFile_RegionOption(t *testing.T) {
	dir := t.TempDir()
	s := New(WithDataDir(dir), WithAccessKey("k", "v"), WithRegion("eu-central-1"))
	if err := s.writeEnvFile(); err != nil {
		t.Fatalf("writeEnvFile: %v", err)
	}
	b, err := os.ReadFile(s.EnvPath())
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	if !strings.Contains(string(b), "AWS_REGION=eu-central-1") {
		t.Errorf("env file missing the configured region:\n%s", b)
	}
}

// TestResolveBinary pins the shared weed binary lookup order: an explicit
// path wins over PATH (the supervisor's resolveBinary and the serve command's
// pre-flight both delegate here); the missing-binary contract itself is
// covered by TestSupervisor_MissingBinary through Start.
func TestResolveBinary(t *testing.T) {
	t.Run("explicit path wins", func(t *testing.T) {
		explicit := filepath.Join(t.TempDir(), "weed")
		if err := os.WriteFile(explicit, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		got, err := ResolveBinary(explicit)
		if err != nil {
			t.Fatalf("ResolveBinary: %v", err)
		}
		if got != explicit {
			t.Errorf("ResolveBinary = %q, want %q", got, explicit)
		}
	})

	t.Run("found on PATH", func(t *testing.T) {
		binDir := t.TempDir()
		fake := filepath.Join(binDir, "weed")
		if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", binDir)
		got, err := ResolveBinary("")
		if err != nil {
			t.Fatalf("ResolveBinary: %v", err)
		}
		if got != fake {
			t.Errorf("ResolveBinary = %q, want %q", got, fake)
		}
	})
}

func contains(xs []string, want string) bool {
	return slices.Contains(xs, want)
}

// TestSignV4 checks the SigV4 request shape: the credential scope, the signed
// header set and the 64-hex signature. Correctness against SeaweedFS is proven
// by the integration test, which signs every request it makes.
func TestSignV4(t *testing.T) {
	req, err := http.NewRequest(http.MethodPut, "http://127.0.0.1:8333/slivingdoc", nil)
	if err != nil {
		t.Fatal(err)
	}
	signV4(req, "AKID", "secret", "us-east-1", "s3", nil)

	if got := req.Header.Get("x-amz-content-sha256"); got != sha256Hex(nil) {
		t.Errorf("x-amz-content-sha256 = %v, want %v", got, sha256Hex(nil))
	}
	if req.Header.Get("x-amz-date") == "" {
		t.Error("x-amz-date not set")
	}
	auth := req.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=AKID/") {
		t.Errorf("Authorization = %q, want AWS4-HMAC-SHA256 credential scope", auth)
	}
	if !strings.Contains(auth, "/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-content-sha256;x-amz-date, Signature=") {
		t.Errorf("Authorization = %q, want signed-headers + signature markers", auth)
	}
	sig := auth[strings.LastIndex(auth, "Signature=")+len("Signature="):]
	if len(sig) != 64 || !isHex(sig) {
		t.Errorf("signature = %q, want 64 hex chars", sig)
	}
}

func isHex(s string) bool {
	_, err := hex.DecodeString(s)
	return err == nil
}

// TestCanonicalQuery checks SigV4 query encoding: sorted keys and %20 spaces.
func TestCanonicalQuery(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:8333/?b=two+words&a=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := canonicalQuery(req), "a=1&b=two%20words"; got != want {
		t.Errorf("canonicalQuery = %q, want %q", got, want)
	}
}

// TestSupervisor_StartStop spawns the real weed binary (S3EMBED_TEST_BIN),
// asserts readiness and a clean stop.
func TestSupervisor_StartStop(t *testing.T) {
	bin := integrationBinary(t)
	s := New(
		WithBinary(bin),
		WithDataDir(t.TempDir()),
		WithS3Port(freePort(t)),
		WithMasterPort(freePort(t)),
		WithVolumePort(freePort(t)),
		WithFilerPort(freePort(t)),
	)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Start persisted the credentials beside the data dir; a fresh supervisor
	// on the same dir must load the same keys.
	if _, err := os.Stat(s.EnvPath()); err != nil {
		t.Fatalf("credentials env file: %v", err)
	}
	reloaded := New(WithDataDir(filepath.Dir(s.EnvPath())))
	if err := reloaded.prepareCredentials(); err != nil {
		t.Fatalf("reload credentials: %v", err)
	}
	if reloaded.accessKey != s.accessKey || reloaded.secretKey != s.secretKey {
		t.Errorf("restart creds %q/%q, want %q/%q", reloaded.accessKey, reloaded.secretKey, s.accessKey, s.secretKey)
	}

	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// TestSupervisor_BucketCreated asserts the configured bucket exists after
// Start: a signed HEAD on the bucket answers 200.
func TestSupervisor_BucketCreated(t *testing.T) {
	bin := integrationBinary(t)
	s := New(
		WithBinary(bin),
		WithDataDir(t.TempDir()),
		WithS3Port(freePort(t)),
		WithMasterPort(freePort(t)),
		WithVolumePort(freePort(t)),
		WithFilerPort(freePort(t)),
		WithBucket("notebook"),
	)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	req, err := http.NewRequest(http.MethodHead, s.Endpoint()+"/notebook", nil)
	if err != nil {
		t.Fatal(err)
	}
	signV4(req, s.accessKey, s.secretKey, s.region, "s3", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("head bucket: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("head bucket status = %d, want 200", resp.StatusCode)
	}
}

// TestSupervisor_Restart proves the warm-restart path: a data dir with
// existing raft state takes longer to become ready than a fresh one, so the
// readiness window must cover it (the smoke run found a 15 s window too tight:
// warm dirs took ~25 s while fresh dirs take ~2 s). Start, stop and start
// again on the same dir must all succeed.
func TestSupervisor_Restart(t *testing.T) {
	bin := integrationBinary(t)
	dataDir := t.TempDir()
	opts := []Option{
		WithBinary(bin),
		WithDataDir(dataDir),
		WithS3Port(freePort(t)),
		WithMasterPort(freePort(t)),
		WithVolumePort(freePort(t)),
		WithFilerPort(freePort(t)),
	}

	first := New(opts...)
	if err := first.Start(context.Background()); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if err := first.Stop(context.Background()); err != nil {
		t.Fatalf("first Stop: %v", err)
	}

	restarted := New(opts...)
	if err := restarted.Start(context.Background()); err != nil {
		t.Fatalf("restart Start (warm data dir): %v", err)
	}
	t.Cleanup(func() { _ = restarted.Stop(context.Background()) })

	// The restarted supervisor must still be able to talk to the gateway.
	if err := restarted.createBucket(context.Background()); err != nil {
		t.Fatalf("createBucket after restart: %v", err)
	}
}

// integrationBinary returns the weed binary for the integration tests: the
// S3EMBED_TEST_BIN env var, skipped in -short mode or when unset so the QA
// gate never depends on an external fixture.
func integrationBinary(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping SeaweedFS integration test in -short mode")
	}
	bin := os.Getenv("S3EMBED_TEST_BIN")
	if bin == "" {
		t.Skip("S3EMBED_TEST_BIN not set; skipping SeaweedFS integration test")
	}
	return bin
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}
