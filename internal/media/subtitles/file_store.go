package subtitles

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

type localFileStore struct {
	rootDir  string
	filesDir string
}

func NewFileStore(rootDir string) (FileStore, error) {
	if rootDir == "" {
		return nil, fmt.Errorf("create subtitle file store: root dir is empty")
	}

	store := &localFileStore{
		rootDir:  rootDir,
		filesDir: filepath.Join(rootDir, "files"),
	}
	if err := os.MkdirAll(store.filesDir, 0o755); err != nil {
		return nil, fmt.Errorf("create subtitle files dir: %w", err)
	}

	return store, nil
}

func (s *localFileStore) WriteCanonical(_ context.Context, storageKey string, data []byte) error {
	path, err := resolveStoragePath(s.filesDir, storageKey)
	if err != nil {
		return fmt.Errorf("resolve canonical subtitle path for key %q: %w", storageKey, err)
	}
	if err := writeFileAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf("write canonical subtitle %q: %w", storageKey, err)
	}
	return nil
}

func (s *localFileStore) WriteOriginal(_ context.Context, storageKey string, data []byte) error {
	path, err := resolveStoragePath(s.filesDir, storageKey)
	if err != nil {
		return fmt.Errorf("resolve original subtitle path for key %q: %w", storageKey, err)
	}
	if err := writeFileAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf("write original subtitle %q: %w", storageKey, err)
	}
	return nil
}

func (s *localFileStore) ResolvePath(storageKey string) (string, error) {
	path, err := resolveStoragePath(s.filesDir, storageKey)
	if err != nil {
		return "", fmt.Errorf("resolve subtitle file path for key %q: %w", storageKey, err)
	}
	return path, nil
}

func (s *localFileStore) DeleteItem(_ context.Context, itemID string) error {
	if itemID == "" {
		return fmt.Errorf("delete subtitle item directory: item id is empty")
	}

	itemDir, err := resolveStoragePath(s.filesDir, itemID)
	if err != nil {
		return fmt.Errorf("resolve subtitle item directory for item %q: %w", itemID, err)
	}
	if err := os.RemoveAll(itemDir); err != nil {
		return fmt.Errorf("remove subtitle item directory for item %q: %w", itemID, err)
	}

	return nil
}