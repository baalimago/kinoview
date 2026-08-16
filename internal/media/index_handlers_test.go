package media

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/baalimago/kinoview/internal/model"
)

// recordingTeller is a Teller stub that captures the context each Prepare
// receives, so a test can pin the trigger path's effective deadline.
type recordingTeller struct {
	deadline chan time.Time // the ctx deadline Prepare saw; zero = no deadline
}

func (t *recordingTeller) Next() model.Story { return model.Story{} }

func (t *recordingTeller) Prepare(ctx context.Context, reason string) bool {
	dl, _ := ctx.Deadline()
	t.deadline <- dl
	return true
}

func (t *recordingTeller) Warm(ctx context.Context) {}

// R3-01: the HTTP-triggered generation path must not undercut the theatre's
// wall-clock flag. prepareNextStory used to wrap Prepare in a fixed 2-minute
// timeout, which hard-cancelled every story-consumed / session-end generation
// at 2 minutes whatever -theatreWallClock says (the earlier parent deadline
// wins in runProduction). The theatre bounds each generation itself (wall
// clock + single-flight + call budgets), so the trigger passes a context
// without a caller-side deadline — the flag is the only authority.
func TestPrepareNextStory_LeavesWallClockToTheTheatre(t *testing.T) {
	teller := &recordingTeller{deadline: make(chan time.Time, 1)}
	idx := &Indexer{theatre: teller}

	idx.prepareNextStory("test")

	select {
	case dl := <-teller.deadline:
		if !dl.IsZero() {
			t.Errorf("Prepare received a caller-side deadline %v from now; the theatre's own wall clock must be the only bound", time.Until(dl))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Prepare never ran")
	}
}

// feedbackRecorder is a Feedbacker stub that records the call so a test can
// pin exactly what the handler forwards, and reports write failures on
// demand (decision Q6 — the handler holds the recorder directly, no theatre
// type-assertion).
type feedbackRecorder struct {
	storyID string
	rating  int
	comment string
	err     error
}

func (f *feedbackRecorder) Feedback(_ context.Context, storyID string, rating int, comment string) error {
	f.storyID = storyID
	f.rating = rating
	f.comment = comment
	return f.err
}

func TestIntroFeedbackHandler_StatusCodes(t *testing.T) {
	fb := &feedbackRecorder{}

	cases := []struct {
		name   string
		idx    *Indexer
		method string
		body   string
		want   int
	}{
		{"happy path", &Indexer{feedback: fb}, http.MethodPost, `{"storyId":"stry_abc12345","rating":1,"comment":"more dog"}`, http.StatusNoContent},
		{"get is method not allowed", &Indexer{feedback: fb}, http.MethodGet, ``, http.StatusMethodNotAllowed},
		{"rating zero", &Indexer{feedback: fb}, http.MethodPost, `{"storyId":"stry_abc12345","rating":0}`, http.StatusBadRequest},
		{"rating out of range", &Indexer{feedback: fb}, http.MethodPost, `{"storyId":"stry_abc12345","rating":5}`, http.StatusBadRequest},
		{"malformed json", &Indexer{feedback: fb}, http.MethodPost, `{`, http.StatusBadRequest},
		{"empty body", &Indexer{feedback: fb}, http.MethodPost, ``, http.StatusBadRequest},
		{"unknown field", &Indexer{feedback: fb}, http.MethodPost, `{"storyId":"stry_abc12345","rating":1,"extra":true}`, http.StatusBadRequest},
		{"bad story id", &Indexer{feedback: fb}, http.MethodPost, `{"storyId":"../etc","rating":1}`, http.StatusBadRequest},
		{"nil recorder", &Indexer{}, http.MethodPost, `{"storyId":"stry_abc12345","rating":1}`, http.StatusNotImplemented},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/intro/feedback", strings.NewReader(tt.body))
			rr := httptest.NewRecorder()
			tt.idx.introFeedbackHandler()(rr, req)
			if rr.Code != tt.want {
				t.Errorf("status = %d, want %d", rr.Code, tt.want)
			}
		})
	}
}

// The happy path forwards exactly what the audience sent and never triggers a
// generation (decision Q3 — feedback does not bypass the cooldown). With the
// recorder held directly, the handler structurally cannot touch the theatre;
// the Teller wired alongside pins the invariant.
func TestIntroFeedbackHandler_HappyPathForwardsAndSkipsPrepare(t *testing.T) {
	fb := &feedbackRecorder{}
	teller := &recordingTeller{deadline: make(chan time.Time, 1)}
	idx := &Indexer{theatre: teller, feedback: fb}

	req := httptest.NewRequest(http.MethodPost, "/intro/feedback",
		strings.NewReader(`{"storyId":"stry_abc12345","rating":1,"comment":"more dog"}`))
	rr := httptest.NewRecorder()
	idx.introFeedbackHandler()(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rr.Code)
	}
	if fb.storyID != "stry_abc12345" || fb.rating != 1 || fb.comment != "more dog" {
		t.Errorf("feedback forwarded (%q, %d, %q), want (stry_abc12345, 1, more dog)", fb.storyID, fb.rating, fb.comment)
	}
	select {
	case <-teller.deadline:
		t.Fatal("feedback triggered a Prepare; the note must never bypass the cooldown")
	default:
	}
}

// A feedback write failure surfaces as a 500 — the note is not silently
// dropped (error coverage row: append/commit fails).
func TestIntroFeedbackHandler_WriteFailureIs500(t *testing.T) {
	fb := &feedbackRecorder{err: errors.New("disk full")}
	idx := &Indexer{feedback: fb}

	req := httptest.NewRequest(http.MethodPost, "/intro/feedback",
		strings.NewReader(`{"storyId":"stry_abc12345","rating":1}`))
	rr := httptest.NewRecorder()
	idx.introFeedbackHandler()(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rr.Code)
	}
}
