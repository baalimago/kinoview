package s3embed

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// DefaultRegion is the AWS region label used for SigV4 signing and written
// into the env file; SeaweedFS and slivingdoc treat it as an opaque label.
// The serve command's -slivingdocRegion flag overrides it (WithRegion).
const DefaultRegion = "us-east-1"

// iamIdentityName is the name of the single IAM identity in the SeaweedFS
// config file.
const iamIdentityName = "kinoview"

// iamFileName is the SeaweedFS IAM config file written beside the data dir.
const iamFileName = "iam.json"

// envFileName is the credentials env file written beside the data dir; the
// slivingdoc MCP server configuration sources it (see EnvPath).
const envFileName = "credentials.env"

// tail keeps the last max lines written to it, for surfacing diagnostics from
// the supervised child when it fails to start or is killed.
type tail struct {
	mu    sync.Mutex
	lines []string
	max   int
}

func newTail(max int) *tail { return &tail{max: max} }

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for line := range strings.SplitSeq(strings.TrimRight(string(p), "\n"), "\n") {
		if line == "" {
			continue
		}
		t.lines = append(t.lines, line)
		if len(t.lines) > t.max {
			t.lines = t.lines[len(t.lines)-t.max:]
		}
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.Join(t.lines, "\n")
}

// iamConfig is the SeaweedFS -s3.config schema: one identity whose credentials
// and actions grant access to the whole gateway (buckets are not scoped).
type iamConfig struct {
	Identities []iamIdentity `json:"identities"`
}

type iamIdentity struct {
	Name        string          `json:"name"`
	Credentials []iamCredential `json:"credentials"`
	Actions     []string        `json:"actions"`
}

type iamCredential struct {
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
}

// prepareCredentials resolves the S3 credentials: explicit options win, then
// the persisted env file, then a freshly generated pair persisted to the env
// file so restarts reuse the same keys.
func (s *Supervisor) prepareCredentials() error {
	if s.accessKey != "" && s.secretKey != "" {
		return nil
	}
	if creds, err := readEnvFile(s.EnvPath()); err == nil {
		s.accessKey = creds.accessKey
		s.secretKey = creds.secretKey
		return nil
	}
	key, secret, err := generateCredentials()
	if err != nil {
		return fmt.Errorf("s3embed: generate credentials: %w", err)
	}
	s.accessKey = key
	s.secretKey = secret
	return nil
}

// writeEnvFile persists the credentials env file the slivingdoc MCP server
// configuration sources for the S3 credentials.
func (s *Supervisor) writeEnvFile() error {
	content := strings.Join([]string{
		"# kinoview SeaweedFS S3 credentials for the slivingdoc notebook",
		"AWS_ACCESS_KEY_ID=" + s.accessKey,
		"AWS_SECRET_ACCESS_KEY=" + s.secretKey,
		"AWS_REGION=" + s.region,
		"AWS_ENDPOINT_URL_S3=" + s.Endpoint(),
		"SLIVINGDOC_BUCKET=" + s.bucket,
		"SLIVINGDOC_PATH_STYLE=true",
		"",
	}, "\n")
	if err := os.WriteFile(s.EnvPath(), []byte(content), 0o600); err != nil {
		return fmt.Errorf("s3embed: write credentials env: %w", err)
	}
	return nil
}

// writeIAMConfig writes the SeaweedFS IAM config granting the generated
// credentials admin/read/write/list/tagging on the gateway.
func (s *Supervisor) writeIAMConfig() error {
	cfg := iamConfig{Identities: []iamIdentity{{
		Name:        iamIdentityName,
		Credentials: []iamCredential{{AccessKey: s.accessKey, SecretKey: s.secretKey}},
		Actions:     []string{"Admin", "Read", "Write", "List", "Tagging"},
	}}}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("s3embed: marshal IAM config: %w", err)
	}
	path := filepath.Join(s.dataDir, iamFileName)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("s3embed: write IAM config: %w", err)
	}
	return nil
}

type credentials struct {
	accessKey string
	secretKey string
}

// readEnvFile parses the credentials env file written by writeEnvFile.
func readEnvFile(path string) (credentials, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return credentials{}, err
	}
	var c credentials
	for line := range strings.SplitSeq(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "AWS_ACCESS_KEY_ID":
			c.accessKey = v
		case "AWS_SECRET_ACCESS_KEY":
			c.secretKey = v
		}
	}
	if c.accessKey == "" || c.secretKey == "" {
		return credentials{}, errors.New("s3embed: credentials env file incomplete")
	}
	return c, nil
}

// generateCredentials returns a random S3-style access key/secret pair.
func generateCredentials() (string, string, error) {
	key, err := randomString(20)
	if err != nil {
		return "", "", err
	}
	secret, err := randomString(40)
	if err != nil {
		return "", "", err
	}
	return key, secret, nil
}

// randomString returns n random base32 characters; the alphabet keeps keys
// shell-safe for the env file and the SeaweedFS command line.
func randomString(n int) (string, error) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, n)
	for i := range b {
		out[i] = alphabet[b[i]&31]
	}
	return string(out), nil
}
