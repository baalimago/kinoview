package storage

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/go_away_boilerplate/pkg/debug"
	"github.com/baalimago/go_away_boilerplate/pkg/misc"
	"github.com/baalimago/kinoview/internal/agents"
	"github.com/baalimago/kinoview/internal/agents/classifier"
	"github.com/baalimago/kinoview/internal/model"
)

type store struct {
	storePath       string
	cacheMu         *sync.RWMutex
	cache           map[string]model.Item
	subtitleManager agents.StreamManager

	classifier               agents.Classifier
	classificationLogsOutdir string
	classificationWorkers    int
	classifierErrors         chan error
	classificationRequest    chan classificationCandidate

	readyChan chan struct{}

	debug bool
}

type StoreOption func(*store)

func WithSubtitlesManager(subsM agents.StreamManager) StoreOption {
	return func(s *store) {
		s.subtitleManager = subsM
	}
}

func WithClassifier(classifier agents.Classifier) StoreOption {
	return func(s *store) {
		s.classifier = classifier
	}
}

func WithStorePath(storePath string) StoreOption {
	return func(s *store) {
		s.storePath = storePath
	}
}

func WithClassificationWorkers(amWorkers int) StoreOption {
	return func(s *store) {
		s.classificationWorkers = amWorkers
	}
}

func NewStore(opts ...StoreOption) *store {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		ancli.Warnf("failed to find user config dir: %v", err)
	}
	kinoviewCfgPath := path.Join(cfgDir, "kinoview")
	storePath := path.Join(kinoviewCfgPath, "store")
	classifierLogOut := path.Join(kinoviewCfgPath, "classifierLogs")
	err = os.MkdirAll(classifierLogOut, 0o755)
	if err != nil {
		ancli.Errf("failed to create classifier logs dir: %v", err)
	}

	s := &store{
		debug:     misc.Truthy(os.Getenv("DEBUG")),
		storePath: storePath,
		cache:     make(map[string]model.Item),
		cacheMu:   &sync.RWMutex{},
		classifier: classifier.New(models.Configurations{
			Model:     "gpt-5",
			ConfigDir: kinoviewCfgPath,
			InternalTools: []models.ToolName{
				models.CatTool,
				models.FindTool,
				models.FFProbeTool,
				models.WebsiteTextTool,
				models.RipGrepTool,
			},
		}),
		classificationRequest:    make(chan classificationCandidate),
		classifierErrors:         make(chan error),
		classificationWorkers:    2,
		classificationLogsOutdir: classifierLogOut,

		// Buffered chanel to not cause regression since it's currently only used in classify
		// Large enough buffre to ever cause congestion due to waiting for it to be ready
		readyChan: make(chan struct{}, 10000),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

func (s *store) Ready() <-chan struct{} {
	return s.readyChan
}

func (s *store) loadPersistedItems(storeDirPath string) error {
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
		var item model.Item
		if err := json.NewDecoder(f).Decode(&item); err != nil {
			ancli.Warnf("failed to decode items in file: '%v', err: %v", filePath, err)
			f.Close()
			continue
		}
		f.Close()
		underlyingFilePath := item.Path

		if _, err := os.Stat(underlyingFilePath); os.IsNotExist(err) {
			ancli.Warnf("couldnt find underlying file: '%v', removing index: '%v'", underlyingFilePath, filePath)
			os.Remove(filePath)
			continue
		}

		s.cacheMu.Lock()
		s.cache[item.ID] = item
		s.cacheMu.Unlock()
	}
	return nil
}

// Setup the jsonStore by loading all files from storeDirPath and adding all
// items found to cache
func (s *store) Setup(ctx context.Context) (<-chan error, error) {
	ancli.Noticef("setting up json store")

	if _, err := os.Stat(s.storePath); err != nil {
		os.MkdirAll(s.storePath, 0o755)
	}
	err := s.loadPersistedItems(s.storePath)
	if err != nil {
		return nil, fmt.Errorf("jsonStore Setup failed to load persisted items: %w", err)
	}

	if s.classifier != nil {
		ancli.Noticef("setting up classifier")
		err = s.classifier.Setup(ctx)
		if err != nil {
			ancli.Errf("failed to setup classifier, classifications wont be possible. Err: %v", err)
		}
	}

	s.classifierErrors = make(chan error)
	s.readyChan <- struct{}{}
	return s.classifierErrors, nil
}

func (s *store) Start(ctx context.Context) {
	go func() {
		err := s.StartClassificationStation(ctx)
		if err != nil {
			s.classifierErrors <- fmt.Errorf("failed to start classification station: %w", err)
		}
	}()
}

// generateID by creating a hash using sha256 on the contents of item.Path
func generateID(path string) string {
	f, err := os.Open(path)
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

func (s *store) store(i model.Item) error {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.cache[i.ID] = i
	storePath := path.Join(s.storePath, i.ID)
	f, err := os.OpenFile(storePath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open store: %w", err)
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	var stored model.Item
	err = dec.Decode(&stored)
	if err != nil {
		ancli.Warnf(
			"failed to decode existing item: '%v', err: '%v'", i.Path, err)
	} else {
		newJSON := debug.IndentedJsonFmt(i)
		storedJSON := debug.IndentedJsonFmt(stored)
		if newJSON == storedJSON {
			return nil
		} else {
			if s.debug {
				ancli.Noticef("new: %v", newJSON)
				ancli.Noticef("stored: %v", storedJSON)
			}
			// Close and reopen so that file is properly truncated
			f.Close()
			f, err = os.OpenFile(storePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
			if err != nil {
				return fmt.Errorf("failed to open store: %w", err)
			}
		}
	}

	if err := json.NewEncoder(f).Encode(i); err != nil {
		return fmt.Errorf("failed to encode items: %w", err)
	}
	ancli.Noticef("updated store for '%v', path: '%v'", i.Name, storePath)
	return nil
}

// Store the item in the local json store and add i to the cache
func (s *store) Store(ctx context.Context, i model.Item) error {
	hadID := i.ID != ""
	if i.ID == "" {
		i.ID = generateID(i.Path)
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

	if i.Metadata == nil && strings.Contains(i.MIMEType, "video") {
		if strings.Contains(i.MIMEType, "video") {
			err := s.handleVideoItem(i)
			if err != nil {
				ancli.Errf("failed to handle video item, continuing. Error is: %v", err)
			}
		}
	}
	if strings.Contains(i.MIMEType, "image") {
		err := s.handleImageItem(&i)
		if err != nil {
			ancli.Errf("failed to handle image item, continuing. Error is: %v", err)
		}
		// Slight hack here, ideally thumbnail should be an models.Item and
		// thumbnail properties should be flattned into the same struct
		i.Thumbnail.ID = generateID(i.Thumbnail.Path)
	}

	return s.store(i)
}

func (s *store) Snapshot() (ret []model.Item) {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	ancli.Okf("Now returning from store path: %v", s.storePath)
	for _, i := range s.cache {
		ret = append(ret, i)
	}
	return
}

func (s *store) GetItemByID(ID string) (model.Item, error) {
	snapshot := s.Snapshot()
	for _, s := range snapshot {
		if s.ID == ID {
			return s, nil
		}
	}
	return model.Item{}, fmt.Errorf("failed to find item with ID: %v", ID)
}

func (s *store) GetItemByName(name string) (model.Item, error) {
	snapshot := s.Snapshot()
	for _, s := range snapshot {
		if s.Name == name {
			return s, nil
		}
	}
	return model.Item{}, fmt.Errorf("failed to find item with name: %v", name)
}

func (s *store) UpdateMetadata(item model.Item, metadata string) error {
	raw := json.RawMessage(metadata)
	_, err := raw.MarshalJSON()
	if err != nil {
		return fmt.Errorf("failed to marshal, invalid json: %w", err)
	}
	item.Metadata = &raw
	return s.store(item)
}
