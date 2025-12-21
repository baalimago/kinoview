package tools

import (
	"fmt"
	"os"
	"path/filepath"
)

type ConciergeContextOption func(*conciergeContextConfig)

type conciergeContextConfig struct {
	cacheDir string // base dir from os.UserCacheDir()
}

func ConciergeContextWithCacheDir(cacheDir string) ConciergeContextOption {
	return func(c *conciergeContextConfig) {
		c.cacheDir = cacheDir
	}
}

func conciergeCacheDir(cacheDir string) (string, error) {
	base := cacheDir
	if base == "" {
		d, err := os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("failed to get user cache dir: %w", err)
		}
		base = d
	}
	return filepath.Join(base, "kinoview", "concierge"), nil
}
