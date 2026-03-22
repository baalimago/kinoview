package model

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestSubtitleResourceJSONRoundTrip(t *testing.T) {
	t.Parallel()

	want := SubtitleResource{
		ID:                 "sub_20260322_abcd1234",
		ItemID:             "item-1",
		Source:             SubtitleSourceEmbedded,
		Origin:             SubtitleOriginEmbedded,
		Language:           "en",
		Format:             SubtitleFormatVTT,
		Label:              "English",
		StorageKey:         "item-1/sub_20260322_abcd1234.vtt",
		OriginalStorageKey: "item-1/sub_20260322_abcd1234.orig.srt",
		OriginalFileName:   "subtitle.srt",
		MIMEType:           "text/vtt",
		ChecksumSHA256:     "deadbeef",
		SizeBytes:          42,
		Score:              9.5,
		SourceRef:          "embedded:stream:2",
		CreatedAt:          time.Unix(123, 0).UTC(),
		UpdatedAt:          time.Unix(456, 0).UTC(),
	}

	got := roundTripJSON[SubtitleResource](t, want)
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("subtitle resource json roundtrip mismatch: want=%+v got=%+v", want, got)
	}
}

func TestSubtitleCandidateJSONRoundTrip(t *testing.T) {
	t.Parallel()

	want := SubtitleCandidate{
		Provider:            "opensubtitles",
		ProviderCandidateID: "cand-1",
		Language:            "en",
		Label:               "English HI",
		Format:              SubtitleFormatSRT,
		Score:               7.2,
		Release:             "Release",
		FileName:            "release.en.srt",
		FetchToken:          "opaque-token",
	}

	got := roundTripJSON[SubtitleCandidate](t, want)
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("subtitle candidate json roundtrip mismatch: want=%+v got=%+v", want, got)
	}
}

func TestSubtitleBindingJSONRoundTrip(t *testing.T) {
	t.Parallel()

	want := SubtitleBinding{
		ItemID:            "item-1",
		DefaultSubtitleID: "sub-1",
		UpdatedAt:         time.Unix(789, 0).UTC(),
	}

	got := roundTripJSON[SubtitleBinding](t, want)
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("subtitle binding json roundtrip mismatch: want=%+v got=%+v", want, got)
	}
}

func TestResolvedSubtitleJSONRoundTrip(t *testing.T) {
	t.Parallel()

	want := ResolvedSubtitle{
		SubtitleID: "sub-1",
		ItemID:     "item-1",
		Path:       "/tmp/sub.vtt",
		Format:     SubtitleFormatVTT,
		Language:   "en",
		Source:     SubtitleSourceEmbedded,
		Origin:     SubtitleOriginEmbedded,
	}

	got := roundTripJSON[ResolvedSubtitle](t, want)
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("resolved subtitle json roundtrip mismatch: want=%+v got=%+v", want, got)
	}
}

func roundTripJSON[T any](t *testing.T, want T) T {
	t.Helper()

	b, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("failed to marshal json: %v", err)
	}

	var got T
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("failed to unmarshal json: %v", err)
	}

	return got
}