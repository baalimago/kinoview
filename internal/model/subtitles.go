package model

import "time"

type SubtitleOrigin string

const (
	SubtitleOriginEmbedded   SubtitleOrigin = "embedded"
	SubtitleOriginDownloaded SubtitleOrigin = "downloaded"
	SubtitleOriginManual     SubtitleOrigin = "manual"
	SubtitleOriginGenerated  SubtitleOrigin = "generated"
)

type SubtitleSource string

const (
	SubtitleSourceEmbedded      SubtitleSource = "embedded"
	SubtitleSourceOpenSubtitles SubtitleSource = "opensubtitles"
	SubtitleSourceManualURL     SubtitleSource = "manual_url"
	SubtitleSourceGenerated     SubtitleSource = "generated"
)

type SubtitleFormat string

const (
	SubtitleFormatVTT     SubtitleFormat = "vtt"
	SubtitleFormatSRT     SubtitleFormat = "srt"
	SubtitleFormatASS     SubtitleFormat = "ass"
	SubtitleFormatSSA     SubtitleFormat = "ssa"
	SubtitleFormatSUB     SubtitleFormat = "sub"
	SubtitleFormatUnknown SubtitleFormat = "unknown"
)

type SubtitleResource struct {
	ID                 string         `json:"id"`
	ItemID             string         `json:"itemID"`
	Source             SubtitleSource `json:"source"`
	Origin             SubtitleOrigin `json:"origin"`
	Language           string         `json:"language"`
	Format             SubtitleFormat `json:"format"`
	Label              string         `json:"label"`
	StorageKey         string         `json:"storageKey"`
	OriginalStorageKey string         `json:"originalStorageKey,omitempty"`
	OriginalFileName   string         `json:"originalFileName,omitempty"`
	MIMEType           string         `json:"mimeType,omitempty"`
	ChecksumSHA256     string         `json:"checksumSHA256,omitempty"`
	SizeBytes          int64          `json:"sizeBytes"`
	Score              float64        `json:"score"`
	SourceRef          string         `json:"sourceRef,omitempty"`
	CreatedAt          time.Time      `json:"createdAt"`
	UpdatedAt          time.Time      `json:"updatedAt"`
}

type SubtitleCandidate struct {
	Provider            string         `json:"provider"`
	ProviderCandidateID string         `json:"providerCandidateID"`
	Language            string         `json:"language"`
	Label               string         `json:"label"`
	Format              SubtitleFormat `json:"format"`
	Score               float64        `json:"score"`
	Release             string         `json:"release,omitempty"`
	FileName            string         `json:"fileName,omitempty"`
	FetchToken          string         `json:"fetchToken,omitempty"`
}

type SubtitleBinding struct {
	ItemID            string    `json:"itemID"`
	DefaultSubtitleID string    `json:"defaultSubtitleID"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type ResolvedSubtitle struct {
	SubtitleID string         `json:"subtitleID"`
	ItemID     string         `json:"itemID"`
	Path       string         `json:"path"`
	Format     SubtitleFormat `json:"format"`
	Language   string         `json:"language"`
	Source     SubtitleSource `json:"source"`
	Origin     SubtitleOrigin `json:"origin"`
}