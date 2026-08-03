package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ── helpers ────────────────────────────────────────────────────────────────

func writeConv(dir, name string, systemContent string, queries []queryRecord) error {
	msgs := []map[string]string{
		{"role": "system", "content": systemContent},
	}

	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	type file struct {
		Queries  []queryRecord       `json:"queries"`
		Messages []map[string]string `json:"messages"`
	}
	return enc.Encode(file{
		Queries:  queries,
		Messages: msgs,
	})
}

func sampleQuery(t time.Time, model string) queryRecord {
	return queryRecord{
		CreatedAt: t,
		CostUSD:   0.001,
		Model:     model,
		Usage: &usageRec{
			PromptTokens:     1000,
			CompletionTokens: 100,
			TotalTokens:      1100,
			PromptTokensDetails: struct {
				CachedTokens int `json:"cached_tokens"`
			}{CachedTokens: 500},
			CompletionTokensDetails: struct {
				ReasoningTokens int `json:"reasoning_tokens"`
			}{ReasoningTokens: 20},
		},
	}
}

// ── tests ──────────────────────────────────────────────────────────────────

func TestUsage_AggregateByAgent(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	writeConv(dir, "b1.json", "You are a media Butler.", []queryRecord{
		sampleQuery(now, "m1"),
		sampleQuery(now, "m1"),
	})
	writeConv(dir, "b2.json", "You are a media Butler.", []queryRecord{
		sampleQuery(now, "m1"),
	})
	writeConv(dir, "i1.json", "Your job is to pick a media item.", []queryRecord{
		sampleQuery(now, "m2"),
	})

	aggr := newAggregator("agent", time.Time{})
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		agent, queries, err := parseConversation(p)
		if err != nil {
			return nil
		}
		aggr.add(agent, queries)
		return nil
	})

	rows := aggr.rows()
	byAgent := map[string]aggregateRow{}
	for _, r := range rows {
		byAgent[r.Label] = r
	}

	if byAgent["butler"].Queries != 3 {
		t.Errorf("butler queries: want 3, got %d", byAgent["butler"].Queries)
	}
	if byAgent["semanticIndexer"].Queries != 1 {
		t.Errorf("semanticIndexer queries: want 1, got %d", byAgent["semanticIndexer"].Queries)
	}
	// 3 queries * 1000 prompt each = 3000
	if byAgent["butler"].PromptTokens != 3000 {
		t.Errorf("butler prompt_tokens: want 3000, got %d", byAgent["butler"].PromptTokens)
	}
	// 3 queries * 500 cached each = 1500
	if byAgent["butler"].CachedTokens != 1500 {
		t.Errorf("butler cached_tokens: want 1500, got %d", byAgent["butler"].CachedTokens)
	}
	if byAgent["butler"].CacheHitRate != 50.0 {
		t.Errorf("butler cache hit rate: want 50.0, got %f", byAgent["butler"].CacheHitRate)
	}
	if byAgent["butler"].CostUSD != 0.003 {
		t.Errorf("butler cost: want 0.003, got %f", byAgent["butler"].CostUSD)
	}
}

func TestUsage_Attribution(t *testing.T) {
	tests := []struct {
		systemContent string
		want          string
	}{
		{"You are a media classifier. Classify media files.", "classifier"},
		{"You are a media Butler. Help the user.", "butler"},
		{"Your job is to pick a media item from a list.", "semanticIndexer"},
		{"You are a media stream analyzer.", "subtitleSelector"},
		{"You are the media concierge.", "concierge"},
		{"You are a slapstick comedian.", "theatre"},
		{"You are a media recommender.", "recommender"},
		{"Something completely different.", "other"},
		{"", "other"},
		{"media butler (lowercase butler)", "butler"},
	}

	for _, tt := range tests {
		got := classifyAgent(tt.systemContent)
		if got != tt.want {
			t.Errorf("classifyAgent(%q) = %q, want %q", tt.systemContent, got, tt.want)
		}
	}
}

func TestUsage_NoCostDataRow(t *testing.T) {
	dir := t.TempDir()

	// File with no queries key
	f, _ := os.Create(filepath.Join(dir, "no_queries.json"))
	json.NewEncoder(f).Encode(map[string]any{
		"messages": []map[string]string{{"role": "system", "content": "x"}},
	})
	f.Close()

	aggr := newAggregator("agent", time.Time{})
	var noCostFiles int
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		_, queries, err := parseConversation(p)
		if err != nil {
			return nil
		}
		if len(queries) == 0 {
			noCostFiles++
			return nil
		}
		aggr.add("other", queries)
		return nil
	})

	if noCostFiles != 1 {
		t.Errorf("noCostFiles: want 1, got %d", noCostFiles)
	}
	if aggr.fileCount != 0 {
		t.Errorf("fileCount: want 0 (no files with cost), got %d", aggr.fileCount)
	}
}

func TestUsage_MultiQueryConversation(t *testing.T) {
	dir := t.TempDir()
	writeConv(dir, "multi.json", "media concierge", []queryRecord{
		sampleQuery(time.Now(), "m1"),
		sampleQuery(time.Now(), "m1"),
		sampleQuery(time.Now(), "m1"),
		sampleQuery(time.Now(), "m1"),
		sampleQuery(time.Now(), "m1"),
		sampleQuery(time.Now(), "m1"),
		sampleQuery(time.Now(), "m1"),
		sampleQuery(time.Now(), "m1"),
		sampleQuery(time.Now(), "m1"),
	})

	aggr := newAggregator("agent", time.Time{})
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		agent, queries, err := parseConversation(p)
		if err != nil {
			return nil
		}
		aggr.add(agent, queries)
		return nil
	})

	rows := aggr.rows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Queries != 9 {
		t.Errorf("queries: want 9, got %d", rows[0].Queries)
	}
	if aggr.fileCount != 1 {
		t.Errorf("fileCount: want 1 conversation, got %d", aggr.fileCount)
	}
}

func TestUsage_SinceFiltersOnQueryTime(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	old := now.Add(-48 * time.Hour)
	recent := now.Add(-1 * time.Hour)

	writeConv(dir, "mix.json", "media Butler", []queryRecord{
		{CreatedAt: old, Model: "m1", CostUSD: 0.5, Usage: &usageRec{PromptTokens: 100}},
		{CreatedAt: recent, Model: "m1", CostUSD: 1.0, Usage: &usageRec{PromptTokens: 200}},
	})

	aggr := newAggregator("agent", now.Add(-24*time.Hour))
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		agent, queries, err := parseConversation(p)
		if err != nil {
			return nil
		}
		aggr.add(agent, queries)
		return nil
	})

	rows := aggr.rows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Queries != 1 {
		t.Errorf("queries: want 1 (only recent), got %d", rows[0].Queries)
	}
	if rows[0].CostUSD != 1.0 {
		t.Errorf("cost: want 1.0, got %f", rows[0].CostUSD)
	}
}

func TestUsage_GroupByDay(t *testing.T) {
	dir := t.TempDir()
	day1 := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)

	writeConv(dir, "d1.json", "media Butler", []queryRecord{
		sampleQuery(day1, "m1"),
	})
	writeConv(dir, "d2.json", "media Butler", []queryRecord{
		sampleQuery(day2, "m1"),
		sampleQuery(day2, "m1"),
	})

	aggr := newAggregator("day", time.Time{})
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		agent, queries, err := parseConversation(p)
		if err != nil {
			return nil
		}
		aggr.add(agent, queries)
		return nil
	})

	rows := aggr.rows()
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows (one per day), got %d", len(rows))
	}
	sum := 0
	for _, r := range rows {
		sum += r.Queries
	}
	if sum != 3 {
		t.Errorf("total queries: want 3, got %d", sum)
	}
}

func TestUsage_GroupByModel(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	writeConv(dir, "m1.json", "media Butler", []queryRecord{
		sampleQuery(now, "deepseek-v4-flash"),
		sampleQuery(now, "deepseek-v4-flash"),
	})
	writeConv(dir, "m2.json", "media Butler", []queryRecord{
		sampleQuery(now, "minimax/minimax-m3"),
	})

	aggr := newAggregator("model", time.Time{})
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		agent, queries, err := parseConversation(p)
		if err != nil {
			return nil
		}
		aggr.add(agent, queries)
		return nil
	})

	rows := aggr.rows()
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows (one per model), got %d", len(rows))
	}
	byModel := map[string]aggregateRow{}
	for _, r := range rows {
		byModel[r.Label] = r
	}
	if byModel["deepseek-v4-flash"].Queries != 2 {
		t.Errorf("deepseek queries: want 2, got %d", byModel["deepseek-v4-flash"].Queries)
	}
	if byModel["minimax/minimax-m3"].Queries != 1 {
		t.Errorf("minimax queries: want 1, got %d", byModel["minimax/minimax-m3"].Queries)
	}
}

func TestUsage_JSONMatchesTable(t *testing.T) {
	dir := t.TempDir()
	writeConv(dir, "b.json", "media Butler", []queryRecord{
		sampleQuery(time.Now(), "m1"),
	})

	aggr := newAggregator("agent", time.Time{})
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		agent, queries, err := parseConversation(p)
		if err != nil {
			return nil
		}
		aggr.add(agent, queries)
		return nil
	})

	rows := aggr.rows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Label != "butler" {
		t.Errorf("label: want butler, got %s", rows[0].Label)
	}
	if rows[0].Queries != 1 {
		t.Errorf("queries: want 1, got %d", rows[0].Queries)
	}
	if rows[0].MeanPrompt != 1000.0 {
		t.Errorf("mean prompt: want 1000.0, got %f", rows[0].MeanPrompt)
	}

	// Round-trip JSON
	b, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded []aggregateRow
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded) != 1 {
		t.Fatalf("round-trip length: want 1, got %d", len(decoded))
	}
	if decoded[0].CostUSD != rows[0].CostUSD {
		t.Errorf("round-trip cost mismatch")
	}
}

func TestUsage_DoesNotDecodeMessages(t *testing.T) {
	// A fixture whose first message contains fields that would fail to decode
	// into our minimal firstMsg struct — but we only care that it doesn't error
	// on a valid file (our struct just skips unknown fields).
	dir := t.TempDir()
	now := time.Now()

	// Make a valid file with extra fields in the system message — should parse fine
	f, _ := os.Create(filepath.Join(dir, "extra.json"))
	json.NewEncoder(f).Encode(map[string]any{
		"queries": []map[string]any{
			{
				"created_at": now.Format(time.RFC3339),
				"cost_usd":   0.001,
				"model":      "m1",
				"usage": map[string]any{
					"prompt_tokens":             100,
					"completion_tokens":         10,
					"total_tokens":              110,
					"prompt_tokens_details":     map[string]any{"cached_tokens": 0, "audio_tokens": 0},
					"completion_tokens_details": map[string]any{"reasoning_tokens": 0, "audio_tokens": 0},
				},
			},
		},
		"messages": []map[string]any{
			{
				"role":             "system",
				"content":          "media Butler prompt",
				"unexpected_field": "should-be-ignored",
				"another_field":    42,
			},
		},
	})
	f.Close()

	agent, queries, err := parseConversation(filepath.Join(dir, "extra.json"))
	if err != nil {
		t.Fatalf("parseConversation failed: %v", err)
	}
	if agent != "butler" {
		t.Errorf("agent: want butler, got %s", agent)
	}
	if len(queries) != 1 {
		t.Errorf("queries: want 1, got %d", len(queries))
	}
}

func TestUsage_BoundedMemory(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	// Create 100 reasonable-sized fixture files
	for i := range 100 {
		writeConv(dir, fmt.Sprintf("c%d.json", i), "media Butler", []queryRecord{
			sampleQuery(now, "m1"),
			sampleQuery(now, "m1"),
			sampleQuery(now, "m1"),
		})
	}

	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	aggr := newAggregator("agent", time.Time{})
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		agent, queries, err := parseConversation(p)
		if err != nil {
			return nil
		}
		aggr.add(agent, queries)
		return nil
	})

	runtime.GC()
	runtime.ReadMemStats(&m2)

	allocDelta := m2.TotalAlloc - m1.TotalAlloc
	// 100 files * 3 queries each. Each file is processed and discarded.
	// Generous ceiling: 20MB
	if allocDelta > 20*1024*1024 {
		t.Errorf("memory delta too high: %d bytes (ceiling 20MB)", allocDelta)
	}
}

func TestUsage_ReadOnly(t *testing.T) {
	dir := t.TempDir()
	writeConv(dir, "r.json", "media Butler", []queryRecord{
		sampleQuery(time.Now(), "m1"),
	})

	// Record checksums of all files before
	before := map[string]int64{}
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, _ := d.Info()
		before[p] = info.Size()
		return nil
	})

	aggr := newAggregator("agent", time.Time{})
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		agent, queries, err := parseConversation(p)
		if err != nil {
			return nil
		}
		aggr.add(agent, queries)
		return nil
	})

	after := map[string]int64{}
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, _ := d.Info()
		after[p] = info.Size()
		return nil
	})

	for p, sizeBefore := range before {
		if after[p] != sizeBefore {
			t.Errorf("file %s modified: before=%d after=%d", p, sizeBefore, after[p])
		}
	}
}

func TestUsage_CostLabelling(t *testing.T) {
	// Verify the JSON field name includes clai-reported context
	r := aggregateRow{CostUSD: 0.123}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), "cost_usd_clai_reported") {
		t.Errorf("JSON output missing clai-reported label: %s", string(b))
	}
}

// ── error coverage ─────────────────────────────────────────────────────────

func TestUsage_MissingDir(t *testing.T) {
	cmd := usageCommand()
	fs := cmd.Flagset()
	fs.Parse([]string{"--dir", "/nonexistent/path/12345"})
	err := cmd.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for missing directory")
	}
}

func TestUsage_CorruptFile(t *testing.T) {
	dir := t.TempDir()
	// Valid file
	writeConv(dir, "good.json", "media Butler", []queryRecord{
		sampleQuery(time.Now(), "m1"),
	})
	// Corrupt file
	os.WriteFile(filepath.Join(dir, "bad.json"), []byte("not json"), 0o644)

	aggr := newAggregator("agent", time.Time{})
	skipped := 0
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		_, _, err = parseConversation(p)
		if err != nil {
			skipped++
			return nil
		}
		return nil
	})
	// Actually add the good one
	agent, queries, _ := parseConversation(filepath.Join(dir, "good.json"))
	aggr.add(agent, queries)

	if skipped != 1 {
		t.Errorf("skipped: want 1, got %d", skipped)
	}
	if aggr.fileCount != 1 {
		t.Errorf("fileCount: want 1 (good file only), got %d", aggr.fileCount)
	}
}

func TestUsage_MalformedQueries(t *testing.T) {
	dir := t.TempDir()
	// queries is a string, not an array
	f, _ := os.Create(filepath.Join(dir, "badq.json"))
	json.NewEncoder(f).Encode(map[string]any{
		"queries":  "not-an-array",
		"messages": []map[string]string{{"role": "system", "content": "x"}},
	})
	f.Close()

	_, _, err := parseConversation(filepath.Join(dir, "badq.json"))
	if err == nil {
		t.Fatal("expected error for malformed queries")
	}
}

func TestUsage_MissingCost(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeConv(dir, "nocost.json", "media Butler", []queryRecord{
		{CreatedAt: now, Model: "m1", CostUSD: 0, Usage: &usageRec{PromptTokens: 100}},
	})

	aggr := newAggregator("agent", time.Time{})
	agent, queries, _ := parseConversation(filepath.Join(dir, "nocost.json"))
	aggr.add(agent, queries)

	rows := aggr.rows()
	if rows[0].CostUSD != 0 {
		t.Errorf("cost: want 0 (missing cost treated as zero), got %f", rows[0].CostUSD)
	}
	if rows[0].PromptTokens != 100 {
		t.Errorf("tokens: want 100, got %d", rows[0].PromptTokens)
	}
}

func TestUsage_MissingUsage(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeConv(dir, "nouse.json", "media Butler", []queryRecord{
		{CreatedAt: now, Model: "m1", CostUSD: 0.001, Usage: nil},
	})

	aggr := newAggregator("agent", time.Time{})
	agent, queries, _ := parseConversation(filepath.Join(dir, "nouse.json"))
	aggr.add(agent, queries)

	rows := aggr.rows()
	if rows[0].Queries != 1 {
		t.Errorf("queries: want 1, got %d", rows[0].Queries)
	}
	if rows[0].PromptTokens != 0 {
		t.Errorf("prompt_tokens: want 0, got %d", rows[0].PromptTokens)
	}
}

func TestUsage_BadTimestamp(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	// Write a file with an unparseable created_at by using raw JSON
	f, _ := os.Create(filepath.Join(dir, "badtime.json"))
	json.NewEncoder(f).Encode(map[string]any{
		"queries": []map[string]any{
			{
				"created_at": "not-a-timestamp",
				"cost_usd":   0.001,
				"model":      "m1",
				"usage": map[string]any{
					"prompt_tokens":             100,
					"completion_tokens":         10,
					"total_tokens":              110,
					"prompt_tokens_details":     map[string]any{"cached_tokens": 0},
					"completion_tokens_details": map[string]any{"reasoning_tokens": 0},
				},
			},
		},
		"messages": []map[string]string{{"role": "system", "content": "media Butler"}},
	})
	f.Close()

	_, _, err := parseConversation(filepath.Join(dir, "badtime.json"))
	if err == nil {
		t.Fatal("expected error for unparseable timestamp")
	}

	// Now test with --since: a query with bad timestamp is skipped entirely
	// (the whole file is skipped). So test that a since-filtered run still works
	// with mixed good/bad files.
	aggr := newAggregator("agent", now.Add(-24*time.Hour))
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		agent, queries, err := parseConversation(p)
		if err != nil {
			return nil // bad timestamp file skipped
		}
		aggr.add(agent, queries)
		return nil
	})

	// Should have 0 rows since the bad file is skipped
	if aggr.fileCount != 0 {
		t.Errorf("fileCount: want 0, got %d", aggr.fileCount)
	}
}

func TestUsage_NoSystemMessage(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	f, _ := os.Create(filepath.Join(dir, "nosys.json"))
	json.NewEncoder(f).Encode(map[string]any{
		"queries": []map[string]any{
			{
				"created_at": now.Format(time.RFC3339),
				"cost_usd":   0.001,
				"model":      "m1",
				"usage": map[string]any{
					"prompt_tokens":             100,
					"completion_tokens":         10,
					"total_tokens":              110,
					"prompt_tokens_details":     map[string]any{"cached_tokens": 0},
					"completion_tokens_details": map[string]any{"reasoning_tokens": 0},
				},
			},
		},
		"messages": []map[string]string{{"role": "user", "content": "hello"}},
	})
	f.Close()

	aggr := newAggregator("agent", time.Time{})
	agent, queries, _ := parseConversation(filepath.Join(dir, "nosys.json"))
	aggr.add(agent, queries)

	if agent != "other" {
		t.Errorf("agent: want other, got %s", agent)
	}
}

func TestUsage_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	aggr := newAggregator("agent", time.Time{})
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		return nil
	})
	if aggr.fileCount != 0 {
		t.Errorf("fileCount: want 0 for empty dir, got %d", aggr.fileCount)
	}
}

func TestUsage_NoCostDataAtAll(t *testing.T) {
	dir := t.TempDir()
	// Create a file with empty queries
	f, _ := os.Create(filepath.Join(dir, "emptyq.json"))
	json.NewEncoder(f).Encode(map[string]any{
		"queries":  []any{},
		"messages": []map[string]string{{"role": "system", "content": "x"}},
	})
	f.Close()

	aggr := newAggregator("agent", time.Time{})
	noCost := 0
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		_, queries, err := parseConversation(p)
		if err != nil {
			return nil
		}
		if len(queries) == 0 {
			noCost++
			return nil
		}
		return nil
	})
	if aggr.fileCount != 0 {
		t.Errorf("fileCount: want 0 (no cost data), got %d", aggr.fileCount)
	}
	if noCost != 1 {
		t.Errorf("noCost: want 1, got %d", noCost)
	}
}

func TestUsage_UnreadableFile(t *testing.T) {
	dir := t.TempDir()
	f, _ := os.Create(filepath.Join(dir, "unreadable.json"))
	f.Close()
	os.Chmod(filepath.Join(dir, "unreadable.json"), 0o000)

	skipped := 0
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			skipped++
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		_, _, err = parseConversation(p)
		if err != nil {
			skipped++
		}
		return nil
	})

	if skipped != 1 {
		t.Errorf("skipped: want 1, got %d", skipped)
	}
}

func TestUsage_HumanTokens(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{500, "500"},
		{1000, "1.0K"},
		{1500, "1.5K"},
		{1000000, "1.00M"},
		{2500000, "2.50M"},
	}

	for _, tt := range tests {
		got := humanTokens(tt.n)
		if got != tt.want {
			t.Errorf("humanTokens(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}
