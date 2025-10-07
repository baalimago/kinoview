package media

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/go_away_boilerplate/pkg/debug"
	"github.com/baalimago/kinoview/internal/model"
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
