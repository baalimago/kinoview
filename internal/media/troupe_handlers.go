package media

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/agents/troupe"
)

// The /api/v1/troupe surface (decision 20): the play API pages over
// plays/index.json and serves resolved plays from disk — never an in-memory
// story — and the feedback endpoint records unified audience notes into
// feedback/, one file per note. The routes are mounted by serve only when
// the troupe is wired (TroupeHandler returns nil otherwise), so a missing
// notebook or model leaves the whole surface unmounted: 404.

// troupePlayIndexHandler serves GET /api/v1/troupe/play: one keyset-paginated
// page of the play index, honouring limit/order/status/author. The response
// carries the page's entries and the next cursor (empty on the last page).
func (i *Indexer) troupePlayIndexHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		query := troupe.PlayPageQuery{
			Limit:  parseIntDefault(q.Get("limit"), 0),
			Order:  q.Get("order"),
			Status: q.Get("status"),
			Author: q.Get("author"),
			Cursor: q.Get("cursor"),
		}
		page, err := i.troupeLibrary.Page(query)
		if err != nil {
			ancli.Errf("troupe: play index: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(page); err != nil {
			ancli.Errf("troupe: play index: encode: %v", err)
		}
	}
}

// troupePlayResolvedHandler serves GET /api/v1/troupe/play/resolved: the
// newest submitted play, read from disk. An empty stage — no play submitted
// yet — is 404, the signal to investigate, never a generated fallback.
func (i *Indexer) troupePlayResolvedHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rp, err := i.troupeLibrary.Newest()
		if errors.Is(err, troupe.ErrPlayNotFound) {
			http.Error(w, "no play submitted yet — the stage is empty", http.StatusNotFound)
			return
		}
		if err != nil {
			ancli.Errf("troupe: newest play: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(rp); err != nil {
			ancli.Errf("troupe: newest play: encode: %v", err)
		}
	}
}

// troupePlayGetHandler serves GET /api/v1/troupe/play/{id}: one play by its
// story_<UTC> datetime id. `resolved` never reaches this route — the literal
// route wins — and the id is validated by the library.
func (i *Indexer) troupePlayGetHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rp, err := i.troupeLibrary.Get(r.PathValue("id"))
		if errors.Is(err, troupe.ErrPlayNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if err != nil {
			ancli.Errf("troupe: play %s: %v", r.PathValue("id"), err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(rp); err != nil {
			ancli.Errf("troupe: play %s: encode: %v", r.PathValue("id"), err)
		}
	}
}

// troupeFeedbackHandler serves POST /api/v1/troupe/feedback: the audience
// sends {playId, type, data}; the writer stamps ts, derives the filename and
// persists the note as one feedback/ file — write and commit are one unit,
// so a commit failure surfaces as 500, never a silent drop (implementation
// note 2). A malformed or invalid note is 400 with the exact error.
func (i *Indexer) troupeFeedbackHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()
		var req troupe.FeedbackRequest
		dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			http.Error(w, "invalid feedback body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if _, err := i.troupeFeedback.Write(req); err != nil {
			if errors.Is(err, troupe.ErrFeedbackInvalid) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			ancli.Errf("troupe: feedback: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// parseIntDefault parses a query int, returning fallback on empty or
// malformed values — the library clamps the actual page size.
func parseIntDefault(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}
