package media

import (
	"context"
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
