package media

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/baalimago/kinoview/internal/media/suggestions"
	"github.com/baalimago/kinoview/internal/model"
	"golang.org/x/net/websocket"
)

type mockButler struct {
	called bool
	ctx    model.ClientContext
	recs   []model.Suggestion
}

func (m *mockButler) Setup(ctx context.Context) error {
	return nil
}

func (m *mockButler) PrepSuggestions(ctx context.Context, c model.ClientContext, items []model.Item) ([]model.Suggestion, error) {
	m.called = true
	m.ctx = c
	return m.recs, nil
}

type mockUserContextMgr struct {
	mu    sync.Mutex
	store []model.ClientContext
	ch    chan struct{}
}

func newMockUserContextMgr() *mockUserContextMgr {
	return &mockUserContextMgr{ch: make(chan struct{}, 100)}
}

func (m *mockUserContextMgr) AllClientContexts() []model.ClientContext {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]model.ClientContext(nil), m.store...)
}

func (m *mockUserContextMgr) StoreClientContext(ctx model.ClientContext) error {
	m.mu.Lock()
	m.store = append(m.store, ctx)
	m.mu.Unlock()
	select {
	case m.ch <- struct{}{}:
	default:
	}
	return nil
}

func (m *mockUserContextMgr) waitForAtLeast(n int, timeout time.Duration) bool {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		m.mu.Lock()
		l := len(m.store)
		m.mu.Unlock()
		if l >= n {
			return true
		}
		select {
		case <-m.ch:
			// try again
		case <-deadline.C:
			return false
		}
	}
}

func TestEventStreamAndSuggestions(t *testing.T) {
	// Setup
	expectedRec := model.Suggestion{
		Item:       model.Item{ID: "test-id", Name: "Test Movie"},
		Motivation: "Because you like tests",
	}
	butler := &mockButler{
		recs: []model.Suggestion{expectedRec},
	}
	ucm := newMockUserContextMgr()
	sugMgr, err := suggestions.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	idx, _ := NewIndexer(WithButler(butler), WithClientContextManager(ucm), WithSuggestionsManager(sugMgr))
	// Need to initialize store to avoid nil pointer in Snapshot called by disconnect
	idx.store = &mockStore{
		items: []model.Item{},
	}

	// Use the full handler to test routing to both /ws and /suggestions
	server := httptest.NewServer(idx.Handler())
	defer server.Close()

	// Convert http URL to ws URL (and append /ws path since we are using full handler)
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	// Connect
	ws, err := websocket.Dial(wsURL, "", "http://localhost/")
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}

	// Send context
	ctx := model.ClientContext{}
	evt := model.Event[model.ClientContext]{
		Type:    model.ClientContextEvent,
		Created: time.Now(),
		Payload: ctx,
	}
	if err := websocket.JSON.Send(ws, evt); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	// Wait for server to process and store context
	if ok := ucm.waitForAtLeast(1, time.Second); !ok {
		t.Fatalf("timeout waiting for stored context")
	}

	// Close connection to trigger butler
	ws.Close()

	// Wait for butler (handled in goroutine)
	time.Sleep(200 * time.Millisecond)

	if !butler.called {
		t.Error("Butler was not called after disconnect")
	}
	// Now check if suggestions are available via HTTP
	resp, err := http.Get(server.URL + "/suggestions")
	if err != nil {
		t.Fatalf("Failed to get suggestions: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var recs []model.Suggestion
	if err := json.NewDecoder(resp.Body).Decode(&recs); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(recs) != 1 {
		t.Fatalf("Expected 1 recommendation, got %d", len(recs))
	}
	if recs[0].ID != "test-id" {
		t.Errorf("Expected ID test-id, got %s", recs[0].ID)
	}
	if recs[0].Motivation != "Because you like tests" {
		t.Errorf("Expected motivation 'Because you like tests', got '%s'", recs[0].Motivation)
	}
}
