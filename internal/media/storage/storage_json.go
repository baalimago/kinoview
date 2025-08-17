package storage

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"sync"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/agent"
	"github.com/baalimago/kinoview/internal/model"
)

type subtitleStreamFinder interface {
	// fid the media info for some item
	find(item model.Item) (model.MediaInfo, error)
}

type subtitleStreamExtractor interface {
	// extract the subs and return path to the file
	// containing subs. Subs are expected to be in .vtt
	// format
	extract(item model.Item, streamIndex string) (string, error)
}

type jsonStore struct {
	storePath          string
	cacheMu            *sync.RWMutex
	cache              map[string]model.Item
	subStreamFinder    subtitleStreamFinder
	subStreamExtractor subtitleStreamExtractor
	classifier         agent.Classifier
}

type JSONStoreOption func(*jsonStore)

func WithSubtitleStreamFinder(finder subtitleStreamFinder) JSONStoreOption {
	return func(s *jsonStore) {
		s.subStreamFinder = finder
	}
}

func WithSubtitleStreamExtractor(extractor subtitleStreamExtractor) JSONStoreOption {
	return func(s *jsonStore) {
		s.subStreamExtractor = extractor
	}
}

func WithClassifier(classifier agent.Classifier) JSONStoreOption {
	return func(s *jsonStore) {
		s.classifier = classifier
	}
}

func WithStorePath(storePath string) JSONStoreOption {
	return func(s *jsonStore) {
		s.storePath = storePath
	}
}

func NewJSONStore(opts ...JSONStoreOption) *jsonStore {
	subUtils := &ffmpegSubsUtil{
		mediaCache: map[string]model.MediaInfo{},
	}

	cfgDir, err := os.UserConfigDir()
	if err != nil {
		ancli.Warnf("failed to find user config dir: %v", err)
	}
	storePath := path.Join(cfgDir, "kinoview", "store")
	claiPath := path.Join(cfgDir, "kinoview", "clai")

	s := &jsonStore{
		subStreamFinder:    subUtils,
		subStreamExtractor: subUtils,
		storePath:          storePath,
		cache:              make(map[string]model.Item),
		cacheMu:            &sync.RWMutex{},
		classifier: agent.NewClassifier(models.Configurations{
			Model:     "gpt-5",
			ConfigDir: claiPath,
			InternalTools: []models.ToolName{
				models.CatTool,
				models.FindTool,
				models.FFProbeTool,
				models.WebsiteTextTool,
				models.RipGrepTool,
			},
		}),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
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
		s.cacheMu.Lock()
		defer s.cacheMu.Unlock()
		for _, item := range items {
			s.cache[item.ID] = item
		}
	}
	return nil
}

// Setup the jsonStore by loading all files from storeDirPath and adding all
// items found to cache
func (s *jsonStore) Setup(ctx context.Context) error {
	ancli.Noticef("setting up json store")

	if _, err := os.Stat(s.storePath); err != nil {
		os.MkdirAll(s.storePath, 0o755)
	}
	err := s.loadPersistedItems(s.storePath)
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
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
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
	s.cacheMu.RLock()
	existingItem, exists := s.cache[i.ID]
	s.cacheMu.RUnlock()

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
