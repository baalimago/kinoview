package media

import (
	"testing"
	"time"

	"github.com/baalimago/kinoview/internal/model"
)

func TestComputeLibraryFingerprint_IdenticalInputs(t *testing.T) {
	items := []model.Item{
		{Path: "/a.mp4", Name: "a.mp4"},
		{Path: "/b.mp4", Name: "b.mp4"},
		{Path: "/c.mp4", Name: "c.mp4"},
	}

	f1 := computeLibraryFingerprint(items)
	f2 := computeLibraryFingerprint(items)
	if f1 != f2 {
		t.Fatal("identical inputs must produce identical fingerprints")
	}
}

func TestComputeLibraryFingerprint_IgnoresUnsentMetadata(t *testing.T) {
	// Even if metadata changes, the projection stays the same (no metadata).
	items1 := []model.Item{{Path: "/a.mp4", Name: "a.mp4"}}
	f1 := computeLibraryFingerprint(items1)

	items2 := []model.Item{{Path: "/a.mp4", Name: "a.mp4"}}
	f2 := computeLibraryFingerprint(items2)

	if f1 != f2 {
		t.Fatal("fingerprint must be stable for items without metadata changes")
	}
}

func TestComputeLibraryFingerprint_LibraryChange(t *testing.T) {
	items1 := []model.Item{
		{Path: "/a.mp4", Name: "a.mp4"},
		{Path: "/b.mp4", Name: "b.mp4"},
	}
	items2 := []model.Item{
		{Path: "/a.mp4", Name: "a.mp4"},
		{Path: "/b.mp4", Name: "b.mp4"},
		{Path: "/c.mp4", Name: "c.mp4"},
	}

	f1 := computeLibraryFingerprint(items1)
	f2 := computeLibraryFingerprint(items2)
	if f1 == f2 {
		t.Fatal("adding an item must change the fingerprint")
	}
}

func TestComputeLibraryFingerprint_ItemRename(t *testing.T) {
	items1 := []model.Item{{Path: "/a.mp4", Name: "old.mp4"}}
	items2 := []model.Item{{Path: "/a.mp4", Name: "new.mp4"}}

	f1 := computeLibraryFingerprint(items1)
	f2 := computeLibraryFingerprint(items2)
	if f1 == f2 {
		t.Fatal("renaming an item must change the fingerprint")
	}
}

func TestComputeLibraryFingerprint_StableAcrossSnapshotOrder(t *testing.T) {
	// Simulate Snapshot() returning items in different map-iteration orders.
	items := []model.Item{
		{Path: "/c.mp4", Name: "c.mp4"},
		{Path: "/a.mp4", Name: "a.mp4"},
		{Path: "/b.mp4", Name: "b.mp4"},
	}

	f1 := computeLibraryFingerprint(items)

	// Reversed order.
	items2 := []model.Item{
		{Path: "/b.mp4", Name: "b.mp4"},
		{Path: "/a.mp4", Name: "a.mp4"},
		{Path: "/c.mp4", Name: "c.mp4"},
	}
	f2 := computeLibraryFingerprint(items2)

	if f1 != f2 {
		t.Fatal("fingerprint must be stable regardless of input order")
	}
}

func TestComputeContextFingerprint_IgnoresSessionIdentity(t *testing.T) {
	now := time.Date(2026, 7, 25, 14, 0, 0, 0, time.UTC)

	c1 := model.ClientContext{
		SessionID:      "session-aaa",
		StartTime:      now.Add(-1 * time.Hour),
		LastPlayedName: "Movie A",
		ViewingHistory: []model.ViewMetadata{
			{Name: "Ep1", ViewedAt: now, PlayedForSec: "300"},
		},
	}
	c2 := model.ClientContext{
		SessionID:      "session-bbb",
		StartTime:      now.Add(-2 * time.Hour),
		LastPlayedName: "Movie A",
		ViewingHistory: []model.ViewMetadata{
			{Name: "Ep1", ViewedAt: now, PlayedForSec: "300"},
		},
	}

	f1 := computeContextFingerprint(c1, now)
	f2 := computeContextFingerprint(c2, now)
	if f1 != f2 {
		t.Fatal("SessionID and StartTime must not affect the fingerprint")
	}
}

func TestComputeContextFingerprint_ContextChange(t *testing.T) {
	now := time.Date(2026, 7, 25, 14, 0, 0, 0, time.UTC)

	c1 := model.ClientContext{LastPlayedName: "Movie A"}
	c2 := model.ClientContext{LastPlayedName: "Movie B"}

	f1 := computeContextFingerprint(c1, now)
	f2 := computeContextFingerprint(c2, now)
	if f1 == f2 {
		t.Fatal("different LastPlayedName must produce different fingerprints")
	}
}

func TestComputeContextFingerprint_DayOfWeek(t *testing.T) {
	c := model.ClientContext{LastPlayedName: "Movie A"}

	friday := time.Date(2026, 7, 24, 14, 0, 0, 0, time.UTC)   // Friday
	saturday := time.Date(2026, 7, 25, 14, 0, 0, 0, time.UTC) // Saturday

	f1 := computeContextFingerprint(c, friday)
	f2 := computeContextFingerprint(c, saturday)
	if f1 == f2 {
		t.Fatal("different days of week must produce different fingerprints")
	}
}

func TestComputeContextFingerprint_PartOfDay(t *testing.T) {
	c := model.ClientContext{LastPlayedName: "Movie A"}

	morning := time.Date(2026, 7, 25, 8, 0, 0, 0, time.UTC)
	evening := time.Date(2026, 7, 25, 20, 0, 0, 0, time.UTC)

	f1 := computeContextFingerprint(c, morning)
	f2 := computeContextFingerprint(c, evening)
	if f1 == f2 {
		t.Fatal("different parts of day must produce different fingerprints")
	}
}

func TestProgressBucket_WithinBucket(t *testing.T) {
	// 300s and 599s are both in bucket 0.  600s and 1199s are both in bucket 1.
	b1 := progressBucket("300")
	b2 := progressBucket("599")
	if b1 != b2 {
		t.Fatalf("300s and 599s must be in the same bucket, got %d and %d", b1, b2)
	}

	b3 := progressBucket("600")
	b4 := progressBucket("1199")
	if b3 != b4 {
		t.Fatalf("600s and 1199s must be in the same bucket, got %d and %d", b3, b4)
	}
}

func TestProgressBucket_CrossesBucket(t *testing.T) {
	b1 := progressBucket("599")
	b2 := progressBucket("600")
	if b1 == b2 {
		t.Fatalf("599s (bucket %d) and 600s (bucket %d) must differ", b1, b2)
	}
}

func TestProgressBucket_HMSFormat(t *testing.T) {
	// 1:30:00 = 5400s -> bucket 9
	// 1:40:00 = 6000s -> bucket 10
	b1 := progressBucket("1:30:00")
	b2 := progressBucket("1:40:00")
	if b1 == b2 {
		t.Fatalf("different HMS durations must be in different buckets, got %d and %d", b1, b2)
	}

	// 0:05:00 = 300s -> bucket 0
	// 0:09:59 = 599s -> bucket 0
	b3 := progressBucket("0:05:00")
	b4 := progressBucket("0:09:59")
	if b3 != b4 {
		t.Fatalf("close HMS durations must be in same bucket, got %d and %d", b3, b4)
	}
}

func TestProgressBucket_EmptyString(t *testing.T) {
	b1 := progressBucket("")
	b2 := progressBucket("")
	if b1 != b2 {
		t.Fatalf("empty strings must produce consistent hash, got %d and %d", b1, b2)
	}
}

func TestComputeContextFingerprint_ProgressWithinBucket(t *testing.T) {
	now := time.Date(2026, 7, 25, 14, 0, 0, 0, time.UTC)

	c1 := model.ClientContext{
		LastPlayedName: "Movie",
		ViewingHistory: []model.ViewMetadata{
			{Name: "Ep1", PlayedForSec: "300"},
		},
	}
	c2 := model.ClientContext{
		LastPlayedName: "Movie",
		ViewingHistory: []model.ViewMetadata{
			{Name: "Ep1", PlayedForSec: "599"},
		},
	}

	f1 := computeContextFingerprint(c1, now)
	f2 := computeContextFingerprint(c2, now)
	if f1 != f2 {
		t.Fatal("progress within same bucket must not change fingerprint")
	}
}

func TestComputeContextFingerprint_ProgressCrossesBucket(t *testing.T) {
	now := time.Date(2026, 7, 25, 14, 0, 0, 0, time.UTC)

	c1 := model.ClientContext{
		LastPlayedName: "Movie",
		ViewingHistory: []model.ViewMetadata{
			{Name: "Ep1", PlayedForSec: "599"},
		},
	}
	c2 := model.ClientContext{
		LastPlayedName: "Movie",
		ViewingHistory: []model.ViewMetadata{
			{Name: "Ep1", PlayedForSec: "600"},
		},
	}

	f1 := computeContextFingerprint(c1, now)
	f2 := computeContextFingerprint(c2, now)
	if f1 == f2 {
		t.Fatal("crossing a progress bucket must change fingerprint")
	}
}

func TestComputeContextFingerprint_EmptyViewingHistory(t *testing.T) {
	now := time.Date(2026, 7, 25, 14, 0, 0, 0, time.UTC)

	c1 := model.ClientContext{LastPlayedName: "Movie"}
	c2 := model.ClientContext{LastPlayedName: "Movie"}

	f1 := computeContextFingerprint(c1, now)
	f2 := computeContextFingerprint(c2, now)
	if f1 != f2 {
		t.Fatal("empty viewing history must produce stable fingerprint")
	}
}

func TestComputeContextFingerprint_ViewingHistoryAdded(t *testing.T) {
	now := time.Date(2026, 7, 25, 14, 0, 0, 0, time.UTC)

	c1 := model.ClientContext{LastPlayedName: "Movie"}
	c2 := model.ClientContext{
		LastPlayedName: "Movie",
		ViewingHistory: []model.ViewMetadata{
			{Name: "Ep1", PlayedForSec: "300"},
		},
	}

	f1 := computeContextFingerprint(c1, now)
	f2 := computeContextFingerprint(c2, now)
	if f1 == f2 {
		t.Fatal("adding viewing history must change fingerprint")
	}
}

func TestPartOfDay(t *testing.T) {
	tests := []struct {
		hour int
		want string
	}{
		{0, "night"},
		{5, "night"},
		{6, "morning"},
		{11, "morning"},
		{12, "afternoon"},
		{17, "afternoon"},
		{18, "evening"},
		{23, "evening"},
	}
	for _, tt := range tests {
		tm := time.Date(2026, 7, 25, tt.hour, 0, 0, 0, time.UTC)
		got := partOfDay(tm)
		if got != tt.want {
			t.Errorf("hour %d: got %q, want %q", tt.hour, got, tt.want)
		}
	}
}

func TestParseDurationSeconds(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"300", 300},
		{"0", 0},
		{"1:30:00", 5400},
		{"0:05:00", 300},
		{"2:00", 120},
	}
	for _, tt := range tests {
		got, err := parseDurationSeconds(tt.input)
		if err != nil {
			t.Errorf("parseDurationSeconds(%q) error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("parseDurationSeconds(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestParseDurationSeconds_Errors(t *testing.T) {
	for _, input := range []string{"", "abc", "1:2:3:4"} {
		_, err := parseDurationSeconds(input)
		if err == nil {
			t.Errorf("parseDurationSeconds(%q) expected error", input)
		}
	}
}
