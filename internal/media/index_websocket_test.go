package media

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestEventStreamAndSuggestions(t *testing.T) {
	// Setup
	expectedRec := model.Suggestion{
		Item:       model.Item{ID: "test-id", Name: "Test Movie"},
		Motivation: "Because you like tests",
	}
	butler := &mockButler{
		recs: []model.Suggestion{expectedRec},
	}
	idx, _ := NewIndexer(WithButler(butler))
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
	ctx := model.ClientContext{
		TimeOfDay: "Evening",
	}
	evt := model.Event[model.ClientContext]{
		Type:    model.ClientContextEvent,
		Created: time.Now(),
		Payload: ctx,
	}
	if err := websocket.JSON.Send(ws, evt); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	// Wait a bit for server to process
	time.Sleep(100 * time.Millisecond)

	// Verify context updated
	idx.clientCtxMu.Lock()
	got := idx.lastClientContext
	idx.clientCtxMu.Unlock()
	if got.TimeOfDay != "Evening" {
		t.Errorf("Want Evening, got %s", got.TimeOfDay)
	}

	// Close connection to trigger butler
	ws.Close()

	// Wait for butler (handled in goroutine)
	time.Sleep(200 * time.Millisecond)

	if !butler.called {
		t.Error("Butler was not called after disconnect")
	}
	if butler.ctx.TimeOfDay != "Evening" {
		t.Errorf("Butler got wrong context: %v", butler.ctx)
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
