package media

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/model"
)

type subtitleStreamFinder interface {
	// fid the media info for some item
	find(item model.Item) (MediaInfo, error)
}

type subtitleStreamExtractor interface {
	// extract the subs and return path to the file
	// containing subs. Subs are expected to be in .vtt
	// format
	extract(item model.Item, streamIndex string) (string, error)
}

type jsonStore struct {
	storePath          string
	cache              map[string]model.Item
	subStreamFinder    subtitleStreamFinder
	subStreamExtractor subtitleStreamExtractor
}

func newJSONStore() *jsonStore {
	subUtils := &ffmpegSubsUtil{
		mediaCache: map[string]MediaInfo{},
	}
	return &jsonStore{
		subStreamFinder:    subUtils,
		subStreamExtractor: subUtils,
	}
}

// Setup the jsonStore by loading 'store.json' from storeDirPath and adding all
// items found to cache
func (s *jsonStore) Setup(ctx context.Context, storeDirPath string) error {
	ancli.Noticef("setting up json store")
	s.storePath = storeDirPath
	if s.cache == nil {
		s.cache = make(map[string]model.Item)
	}
	storePath := path.Join(storeDirPath, "store.json")
	if _, err := os.Stat(storePath); os.IsNotExist(err) {
		ancli.Noticef("found no '%v', creating new", storePath)
		empty, err := os.Create(storePath)
		if err != nil {
			return fmt.Errorf("failed to create: '%v', err: %w", storePath, err)
		}
		_, err = empty.WriteString("[]")
		if err != nil {
			return fmt.Errorf("failed to write empty list into: '%v', err: %w", storePath, err)
		}
		empty.Close()
	}

	f, err := os.Open(storePath)
	if err != nil {
		return fmt.Errorf("failed to open: '%v', err: %w", storePath, err)
	}
	defer f.Close()

	var items []model.Item
	if err := json.NewDecoder(f).Decode(&items); err != nil {
		return fmt.Errorf("failed to decode []model.Item: %w", err)
	}
	for _, item := range items {
		s.cache[item.ID] = item
	}
	return nil
}

// generateID by creating a hash using sha256 on the contents of item.Path
func generateID(i model.Item) string {
	f, err := os.Open(i.Path)
	if err != nil {
		return ""
	}
	defer f.Close()

	const chunk = 256
	b := make([]byte, chunk)
	var fileBytes []byte

	n, err := f.Read(b)
	if err == nil && n > 0 {
		fileBytes = append(fileBytes, b[:n]...)
	}

	fi, err := f.Stat()
	if err == nil && fi.Size() > chunk {
		endOff := fi.Size() - chunk
		if _, err := f.Seek(endOff, 0); err == nil {
			b2 := make([]byte, chunk)
			n2, err2 := f.Read(b2)
			if err2 == nil && n2 > 0 {
				fileBytes = append(fileBytes, b2[:n2]...)
			}
		}
	}

	sum := sha256.Sum256(fileBytes)
	return fmt.Sprintf("%x", sum)[:16]
}

// Store the item in the local json store and add i to the cache
func (s *jsonStore) Store(i model.Item) error {
	hadID := i.ID != ""
	if i.ID == "" {
		i.ID = generateID(i)
	}
	existingItem, exists := s.cache[i.ID]

	// Only keep path if the item when it exists
	// yet new item lacks generated ID. This is a cheap way of not
	// overwriting existing item on re-scan since the IDs
	// are deterministic. Although, since the file might have
	// been moved, update the path
	if exists && !hadID {
		maybeNewPath := i.Path
		i = existingItem
		i.Path = maybeNewPath
	}
	if !exists {
		ancli.Noticef("registering new media: %v", i.Name)
	}
	s.cache[i.ID] = i
	f, err := os.OpenFile(path.Join(s.storePath, "store.json"), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open store: %w", err)
	}
	defer f.Close()
	items := make([]model.Item, 0, len(s.cache))
	for _, v := range s.cache {
		items = append(items, v)
	}
	if err := json.NewEncoder(f).Encode(items); err != nil {
		return fmt.Errorf("failed to encode items: %w", err)
	}
	return nil
}

// ListHandlerFunc returns a list of all available items in the gallery
func (s *jsonStore) ListHandlerFunc() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		item, ok := s.cache[id]
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
		item, ok := s.cache[id]
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

		cacheFile, exists := s.cache[vid]
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
