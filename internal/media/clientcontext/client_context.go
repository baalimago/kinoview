package clientcontext

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/go_away_boilerplate/pkg/debug"
	"github.com/baalimago/go_away_boilerplate/pkg/misc"
	"github.com/baalimago/kinoview/internal/model"
)

// Manager stores client contexts in-memory and persists them incrementally to disk.
// Uses append-only JSONL format for incremental updates.
type Manager struct {
	mu       sync.Mutex
	logPath  string
	contexts []model.ClientContext
	debug    bool
}

// New creates a new user context manager.
func New(cacheDir string) (*Manager, error) {
	if cacheDir == "" {
		return nil, errors.New("cacheDir can't be nil")
	}
	p := filepath.Join(cacheDir, "client", "context.log")
	m := &Manager{logPath: p}
	if err := m.load(); err != nil {
		return nil, err
	}

	if misc.Truthy(os.Getenv("DEBUG")) || misc.Truthy(os.Getenv("DEBUG_CLIENT_CONTEXT")) {
		m.debug = true
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
func (m *Manager) StoreClientContext(clientCtx model.ClientContext) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.debug {
		ancli.Okf("Attempting to store:\n%v", debug.IndentedJsonFmt(clientCtx))
	}

	err := m.appendToLogLocked(clientCtx)
	if err != nil {
		return err
	}
	m.contexts = append(m.contexts, clientCtx)
	return nil
}

// separateDelta from the clientCtx so that only the updated ViewHistory is stored instead of the
// entire client object. Uses the existing context to separate the minimum delta between the current
// client contexts and the new appending one.
//
// If there is no viewing history change, it returns (zeroDelta, false, nil).
func (m *Manager) separateDelta(clientCxt model.ClientContext) (model.ClientContextDelta, bool, error) {
	if clientCxt.SessionID == "" {
		return model.ClientContextDelta{}, false, fmt.Errorf("missing session id")
	}

	// We call this from StoreClientContext after appending clientCxt to m.contexts.
	// Therefore, the prior state for this session is the latest context before the last append.
	var existing *model.ClientContext
	for i := len(m.contexts) - 2; i >= 0; i-- {
		if m.contexts[i].SessionID == clientCxt.SessionID {
			existing = &m.contexts[i]
			break
		}
	}
	if m.debug {
		ancli.Okf("existing: %v", debug.IndentedJsonFmt(existing))
	}

	allViewHistory := make(map[string]string, 0)
	for _, c := range m.contexts {
		for _, v := range c.ViewingHistory {
			allViewHistory[v.Name] = v.PlayedForSec
		}
	}

	changed := make([]model.ViewMetadata, 0)
	for _, vh := range clientCxt.ViewingHistory {
		dur, exists := allViewHistory[vh.Name]
		if !exists {
			changed = append(changed, vh)
			continue
		}

		if dur != vh.PlayedForSec {
			changed = append(changed, vh)
		}
	}

	if m.debug {
		ancli.Okf("changed: %v", debug.IndentedJsonFmt(changed))
	}
	if len(changed) == 0 {
		return model.ClientContextDelta{}, false, nil
	}

	return model.ClientContextDelta{
		SessionID:      clientCxt.SessionID,
		ViewingHistory: changed,
	}, true, nil
}

func viewMetadataEqual(a, b model.ViewMetadata) bool {
	if a.Name != b.Name {
		return false
	}
	if a.PlayedForSec != b.PlayedForSec {
		return false
	}
	// ViewedAt equality: we expect exact timestamps when coming from the same source.
	// Use Equal to be safe re: monotonic clock.
	if !a.ViewedAt.Equal(b.ViewedAt) {
		return false
	}
	return true
}

// appendToLogLocked writes a single context entry to the log in JSONL format.
func (m *Manager) appendToLogLocked(clientCtx model.ClientContext) error {
	dir := filepath.Dir(m.logPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir user context dir: %w", err)
	}

	delta, ok, err := m.separateDelta(clientCtx)
	if err != nil {
		return fmt.Errorf("failed to separate: %v", err)
	}
	if !ok {
		// No viewing history changes => do not persist.
		return nil
	}

	b, err := json.Marshal(delta)
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

// mergeDeltas combines ClientContextDeltas by SessionID and
// reconstructs ClientContext with metadata (TimeOfDay, LastPlayedName,
// StartTime) derived from ViewingHistory.
func mergeDeltas(deltas []model.ClientContextDelta) []model.ClientContext {
	// We need to support "upserts" (per-item updates) for a session.
	// Therefore, we replay deltas in order and maintain a per-session index by Name.
	type acc struct {
		order  []string
		byName map[string]model.ViewMetadata
	}

	accs := make(map[string]*acc)
	seen := make(map[string]bool, len(deltas))
	var sessionOrder []string

	for _, d := range deltas {
		if !seen[d.SessionID] {
			seen[d.SessionID] = true
			sessionOrder = append(sessionOrder, d.SessionID)
		}
		a := accs[d.SessionID]
		if a == nil {
			a = &acc{byName: make(map[string]model.ViewMetadata)}
			accs[d.SessionID] = a
		}

		for _, vm := range d.ViewingHistory {
			if _, ok := a.byName[vm.Name]; !ok {
				a.order = append(a.order, vm.Name)
			}
			a.byName[vm.Name] = vm
		}
	}

	var contexts []model.ClientContext
	for _, sid := range sessionOrder {
		a := accs[sid]
		ctx := model.ClientContext{SessionID: sid}
		for _, name := range a.order {
			ctx.ViewingHistory = append(ctx.ViewingHistory, a.byName[name])
		}

		if len(ctx.ViewingHistory) > 0 {
			earliestTime := ctx.ViewingHistory[0].ViewedAt
			latestTime := ctx.ViewingHistory[0].ViewedAt
			latestIdx := 0
			for i, vm := range ctx.ViewingHistory {
				if vm.ViewedAt.Before(earliestTime) {
					earliestTime = vm.ViewedAt
				}
				if vm.ViewedAt.After(latestTime) {
					latestTime = vm.ViewedAt
					latestIdx = i
				}
			}
			ctx.StartTime = earliestTime
			ctx.LastPlayedName = ctx.ViewingHistory[latestIdx].Name
		}
		contexts = append(contexts, ctx)
	}
	return contexts
}

// load reads the entire log and replays it into memory.
func (m *Manager) load() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.debug {
		ancli.Okf("loading contexts")
	}

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

	var deltas []model.ClientContextDelta
	for _, line := range bytes.Split(bytes.TrimSpace(b), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var ctx model.ClientContextDelta
		if err := json.Unmarshal(line, &ctx); err != nil {
			return fmt.Errorf("unmarshal user context log entry: %w", err)
		}
		deltas = append(deltas, ctx)
	}

	m.contexts = mergeDeltas(deltas)
	if m.debug {
		ancli.Okf("loaded contexts:%v", debug.IndentedJsonFmt(m.contexts))
	}
	return nil
}
