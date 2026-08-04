package theatre

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomic_RoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "doc.json")
	if err := writeFileAtomic(path, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "hello" {
		t.Fatalf("read back = %q, %v", b, err)
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "*.tmp"))
	if len(leftovers) != 0 {
		t.Errorf("temp files left after success: %v", leftovers)
	}
}

// A failure mid-write must remove the temp file and leave the target
// untouched. The rename onto a directory is the failure: the temp file is
// already written by then, so the cleanup path is what this exercises.
func TestWriteFileAtomic_FailureCleansTempAndKeepsTarget(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.json")
	if err := writeFileAtomic(path, []byte("old")); err != nil {
		t.Fatal(err)
	}
	// Replace the target with a directory so the final rename cannot succeed.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(path, []byte("new")); err == nil {
		t.Fatal("expected the rename onto a directory to fail")
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !fi.IsDir() {
		t.Error("target was replaced despite the failed write")
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if len(leftovers) != 0 {
		t.Errorf("temp files left behind: %v", leftovers)
	}
}

func TestTruncateRunes(t *testing.T) {
	t.Parallel()
	if got := truncateRunes("short", 10); got != "short" {
		t.Errorf("truncateRunes(short) = %q", got)
	}
	// Truncation must not split a multi-byte rune: the result is valid UTF-8.
	got := truncateRunes("🫨🫨🫨🫨", 3)
	if len([]rune(got)) != 3 {
		t.Errorf("truncateRunes = %q (%d runes), want 3", got, len([]rune(got)))
	}
	// Byte length may exceed the cap while the rune count does not: multi-byte
	// runes under the cap survive untouched.
	got = truncateRunes("🫨🫨", 3)
	if got != "🫨🫨" {
		t.Errorf("truncateRunes = %q, want the two runes intact", got)
	}
}
