package suggestions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/model"
)

type Manager struct {
	mu            sync.Mutex
	suggestions   []model.Suggestion
	cacheFilePath string
}

func NewManager(kinoviewCacheDir string) (*Manager, error) {
	cacheFilePath := filepath.Join(kinoviewCacheDir, "suggestions.json")

	m := &Manager{
		cacheFilePath: cacheFilePath,
		suggestions:   []model.Suggestion{},
	}

	err := m.load()
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load suggestions: %w", err)
	}

	ancli.Okf("suggestion manager setup, loaded: '%v' items", len(m.Get()))
	return m, nil
}

func (m *Manager) List() ([]model.Suggestion, error) {
	return m.Get(), nil
}

func (m *Manager) Add(s model.Suggestion) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.suggestions = append(m.suggestions, s)
	return m.save()
}

func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	newRecs := make([]model.Suggestion, 0, len(m.suggestions))
	for _, s := range m.suggestions {
		if s.ID == id {
			continue
		}
		newRecs = append(newRecs, s)
	}
	m.suggestions = newRecs
	return m.save()
}

func (m *Manager) Get() []model.Suggestion {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Return a copy to avoid data races if the caller modifies it
	res := make([]model.Suggestion, len(m.suggestions))
	copy(res, m.suggestions)
	return res
}

func (m *Manager) Update(suggestions []model.Suggestion) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.suggestions = suggestions
	return m.save()
}

func (m *Manager) load() error {
	data, err := os.ReadFile(m.cacheFilePath)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &m.suggestions)
}

func (m *Manager) save() error {
	data, err := json.Marshal(m.suggestions)
	if err != nil {
		return err
	}
	return os.WriteFile(m.cacheFilePath, data, 0o644)
}
