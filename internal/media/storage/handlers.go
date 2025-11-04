package storage

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/media/thumbnail"
	"github.com/baalimago/kinoview/internal/model"
)

// handleImageItem by:
// 1. Checking if thumbnail exists
// 2. Adding thumbnail if it does
// 3. Creating thumbnail if it doesnt
//
// Exceptions: If the image is a thumbnail itself, then
// set the thumbnail to itself and return
func (s *store) handleImageItem(i *model.Item) error {
	if thumbnail.IsThumbnail(i.Path) {
		img, err := thumbnail.LoadImage(i.Path)
		if err != nil {
			return fmt.Errorf("load existing thumb: %w", err)
		}
		i.Thumbnail = img
		return nil
	}

	thumbPath := thumbnail.GetThumbnailPath(i.Path)
	if _, err := os.Stat(thumbPath); err == nil {
		img, thumbErr := thumbnail.LoadImage(thumbPath)
		if thumbErr != nil {
			return fmt.Errorf("load existing thumb: %w", thumbErr)
		}
		i.Thumbnail = img
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat thumb: %w", err)
	}

	img, err := thumbnail.CreateThumbnail(*i)
	if err != nil {
		return fmt.Errorf("create thumb: %w", err)
	}
	i.Thumbnail = img
	return nil
}

func (s *store) handleVideoItem(i model.Item) error {
	s.AddToClassificationQueue(i)
	return nil
}

// handlePaginatedRequest by:
// 1. Verifying that Start is a positive number less than totalAm
// 2. Read start, am, mime from URL path parameters
// 3. Verify that start is a positive number less than totalAm
// 4. Set requested am to totalAm if it's larger
// 5. Unarshal into model.PaginatedRequest
func handlePaginatedRequest(
	totalAm int,
	r *http.Request,
) (model.PaginatedRequest, error) {
	if r == nil {
		return model.PaginatedRequest{}, fmt.Errorf("nil request")
	}
	startStr := r.URL.Query().Get("start")
	amStr := r.URL.Query().Get("am")
	if startStr == "" || amStr == "" {
		return model.PaginatedRequest{}, fmt.Errorf("missing start: '%v', or am: '%v'", startStr, amStr)
	}
	start, err := strconv.Atoi(startStr)
	if err != nil {
		return model.PaginatedRequest{},
			fmt.Errorf("invalid start: '%v', err is %w", startStr, err)
	}
	am, err := strconv.Atoi(amStr)
	if err != nil {
		return model.PaginatedRequest{},
			fmt.Errorf("invalid am: %w", err)
	}
	if start < 0 || start >= totalAm {
		return model.PaginatedRequest{},
			fmt.Errorf("invalid start: '%v'", startStr)
	}
	mime := r.URL.Query().Get("mime")
	retAm := start + am
	if retAm >= totalAm {
		retAm = totalAm
	}
	return model.PaginatedRequest{
		Start:    start,
		Am:       retAm,
		MIMEType: mime,
	}, nil
}

// ListHandlerFunc returns a list of all available items in the gallery
func (s *store) ListHandlerFunc() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.cacheMu.RLock()
		defer s.cacheMu.RUnlock()
		if s.cache == nil {
			http.Error(w, "store not initialized", http.StatusInternalServerError)
			return
		}
		paginatedRequest, err := handlePaginatedRequest(len(s.cache), r)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to handle paginated request: %v", err), http.StatusBadRequest)
		}
		items := make([]model.Item, 0, paginatedRequest.Am)
		keys := make([]string, 0, len(s.cache))
		for key, v := range s.cache {
			if paginatedRequest.MIMEType != "" && !strings.Contains(v.MIMEType, paginatedRequest.MIMEType) {
				continue
			}
			keys = append(keys, key)
		}
		slices.Sort(keys)
		i := paginatedRequest.Start
		end := paginatedRequest.Am
		if len(keys) < end {
			end = len(keys)
		}
		for ; i < end; i++ {
			items = append(items, s.cache[keys[i]])
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(model.PaginatedResponse[model.Item]{
			Total: len(keys),
			Start: paginatedRequest.Start,
			End:   i,
			Items: items,
		}); err != nil {
			http.Error(w, "failed to encode items", http.StatusInternalServerError)
		}
	}
}

// VideoHandlerFunc returns a handler to get a video by ID, if item is not a video
// it will return 404
func (s *store) VideoHandlerFunc() http.HandlerFunc {
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
func (s *store) SubsListHandlerFunc() http.HandlerFunc {
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

func (s *store) SubsHandlerFunc() http.HandlerFunc {
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

func (s *store) ImageHandlerFunc() http.HandlerFunc {
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

		if !strings.Contains(mimeType, "image") {
			http.Error(w, "media found, but its not an image", http.StatusNotFound)
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

		modTime := info.ModTime()
		http.ServeContent(w, r, item.Name, modTime, file)
	}
}
