package watcher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/go_away_boilerplate/pkg/testboil"
	"github.com/baalimago/kinoview/internal/model"
	"github.com/fsnotify/fsnotify"
)

func Test_recursiveWatcher_Setup(t *testing.T) {
	// This is kind of dumb but I like the idea of having a setup
	// function, evne though it doesn't do much
	t.Run("error on no updates chan", func(t *testing.T) {
		rw := recursiveWatcher{}
		_, _, err := rw.Setup(context.Background())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func Test_recursiveWatcher_checkFile(t *testing.T) {
	t.Run("error if file not exists", func(t *testing.T) {
		rw, error := NewRecursiveWatcher()
		if error != nil {
			t.Fatal(error)
		}
		filePath := "/non/existing/file"
		got := rw.checkFile(filePath)
		if got == nil {
			t.Fatalf("wanted error, got nil")
		}

		testboil.AssertStringContains(t, got.Error(), "file does not exist")
	})

	t.Run("error if file can't be opened", func(t *testing.T) {
		fname := "/root/denied-file"
		rw, err := NewRecursiveWatcher()
		if err != nil {
			t.Fatal(err)
		}
		got := rw.checkFile(fname)
		if got == nil {
			t.Fatalf("wanted error, got nil")
		}
		testboil.AssertStringContains(t, got.Error(), "permission denied")
	})

	t.Run("return nil when file isnt video or image", func(t *testing.T) {
		tmpfile := testboil.CreateTestFile(t, "somefile")
		tmpfile.Write([]byte("Some content"))
		defer tmpfile.Close()
		rw, err := NewRecursiveWatcher()
		if err != nil {
			t.Fatal(err)
		}
		got := rw.checkFile(tmpfile.Name())
		if got != nil {
			t.Errorf("wanted nil, got error: %v", got)
		}
	})

	t.Run("send Item update on video file and image file", func(t *testing.T) {
		video := testboil.CreateTestFile(t, "video.mp4")
		// Not sure what this is, llm gave it to me and it works
		video.Write([]byte{0x00, 0x00, 0x00, 0x18, 0x66, 0x74, 0x79, 0x70, 0x6D, 0x70, 0x34, 0x32, 0x00, 0x00, 0x00, 0x00, 0x6D, 0x70, 0x34, 0x31, 0x6D, 0x70, 0x34, 0x32})
		image := testboil.CreateTestFile(t, "image.jpg")
		image.Write([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00})
		defer video.Close()
		defer image.Close()
		updateChan := make(chan model.Item, 2)
		rw, err := NewRecursiveWatcher()
		if err != nil {
			t.Fatal(err)
		}
		rw.updates = updateChan
		if got := rw.checkFile(video.Name()); got != nil {
			t.Fatalf("unexpected error: %v", got)
		}
		if got := rw.checkFile(image.Name()); got != nil {
			t.Fatalf("unexpected error: %v", got)
		}
		select {
		case <-rw.updates:
		case <-rw.updates:
		default:
			t.Error("no update sent on video/image check")
		}
	})
}

func Test_recursiveWatcher_Watch(t *testing.T) {
	t.Run("it should error on non-existent path", func(t *testing.T) {
		rw, err := NewRecursiveWatcher()
		if err != nil {
			t.Fatal(err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		err = rw.Watch(ctx, "/i/dont/exist")
		if err == nil {
			t.Errorf("expected error, got nil")
		}
	})

	t.Run("break on context cancel", func(t *testing.T) {
		testboil.ReturnsOnContextCancel(t, func(ctx context.Context) {
			tmpDir := t.TempDir()
			rw, err := NewRecursiveWatcher()
			if err != nil {
				t.Fatal(err)
			}
			err = rw.Watch(ctx, tmpDir)
			if err != nil {
				t.Errorf("un expected error: %v", err)
			}
		}, time.Millisecond*100)
	})

	t.Run("fails with non-existent path", func(t *testing.T) {
		rw, err := NewRecursiveWatcher()
		if err != nil {
			t.Fatalf("newRecursiveWatcher() failed: %v", err)
		}
		t.Cleanup(func() {
			if rw != nil && rw.watcher != nil {
				rw.watcher.Close()
			}
		})

		nonExistentPath := filepath.Join(os.TempDir(), fmt.Sprintf("non_existent_dir_for_test_%d", time.Now().UnixNano()))
		err = rw.Watch(context.Background(), nonExistentPath)
		if err == nil {
			t.Errorf("expected an error for non-existent path, but got none")
		}
		expectedErrMsgPart1 := fmt.Sprintf("failed to get file info for path '%s'", nonExistentPath)
		if !strings.Contains(err.Error(), expectedErrMsgPart1) {
			t.Errorf("error message did not contain '%s', got: %v", expectedErrMsgPart1, err)
		}
		expectedErrMsgPart2 := "no such file or directory"
		if !strings.Contains(err.Error(), expectedErrMsgPart2) {
			t.Errorf("error message did not contain '%s', got: %v", expectedErrMsgPart2, err)
		}
	})

	t.Run("fails when path is a regular file, not a directory", func(t *testing.T) {
		rw, err := NewRecursiveWatcher()
		if err != nil {
			t.Fatalf("newRecursiveWatcher() failed: %v", err)
		}
		t.Cleanup(func() {
			if rw != nil && rw.watcher != nil {
				rw.watcher.Close()
			}
		})

		tempFile, err := os.CreateTemp("", "regular_file_for_test_*.txt")
		if err != nil {
			t.Fatalf("os.CreateTemp() failed: %v", err)
		}
		tempFilePath := tempFile.Name()
		if err := tempFile.Close(); err != nil {
			t.Errorf("tempFile.Close() failed: %v", err)
		}
		t.Cleanup(func() {
			os.Remove(tempFilePath)
		})

		err = rw.Watch(context.Background(), tempFilePath)
		if err == nil {
			t.Errorf("expected an error for regular file path, but got none")
		}
		expectedErrMsgPart := fmt.Sprintf("path '%s' is not a directory", tempFilePath)
		if !strings.Contains(err.Error(), expectedErrMsgPart) {
			t.Errorf("error message did not contain '%s', got: %v", expectedErrMsgPart, err)
		}
	})
}

func Test_walkDo(t *testing.T) {
	t.Run("return error if passed is not nil", func(t *testing.T) {
		rw, err := NewRecursiveWatcher()
		if err != nil {
			t.Fatal(err)
		}
		want := errors.New("pingpong")
		got := rw.walkDo("", nil, want)
		testboil.FailTestIfDiff(t, got, want)
	})
	t.Run("it should add to watcher if dir", func(t *testing.T) {
		rw, err := NewRecursiveWatcher()
		if err != nil {
			t.Fatal(err)
		}
		tmpDirPath := t.TempDir()

		err = os.Mkdir(path.Join(tmpDirPath, "someDir"), 0x755)
		if err != nil {
			t.Fatal(fmt.Errorf("failed to create test tmp dir: %w", err))
		}

		entries, err := os.ReadDir(tmpDirPath)
		tmpDir := entries[0]
		if err != nil {
			t.Fatalf("could not read tmpDir: %v", err)
		}
		err = rw.walkDo(tmpDirPath, tmpDir, nil)
		if err != nil {
			t.Fatal(fmt.Errorf("unepected error on walkDo: %w", err))
		}
		got := rw.watcher.WatchList()
		if !slices.Contains(got, tmpDirPath) {
			t.Fatalf("expected: %v to contain: %v", got, tmpDirPath)
		}
	})

	t.Run("it should add files when added after start", func(t *testing.T) {
		rw, err := NewRecursiveWatcher()
		if err != nil {
			t.Fatal(err)
		}
		tmpDirPath := t.TempDir()

		err = os.Mkdir(path.Join(tmpDirPath, "someDir"), 0x755)
		if err != nil {
			t.Fatal(fmt.Errorf("failed to create test tmp dir: %w", err))
		}

		ctx, ctxCancel := context.WithCancel(context.Background())
		t.Cleanup(ctxCancel)
		waitStart := make(chan struct{})
		go func() {
			close(waitStart)
			watchErr := rw.Watch(ctx, tmpDirPath)
			if watchErr != nil {
				ancli.Errf("unexpected error, probably no good: %v", watchErr)
			}
		}()
		<-waitStart

		time.Sleep(time.Microsecond * 50)
		got := rw.watcher.WatchList()
		if !slices.Contains(got, tmpDirPath) {
			t.Fatalf("expected: %v to contain: %v", got, tmpDirPath)
		}

		testCtx, testCtxCancel := context.WithTimeout(context.Background(), time.Second)
		t.Cleanup(testCtxCancel)
		hasItem := make(chan string)
		go func() {
			select {
			case f := <-rw.updates:
				hasItem <- f.Path
			case <-testCtx.Done():
				return
			}
		}()

		// Create a new file in the tmpDirPath
		want := path.Join(tmpDirPath, "addedfile.txt")
		content := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00}
		err = os.WriteFile(want, content, 0o644)
		if err != nil {
			t.Fatalf("failed to create file: %v", err)
		}

		// Verify the file is there
		entries, err := os.ReadDir(tmpDirPath)
		if err != nil {
			t.Fatalf("could not re-read tmpDir: %v", err)
		}
		found := false
		for _, e := range entries {
			if e.Name() == "addedfile.txt" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("added file not found in directory")
		}
		select {
		case <-testCtx.Done():
			t.Fatal("Test timeout, didn't receive any updates")
		case got := <-hasItem:
			testboil.FailTestIfDiff(t, got, want)
		}
	})

	t.Run("it should error if file doesnt exist", func(t *testing.T) {
		rw, err := NewRecursiveWatcher()
		if err != nil {
			t.Fatal(err)
		}
		tmpDir := t.TempDir()
		tmpFilePath := path.Join(tmpDir, "somefile")

		err = os.WriteFile(tmpFilePath, []byte("doesntmatter"), 0x755)
		if err != nil {
			t.Fatal(fmt.Errorf("failed to create test tmp dir: %w", err))
		}

		entries, err := os.ReadDir(tmpDir)
		fileWhichExists := entries[0]
		if err != nil {
			t.Fatalf("could not read tmpDir: %v", err)
		}
		got := rw.walkDo("/i/dont/exist", fileWhichExists, nil)
		if got == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func Test_walkDo_ErrorManagement(t *testing.T) {
	t.Run("EOF error handling", func(t *testing.T) {
		mockWarnlog := &mockWarnlog{}
		rw := &recursiveWatcher{
			warnlog: mockWarnlog.log,
		}

		err := rw.walkDo("/test/path", nil, io.EOF)
		if err != nil {
			t.Errorf("Expected nil, got error: %v", err)
		}

		messages := mockWarnlog.getMessages()
		if len(messages) == 0 {
			t.Fatal("Expected warning message, got none")
		}

		expected := "skipping: '/test/path', got EOF error"
		if messages[0] != expected {
			t.Errorf("Expected warning '%s', got '%s'",
				expected, messages[0])
		}
	})

	t.Run("non-EOF error passthrough", func(t *testing.T) {
		rw := &recursiveWatcher{}
		testErr := errors.New("permission denied")

		err := rw.walkDo("/test/path", nil, testErr)

		if err != testErr {
			t.Errorf("Expected exact error %v, got %v",
				testErr, err)
		}
	})
}

// Mock warnlog function and its capturing mechanism
type mockWarnlog struct {
	mu       sync.Mutex
	messages []string
}

func (m *mockWarnlog) log(format string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, fmt.Sprintf(format, args...))
}

func (m *mockWarnlog) getMessages() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.messages
}

func Test_handleError(t *testing.T) {
	// Test case 1: nil error
	t.Run("should do nothing for nil error", func(t *testing.T) {
		mockLogger := &mockWarnlog{}
		rw := &recursiveWatcher{
			errChan: make(chan error, 1), // Buffered but won't be used
			warnlog: mockLogger.log,
		}

		rw.handleError(nil)

		// Verify errChan remains empty (non-blocking read with timeout)
		select {
		case err := <-rw.errChan:
			t.Errorf("Expected errChan to be empty, but received: %v", err)
		case <-time.After(10 * time.Millisecond):
			// Expected: channel should be empty, timeout means no data was available
		}

		// Verify no warning was logged
		if len(mockLogger.getMessages()) > 0 {
			t.Errorf("Expected no warning messages, but got: %v", mockLogger.getMessages())
		}
	})

	// Test case 2: non-nil error, channel not full
	t.Run("should send non-nil error when channel is not full", func(t *testing.T) {
		mockLogger := &mockWarnlog{}
		rw := &recursiveWatcher{
			errChan: make(chan error, 1), // Buffered channel, capacity 1
			warnlog: mockLogger.log,
		}
		testErr := errors.New("a test error")

		rw.handleError(testErr)

		// Verify testErr is received from errChan within a timeout
		select {
		case receivedErr := <-rw.errChan:
			if receivedErr != testErr {
				t.Errorf("Expected error '%v', got '%v'", testErr, receivedErr)
			}
		case <-time.After(10 * time.Millisecond):
			t.Fatal("Expected to receive error from errChan, but timed out")
		}

		// Verify no warning was logged
		if len(mockLogger.getMessages()) > 0 {
			t.Errorf("Expected no warning messages, but got: %v", mockLogger.getMessages())
		}
	})

	// Test case 3: non-nil error, channel full
	t.Run("should log warning when channel is full", func(t *testing.T) {
		mockLogger := &mockWarnlog{}
		rw := &recursiveWatcher{
			errChan: make(chan error, 1), // Buffered channel, capacity 1
			warnlog: mockLogger.log,
		}
		initialErr := errors.New("initial error filling channel")
		testErr2 := errors.New("second error, channel full")

		// Fill the channel
		rw.errChan <- initialErr

		rw.handleError(testErr2)

		// Verify the second error is *not* received from errChan,
		// and the initial error is still there by trying to read it.
		select {
		case receivedErr := <-rw.errChan:
			if receivedErr != initialErr {
				t.Errorf("Expected initial error '%v' to still be in channel, got '%v'", initialErr, receivedErr)
			}
		case <-time.After(10 * time.Millisecond):
			t.Fatal("Expected to receive initial error from errChan, but timed out (channel might not have been full or handled incorrectly)")
		}

		// Verify a warning was logged by our mockLogger
		messages := mockLogger.getMessages()
		if len(messages) == 0 {
			t.Fatal("Expected a warning message, but got none")
		}
		expectedWarningPart := fmt.Sprintf("Error channel full or unavailable: %v", testErr2)
		if messages[0] != expectedWarningPart {
			t.Errorf("Expected warning message to be '%s', got '%s'", expectedWarningPart, messages[0])
		}
	})
}

func Test_recursiveWatcher_checkPath(t *testing.T) {
	type args struct {
		path string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "valid directory",
			args: args{
				path: func() string {
					dir, err := os.MkdirTemp("", "testdir")
					if err != nil {
						t.Fatalf("failed to create temp dir: %v", err)
					}
					return dir
				}(),
			},
			wantErr: false,
		},
		{
			name: "path is a file",
			args: args{
				path: func() string {
					tmpfile, err := os.CreateTemp("", "testfile")
					if err != nil {
						t.Fatalf("failed to create temp file: %v", err)
					}
					tmpfile.Close()
					return tmpfile.Name()
				}(),
			},
			wantErr: true,
		},
		{
			name: "non-existent path",
			args: args{
				path: filepath.Join(os.TempDir(), "nonexistent", "path"),
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rw := &recursiveWatcher{}
			if err := rw.checkPath(tt.args.path); (err != nil) != tt.wantErr {
				t.Errorf("recursiveWatcher.checkPath() error = %v, wantErr %v", err, tt.wantErr)
			}

			// Clean up created files/directories
			if tt.name == "valid directory" || tt.name == "path is a file" {
				os.RemoveAll(tt.args.path)
			}
		})
	}
}

func Test_Watch_Event_Write_NonExistent_ErrorChan(t *testing.T) {
	dir := t.TempDir()
	rw, err := NewRecursiveWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer rw.watcher.Close()

	// use buffered errChan to avoid blocking
	rw.errChan = make(chan error, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- rw.Watch(ctx, dir) }()

	time.Sleep(50 * time.Millisecond)

	bad := filepath.Join(dir, "nope.mp4")
	ev := fsnotify.Event{Name: bad, Op: fsnotify.Write}
	select {
	case rw.watcher.Events <- ev:
	case <-time.After(time.Second):
		t.Fatal("send event timeout")
	}

	select {
	case got := <-rw.errChan:
		if !strings.Contains(got.Error(), "does not exist") {
			t.Fatalf("want not exist, got: %v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("no error received from errChan")
	}

	cancel()
	<-done
}

func Test_Watch_ErrorsChannel_Forward(t *testing.T) {
	dir := t.TempDir()
	rw, err := NewRecursiveWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer rw.watcher.Close()
	rw.errChan = make(chan error, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- rw.Watch(ctx, dir) }()

	time.Sleep(50 * time.Millisecond)

	want := errors.New("boom")
	select {
	case rw.watcher.Errors <- want:
	case <-time.After(time.Second):
		t.Fatal("send error timeout")
	}

	select {
	case got := <-rw.errChan:
		if got.Error() != want.Error() {
			t.Fatalf("want %v, got %v", want, got)
		}
	case <-time.After(time.Second):
		t.Fatal("no error received")
	}

	cancel()
	<-done
}

func Test_Watch_HandleError_ChannelFull_Warns(t *testing.T) {
	dir := t.TempDir()
	rw, err := NewRecursiveWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer rw.watcher.Close()
	rw.errChan = make(chan error, 1)

	mock := &mockWarnlog{}
	rw.warnlog = mock.log

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- rw.Watch(ctx, dir) }()

	time.Sleep(50 * time.Millisecond)

	// fill channel
	rw.errChan <- errors.New("filled")

	// inject error into watcher.Errors
	inj := errors.New("second")
	select {
	case rw.watcher.Errors <- inj:
	case <-time.After(time.Second):
		t.Fatal("send error timeout")
	}

	// give handleError a moment
	time.Sleep(50 * time.Millisecond)

	msgs := mock.getMessages()
	if len(msgs) == 0 {
		t.Fatal("want warnlog message, got none")
	}
	if !strings.Contains(msgs[0], "Error channel full") {
		t.Fatalf("unexpected warn message: %v", msgs[0])
	}

	cancel()
	<-done
}

func Test_Watch_CreateDirectory_AddsToWatcherAndIndexesFiles(t *testing.T) {
	dir := t.TempDir()
	rw, err := NewRecursiveWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer rw.watcher.Close()

	// make updates buffered so we don't block
	rw.updates = make(chan model.Item, 2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- rw.Watch(ctx, dir) }()

	// give watcher some time to start
	time.Sleep(50 * time.Millisecond)

	// create subdirectory
	sub := filepath.Join(dir, "subdir")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	// create a "video-like" file in the subdir
	fname := filepath.Join(sub, "vid.mp4")
	content := []byte{0x00, 0x00, 0x00, 0x18, 0x66, 0x74, 0x79, 0x70, 0x6D, 0x70, 0x34, 0x32, 0x00, 0x00, 0x00, 0x00, 0x6D, 0x70, 0x34, 0x31, 0x6D, 0x70, 0x34, 0x32}
	if err := os.WriteFile(fname, content, 0o644); err != nil {
		t.Fatalf("failed to create video file: %v", err)
	}

	// Allow fsnotify to emit both create(dir) and create(file)
	timeout := time.After(2 * time.Second)
	var gotPath string
	for gotPath == "" {
		select {
		case item := <-rw.updates:
			if item.Path == fname {
				gotPath = item.Path
			}
		case <-timeout:
			t.Fatalf("timeout waiting for item from updates, got: %q", gotPath)
		}
	}

	// Ensure new directory is part of watch list
	wl := rw.watcher.WatchList()
	if !slices.Contains(wl, sub) {
		t.Fatalf("expected watch list %v to contain new directory %q", wl, sub)
	}

	cancel()
	<-done
}
