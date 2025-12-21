package usercontext

import (
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
	appName     = "kinoview"
	storeDir    = "userContext"
	storeFile   = "contexts.json"
	maxContexts = 1000
)

// Manager stores client contexts in-memory and persists them to disk.
// Data is stored under: $XDG_CACHE_HOME/kinoview/userContext/contexts.json
// (or os.UserCacheDir() fallback).
type Manager struct {
	mu       sync.Mutex
	path     string
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

	p := filepath.Join(cacheDir, appName, storeDir, storeFile)
	m := &Manager{path: p}
	if err := m.load(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) AllClientContexts() []model.ClientContext {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]model.ClientContext(nil), m.contexts...)
}

func (m *Manager) StoreClientContext(ctx model.ClientContext) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.contexts = append(m.contexts, ctx)
	if len(m.contexts) > maxContexts {
		m.contexts = m.contexts[len(m.contexts)-maxContexts:]
	}
	return m.persistLocked()
}

func (m *Manager) load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	b, err := os.ReadFile(m.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			m.contexts = nil
			return nil
		}
		return fmt.Errorf("read user context store: %w", err)
	}
	if len(b) == 0 {
		m.contexts = nil
		return nil
	}

	var contexts []model.ClientContext
	if err := json.Unmarshal(b, &contexts); err != nil {
		return fmt.Errorf("unmarshal user contexts: %w", err)
	}
	m.contexts = contexts
	if len(m.contexts) > maxContexts {
		m.contexts = m.contexts[len(m.contexts)-maxContexts:]
	}
	return nil
}

func (m *Manager) persistLocked() error {
	dir := filepath.Dir(m.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir user context dir: %w", err)
	}

	b, err := json.MarshalIndent(m.contexts, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal user contexts: %w", err)
	}

	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("write tmp user contexts: %w", err)
	}
	if err := os.Rename(tmp, m.path); err != nil {
		return fmt.Errorf("rename tmp user contexts: %w", err)
	}
	return nil
}
