// Package s3embed supervises a SeaweedFS child process that provides the
// S3-compatible backend for kinoview's shared agent notebook (slivingdoc).
//
// The child is the official static weed binary, spawned bound to loopback with
// a single S3 gateway and one bucket. kinoview owns the lifecycle: Start spawns
// it, waits for readiness, creates the bucket and persists the IAM credentials;
// Stop SIGTERMs the child and escalates to SIGKILL if it does not exit within a
// bounded window. If the weed binary cannot be resolved, Start fails fast with
// ErrBinaryNotFound so the caller can run without the notebook.
package s3embed

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

const (
	// DefaultS3Port is the S3 gateway listen port.
	DefaultS3Port = 8333
	// DefaultMasterPort is the SeaweedFS master HTTP listen port.
	DefaultMasterPort = 9333
	// DefaultVolumePort is the SeaweedFS volume server HTTP listen port.
	DefaultVolumePort = 8080
	// DefaultFilerPort is the SeaweedFS filer HTTP listen port.
	DefaultFilerPort = 8888
	// DefaultBucket is the bucket created for the notebook.
	DefaultBucket = "slivingdoc"

	// readinessWindow is how long Start waits for the S3 gateway to answer.
	// It must cover a warm restart: a data dir with existing raft state can
	// take ~25 s for the master to elect a leader and the filer to come up
	// (fresh dirs are ~2 s). The child-death check still fails fast, so the
	// window only bounds the wedged-but-alive case.
	readinessWindow = 60 * time.Second
	// readinessInterval is the pause between readiness polls.
	readinessInterval = 500 * time.Millisecond
	// stopGrace is how long Stop waits for the child to exit after SIGTERM.
	stopGrace = 15 * time.Second
	// killGrace is how long Stop waits for the child to exit after SIGKILL.
	killGrace = 5 * time.Second
)

// ErrBinaryNotFound is returned by Start when the weed binary cannot be
// resolved: no explicit path was given, none sits next to the current
// executable, and none is on PATH.
var ErrBinaryNotFound = errors.New("s3embed: weed binary not found")

// s3Client bounds the readiness and bucket requests so a wedged gateway can
// never hang Start or Stop forever.
var s3Client = &http.Client{Timeout: 2 * time.Second}

// Supervisor owns one SeaweedFS child process.
type Supervisor struct {
	binPath    string
	dataDir    string
	s3Port     int
	masterPort int
	volumePort int
	filerPort  int
	bucket     string
	accessKey  string
	secretKey  string
	region     string

	mu      sync.Mutex
	cmd     *exec.Cmd
	done    chan struct{} // closed when the child exits
	waitErr error
	logs    *tail
}

// Option configures a Supervisor.
type Option func(*Supervisor)

// WithBinary sets the weed binary path. Empty resolves next to the current
// executable, then on PATH.
func WithBinary(path string) Option { return func(s *Supervisor) { s.binPath = path } }

// WithDataDir sets the durable volume/master data directory. Defaults to
// <user-config-dir>/kinoview/s3.
func WithDataDir(dir string) Option { return func(s *Supervisor) { s.dataDir = dir } }

// WithS3Port sets the S3 gateway listen port. Defaults to DefaultS3Port.
func WithS3Port(port int) Option { return func(s *Supervisor) { s.s3Port = port } }

// WithMasterPort sets the master HTTP listen port. Defaults to DefaultMasterPort.
func WithMasterPort(port int) Option { return func(s *Supervisor) { s.masterPort = port } }

// WithVolumePort sets the volume server HTTP listen port. Defaults to DefaultVolumePort.
func WithVolumePort(port int) Option { return func(s *Supervisor) { s.volumePort = port } }

// WithFilerPort sets the filer HTTP listen port. Defaults to DefaultFilerPort.
func WithFilerPort(port int) Option { return func(s *Supervisor) { s.filerPort = port } }

// WithBucket sets the bucket to create. Defaults to DefaultBucket.
func WithBucket(bucket string) Option { return func(s *Supervisor) { s.bucket = bucket } }

// WithRegion sets the AWS region label used for SigV4 signing and written
// into the credentials env file, so signing, the env file and the slivingdoc
// MCP server's --region argument all agree. Defaults to DefaultRegion.
func WithRegion(region string) Option { return func(s *Supervisor) { s.region = region } }

// WithAccessKey sets the S3 credentials instead of generating and persisting them.
func WithAccessKey(accessKey, secretKey string) Option {
	return func(s *Supervisor) {
		s.accessKey = accessKey
		s.secretKey = secretKey
	}
}

// New creates a Supervisor with the given options.
func New(opts ...Option) *Supervisor {
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = ""
	}
	s := &Supervisor{
		dataDir:    filepath.Join(configDir, "kinoview", "s3"),
		s3Port:     DefaultS3Port,
		masterPort: DefaultMasterPort,
		volumePort: DefaultVolumePort,
		filerPort:  DefaultFilerPort,
		bucket:     DefaultBucket,
		region:     DefaultRegion,
		logs:       newTail(200),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Endpoint returns the S3 gateway endpoint the notebook connects to.
func (s *Supervisor) Endpoint() string {
	return fmt.Sprintf("http://127.0.0.1:%d", s.s3Port)
}

// Bucket returns the bucket created for the notebook.
func (s *Supervisor) Bucket() string {
	return s.bucket
}

// EnvPath returns the credentials env file written by Start, which the
// slivingdoc MCP server configuration sources for the S3 credentials.
func (s *Supervisor) EnvPath() string {
	return filepath.Join(s.dataDir, envFileName)
}

// Start spawns the weed child, waits for S3 readiness and creates the bucket.
// It returns ErrBinaryNotFound when no weed binary can be resolved.
func (s *Supervisor) Start(ctx context.Context) error {
	bin, err := s.resolveBinary()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dataDir, 0o755); err != nil {
		return fmt.Errorf("s3embed: create data dir %q: %w", s.dataDir, err)
	}
	if err := s.prepareCredentials(); err != nil {
		return err
	}
	if err := s.writeIAMConfig(); err != nil {
		return err
	}
	if err := s.writeEnvFile(); err != nil {
		return err
	}

	iamPath := filepath.Join(s.dataDir, iamFileName)

	s.mu.Lock()
	if s.cmd != nil {
		s.mu.Unlock()
		return errors.New("s3embed: already started")
	}
	cmd := exec.CommandContext(ctx, bin, s.serverArgs(iamPath)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = s.logs
	cmd.Stderr = s.logs
	s.cmd = cmd
	s.done = make(chan struct{})
	s.waitErr = nil
	s.mu.Unlock()

	if err := cmd.Start(); err != nil {
		s.mu.Lock()
		s.cmd = nil
		s.mu.Unlock()
		return fmt.Errorf("s3embed: start weed: %w", err)
	}
	go func() {
		s.waitErr = cmd.Wait()
		close(s.done)
	}()

	if err := s.waitReady(ctx); err != nil {
		s.Stop(context.Background())
		return err
	}
	if err := s.createBucket(ctx); err != nil {
		s.Stop(context.Background())
		return err
	}
	return nil
}

// Stop SIGTERMs the child and waits stopGrace for it to exit on its own; on
// timeout it escalates to SIGKILL and waits killGrace. ctx only aborts the
// final SIGKILL wait; the graceful window is always honoured so a shutdown
// path with a cancelled context still stops the child cleanly.
func (s *Supervisor) Stop(ctx context.Context) error {
	s.mu.Lock()
	cmd := s.cmd
	done := s.done
	s.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	_ = cmd.Process.Signal(syscall.SIGTERM)
	grace := time.NewTimer(stopGrace)
	defer grace.Stop()
	select {
	case <-done:
		return nil
	case <-grace.C:
	}

	_ = cmd.Process.Kill()
	kill := time.NewTimer(killGrace)
	defer kill.Stop()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("s3embed: weed did not exit after SIGKILL: %w", ctx.Err())
	case <-kill.C:
		return fmt.Errorf("s3embed: weed did not exit after SIGKILL\n%s", s.logs)
	}
}

// serverArgs builds the weed server command line. The IAM JSON path is passed
// through -s3.config so only the generated credentials can access the gateway.
func (s *Supervisor) serverArgs(iamPath string) []string {
	return []string{
		"server",
		"-dir=" + s.dataDir,
		"-ip=127.0.0.1",
		"-ip.bind=127.0.0.1",
		fmt.Sprintf("-master.port=%d", s.masterPort),
		fmt.Sprintf("-volume.port=%d", s.volumePort),
		"-filer",
		fmt.Sprintf("-filer.port=%d", s.filerPort),
		"-s3",
		fmt.Sprintf("-s3.port=%d", s.s3Port),
		"-s3.config=" + iamPath,
		// Loopback-only local server: no telemetry home calls, no extra
		// listeners that could collide with other software on the host.
		"-master.telemetry=false",
		"-s3.port.iceberg=0",
		"-volume.preStopSeconds=0",
	}
}

// ResolveBinary finds the weed binary: an explicit path, then next to the
// current executable, then on PATH. Nothing resolves to ErrBinaryNotFound;
// an explicit-but-missing path fails naming the path. The serve command calls
// it before constructing the supervisor so a missing dependency disables the
// notebook with one warning and no side effects.
func ResolveBinary(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", fmt.Errorf("s3embed: weed binary at %q: %w", explicit, err)
		}
		return explicit, nil
	}
	if exe, err := os.Executable(); err == nil {
		next := filepath.Join(filepath.Dir(exe), "weed")
		if _, err := os.Stat(next); err == nil {
			return next, nil
		}
	}
	if p, err := exec.LookPath("weed"); err == nil {
		return p, nil
	}
	return "", ErrBinaryNotFound
}

// resolveBinary is the supervisor's binary lookup: the explicit WithBinary
// path, then the shared ResolveBinary order.
func (s *Supervisor) resolveBinary() (string, error) {
	return ResolveBinary(s.binPath)
}

// waitReady polls the S3 gateway until it answers ListBuckets.
func (s *Supervisor) waitReady(ctx context.Context) error {
	deadline := time.Now().Add(readinessWindow)
	for {
		if err := s.listBuckets(ctx); err == nil {
			return nil
		}
		select {
		case <-s.done:
			return fmt.Errorf("s3embed: weed exited during startup: %w\n%s", s.waitErr, s.logs)
		default:
		}
		if ctx.Err() != nil {
			s.Stop(context.Background())
			return fmt.Errorf("s3embed: start interrupted: %w", ctx.Err())
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("s3embed: weed did not become ready on port %d within %v\n%s",
				s.s3Port, readinessWindow, s.logs)
		}
		time.Sleep(readinessInterval)
	}
}

// listBuckets issues a signed ListBuckets request; any HTTP answer from the
// gateway counts as ready.
func (s *Supervisor) listBuckets(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.Endpoint()+"/", nil)
	if err != nil {
		return err
	}
	signV4(req, s.accessKey, s.secretKey, s.region, "s3", nil)
	resp, err := s3Client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("s3embed: list buckets: %s", resp.Status)
	}
	return nil
}

// createBucket issues a signed CreateBucket request; an existing bucket owned
// by this identity answers 409 and counts as success.
func (s *Supervisor) createBucket(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, s.Endpoint()+"/"+s.bucket, nil)
	if err != nil {
		return err
	}
	signV4(req, s.accessKey, s.secretKey, s.region, "s3", nil)
	resp, err := s3Client.Do(req)
	if err != nil {
		return fmt.Errorf("s3embed: create bucket %q: %w", s.bucket, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusConflict:
		return nil
	default:
		return fmt.Errorf("s3embed: create bucket %q: %s", s.bucket, resp.Status)
	}
}
