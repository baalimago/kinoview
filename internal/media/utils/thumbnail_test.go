package utils

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baalimago/go_away_boilerplate/pkg/debug"
	"github.com/baalimago/kinoview/internal/model"
)

// TestCreateThumbnail validates thumbnail creation
func TestCreateThumbnail(t *testing.T) {
	t.Run("creates thumbnail with correct suffix", func(t *testing.T) {
		// Create temp image file
		tmpDir := t.TempDir()
		imgPath := filepath.Join(tmpDir, "test.png")
		createTestImage(t, imgPath)

		item := model.Item{
			Path:     imgPath,
			Name:     "test.png",
			MIMEType: "image/png",
		}

		thumb, err := CreateThumbnail(item)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify thumbnail path contains ThumbnailSuffix
		if !strings.Contains(thumb.Path, ThumbnailSuffix) {
			t.Errorf("thumbnail path missing suffix: %s", debug.IndentedJsonFmt(thumb))
		}

		// Verify thumbnail exists
		if _, err := os.Stat(thumb.Path); os.IsNotExist(err) {
			t.Errorf("thumbnail file not created at: %s", thumb.Path)
		}
	})

	t.Run("skips thumbnail creation for existing thumbnails", func(t *testing.T) {
		tmpDir := t.TempDir()
		thumbPath := filepath.Join(tmpDir, "test_thumb.png")
		createTestImage(t, thumbPath)

		item := model.Item{
			Path:     thumbPath,
			Name:     "test_thumb.png",
			MIMEType: "image/png",
		}

		thumb, err := CreateThumbnail(item)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should return empty Image to avoid recursion
		if thumb.Path != "" {
			t.Errorf("expected empty Image for existing thumbnail, got: %v", thumb)
		}
	})
}

// TestIsThumbnail validates thumbnail detection
func TestIsThumbnail(t *testing.T) {
	t.Run("detects file with ThumbnailSuffix", func(t *testing.T) {
		tmpDir := t.TempDir()
		thumbPath := filepath.Join(tmpDir, "test_thumb.png")
		createTestImage(t, thumbPath)

		if !IsThumbnail(thumbPath) {
			t.Errorf("failed to detect thumbnail with suffix: %s", thumbPath)
		}
	})

	t.Run("rejects file without ThumbnailSuffix", func(t *testing.T) {
		tmpDir := t.TempDir()
		imgPath := filepath.Join(tmpDir, "test.png")
		createTestImage(t, imgPath)

		if IsThumbnail(imgPath) {
			t.Errorf("incorrectly detected non-thumbnail as thumbnail: %s", imgPath)
		}
	})
}

// TestGetThumbnailPath validates path generation
func TestGetThumbnailPath(t *testing.T) {
	t.Run("generates correct thumbnail path", func(t *testing.T) {
		mediaPath := "/path/to/image.png"
		thumbPath := GetThumbnailPath(mediaPath)

		if !strings.Contains(thumbPath, "_thumb") {
			t.Errorf("thumbnail path missing infix: %s", thumbPath)
		}

		if !strings.HasSuffix(thumbPath, ".png") {
			t.Errorf("thumbnail path missing extension: %s", thumbPath)
		}
	})
}

// Helper function to create test images
func createTestImage(t *testing.T, path string) {
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("failed to create test image: %v", err)
	}
	defer f.Close()

	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("failed to encode test image: %v", err)
	}
}
