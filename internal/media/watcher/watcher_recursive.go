package watcher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/model"
	"github.com/fsnotify/fsnotify"
)

type recursiveWatcher struct {
	watcher *fsnotify.Watcher
	updates chan model.Item
	errChan chan error
	warnlog func(msg string, a ...any)
}

func NewRecursiveWatcher() (*recursiveWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("newRecursiveWatcher failed to create fsnotify.Watcher: %w", err)
	}
	return &recursiveWatcher{
		w,
		make(chan model.Item),
		make(chan error),
		ancli.Warnf,
	}, nil
}

func (rw *recursiveWatcher) Setup(ctx context.Context) (<-chan model.Item, <-chan error, error) {
	ancli.Noticef("setting up recursive watcher")
	if rw.updates == nil {
		return nil, nil, errors.New("updates channel is nil. Please create with newRecursiveWatcher")
	}
	return rw.updates, rw.errChan, nil
}

// checkFile and emit model.Item on updates channel if file is
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
		rw.updates <- model.Item{Name: path.Base(p), Path: p, MIMEType: mimeType}
	}

	return nil
}

func (rw *recursiveWatcher) walkDo(p string, info os.DirEntry, err error) error {
	if err != nil {
		// Simply skip EOF errors
		if errors.Is(err, io.EOF) {
			rw.warnlog("skipping: '%v', got EOF error", p)
			return nil
		}
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

// handleError by sending it to rw.errChan in a non-blocking way
func (rw *recursiveWatcher) handleError(err error) {
	if err != nil {
		select {
		case rw.errChan <- err:
		default:
			rw.warnlog("Error channel full or unavailable: %v", err)
		}
	}
}

func (rw *recursiveWatcher) handleEvent(ev fsnotify.Event) error {
	if ev.Has(fsnotify.Write) || ev.Has(fsnotify.Create) {
		ancli.Noticef("Got file event: %v", ev)
		return rw.checkFile(ev.Name)
	}
	// TODO: Implement this (need to change updates channel)
	// if ev.Has(fsnotify.Rename) || ev.Has(fsnotify.Remove) {
	// 	return rw.removeFile(ev.Name)
	// }
	return nil
}

func (rw recursiveWatcher) checkPath(path string) error {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to get file info for path '%s': %w", path, err)
	}

	if !fileInfo.IsDir() {
		return fmt.Errorf("path '%s' is not a directory", path)
	}
	return nil
}

func (rw *recursiveWatcher) Watch(ctx context.Context, path string) error {
	err := rw.checkPath(path)
	if err != nil {
		return fmt.Errorf("recursiveWatcher pathCheck failed: %v", err)
	}
	err = filepath.WalkDir(path, rw.walkDo)
	if err != nil {
		if !errors.Is(err, io.EOF) {
			return fmt.Errorf("filepath.WalkDir error: %w", err)
		}
	}
	defer rw.watcher.Close()
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, open := <-rw.watcher.Events:
			if open {
				err := rw.handleEvent(ev)
				if err != nil {
					rw.handleError(err)
				}
			}
		case err, open := <-rw.watcher.Errors:
			if open {
				rw.handleError(err)
			}
		}
	}
}
