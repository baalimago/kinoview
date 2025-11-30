package model

import (
	"encoding/json"
	"testing"
)

func TestMediaInfo_UnmarshalJSON(t *testing.T) {
	// Sample JSON output from ffprobe
	jsonInput := `{
    "streams": [
        {
            "index": 3,
            "codec_name": "subrip",
            "codec_long_name": "SubRip subtitle",
            "codec_type": "subtitle",
            "codec_tag_string": "[0][0][0][0]",
            "codec_tag": "0x0000",
            "r_frame_rate": "0/0",
            "avg_frame_rate": "0/0",
            "time_base": "1/1000",
            "start_pts": 0,
            "start_time": "0.000000",
            "duration_ts": 3591465,
            "duration": "3591.465000",
            "disposition": {
                "default": 1,
                "dub": 0,
                "original": 0,
                "comment": 0,
                "lyrics": 0,
                "karaoke": 0,
                "forced": 0,
                "hearing_impaired": 0,
                "visual_impaired": 0,
                "clean_effects": 0,
                "attached_pic": 0
            },
            "tags": {
                "language": "eng",
                "title": "English"
            }
        }
    ]
}`

	var info MediaInfo
	if err := json.Unmarshal([]byte(jsonInput), &info); err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	if len(info.Streams) != 1 {
		t.Fatalf("Expected 1 stream, got %d", len(info.Streams))
	}

	stream := info.Streams[0]
	if stream.Index != 3 {
		t.Errorf("Expected index 3, got %d", stream.Index)
	}
	if stream.CodecName != "subrip" {
		t.Errorf("Expected codec_name subrip, got %s", stream.CodecName)
	}
	if stream.Tags.Language != "eng" {
		t.Errorf("Expected language eng, got %s", stream.Tags.Language)
	}
	if stream.Disposition.Default != 1 {
		t.Errorf("Expected disposition default 1, got %d", stream.Disposition.Default)
	}
}
