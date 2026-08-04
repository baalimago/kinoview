package theatre

import (
	"testing"
)

// The deliverable envelope splits into the report and the collaborations it
// requests; anything without a parseable envelope is the report itself, and
// collaborations naming non-production roles are dropped at the door.
func TestParseReport(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		text       string
		wantReport string
		wantCollab int
	}{
		{"plain text", "16 beats / 3 acts", "16 beats / 3 acts", 0},
		{"envelope with collaborations", `{"report": "the brief", "collaborations": [{"role": "wardrobe", "question": "does silver read?"}]}`, "the brief", 1},
		{"empty report falls back to the text", `{"report": "", "collaborations": []}`, `{"report": "", "collaborations": []}`, 0},
		{"malformed JSON is the report itself", `{"report": broken`, `{"report": broken`, 0},
		{"prose around the envelope", `here you go: {"report": "the brief", "collaborations": [{"role": "scenographer", "question": "which backdrop?"}]}`, "the brief", 1},
		{"director collaboration dropped", `{"report": "r", "collaborations": [{"role": "director", "question": "q"}]}`, "r", 0},
		{"blank role dropped", `{"report": "r", "collaborations": [{"role": " ", "question": "q"}]}`, "r", 0},
		{"blank question dropped", `{"report": "r", "collaborations": [{"role": "wardrobe", "question": " "}]}`, "r", 0},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			report, collabs := parseReport(tt.text)
			if report != tt.wantReport {
				t.Errorf("report = %q, want %q", report, tt.wantReport)
			}
			if len(collabs) != tt.wantCollab {
				t.Errorf("collaborations = %d, want %d (%+v)", len(collabs), tt.wantCollab, collabs)
			}
		})
	}
}
