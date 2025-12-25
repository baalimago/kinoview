package subtitles

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/model"
)

// CommandRunner abstracts exec.Command for testing purposes
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) error
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

type defaultRunner struct{}

func (d *defaultRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	// Capture stderr for debugging/logging, similar to original implementation
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("%w: %s", err, stderr.String())
	}
	return nil
}

func (d *defaultRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, stderr.String())
	}
	return out, nil
}

type SubtitleAssociation map[string]os.File

type Manager struct {
	storePath string
	runner    CommandRunner

	// mediaCache caches the ffprobe results to avoid repeated scanning
	mediaCache map[string]model.MediaInfo
	mediaMu    sync.RWMutex

	// extractionMu prevents multiple extractions of the same subtitle at once
	extractionMu sync.Mutex
}

type Option func(*Manager)

func WithStoragePath(path string) Option {
	return func(m *Manager) {
		m.storePath = path
	}
}

// withRunner is internal, used for injection in tests
func withRunner(r CommandRunner) Option {
	return func(m *Manager) {
		m.runner = r
	}
}

func NewManager(opts ...Option) (*Manager, error) {
	// Default to a folder in user config if not specified
	defaultPath := "subtitles"
	cfg, err := os.UserConfigDir()
	if err == nil {
		defaultPath = filepath.Join(cfg, "kinoview", "subtitles")
	}

	m := &Manager{
		storePath:  defaultPath,
		runner:     &defaultRunner{},
		mediaCache: make(map[string]model.MediaInfo),
	}

	for _, opt := range opts {
		opt(m)
	}

	// Ensure storage directory exists
	if err := os.MkdirAll(m.storePath, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create subtitle storage directory: %w", err)
	}

	return m, nil
}

// Find media info (subtitles/streams) for an item using ffprobe.
// Checks memory cache first.
func (m *Manager) Find(item model.Item) (model.MediaInfo, error) {
	m.mediaMu.RLock()
	info, exists := m.mediaCache[item.ID]
	m.mediaMu.RUnlock()
	if exists {
		return info, nil
	}

	// We use a short timeout for probe operations to avoid blocking indefinitely
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	args := []string{
		item.Path,
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams",
		"-select_streams", "s",
	}

	out, err := m.runner.Output(ctx, "ffprobe", args...)
	if err != nil {
		return model.MediaInfo{}, fmt.Errorf("ffprobe failed for %s: %w", item.Name, err)
	}

	if err := json.Unmarshal(out, &info); err != nil {
		return model.MediaInfo{}, fmt.Errorf("failed to unmarshal media info: %w", err)
	}

	m.mediaMu.Lock()
	m.mediaCache[item.ID] = info
	m.mediaMu.Unlock()

	return info, nil
}

// Extract subtitles for a specific stream index to .vtt format.
// Stores the result in the configured storePath.
// If the file already exists, returns the path immediately.
func (m *Manager) Extract(item model.Item, streamIndex string) (string, error) {
	// Unique filename based on Item ID and stream index
	filename := fmt.Sprintf("%s_%s.vtt", item.ID, streamIndex)
	destPath := filepath.Join(m.storePath, filename)

	// Check if already exists
	if _, err := os.Stat(destPath); err == nil {
		// File exists, assume it's good
		// In a production system we might want to check size > 0
		return destPath, nil
	}

	// Lock purely to prevent thundering herd on the exact same file
	m.extractionMu.Lock()
	defer m.extractionMu.Unlock()

	// Double check after lock
	if _, err := os.Stat(destPath); err == nil {
		return destPath, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// ffmpeg -y -i <input> -map 0:<stream> -f webvtt <output>
	mapArg := "0:" + streamIndex
	args := []string{
		"-y", // overwrite output files (though we checked existence, good practice for ffmpeg)
		"-i", item.Path,
		"-map", mapArg,
		"-f", "webvtt",
		destPath,
	}

	start := time.Now()
	if err := m.runner.Run(ctx, "ffmpeg", args...); err != nil {
		return "", fmt.Errorf("ffmpeg extraction failed: %w", err)
	}

	ancli.Noticef("Extracted subtitle %s for %s in %v", streamIndex, item.Name, time.Since(start))
	return destPath, nil
}
