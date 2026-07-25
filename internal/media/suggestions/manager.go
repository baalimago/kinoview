package suggestions

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/model"
)

// ErrWouldEmpty is returned by Update when called with an empty slice while
// existing suggestions are present — a guard against silently wiping the shelf.
var ErrWouldEmpty = errors.New("refusing to empty non-empty shelf: use Remove to clear individual items")

type Manager struct {
	mu            sync.Mutex
	suggestions   []model.Suggestion
	fingerprint   *model.SuggestionFingerprint
	generated     time.Time
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
	return m.updateInternal(suggestions, nil, time.Time{})
}

// UpdateWithFingerprint stores suggestions together with their cache
// fingerprint so the next cascade can decide whether to skip the butler.
func (m *Manager) UpdateWithFingerprint(suggestions []model.Suggestion, fp model.SuggestionFingerprint, generated time.Time) error {
	return m.updateInternal(suggestions, &fp, generated)
}

func (m *Manager) updateInternal(suggestions []model.Suggestion, fp *model.SuggestionFingerprint, generated time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(suggestions) == 0 && len(m.suggestions) > 0 {
		return ErrWouldEmpty
	}
	m.suggestions = suggestions
	m.fingerprint = fp
	if !generated.IsZero() {
		m.generated = generated
	}
	return m.save()
}

// Fingerprint returns the stored cache fingerprint, or nil if none exists
// (legacy file or first run).
func (m *Manager) Fingerprint() *model.SuggestionFingerprint {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fingerprint == nil {
		return nil
	}
	fp := *m.fingerprint
	return &fp
}

// Generated returns the timestamp of the last successful suggestion generation.
func (m *Manager) Generated() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.generated
}

// Envelope returns a snapshot of the full on-disk state.
func (m *Manager) Envelope() model.SuggestionsFile {
	m.mu.Lock()
	defer m.mu.Unlock()
	env := model.SuggestionsFile{
		Suggestions: make([]model.Suggestion, len(m.suggestions)),
	}
	copy(env.Suggestions, m.suggestions)
	if m.fingerprint != nil {
		fp := *m.fingerprint
		env.Fingerprint = &fp
	}
	if !m.generated.IsZero() {
		env.Generated = m.generated.UTC().Format(time.RFC3339)
	}
	return env
}

func (m *Manager) load() error {
	data, err := os.ReadFile(m.cacheFilePath)
	if err != nil {
		return err
	}

	// Try object format first: {"suggestions":[...],"fingerprint":{...},"generated":"..."}
	var envelope model.SuggestionsFile
	if err := json.Unmarshal(data, &envelope); err == nil && envelope.Suggestions != nil {
		m.suggestions = envelope.Suggestions
		m.fingerprint = envelope.Fingerprint
		if envelope.Generated != "" {
			if t, err := time.Parse(time.RFC3339, envelope.Generated); err == nil {
				m.generated = t
			}
		}
		return nil
	}

	// Fall back to legacy bare-array format: [...]
	var suggestions []model.Suggestion
	if err := json.Unmarshal(data, &suggestions); err == nil {
		m.suggestions = suggestions
		m.fingerprint = nil
		return nil
	}

	// Partial object: extract just the suggestions field when the envelope
	// has a malformed fingerprint or other non-critical field.
	var partial struct {
		Suggestions []model.Suggestion `json:"suggestions"`
	}
	if err := json.Unmarshal(data, &partial); err == nil && partial.Suggestions != nil {
		m.suggestions = partial.Suggestions
		m.fingerprint = nil
		return nil
	}

	return fmt.Errorf("suggestions.json: %w", err)
}

func (m *Manager) save() error {
	env := model.SuggestionsFile{
		Suggestions: m.suggestions,
		Fingerprint: m.fingerprint,
	}
	if !m.generated.IsZero() {
		env.Generated = m.generated.UTC().Format(time.RFC3339)
	}
	if env.Suggestions == nil {
		env.Suggestions = []model.Suggestion{}
	}
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	// Write to temp then rename for atomicity.
	tmpPath := m.cacheFilePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, m.cacheFilePath)
}
