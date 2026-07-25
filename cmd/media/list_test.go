package media

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/baalimago/kinoview/internal/model"
)

func TestSplitAtSelection(t *testing.T) {
	tests := []struct {
		name          string
		tokens        []string
		wantTable     []string
		wantRemaining []string
	}{
		{
			name:          "simple select",
			tokens:        []string{"0"},
			wantTable:     []string{"0"},
			wantRemaining: []string{},
		},
		{
			name:          "nav then select",
			tokens:        []string{"n", "n", "5"},
			wantTable:     []string{"n", "n", "5"},
			wantRemaining: []string{},
		},
		{
			name:          "nav select action",
			tokens:        []string{"n", "n", "5", "i"},
			wantTable:     []string{"n", "n", "5"},
			wantRemaining: []string{"i"},
		},
		{
			name:          "filter select action",
			tokens:        []string{"/office", "0", "d"},
			wantTable:     []string{"/office", "0"},
			wantRemaining: []string{"d"},
		},
		{
			name:          "select back re-select",
			tokens:        []string{"0", "b", "1", "i"},
			wantTable:     []string{"0"},
			wantRemaining: []string{"b", "1", "i"},
		},
		{
			name:          "range selection",
			tokens:        []string{"0:5", "i"},
			wantTable:     []string{"0:5"},
			wantRemaining: []string{"i"},
		},
		{
			name:          "comma selection",
			tokens:        []string{"0,1,2", "d"},
			wantTable:     []string{"0,1,2"},
			wantRemaining: []string{"d"},
		},
		{
			name:          "only actions no selection",
			tokens:        []string{"n", "p", "/test", "b"},
			wantTable:     []string{"n", "p", "/test", "b"},
			wantRemaining: nil,
		},
		{
			name:          "empty",
			tokens:        []string{},
			wantTable:     []string{},
			wantRemaining: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTable, gotRemaining := splitAtSelection(tt.tokens)
			if !reflect.DeepEqual(gotTable, tt.wantTable) {
				t.Errorf("tableTokens = %v, want %v", gotTable, tt.wantTable)
			}
			if !reflect.DeepEqual(gotRemaining, tt.wantRemaining) {
				t.Errorf("remaining = %v, want %v", gotRemaining, tt.wantRemaining)
			}
		})
	}
}

func TestIsTableAction(t *testing.T) {
	actions := []string{"n", "next", "p", "prev", "b", "back", "q", "quit"}
	for _, a := range actions {
		if !isTableAction(a) {
			t.Errorf("expected %q to be a table action", a)
		}
	}

	if !isTableAction("/somefilter") {
		t.Error("expected / filter to be a table action")
	}

	nonActions := []string{"0", "5", "i", "d", "r", "0:5", "0,1,2", "", "something"}
	for _, a := range nonActions {
		if isTableAction(a) {
			t.Errorf("expected %q NOT to be a table action", a)
		}
	}
}

func TestIsNumericSelection(t *testing.T) {
	valid := []string{"0", "5", "10", "100", "0:5", "1:10", "0,1", "0,1,2"}
	for _, v := range valid {
		if !isNumericSelection(v) {
			t.Errorf("expected %q to be a numeric selection", v)
		}
	}

	invalid := []string{"", "abc", "i", "d", "r", "b", "/filter", "n", "next", "1:abc", "a,b", "1:", ":2"}
	for _, v := range invalid {
		if isNumericSelection(v) {
			t.Errorf("expected %q NOT to be a numeric selection", v)
		}
	}
}

func TestShortMIME(t *testing.T) {
	tests := []struct {
		mime string
		want string
	}{
		{"video/mp4", "video"},
		{"video/x-matroska", "video"},
		{"image/jpeg", "image"},
		{"image/png", "image"},
		{"application/pdf", "other"},
		{"audio/mp3", "other"},
	}
	for _, tt := range tests {
		got := shortMIME(tt.mime)
		if got != tt.want {
			t.Errorf("shortMIME(%q) = %q, want %q", tt.mime, got, tt.want)
		}
	}
}

func TestTruncateTo(t *testing.T) {
	tests := []struct {
		s      string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello world this is long", 10, "hello w..."},
		{"abcdefghij", 10, "abcdefghij"},
		{"abcdefghijk", 10, "abcdefg..."},
		{"", 5, ""},
	}
	for _, tt := range tests {
		got := truncateTo(tt.s, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncateTo(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
		}
	}
}

func TestHumanSize(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
	}
	for _, tt := range tests {
		got := humanSize(tt.bytes)
		if got != tt.want {
			t.Errorf("humanSize(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

func TestFileSizeStr(t *testing.T) {
	// Create a temp file with known size.
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "testfile"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Write([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	got := fileSizeStr(filepath.Join(dir, "testfile"))
	if got != "5 B" {
		t.Errorf("fileSizeStr = %q, want %q", got, "5 B")
	}

	// Non-existent file.
	got = fileSizeStr("/nonexistent/file/path")
	if got != "?" {
		t.Errorf("fileSizeStr for missing file = %q, want ?", got)
	}
}

func TestRowFormatter(t *testing.T) {
	lc := &listController{}
	item := model.Item{
		Name:     "Test Movie.mkv",
		MIMEType: "video/x-matroska",
		Path:     "/dev/null",
	}
	got, err := lc.rowFormatter(0, item)
	if err != nil {
		t.Fatalf("rowFormatter error: %v", err)
	}
	// Verify basic format: index, name, type, metadata, attempts, size
	if len(got) == 0 {
		t.Error("expected non-empty row")
	}

	// With metadata.
	raw := json.RawMessage(`{"title":"Test"}`)
	item.Metadata = &raw
	got, err = lc.rowFormatter(0, item)
	if err != nil {
		t.Fatalf("rowFormatter error: %v", err)
	}
	if len(got) == 0 {
		t.Error("expected non-empty row with metadata")
	}

	// With subtitle paths — verify subs column shows ✓
	item.SubtitlePaths = []string{"/some/sub.srt"}
	got, err = lc.rowFormatter(0, item)
	if err != nil {
		t.Fatalf("rowFormatter error: %v", err)
	}
	if !strings.Contains(got, "✓") {
		t.Error("expected ✓ for subs column when subtitles are associated")
	}

	// Without subtitle paths — verify subs column shows ✗
	item.SubtitlePaths = nil
	got, err = lc.rowFormatter(0, item)
	if err != nil {
		t.Fatalf("rowFormatter error: %v", err)
	}
	if !strings.Contains(got, "✗") {
		t.Error("expected ✗ for subs column when no subtitles associated")
	}
}

func TestMediaTableHeader(t *testing.T) {
	h := mediaTableHeader()
	if len(h) == 0 {
		t.Error("expected non-empty header")
	}
}

func TestFormatMetadata(t *testing.T) {
	// Valid JSON.
	raw := json.RawMessage(`{"title":"Inception","year":2010}`)
	got := formatMetadata(raw)
	if len(got) == 0 {
		t.Error("expected non-empty metadata")
	}

	// Long JSON gets truncated.
	long := make(map[string]string)
	for i := range 20 {
		long[fmt.Sprintf("key%d", i)] = "value"
	}
	b, _ := json.Marshal(long)
	raw = json.RawMessage(b)
	got = formatMetadata(raw)
	if len(got) > 123 { // 120 + "..."
		t.Errorf("expected truncated metadata, got len=%d", len(got))
	}

	// Invalid JSON.
	raw = json.RawMessage(`not json`)
	got = formatMetadata(raw)
	if got != "not json" {
		t.Errorf("expected raw string for invalid JSON, got %q", got)
	}
}

func TestPrintItemSummary(t *testing.T) {
	item := model.Item{
		Name:     "Test",
		Path:     "/tmp/test",
		MIMEType: "video/mp4",
		ID:       "abc123",
	}
	// Just verify it doesn't panic.
	printItemSummary(item)

	raw := json.RawMessage(`{"key":"val"}`)
	item.Metadata = &raw
	item.ClassificationAttempts = 3
	item.ClassificationError = "some error"
	printItemSummary(item)

	// Test with subtitle paths
	item.SubtitlePaths = []string{"/tmp/sub-en.srt", "/tmp/sub-fr.vtt"}
	printItemSummary(item)
}

func TestAddSubtitlePath(t *testing.T) {
	tmp := t.TempDir()

	// Create a valid subtitle file
	validSrt := filepath.Join(tmp, "sub.srt")
	os.WriteFile(validSrt, []byte("1\n00:00:01,000 --> 00:00:02,000\nHello"), 0o644)

	// Create a non-subtitle file
	txtFile := filepath.Join(tmp, "notes.txt")
	os.WriteFile(txtFile, []byte("hello"), 0o644)

	t.Run("valid srt", func(t *testing.T) {
		item := &model.Item{Name: "test"}
		if err := addSubtitlePath(item, validSrt); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(item.SubtitlePaths) != 1 {
			t.Fatalf("expected 1 path, got %d", len(item.SubtitlePaths))
		}
	})

	t.Run("valid vtt", func(t *testing.T) {
		vtt := filepath.Join(tmp, "sub.vtt")
		os.WriteFile(vtt, []byte("WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nHello"), 0o644)
		item := &model.Item{Name: "test"}
		if err := addSubtitlePath(item, vtt); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects non-existent file", func(t *testing.T) {
		item := &model.Item{Name: "test"}
		err := addSubtitlePath(item, "/nonexistent/path.srt")
		if err == nil {
			t.Fatal("expected error for non-existent file")
		}
	})

	t.Run("rejects directory", func(t *testing.T) {
		item := &model.Item{Name: "test"}
		err := addSubtitlePath(item, tmp)
		if err == nil {
			t.Fatal("expected error for directory")
		}
	})

	t.Run("rejects non-subtitle extension", func(t *testing.T) {
		item := &model.Item{Name: "test"}
		err := addSubtitlePath(item, txtFile)
		if err == nil {
			t.Fatal("expected error for .txt file")
		}
	})

	t.Run("rejects duplicate", func(t *testing.T) {
		item := &model.Item{Name: "test"}
		if err := addSubtitlePath(item, validSrt); err != nil {
			t.Fatalf("first add: %v", err)
		}
		err := addSubtitlePath(item, validSrt)
		if err == nil {
			t.Fatal("expected error for duplicate")
		}
	})

	t.Run("accepts .ass and .ssa", func(t *testing.T) {
		for _, ext := range []string{".ass", ".ssa", ".sub"} {
			f := filepath.Join(tmp, "sub"+ext)
			os.WriteFile(f, []byte("dummy"), 0o644)
			item := &model.Item{Name: "test"}
			if err := addSubtitlePath(item, f); err != nil {
				t.Errorf("unexpected error for %s: %v", ext, err)
			}
		}
	})

	t.Run("rejects relative paths that don't exist", func(t *testing.T) {
		item := &model.Item{Name: "test"}
		err := addSubtitlePath(item, "relative/sub.srt")
		if err == nil {
			t.Fatal("expected error for non-existent relative path")
		}
	})

	t.Run("accepts existing relative path", func(t *testing.T) {
		relSrt := filepath.Join(tmp, "rel-sub.srt")
		os.WriteFile(relSrt, []byte("dummy"), 0o644)
		item := &model.Item{Name: "test"}
		if err := addSubtitlePath(item, relSrt); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestSmartSplit(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "simple",
			input: "a /path/to/file.srt",
			want:  []string{"a", "/path/to/file.srt"},
		},
		{
			name:  "quoted path with spaces",
			input: `a "/path/with spaces/file.srt"`,
			want:  []string{"a", "/path/with spaces/file.srt"},
		},
		{
			name:  "single-quoted path",
			input: `a '/path/with spaces/file.srt'`,
			want:  []string{"a", "/path/with spaces/file.srt"},
		},
		{
			name:  "remove with index",
			input: "r 0",
			want:  []string{"r", "0"},
		},
		{
			name:  "single word",
			input: "b",
			want:  []string{"b"},
		},
		{
			name:  "empty",
			input: "",
			want:  nil,
		},
		{
			name:  "only spaces",
			input: "   ",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := smartSplit(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("smartSplit(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
