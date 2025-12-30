package tools

import (
	"strings"
	"testing"
	"time"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/model"
)

type mockUserContextManager struct {
	contexts []model.ClientContext
}

func (m *mockUserContextManager) AllClientContexts() []model.ClientContext {
	return m.contexts
}

func (m *mockUserContextManager) StoreClientContext(ctx model.ClientContext) error {
	m.contexts = append(m.contexts, ctx)
	return nil
}

func TestUserContextIntegration_BasicRetrieval(t *testing.T) {
	// Create a simple ClientContext with known fields
	ctx1 := model.ClientContext{
		ViewingHistory: []model.ViewMetadata{
			{
				Name:         "Movie A",
				ViewedAt:     time.Now().Add(-2 * time.Hour),
				PlayedForSec: "1:30:45",
			},
		},
		LastPlayedName: "Movie A",
	}

	mgr := &mockUserContextManager{
		contexts: []model.ClientContext{ctx1},
	}

	tool, err := NewUserContextGetter(mgr)
	if err != nil {
		t.Fatalf("NewUserContextGetter: %v", err)
	}

	// Get summary
	respStr, err := tool.Call(models.Input{"mode": "summary"})
	if err != nil {
		t.Fatalf("Call summary: %v", err)
	}

	if respStr == "" {
		t.Fatalf("expected non-empty response")
	}
	if strings.Contains(respStr, "unknown mode") {
		t.Fatalf("unexpected error in response: %s", respStr)
	}
}

func TestUserContextIntegration_EmptyContexts(t *testing.T) {
	mgr := &mockUserContextManager{
		contexts: []model.ClientContext{},
	}

	tool, err := NewUserContextGetter(mgr)
	if err != nil {
		t.Fatalf("NewUserContextGetter: %v", err)
	}

	respStr, err := tool.Call(models.Input{})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	if !strings.Contains(respStr, "no user contexts have been recorded") {
		t.Fatalf("expected 'no contexts' message, got: %s", respStr)
	}
}

func TestUserContextIntegration_Pagination(t *testing.T) {
	// Create 5 contexts with different timestamps
	contexts := make([]model.ClientContext, 5)
	baseTime := time.Now()

	for i := 0; i < 5; i++ {
		contexts[i] = model.ClientContext{
			ViewingHistory: []model.ViewMetadata{
				{
					Name:     "Movie " + string(rune('A'+i)),
					ViewedAt: baseTime.Add(time.Duration(i) * time.Hour),
				},
			},
		}
	}

	mgr := &mockUserContextManager{contexts: contexts}

	tool, err := NewUserContextGetter(mgr)
	if err != nil {
		t.Fatalf("NewUserContextGetter: %v", err)
	}

	// Get first 2 contexts
	respStr, err := tool.Call(models.Input{"limit": float64(2), "offset": float64(0)})
	if err != nil {
		t.Fatalf("Call with limit 2: %v", err)
	}

	if respStr == "" {
		t.Fatalf("expected non-empty response")
	}

	// Get next 2 contexts
	respStr, err = tool.Call(models.Input{"limit": float64(2), "offset": float64(2)})
	if err != nil {
		t.Fatalf("Call with offset 2: %v", err)
	}

	if respStr == "" {
		t.Fatalf("expected non-empty response")
	}

	// Offset past end
	respStr, err = tool.Call(models.Input{"offset": float64(10)})
	if err != nil {
		t.Fatalf("Call with large offset: %v", err)
	}

	if !strings.Contains(respStr, "no results") {
		t.Fatalf("expected 'no results' message, got: %s", respStr)
	}
}

func TestUserContextIntegration_MostRecentMode(t *testing.T) {
	// Create multiple contexts
	ctx1 := model.ClientContext{
		ViewingHistory: []model.ViewMetadata{
			{Name: "Old Movie", ViewedAt: time.Now().Add(-24 * time.Hour)},
		},
	}

	ctx2 := model.ClientContext{
		ViewingHistory: []model.ViewMetadata{
			{Name: "Recent Movie", ViewedAt: time.Now().Add(-1 * time.Hour)},
		},
	}

	mgr := &mockUserContextManager{
		contexts: []model.ClientContext{ctx1, ctx2},
	}

	tool, err := NewUserContextGetter(mgr)
	if err != nil {
		t.Fatalf("NewUserContextGetter: %v", err)
	}

	// Get most recent
	respStr, err := tool.Call(models.Input{"mode": "most_recent"})
	if err != nil {
		t.Fatalf("Call most_recent: %v", err)
	}

	if respStr == "" {
		t.Fatalf("expected non-empty response")
	}
}

func TestUserContextIntegration_SessionsMode(t *testing.T) {
	// Create contexts with session information
	ctx1 := model.ClientContext{
		ViewingHistory: []model.ViewMetadata{
			{Name: "Movie 1", ViewedAt: time.Now().Add(-2 * time.Hour)},
		},
	}

	ctx2 := model.ClientContext{
		ViewingHistory: []model.ViewMetadata{
			{Name: "Movie 2", ViewedAt: time.Now().Add(-1 * time.Hour)},
		},
	}

	mgr := &mockUserContextManager{
		contexts: []model.ClientContext{ctx1, ctx2},
	}

	tool, err := NewUserContextGetter(mgr)
	if err != nil {
		t.Fatalf("NewUserContextGetter: %v", err)
	}

	// Get sessions
	respStr, err := tool.Call(models.Input{"mode": "sessions"})
	if err != nil {
		t.Fatalf("Call sessions: %v", err)
	}

	if !strings.Contains(respStr, "sessions:") {
		t.Fatalf("expected 'sessions:' in response, got: %s", respStr)
	}
}

func TestUserContextIntegration_ViewedMode(t *testing.T) {
	// Create contexts with viewing history
	ctx1 := model.ClientContext{
		ViewingHistory: []model.ViewMetadata{
			{Name: "Action Movie", ViewedAt: time.Now().Add(-2 * time.Hour)},
			{Name: "Drama Movie", ViewedAt: time.Now().Add(-1 * time.Hour)},
		},
	}

	ctx2 := model.ClientContext{
		ViewingHistory: []model.ViewMetadata{
			{Name: "Comedy Movie", ViewedAt: time.Now()},
		},
	}

	mgr := &mockUserContextManager{
		contexts: []model.ClientContext{ctx1, ctx2},
	}

	tool, err := NewUserContextGetter(mgr)
	if err != nil {
		t.Fatalf("NewUserContextGetter: %v", err)
	}

	// Get viewed
	respStr, err := tool.Call(models.Input{"mode": "viewed"})
	if err != nil {
		t.Fatalf("Call viewed: %v", err)
	}

	if !strings.Contains(respStr, "viewed per session:") {
		t.Fatalf("expected 'viewed per session:' in response, got: %s", respStr)
	}
}

func TestUserContextIntegration_InvalidMode(t *testing.T) {
	mgr := &mockUserContextManager{
		contexts: []model.ClientContext{
			{},
		},
	}

	tool, err := NewUserContextGetter(mgr)
	if err != nil {
		t.Fatalf("NewUserContextGetter: %v", err)
	}

	respStr, err := tool.Call(models.Input{"mode": "invalid_mode"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	if !strings.Contains(respStr, "unknown mode") {
		t.Fatalf("expected 'unknown mode' error, got: %s", respStr)
	}
}

func TestUserContextIntegration_DefaultMode(t *testing.T) {
	ctx := model.ClientContext{
		ViewingHistory: []model.ViewMetadata{
			{Name: "Movie", ViewedAt: time.Now()},
		},
	}

	mgr := &mockUserContextManager{
		contexts: []model.ClientContext{ctx},
	}

	tool, err := NewUserContextGetter(mgr)
	if err != nil {
		t.Fatalf("NewUserContextGetter: %v", err)
	}

	// Call without mode - should default to summary
	respStr, err := tool.Call(models.Input{})
	if err != nil {
		t.Fatalf("Call without mode: %v", err)
	}

	if respStr == "" {
		t.Fatalf("expected non-empty response")
	}
}

func TestUserContextIntegration_DefaultLimit(t *testing.T) {
	// Create 10 contexts
	contexts := make([]model.ClientContext, 10)
	for i := 0; i < 10; i++ {
		contexts[i] = model.ClientContext{
			ViewingHistory: []model.ViewMetadata{
				{Name: "Movie " + string(rune('A'+i)), ViewedAt: time.Now()},
			},
		}
	}

	mgr := &mockUserContextManager{contexts: contexts}

	tool, err := NewUserContextGetter(mgr)
	if err != nil {
		t.Fatalf("NewUserContextGetter: %v", err)
	}

	// Call without limit - should default to 5
	respStr, err := tool.Call(models.Input{})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	if respStr == "" {
		t.Fatalf("expected non-empty response")
	}
}

func TestUserContextIntegration_NilManager(t *testing.T) {
	_, err := NewUserContextGetter(nil)
	if err == nil {
		t.Fatalf("expected error for nil manager, got nil")
	}
}

func TestUserContextIntegration_ReflectionFieldDiscovery(t *testing.T) {
	// Test that the tool can handle various field names through reflection
	ctx := model.ClientContext{
		ViewingHistory: []model.ViewMetadata{
			{Name: "Test Movie", ViewedAt: time.Now()},
		},
		LastPlayedName: "Test Movie",
	}

	mgr := &mockUserContextManager{
		contexts: []model.ClientContext{ctx},
	}

	tool, err := NewUserContextGetter(mgr)
	if err != nil {
		t.Fatalf("NewUserContextGetter: %v", err)
	}

	respStr, err := tool.Call(models.Input{})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	if respStr == "" {
		t.Fatalf("expected non-empty response")
	}
}

func TestUserContextIntegration_MultipleContextsOrdering(t *testing.T) {
	// Create contexts with different timestamps
	ctx1 := model.ClientContext{
		ViewingHistory: []model.ViewMetadata{
			{Name: "Movie 1", ViewedAt: time.Now().Add(-3 * time.Hour)},
		},
	}

	ctx2 := model.ClientContext{
		ViewingHistory: []model.ViewMetadata{
			{Name: "Movie 2", ViewedAt: time.Now().Add(-1 * time.Hour)},
		},
	}

	ctx3 := model.ClientContext{
		ViewingHistory: []model.ViewMetadata{
			{Name: "Movie 3", ViewedAt: time.Now()},
		},
	}

	mgr := &mockUserContextManager{
		contexts: []model.ClientContext{ctx1, ctx2, ctx3},
	}

	tool, err := NewUserContextGetter(mgr)
	if err != nil {
		t.Fatalf("NewUserContextGetter: %v", err)
	}

	respStr, err := tool.Call(models.Input{})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	if respStr == "" {
		t.Fatalf("expected non-empty response")
	}
}

func TestUserContextIntegration_ViewingHistoryWithMultipleMovies(t *testing.T) {
	// Create context with multiple viewing entries
	ctx := model.ClientContext{
		ViewingHistory: []model.ViewMetadata{
			{Name: "Movie A", ViewedAt: time.Now().Add(-3 * time.Hour), PlayedForSec: "1:30:00"},
			{Name: "Movie B", ViewedAt: time.Now().Add(-2 * time.Hour), PlayedForSec: "2:00:00"},
			{Name: "Movie C", ViewedAt: time.Now().Add(-1 * time.Hour), PlayedForSec: "1:45:00"},
		},
		LastPlayedName: "Movie C",
	}

	mgr := &mockUserContextManager{
		contexts: []model.ClientContext{ctx},
	}

	tool, err := NewUserContextGetter(mgr)
	if err != nil {
		t.Fatalf("NewUserContextGetter: %v", err)
	}

	// Get viewed mode
	respStr, err := tool.Call(models.Input{"mode": "viewed"})
	if err != nil {
		t.Fatalf("Call viewed: %v", err)
	}

	if respStr == "" {
		t.Fatalf("expected non-empty response")
	}
}

func TestUserContextIntegration_LargeLimitValue(t *testing.T) {
	// Create 3 contexts
	contexts := make([]model.ClientContext, 3)
	for i := 0; i < 3; i++ {
		contexts[i] = model.ClientContext{
			ViewingHistory: []model.ViewMetadata{
				{Name: "Movie " + string(rune('A'+i)), ViewedAt: time.Now()},
			},
		}
	}

	mgr := &mockUserContextManager{contexts: contexts}

	tool, err := NewUserContextGetter(mgr)
	if err != nil {
		t.Fatalf("NewUserContextGetter: %v", err)
	}

	// Request with limit larger than available
	respStr, err := tool.Call(models.Input{"limit": float64(100)})
	if err != nil {
		t.Fatalf("Call with large limit: %v", err)
	}

	if respStr == "" {
		t.Fatalf("expected non-empty response")
	}
}

func TestUserContextIntegration_NegativeLimitAndOffset(t *testing.T) {
	ctx := model.ClientContext{
		ViewingHistory: []model.ViewMetadata{
			{Name: "Movie", ViewedAt: time.Now()},
		},
	}

	mgr := &mockUserContextManager{
		contexts: []model.ClientContext{ctx},
	}

	tool, err := NewUserContextGetter(mgr)
	if err != nil {
		t.Fatalf("NewUserContextGetter: %v", err)
	}

	// Negative limit should be ignored, use default
	respStr, err := tool.Call(models.Input{"limit": float64(-5), "offset": float64(-1)})
	if err != nil {
		t.Fatalf("Call with negative values: %v", err)
	}

	if respStr == "" {
		t.Fatalf("expected non-empty response")
	}
}
