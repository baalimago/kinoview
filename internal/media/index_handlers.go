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

func (i *Indexer) handleDisconnect() {
	if i.butler == nil {
		return
	}
	i.clientCtxMu.Lock()
	clientCtx := i.lastClientContext
	i.clientCtxMu.Unlock()

	// Use background context as the request context is dead/dying
	// Use a detached thread to not block the handler return
	go func() {
		ancli.Okf("butler triggered by disconnect, prepping suggestions")
		// 1 minute timeout for butler work
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
		defer cancel()

		allItmes := i.store.Snapshot()
		var videos []model.Item
		for _, i := range allItmes {
			if strings.Contains(i.MIMEType, "video") {
				videos = append(videos, i)
			}
		}
		recs, err := i.butler.PrepSuggestions(ctx, clientCtx, videos)
		if err != nil {
			ancli.Warnf("Butler failed to prep suggestions: %v", err)
		} else {
			i.clientRecsMu.Lock()
			i.clientRecommendations = recs
			i.clientRecsMu.Unlock()
			ancli.Okf("Stored %d suggestions from Butler", len(recs))
		}
	}()
}

func (i *Indexer) suggestionsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		i.clientRecsMu.Lock()
		recs := i.clientRecommendations
		i.clientRecsMu.Unlock()

		if recs == nil {
			recs = []model.Recommendation{}
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(recs); err != nil {
			ancli.Errf("failed to encode recommendations: %v", err)
		}
	}
}
