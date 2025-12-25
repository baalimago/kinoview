package clientcontext

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/baalimago/kinoview/internal/agents"
	"github.com/baalimago/kinoview/internal/model"
)

const (
	appName  = "kinoview"
	storeDir = "clientContext"
	logFile  = "contexts.log"
)

// Manager stores client contexts in-memory and persists them incrementally to disk.
// Data is stored under: $XDG_CACHE_HOME/kinoview/userContext/contexts.log
// (or os.UserCacheDir() fallback).
// Uses append-only JSONL format for incremental updates.
type Manager struct {
	mu       sync.Mutex
	logPath  string
	contexts []model.ClientContext
}

var _ agents.UserContextManager = (*Manager)(nil)

// New creates a new user context manager.
// If cacheDir is empty, os.UserCacheDir() is used.
func New(cacheDir string) (*Manager, error) {
	if cacheDir == "" {
		d, err := os.UserCacheDir()
		if err != nil {
			return nil, fmt.Errorf("user cache dir: %w", err)
		}
		cacheDir = d
	}

	p := filepath.Join(cacheDir, appName, storeDir, logFile)
	m := &Manager{logPath: p}
	if err := m.load(); err != nil {
		return nil, err
	}
	return m, nil
}

// AllClientContexts returns all stored contexts (raw events).
func (m *Manager) AllClientContexts() []model.ClientContext {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]model.ClientContext(nil), m.contexts...)
}

// StoreClientContext appends only the new entry to the log (incremental storage).
func (m *Manager) StoreClientContext(userCtx model.ClientContext) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.contexts = append(m.contexts, userCtx)
	return m.appendToLogLocked(userCtx)
}

// appendToLogLocked writes a single context entry to the log in JSONL format.
func (m *Manager) appendToLogLocked(ctx model.ClientContext) error {
	dir := filepath.Dir(m.logPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir user context dir: %w", err)
	}

	b, err := json.Marshal(ctx)
	if err != nil {
		return fmt.Errorf("marshal user context: %w", err)
	}

	f, err := os.OpenFile(m.logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer f.Close()

	// Write JSON + newline (JSONL format)
	if _, err := f.Write(append(b, byte('\n'))); err != nil {
		return fmt.Errorf("write log: %w", err)
	}
	return nil
}

// load reads the entire log and replays it into memory.
func (m *Manager) load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	b, err := os.ReadFile(m.logPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			m.contexts = nil
			return nil
		}
		return fmt.Errorf("read user context log: %w", err)
	}

	if len(b) == 0 {
		m.contexts = nil
		return nil
	}

	m.contexts = nil
	for _, line := range bytes.Split(bytes.TrimSpace(b), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var ctx model.ClientContext
		if err := json.Unmarshal(line, &ctx); err != nil {
			return fmt.Errorf("unmarshal user context log entry: %w", err)
		}
		m.contexts = append(m.contexts, ctx)
	}
	return nil
}
