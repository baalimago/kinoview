package subtitles

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

func NewSubtitleID(now time.Time) (string, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}

	randomBytes := make([]byte, 8)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("read random bytes for subtitle id: %w", err)
	}

	return fmt.Sprintf("sub_%s_%s", now.UTC().Format("20060102T150405.000000000"), hex.EncodeToString(randomBytes)), nil
}

func CanonicalStorageKey(itemID, subtitleID string) (string, error) {
	if itemID == "" {
		return "", fmt.Errorf("build canonical storage key: item id is empty")
	}
	if subtitleID == "" {
		return "", fmt.Errorf("build canonical storage key: subtitle id is empty")
	}

	return filepath.ToSlash(filepath.Join(itemID, subtitleID+".vtt")), nil
}

func OriginalStorageKey(itemID, subtitleID, ext string) (string, error) {
	if itemID == "" {
		return "", fmt.Errorf("build original storage key: item id is empty")
	}
	if subtitleID == "" {
		return "", fmt.Errorf("build original storage key: subtitle id is empty")
	}
	normalizedExt := strings.TrimPrefix(strings.ToLower(ext), ".")
	if normalizedExt == "" {
		return "", fmt.Errorf("build original storage key: extension is empty")
	}

	return filepath.ToSlash(filepath.Join(itemID, subtitleID+".orig."+normalizedExt)), nil
}