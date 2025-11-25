package butler

import (
	"context"
	"errors"
	"testing"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/model"
)

func TestSelectEnglish(t *testing.T) {
	tests := []struct {
		name      string
		llmOutput string
		streams   []model.Stream
		wantIdx   int
		wantErr   bool
	}{
		{
			name:      "success",
			llmOutput: `{"index": 2}`,
			streams: []model.Stream{
				{Index: 0, CodecType: "video"},
				{Index: 2, CodecType: "subtitle"},
			},
			wantIdx: 2,
			wantErr: false,
		},
		{
			name:      "no subtitles",
			llmOutput: "",
			streams: []model.Stream{
				{Index: 0, CodecType: "video"},
			},
			wantIdx: -1,
			wantErr: true,
		},
		{
			name:      "llm error response",
			llmOutput: `{"error": "none found"}`,
			streams: []model.Stream{
				{Index: 2, CodecType: "subtitle"},
			},
			wantIdx: -1,
			wantErr: true,
		},
		{
			name:      "invalid json",
			llmOutput: `not json`,
			streams: []model.Stream{
				{Index: 2, CodecType: "subtitle"},
			},
			wantIdx: -1,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeLLM{
				resp: models.Chat{
					Messages: []models.Message{
						{Role: "assistant", Content: tc.llmOutput},
					},
				},
			}
			s := &selector{llm: f}
			got, err := s.SelectEnglish(context.Background(), tc.streams)
			if (err != nil) != tc.wantErr {
				t.Errorf("wantErr %v, got error %v", tc.wantErr, err)
			}
			if !tc.wantErr && got != tc.wantIdx {
				t.Errorf("got index %d, want %d", got, tc.wantIdx)
			}
		})
	}
}

func TestSelectEnglish_LLMFailure(t *testing.T) {
	f := &fakeLLM{err: errors.New("boom")}
	s := &selector{llm: f}
	streams := []model.Stream{{Index: 1, CodecType: "subtitle"}}
	_, err := s.SelectEnglish(context.Background(), streams)
	if err == nil {
		t.Error("expected error, got nil")
	}
}
