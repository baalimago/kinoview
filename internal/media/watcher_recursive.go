package media

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/fsnotify/fsnotify"
)

type recursiveWatcher struct {
	watcher *fsnotify.Watcher
	updates chan Item
}

func newRecursiveWatcher() (*recursiveWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("newRecursiveWatcher failed to create fsnotify.Watcher: %w", err)
	}
	return &recursiveWatcher{
		w,
		make(chan Item),
	}, nil
}

func (rw *recursiveWatcher) Setup(ctx context.Context) (<-chan Item, error) {
	ancli.Noticef("setting up recursive watcher")
	if rw.updates == nil {
		return nil, errors.New("updates channel is nil. Please create with newRecursiveWatcher")
	}
	return rw.updates, nil
}

// checkFile and emit Item on updates channel if file is
// is video-like or image-like
func (rw *recursiveWatcher) checkFile(p string) error {
	_, err := os.Stat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file does not exist: %s", p)
		}
		return err
	}
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil {
		return err
	}
	mimeType := http.DetectContentType(buf[:n])

	if mimeType[:5] == "video" || mimeType[:5] == "image" {
		rw.updates <- Item{Name: path.Base(p), Path: p, MIMEType: mimeType}
	}

	return nil
}

func (rw *recursiveWatcher) walkDo(p string, info os.DirEntry, err error) error {
	if err != nil {
		return err
	}
	if info.IsDir() {
		err = rw.watcher.Add(p)
		if err != nil {
			return fmt.Errorf("failed to add recursive path: %v", err)
		}
		return nil
	}

	return rw.checkFile(p)
}

func (rw *recursiveWatcher) Watch(ctx context.Context, path string) error {
	err := filepath.WalkDir(path, rw.walkDo)
	if err != nil {
		return fmt.Errorf("filepath.WalkDir error: %w", err)
	}
	<-ctx.Done()
	return rw.watcher.Close()
}
