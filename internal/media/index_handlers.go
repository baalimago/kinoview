package media

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/go_away_boilerplate/pkg/debug"
	"github.com/baalimago/kinoview/internal/agents/butler"
	"github.com/baalimago/kinoview/internal/model"
	"golang.org/x/net/websocket"
)

// recomendHandler which returns a media recommendation from the store based
// on the user request
func (i *Indexer) recomendHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		defer r.Body.Close()
		lr := io.LimitReader(r.Body, 1<<20)
		dec := json.NewDecoder(lr)
		dec.DisallowUnknownFields()
		var req model.UserRequest
		if err := dec.Decode(&req); err != nil {
			http.Error(w,
				fmt.Sprintf("invalid json: %v, err: %v", debug.IndentedJsonFmt(req), err),
				http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.Request) == "" {
			http.Error(w, "empty request", http.StatusBadRequest)
			return
		}
		goCtx := r.Context()
		items := i.store.Snapshot()
		it, err := i.recommender.Recommend(goCtx, debug.IndentedJsonFmt(req), items)
		if err != nil {
			ancli.Errf("recommender failed: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := json.NewEncoder(w).Encode(it); err != nil {
			http.Error(w, "failed to encode json", http.StatusInternalServerError)
			return
		}
	}
}

// eventStream is bidirectional via websocket sending events
func (i *Indexer) eventStream() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s := websocket.Server{
			Handler: websocket.Handler(i.handleWebsocketConnection),
		}
		s.ServeHTTP(w, r)
	}
}

// triggerCascade attempts to start a butler suggestion cascade, subject to
// single-flight, debounce, and empty-context guards. Called from both
// handleConnect (websocket open) and handleDisconnect (websocket close).
func (i *Indexer) triggerCascade(reason disconnectReason) {
	if i.butler == nil {
		return
	}
	if i.clientContextMgr == nil {
		ancli.Warnf("user context manager not set; skipping butler suggestions")
		return
	}

	// Empty-context guard: if there is no viewing history and no last-played
	// name, the butler has nothing to personalise from.
	clientCtx := i.latestClientContext()
	if len(clientCtx.ViewingHistory) == 0 && clientCtx.LastPlayedName == "" {
		ancli.Noticef("cascade (%s): empty context, skipping", reason)
		return
	}

	// Single-flight and debounce share the same lock so that in-flight
	// coalescing and post-completion debounce are atomic with respect to
	// each other.
	i.butlerMu.Lock()

	if i.butlerInFlight {
		if !i.butlerRerunConsumed {
			i.butlerRerunRequested = true
		}
		i.butlerMu.Unlock()
		ancli.Noticef("cascade (%s): in flight, rerun requested", reason)
		return
	}

	// Debounce: suppress triggers within the configured window after a
	// cascade has completed.
	now := i.clock()
	if i.butlerDebounce > 0 {
		if elapsed := now.Sub(i.butlerLastCascadeAt); elapsed < i.butlerDebounce {
			remaining := i.butlerDebounce - elapsed
			i.butlerMu.Unlock()
			ancli.Noticef("cascade (%s): debounced, %v remaining", reason, remaining.Round(time.Millisecond))
			return
		}
	}

	i.butlerInFlight = true
	i.butlerLastCascadeAt = now
	i.butlerRerunConsumed = false
	gen := i.cascadeGen.Add(1)
	i.butlerMu.Unlock()

	go i.runCascade(reason, clientCtx, gen)
}

// latestClientContext returns the most recent stored client context, or the
// zero value when nothing has been stored yet. The context manager may be nil
// only in tests; production always wires one.
func (i *Indexer) latestClientContext() model.ClientContext {
	if i.clientContextMgr == nil {
		return model.ClientContext{}
	}
	contexts := i.clientContextMgr.AllClientContexts()
	if len(contexts) == 0 {
		return model.ClientContext{}
	}
	return contexts[len(contexts)-1]
}

func (i *Indexer) handleDisconnect(reason disconnectReason) {
	i.triggerCascade(reason)
}

// handleConnect triggers a suggestion cascade when a websocket client connects.
// This is the mechanism that makes suggestions available on arrival — previously
// they were only computed on disconnect (one session behind).
func (i *Indexer) handleConnect() {
	i.triggerCascade(reasonConnect)
}

// runCascade executes a single butler suggestion cascade. It runs in its own
// goroutine with a 1-minute timeout. On completion it honours any coalesced
// rerun request (with a fresh debounce check). The caller must have already
// set butlerInFlight = true.
func (i *Indexer) runCascade(reason disconnectReason, clientCtx model.ClientContext, gen int64) {
	defer func() {
		if r := recover(); r != nil {
			ancli.Errf("butler cascade panicked (%s): %v", reason, r)
		}

		i.butlerMu.Lock()

		// If the generation has advanced, another triggerCascade has
		// already taken over the flight slot. Our defer must not touch
		// butlerInFlight or schedule a rerun — the owning cascade
		// will handle both.
		if i.cascadeGen.Load() != gen {
			i.butlerMu.Unlock()
			return
		}

		i.butlerInFlight = false
		rerun := i.butlerRerunRequested
		i.butlerRerunRequested = false

		if rerun {
			// At most one rerun is coalesced per original cascade.
			i.butlerRerunConsumed = true
			// Re-check debounce before firing the rerun.
			now := i.clock()
			if i.butlerDebounce > 0 && now.Sub(i.butlerLastCascadeAt) < i.butlerDebounce {
				i.butlerMu.Unlock()
				return
			}
			// Re-read the client context: a trigger coalesced while the
			// cascade was in flight may carry a new viewing session, and
			// the rerun must not serve the previous session's cached
			// suggestions against it. An empty latest context skips, as a
			// fresh trigger would.
			clientCtx := i.latestClientContext()
			if len(clientCtx.ViewingHistory) == 0 && clientCtx.LastPlayedName == "" {
				i.butlerMu.Unlock()
				ancli.Noticef("cascade (%s): rerun skipped, empty context", reason)
				return
			}
			i.butlerLastCascadeAt = now
			i.butlerInFlight = true
			i.butlerMu.Unlock()
			go i.runCascade(reason, clientCtx, gen)
		} else {
			i.butlerMu.Unlock()
		}
	}()

	ancli.Okf("cascade (%s): prepping suggestions", reason)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	allItems := i.store.Snapshot()
	var videos []model.Item
	for _, it := range allItems {
		if strings.Contains(it.MIMEType, "video") {
			videos = append(videos, it)
		}
	}

	// Cache check: if the library and context haven't changed since the
	// last run, and the TTL hasn't expired, serve the cached result.
	if i.butlerCacheTTL > 0 {
		now := i.clock()
		fp := model.SuggestionFingerprint{
			Library: computeLibraryFingerprint(videos),
			Context: computeContextFingerprint(clientCtx, now),
			Version: butler.SuggestionFingerprintVersion,
		}
		cachedFP := i.suggestions.Fingerprint()
		generated := i.suggestions.Generated()
		if cachedFP != nil &&
			cachedFP.Library == fp.Library &&
			cachedFP.Context == fp.Context &&
			cachedFP.Version == fp.Version &&
			!generated.IsZero() &&
			now.Sub(generated) >= 0 &&
			now.Sub(generated) < i.butlerCacheTTL {
			ancli.Okf("cascade (%s): cache hit, skipping butler (age: %v)", reason, now.Sub(generated).Round(time.Second))
			return
		}
	}

	recs, err := i.butler.PrepSuggestions(ctx, clientCtx, videos)
	if err != nil {
		ancli.Warnf("Butler failed to prep suggestions: %v", err)
		return
	}

	// Only cache non-empty, successful results.
	var generated time.Time
	if i.butlerCacheTTL > 0 && len(recs) > 0 {
		now := i.clock()
		fp := model.SuggestionFingerprint{
			Library: computeLibraryFingerprint(videos),
			Context: computeContextFingerprint(clientCtx, now),
			Version: butler.SuggestionFingerprintVersion,
		}
		generated = now
		err = i.suggestions.UpdateWithFingerprint(recs, fp, now)
	} else {
		err = i.suggestions.Update(recs)
		generated = i.clock()
	}
	if err != nil {
		ancli.Warnf("failed to update suggestions: %v", err)
		return
	}
	ancli.Okf("Stored %d suggestions from Butler", len(recs))

	// Push to connected websocket clients so they re-render live.
	payload := model.SuggestionsPayload{
		State:       "available",
		Suggestions: enrichSuggestions(recs),
		Generated:   generated.UTC().Format(time.RFC3339),
	}
	i.broadcastSuggestions(payload)
}

func (i *Indexer) suggestionsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Check if cascade is in flight; read butlerInFlight behind the mutex.
		i.butlerMu.Lock()
		computing := i.butlerInFlight
		i.butlerMu.Unlock()

		recs := i.suggestions.Get()
		if recs == nil {
			recs = []model.Suggestion{}
		}

		generated := ""
		if t := i.suggestions.Generated(); !t.IsZero() {
			generated = t.UTC().Format(time.RFC3339)
		}

		state := "empty"
		if len(recs) > 0 {
			state = "available"
		} else if computing {
			state = "computing"
		}

		payload := model.SuggestionsPayload{
			State:       state,
			Suggestions: enrichSuggestions(recs),
			Generated:   generated,
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			ancli.Errf("failed to encode suggestions: %v", err)
		}
	}
}
