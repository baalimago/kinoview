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

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/agent"
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
	classifier         agent.Classifier
}

func newJSONStore(configPath string) *jsonStore {
	subUtils := &ffmpegSubsUtil{
		mediaCache: map[string]MediaInfo{},
	}
	return &jsonStore{
		subStreamFinder:    subUtils,
		subStreamExtractor: subUtils,
		classifier: agent.NewClassifier(models.Configurations{
			Model:     "gpt-5",
			ConfigDir: configPath,
			InternalTools: []models.ToolName{
				models.CatTool,
				models.FindTool,
				models.FFProbeTool,
				models.WebsiteTextTool,
				models.RipGrepTool,
			},
		}),
	}
}

func (s *jsonStore) loadPersistedItems(storeDirPath string) error {
	files, err := os.ReadDir(storeDirPath)
	if err != nil {
		return fmt.Errorf("failed to list directory: '%v', err: %w", storeDirPath, err)
	}
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		filePath := path.Join(storeDirPath, file.Name())
		f, err := os.Open(filePath)
		if err != nil {
			ancli.Warnf("failed to open file: '%v', err: %v", filePath, err)
			continue
		}
		var items []model.Item
		if err := json.NewDecoder(f).Decode(&items); err != nil {
			ancli.Warnf("failed to decode items in file: '%v', err: %v", filePath, err)
			f.Close()
			continue
		}
		f.Close()
		for _, item := range items {
			s.cache[item.ID] = item
		}
	}
	return nil
}

// Setup the jsonStore by loading all files from storeDirPath and adding all
// items found to cache
func (s *jsonStore) Setup(ctx context.Context, storeDirPath string) error {
	ancli.Noticef("setting up json store")
	s.storePath = storeDirPath
	if s.cache == nil {
		s.cache = make(map[string]model.Item)
	}
	err := s.loadPersistedItems(storeDirPath)
	if err != nil {
		return fmt.Errorf("jsonStore Setup failed to load persisted items: %w", err)
	}

	ancli.Noticef("setting up classifier")
	err = s.classifier.Setup(ctx)
	if err != nil {
		return fmt.Errorf("failed to setup classifier: %w", err)
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

func (s *jsonStore) store(i model.Item) error {
	s.cache[i.ID] = i
	storePath := path.Join(s.storePath, i.ID)
	f, err := os.OpenFile(storePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
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
	ancli.Noticef("updated store: '%v'", storePath)
	return nil
}

func (s *jsonStore) addMetadata(ctx context.Context, i *model.Item) error {
	withMetadata, err := s.classifier.Classify(ctx, *i)
	if err != nil {
		return fmt.Errorf("failed to add metadata: %w", err)
	}
	i.Metadata = withMetadata.Metadata
	return nil
}

// Store the item in the local json store and add i to the cache
func (s *jsonStore) Store(ctx context.Context, i model.Item) error {
	hadID := i.ID != ""
	if i.ID == "" {
		i.ID = generateID(i)
	}
	existingItem, exists := s.cache[i.ID]

	if exists {
		// Only keep path if the item when it exists
		// yet new item lacks generated ID. This is a cheap way of not
		// overwriting existing item on re-scan since the IDs
		// are deterministic. Although, since the file might have
		// been moved, update the path
		if !hadID {
			maybeNewPath := i.Path
			i = existingItem
			i.Path = maybeNewPath
		}
		i.Metadata = existingItem.Metadata
	}
	if !exists {
		ancli.Noticef("registering new media: %v", i.Name)
	}

	if i.Metadata == nil {
		err := s.addMetadata(ctx, &i)
		if err != nil {
			ancli.Errf("failed to append metadata: %v", err)
		}
	}
	return s.store(i)
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
