package storage

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/model"
)

// ListHandlerFunc returns a list of all available items in the gallery
func (s *jsonStore) ListHandlerFunc() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.cacheMu.RLock()
		defer s.cacheMu.RUnlock()
		if s.cache == nil {
			http.Error(w, "store not initialized", http.StatusInternalServerError)
			return
		}
		items := make([]model.Item, 0, len(s.cache))
		for _, v := range s.cache {
			items = append(items, v)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(items); err != nil {
			http.Error(w, "failed to encode items", http.StatusInternalServerError)
		}
	}
}

// VideoHandlerFunc returns a handler to get a video by ID, if item is not a video
// it will return 404
func (s *jsonStore) VideoHandlerFunc() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			http.Error(w, "missing id", http.StatusBadRequest)
			return
		}
		s.cacheMu.RLock()
		item, ok := s.cache[id]
		s.cacheMu.RUnlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		pathToMedia := item.Path
		mimeType := item.MIMEType

		if !strings.Contains(mimeType, "video") {
			http.Error(w, "media found, but its not a video", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", mimeType)
		file, err := os.Open(pathToMedia)
		if err != nil {
			http.Error(w, "media not found", http.StatusNotFound)
			return
		}
		defer file.Close()

		info, err := os.Stat(pathToMedia)
		if err != nil {
			http.Error(w, "media not found", http.StatusNotFound)
			return
		}

		// We'll see how robus this is. This rule covers all of my devices!
		if strings.HasSuffix(strings.ToLower(item.Name), ".mkv") && !strings.Contains(r.UserAgent(), "SmartTV") {
			streamMkvToMp4(w, r, pathToMedia)
			return
		}

		modTime := info.ModTime()
		http.ServeContent(w, r, item.Name, modTime, file)
	}
}

// SubsHandlerFunc by stripping out the substitle streams using ffmpeg from video media found at
// PathValue id. If there are multiple subtitle streams found, select one at random
func (s *jsonStore) SubsListHandlerFunc() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("vid")
		if id == "" {
			http.Error(w, "missing vid", http.StatusBadRequest)
			return
		}
		s.cacheMu.Lock()
		item, ok := s.cache[id]
		s.cacheMu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		if !strings.Contains(item.MIMEType, "video") {
			http.Error(w, "media found, but its not a video", http.StatusNotFound)
			return
		}

		info, err := s.subStreamFinder.find(item)
		if err != nil {
			ancli.Errf("jsonStore failed to handle SubsList when subStripper.extract, err: %v", err)
			http.Error(w, "failed to extract subtitles from media", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
		ancli.Okf("media info served")
	}
}

func (s *jsonStore) SubsHandlerFunc() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vid := r.PathValue("vid")
		if vid == "" {
			http.Error(w, "missing video id", http.StatusBadRequest)
			return
		}

		sid := r.PathValue("sub_idx")
		if sid == "" {
			http.Error(w, "missing subtitle index", http.StatusBadRequest)
			return
		}

		s.cacheMu.RLock()
		cacheFile, exists := s.cache[vid]
		s.cacheMu.RUnlock()
		if !exists {
			http.Error(w, fmt.Sprintf("cache miss for: '%v'", vid), http.StatusNotFound)
			return
		}
		subs, err := s.subStreamExtractor.extract(cacheFile, sid)
		if err != nil {
			ancli.Errf("failed to extract subs: %v", err)
			http.Error(w, "failed to extract subs", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
		http.ServeFile(w, r, subs)
	}
}
