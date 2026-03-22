package subtitles

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func writeFileAtomic(path string, data []byte, perm os.FileMode) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory for atomic write: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file for atomic write: %w", err)
	}

	tmpPath := tmpFile.Name()
	defer func() {
		if removeErr := os.Remove(tmpPath); removeErr != nil && !os.IsNotExist(removeErr) && err == nil {
			err = fmt.Errorf("cleanup temp file after atomic write: %w", removeErr)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return fmt.Errorf("write temp file for atomic write: %w", err)
	}
	if err := tmpFile.Chmod(perm); err != nil {
		tmpFile.Close()
		return fmt.Errorf("chmod temp file for atomic write: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file for atomic write: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp file for atomic write: %w", err)
	}

	return nil
}

func resolveStoragePath(rootDir, storageKey string) (string, error) {
	if rootDir == "" {
		return "", fmt.Errorf("resolve storage path: root dir is empty")
	}
	if storageKey == "" {
		return "", fmt.Errorf("resolve storage path: storage key is empty")
	}
	if filepath.IsAbs(storageKey) {
		return "", fmt.Errorf("resolve storage path: storage key must be relative")
	}

	cleanedKey := filepath.Clean(storageKey)
	if cleanedKey == "." {
		return "", fmt.Errorf("resolve storage path: storage key resolved to current directory")
	}
	if cleanedKey == ".." {
		return "", fmt.Errorf("resolve storage path: storage key resolved outside root")
	}
	if stringsContainParentTraversal(cleanedKey) {
		return "", fmt.Errorf("resolve storage path: storage key contains parent traversal")
	}

	rootClean := filepath.Clean(rootDir)
	resolvedPath := filepath.Join(rootClean, cleanedKey)
	relativeToRoot, err := filepath.Rel(rootClean, resolvedPath)
	if err != nil {
		return "", fmt.Errorf("resolve storage path relative to root: %w", err)
	}
	if relativeToRoot == ".." || stringsContainParentTraversal(relativeToRoot) {
		return "", fmt.Errorf("resolve storage path: resolved path escapes root")
	}

	return resolvedPath, nil
}

func stringsContainParentTraversal(path string) bool {
	for _, segment := range strings.Split(filepath.ToSlash(path), "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}