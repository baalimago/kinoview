package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"slices"
	"testing"
	"time"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/go_away_boilerplate/pkg/testboil"
	"github.com/baalimago/kinoview/internal/model"
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
		rw, error := newRecursiveWatcher()
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
		rw, err := newRecursiveWatcher()
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
		rw, err := newRecursiveWatcher()
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
		rw, err := newRecursiveWatcher()
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
		rw, err := newRecursiveWatcher()
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
			rw, err := newRecursiveWatcher()
			if err != nil {
				t.Fatal(err)
			}
			err = rw.Watch(ctx, tmpDir)
			if err != nil {
				t.Errorf("un expected error: %v", err)
			}
		}, time.Millisecond*100)
	})
}

func Test_walkDo(t *testing.T) {
	t.Run("return error if passed is not nil", func(t *testing.T) {
		rw, err := newRecursiveWatcher()
		if err != nil {
			t.Fatal(err)
		}
		want := errors.New("pingpong")
		got := rw.walkDo("", nil, want)
		testboil.FailTestIfDiff(t, got, want)
	})
	t.Run("it should add to watcher if dir", func(t *testing.T) {
		rw, err := newRecursiveWatcher()
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
		rw, err := newRecursiveWatcher()
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

		got := rw.watcher.WatchList()
		if !slices.Contains(got, tmpDirPath) {
			t.Fatalf("expected: %v to contain: %v", got, tmpDirPath)
		}

		testCtx, testCtxCancel := context.WithTimeout(context.Background(), time.Second/2)
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
		rw, err := newRecursiveWatcher()
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
