package media

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
)

const chatIndexCacheFile = "chat_index.cache"

// chatIndexCache is the v2 cache structure used by clai.
type chatIndexCache struct {
	Version int            `json:"version"`
	Rows    []chatIndexRow `json:"rows"`
}

type chatIndexRow struct {
	ID               string  `json:"id"`
	Created          string  `json:"created"`
	MessageCount     int     `json:"message_count"`
	FirstUserMessage string  `json:"first_user_message,omitempty"`
	TotalTokens      int     `json:"total_tokens,omitempty"`
	TotalCostUSD     float64 `json:"total_cost_usd,omitempty"`
}

// EnsureChatIndexCache checks if the clai chat index cache exists and creates
// a minimal valid one if missing. This prevents clai's rebuildChatIndex from
// loading all conversation files into memory on first classification save,
// which caused OOM when multiple agents triggered concurrent rebuilds.
//
// The cache path is derived from claiConfigDir: {claiConfigDir}/conversations/chat_index.cache.
// For kinoview, claiConfigDir is typically {configDir}/clai.
func EnsureChatIndexCache(claiConfigDir string) error {
	convDir := path.Join(claiConfigDir, "conversations")
	cachePath := path.Join(convDir, chatIndexCacheFile)

	if _, err := os.Stat(cachePath); err == nil {
		// Cache exists, nothing to do.
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat chat index cache: %w", err)
	}

	// Ensure conversations directory exists.
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		return fmt.Errorf("create conversations dir: %w", err)
	}

	// Write minimal valid v2 cache. If the cache is non-empty (has rows),
	// clai's readChatIndex will use it directly. An empty cache prevents
	// the full rebuild but means the first upsertChatIndex call will add
	// the first row without scanning all files.
	cache := chatIndexCache{
		Version: 2,
		Rows:    make([]chatIndexRow, 0),
	}

	b, err := json.Marshal(cache)
	if err != nil {
		return fmt.Errorf("marshal chat index cache: %w", err)
	}

	if err := os.WriteFile(cachePath, b, 0o644); err != nil {
		return fmt.Errorf("write chat index cache: %w", err)
	}

	return nil
}
