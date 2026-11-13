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
	"strings"

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

// Close releases the underlying inotify instance. Idempotent: fsnotify's
// Close is safe to call more than once, and Watch also closes on return.
func (rw *recursiveWatcher) Close() error {
	return rw.watcher.Close()
}

// mediaMIMEByExtension covers containers the mimesniff sniffer cannot name.
// ISO-BMFF needs an "mp4" brand inside the ftyp box, so an x264 BRRip branded
// isom/avc1 sniffs as application/octet-stream.
// https://mimesniff.spec.whatwg.org/#signature-for-mp4
var mediaMIMEByExtension = map[string]string{
	".3gp":  "video/3gpp",
	".avi":  "video/x-msvideo",
	".m2ts": "video/mp2t",
	".m4v":  "video/mp4",
	".mkv":  "video/x-matroska",
	".mov":  "video/quicktime",
	".mp4":  "video/mp4",
	".mpeg": "video/mpeg",
	".mpg":  "video/mpeg",
	".ts":   "video/mp2t",
	".webm": "video/webm",
	".wmv":  "video/x-ms-wmv",
}

// detectMediaMIME returns the media MIME type of p, or "" when p is neither
// video nor image. The sniffed type wins; the extension only answers when the
// sniffer is generic.
func detectMediaMIME(p string, header []byte) string {
	sniffed := http.DetectContentType(header)
	if strings.HasPrefix(sniffed, "video") || strings.HasPrefix(sniffed, "image") {
		return sniffed
	}
	return mediaMIMEByExtension[strings.ToLower(filepath.Ext(p))]
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
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}

	mimeType := detectMediaMIME(p, buf[:n])
	if mimeType == "" {
		return nil
	}

	rw.updates <- model.Item{Name: path.Base(p), Path: p, MIMEType: mimeType}
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

		// On create, determine whether it's a file or directory
		if ev.Has(fsnotify.Create) {
			fi, err := os.Stat(ev.Name)
			if err != nil {
				// It might have disappeared, treat as non-fatal
				return nil
			}
			if fi.IsDir() {
				// Add the new directory itself
				if err := rw.watcher.Add(ev.Name); err != nil {
					return fmt.Errorf("failed to add new directory to watcher: %w", err)
				}

				// Walk it to pick up any existing children
				return filepath.WalkDir(ev.Name, func(p string, d os.DirEntry, err error) error {
					if err != nil {
						if errors.Is(err, io.EOF) {
							rw.warnlog("skipping: '%v', got EOF error", p)
							return nil
						}
						return err
					}
					if d.IsDir() {
						if err := rw.watcher.Add(p); err != nil {
							return fmt.Errorf("failed to add recursive path: %v", err)
						}
						return nil
					}
					return rw.checkFile(p)
				})
			}

			// It's a file: check it
			return rw.checkFile(ev.Name)
		}

		// Non-create write events: treat as file updates
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
