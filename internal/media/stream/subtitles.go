package stream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/go_away_boilerplate/pkg/misc"
	"github.com/baalimago/kinoview/internal/model"
)

// CommandRunner abstracts exec.Command for testing purposes
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) error
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

type defaultRunner struct{}

func (d defaultRunner) Run(ctx context.Context, name string, args ...string) error {
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

func (d defaultRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, stderr.String())
	}
	return out, nil
}

type Manager struct {
	storePath string
	runner    CommandRunner

	debug bool

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
		defaultPath = filepath.Join(cfg, "kinoview", "stream")
	}

	m := &Manager{
		storePath:  defaultPath,
		runner:     defaultRunner{},
		mediaCache: make(map[string]model.MediaInfo),
	}

	if misc.Truthy(os.Getenv("DEBUG")) || misc.Truthy(os.Getenv("DEBUG_SUBS")) {
		m.debug = true
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
// Checks memory cache first. Also discovers external sidecar subtitle files
// and appends them as synthetic subtitle streams.
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
	}

	out, err := m.runner.Output(ctx, "ffprobe", args...)
	if err != nil {
		return model.MediaInfo{}, fmt.Errorf("ffprobe failed for %s: %w", item.Name, err)
	}

	if err := json.Unmarshal(out, &info); err != nil {
		return model.MediaInfo{}, fmt.Errorf("failed to unmarshal media info: %w", err)
	}

	// Discover external sidecar subtitles and merge them in.
	extStreams := m.findExternal(item)
	info.Streams = append(info.Streams, extStreams...)

	m.mediaMu.Lock()
	m.mediaCache[item.ID] = info
	m.mediaMu.Unlock()

	return info, nil
}

// findExternal discovers sidecar subtitle files (.srt, .vtt) near the media item.
// Returns synthetic Stream entries with negative indices and ExternalPath set.
//
// Patterns scanned:
//  1. {dir}/Subs/{basename}/*.srt, *.vtt       (exact match on episode dir)
//  2. {dir}/{basename}.srt, {basename}.vtt      (same-name sidecar)
//  3. {dir}/Subs/*.srt, *.vtt                   (flat Subs directory fallback)
//
// Indices start from -1, decrementing.
func (m *Manager) findExternal(item model.Item) []model.Stream {
	dir := filepath.Dir(item.Path)
	basename := strings.TrimSuffix(item.Name, filepath.Ext(item.Name))

	var paths []string

	// Pattern 1: Subs/{basename}/*.srt, *.vtt
	subsDir := filepath.Join(dir, "Subs", basename)
	paths = append(paths, globFiles(subsDir)...)

	// Pattern 2: {basename}.srt, {basename}.vtt in same dir
	paths = append(paths, sidecarFiles(dir, basename)...)

	// Pattern 3: flat Subs/*.srt, *.vtt
	flatSubsDir := filepath.Join(dir, "Subs")
	paths = append(paths, globFiles(flatSubsDir)...)

	// Deduplicate
	seen := make(map[string]bool)
	var unique []string
	for _, p := range paths {
		if !seen[p] {
			seen[p] = true
			unique = append(unique, p)
		}
	}

	// Build synthetic streams
	var streams []model.Stream
	for i, p := range unique {
		ext := strings.ToLower(filepath.Ext(p))
		codecName := "subrip"
		codecTag := "external_srt"
		if ext == ".vtt" {
			codecName = "webvtt"
			codecTag = "external_vtt"
		}
		// Extract language hint from filename (e.g., "5_English.srt" → "English")
		lang := extractLanguage(p)
		streams = append(streams, model.Stream{
			Index:          -(1 + i), // negative indices: -1, -2, -3...
			CodecName:      codecName,
			CodecLongName:  "External " + strings.ToUpper(codecName) + " subtitle",
			CodecType:      "subtitle",
			CodecTagString: codecTag,
			ExternalPath:   p,
			Tags: model.Tags{
				Language: lang,
				Title:    filepath.Base(p),
			},
		})
	}

	return streams
}

// globFiles returns files matching pattern *.srt or *.vtt in the given directory.
// Silently handles non-existent directories.
func globFiles(dir string) []string {
	var files []string
	for _, ext := range []string{"*.srt", "*.vtt"} {
		matches, err := filepath.Glob(filepath.Join(dir, ext))
		if err != nil {
			continue
		}
		files = append(files, matches...)
	}
	return files
}

// sidecarFiles checks for basename.srt and basename.vtt in the given directory.
func sidecarFiles(dir, basename string) []string {
	var files []string
	for _, ext := range []string{".srt", ".vtt"} {
		p := filepath.Join(dir, basename+ext)
		if _, err := os.Stat(p); err == nil {
			files = append(files, p)
		}
	}
	return files
}

// extractLanguage attempts to extract a language string from the filename.
// e.g., "5_English.srt" → "eng", "English.srt" → "eng"
func extractLanguage(path string) string {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	// Try splitting on underscore and looking for known languages
	parts := strings.Split(name, "_")
	for _, part := range parts {
		pl := strings.ToLower(part)
		switch pl {
		case "english", "eng", "en":
			return "eng"
		case "swedish", "swe", "sv":
			return "swe"
		case "french", "fre", "fr":
			return "fre"
		case "german", "ger", "de":
			return "ger"
		case "spanish", "spa", "es":
			return "spa"
		case "italian", "ita", "it":
			return "ita"
		}
	}
	return "und" // undetermined
}

// Extract subtitles for a specific stream index to .vtt format.
// Stores the result in the configured storePath.
// If the file already exists, returns the path immediately.
//
// For embedded streams (positive index): uses ffmpeg -map 0:<idx>
// For external streams (negative index): converts the sidecar .srt/.vtt file
func (m *Manager) ExtractSubtitles(item model.Item, streamIndex string) (string, error) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Determine if this is an external subtitle stream (negative index)
	idx, parseErr := strconv.Atoi(streamIndex)
	var args []string
	if parseErr == nil && idx < 0 {
		// External subtitle: look up the file path from cached info.
		// If cache is cold, call Find first to populate it.
		extPath, findErr := m.findExternalPath(item, idx)
		if findErr != nil {
			// Try populating cache via Find, then retry
			if _, findErr2 := m.Find(item); findErr2 == nil {
				extPath, findErr = m.findExternalPath(item, idx)
			}
			if findErr != nil {
				return "", fmt.Errorf("external subtitle not found: %w", findErr)
			}
		}
		args = []string{
			"-y",
			"-i", extPath,
			"-f", "webvtt",
			destPath,
		}
	} else {
		// Embedded subtitle: use ffmpeg map
		mapArg := "0:" + streamIndex
		args = []string{
			"-y", // overwrite output files (though we checked existence, good practice for ffmpeg)
			"-i", item.Path,
			"-map", mapArg,
			"-f", "webvtt",
			destPath,
		}
	}

	start := time.Now()

	if m.debug {
		ancli.Noticef("Extracting subtitle %s for %s. Command:\nffmpeg %v", streamIndex, item.Name, strings.Join(args, " "))
	}
	if err := m.runner.Run(ctx, "ffmpeg", args...); err != nil {
		return "", fmt.Errorf("ffmpeg extraction failed: %w", err)
	}

	if m.debug {
		ancli.Okf("Extracted subtitle %s for %s in %v", streamIndex, item.Name, time.Since(start))
	}
	return destPath, nil
}

// findExternalPath returns the absolute path to an external subtitle file
// for the given stream index from the cached media info.
func (m *Manager) findExternalPath(item model.Item, index int) (string, error) {
	m.mediaMu.RLock()
	info, ok := m.mediaCache[item.ID]
	m.mediaMu.RUnlock()
	if !ok {
		return "", fmt.Errorf("no cached media info for item %s", item.ID)
	}
	for _, s := range info.Streams {
		if s.Index == index && s.ExternalPath != "" {
			return s.ExternalPath, nil
		}
	}
	return "", fmt.Errorf("external path not found for stream index %d", index)
}
