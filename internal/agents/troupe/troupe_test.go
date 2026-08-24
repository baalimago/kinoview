package troupe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestTroupe builds a facade over one director.
func newTestTroupe(t *testing.T, d *Director, opts ...TroupeOption) *Troupe {
	t.Helper()
	all := append([]TroupeOption{WithGenerationDirector(d)}, opts...)
	tr, err := NewTroupe(all...)
	if err != nil {
		t.Fatalf("NewTroupe: %v", err)
	}
	return tr
}

// TestNewTroupe_Errors pins the required option and the hardcoded default:
// a facade without a director is refused, and the cooldown defaults to the
// hardcoded generationCooldown (decision 13 — the cooldown is hardcoded,
// never a flag).
func TestNewTroupe_Errors(t *testing.T) {
	if _, err := NewTroupe(); err == nil || !strings.Contains(err.Error(), "director can't be nil") {
		t.Fatalf("err = %v, want a director error", err)
	}
	d, _ := newTestDirector(t, &fakeSwarm{})
	if tr, err := NewTroupe(WithGenerationDirector(d)); err != nil {
		t.Fatalf("NewTroupe: %v", err)
	} else if tr.cooldown != generationCooldown {
		t.Errorf("cooldown = %v, want the hardcoded %v", tr.cooldown, generationCooldown)
	}
}

// TestTroupe_Prepare_SingleFlight pins the facade's first gate: a
// generation already in flight refuses a concurrent Prepare, so concurrent
// triggers run at most one generation.
func TestTroupe_Prepare_SingleFlight(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var runs atomic.Int32
	d, _ := newTestDirector(t, &fakeSwarm{}, WithRunDirector(func(_ context.Context, _ directorParams) (string, error) {
		runs.Add(1)
		close(entered)
		<-release
		return "", nil
	}))
	tr := newTestTroupe(t, d)

	var first bool
	done := make(chan struct{})
	go func() {
		first = tr.Prepare(t.Context(), "test")
		close(done)
	}()
	<-entered

	if tr.Prepare(t.Context(), "concurrent") {
		t.Error("a concurrent Prepare must be refused while a generation is in flight")
	}
	close(release)
	<-done
	if !first {
		t.Error("the first Prepare must run its generation")
	}
	if runs.Load() != 1 {
		t.Errorf("generations ran = %d, want exactly 1", runs.Load())
	}
}

// TestTroupe_Prepare_Cooldown pins the facade's second gate: a generation
// started recently refuses new triggers, and the gate lifts once the
// cooldown elapses.
func TestTroupe_Prepare_Cooldown(t *testing.T) {
	var runs atomic.Int32
	d, _ := newTestDirector(t, &fakeSwarm{}, WithRunDirector(func(_ context.Context, _ directorParams) (string, error) {
		runs.Add(1)
		return "", nil
	}))
	tr := newTestTroupe(t, d, WithGenerationCooldown(50*time.Millisecond))

	if !tr.Prepare(t.Context(), "first") {
		t.Fatal("the first generation must run")
	}
	if tr.Prepare(t.Context(), "second") {
		t.Error("a generation inside the cooldown must be refused")
	}
	time.Sleep(60 * time.Millisecond)
	if !tr.Prepare(t.Context(), "third") {
		t.Error("a generation after the cooldown must run")
	}
	if runs.Load() != 2 {
		t.Errorf("generations ran = %d, want 2", runs.Load())
	}
}

// TestTroupe_Prepare_GenerationSubmits is the facade-level acceptance: a
// generation that submits produces one resolved play on disk, and Prepare
// reports the run.
func TestTroupe_Prepare_GenerationSubmits(t *testing.T) {
	const playID = "story_20260821T093000Z"
	d, sub := newTestDirector(t, &fakeSwarm{})
	d.runDirector = func(_ context.Context, p directorParams) (string, error) {
		writeDraft(t, sub.worktree, playID)
		submit := findTool(t, p.tools, "submit_play")
		_, err := submit.Call(map[string]any{"play": playID})
		return "", err
	}
	tr := newTestTroupe(t, d)

	if !tr.Prepare(t.Context(), "startup") {
		t.Fatal("Prepare must run the generation")
	}
	playPath := filepath.Join(sub.worktree, "plays", playID+".json")
	data, err := os.ReadFile(playPath)
	if err != nil {
		t.Fatalf("the submitted play is on disk: %v", err)
	}
	if !isResolvedPlay(data) {
		t.Error("the on-disk play is not the resolved served artifact")
	}
}

// TestTroupe_Prepare_GenerationError pins that a failing generation still
// counts as a run — the gates guard starts, not outcomes — and that the
// gates release afterwards.
func TestTroupe_Prepare_GenerationError(t *testing.T) {
	d, _ := newTestDirector(t, &fakeSwarm{}, WithRunDirector(func(_ context.Context, _ directorParams) (string, error) {
		return "", errors.New("model exploded")
	}))
	tr := newTestTroupe(t, d, WithGenerationCooldown(10*time.Millisecond))

	if !tr.Prepare(t.Context(), "failed") {
		t.Fatal("a failing generation must still count as a run")
	}
	time.Sleep(20 * time.Millisecond)
	if !tr.Prepare(t.Context(), "retry") {
		t.Error("a retry after the cooldown must run")
	}
}

// TestTroupe_Prepare_ResetsBudgetPerGeneration pins the fix for the
// cross-generation budget leak: a spent generation — one that burned the
// whole hardcoded call max — must not refuse the next generation. Prepare
// resets the shared budget at the generation boundary, so the termination
// authority bounds one generation, never the accumulated history of many.
func TestTroupe_Prepare_ResetsBudgetPerGeneration(t *testing.T) {
	var runs atomic.Int32
	d, _ := newTestDirector(t, &fakeSwarm{}, WithRunDirector(func(_ context.Context, _ directorParams) (string, error) {
		runs.Add(1)
		return "", nil
	}))
	tr := newTestTroupe(t, d, WithGenerationCooldown(20*time.Millisecond))

	if !tr.Prepare(t.Context(), "first") {
		t.Fatal("the first generation must run")
	}

	// Spend the whole call budget — the state a prior generation would
	// leave behind. Without the per-generation reset, the next generation
	// would be refused at the gate before its agent loop ever runs.
	for range maxGenerationCalls {
		if err := d.budget.Record(t.Context(), budgetedCall(1)); err != nil {
			t.Fatalf("seed Record: %v", err)
		}
	}
	if _, err := d.budget.Admit(); !errors.Is(err, ErrCallMax) {
		t.Fatalf("budget should be spent: admit err = %v, want ErrCallMax", err)
	}

	time.Sleep(30 * time.Millisecond)
	if !tr.Prepare(t.Context(), "second") {
		t.Fatal("the second generation must be admitted on a fresh budget")
	}
	if runs.Load() != 2 {
		t.Errorf("generations ran = %d, want 2 (the reset admits the second)", runs.Load())
	}
}

// TestTroupe_Warm_MaterialisesWithoutAuthoring pins Warm's contract: it
// materialises the notebook through the seam and authors nothing — no
// generation runs, no content is written (no seed, no composer floor).
func TestTroupe_Warm_MaterialisesWithoutAuthoring(t *testing.T) {
	var runs atomic.Int32
	d, _ := newTestDirector(t, &fakeSwarm{}, WithRunDirector(func(_ context.Context, _ directorParams) (string, error) {
		runs.Add(1)
		return "", nil
	}))
	var pulls atomic.Int32
	tr := newTestTroupe(t, d, WithMaterialise(func(context.Context) error {
		pulls.Add(1)
		return nil
	}))

	tr.Warm(t.Context())
	if pulls.Load() != 1 {
		t.Errorf("materialise calls = %d, want 1", pulls.Load())
	}
	if runs.Load() != 0 {
		t.Errorf("Warm must not author content: generations ran = %d, want 0", runs.Load())
	}
}

// TestTroupe_Warm_MaterialiseFailure pins the degraded warm: a failed
// materialisation is logged, never fatal, and still authors nothing.
func TestTroupe_Warm_MaterialiseFailure(t *testing.T) {
	var runs atomic.Int32
	d, _ := newTestDirector(t, &fakeSwarm{}, WithRunDirector(func(_ context.Context, _ directorParams) (string, error) {
		runs.Add(1)
		return "", nil
	}))
	tr := newTestTroupe(t, d, WithMaterialise(func(context.Context) error {
		return errors.New("bucket unreachable")
	}))

	tr.Warm(t.Context()) // logs the failure, returns
	if runs.Load() != 0 {
		t.Errorf("a failed materialisation must not trigger a generation: runs = %d", runs.Load())
	}
}

// TestTroupe_Prepare_RunsCriticAfterGeneration pins phase 8's wiring in the
// facade: a generation that ran is reviewed by the critic with a stamped
// g_<UTC> generation id and the generation's outcome — the submitted play id
// on a submit, the honest empty stage on exhaustion. The review runs outside
// the generation budget (the critic carries its own bound).
func TestTroupe_Prepare_RunsCriticAfterGeneration(t *testing.T) {
	const playID = "story_20260821T093000Z"

	submitted := func(t *testing.T) (*Troupe, *criticCapture) {
		d, sub := newTestDirector(t, &fakeSwarm{})
		d.runDirector = func(_ context.Context, p directorParams) (string, error) {
			writeDraft(t, sub.worktree, playID)
			tool := findTool(t, p.tools, "submit_play")
			_, err := tool.Call(map[string]any{"play": playID})
			return "", err
		}
		c, cap := newTestCriticWithCapture(t)
		tr := newTestTroupe(t, d, WithGenerationCritic(c))
		return tr, cap
	}

	t.Run("submitted play is reviewed", func(t *testing.T) {
		tr, cap := submitted(t)
		if !tr.Prepare(t.Context(), "test") {
			t.Fatal("Prepare must run the generation")
		}
		if !cap.ran {
			t.Fatal("the critic must run after a generation")
		}
		if !strings.Contains(cap.prompt, "GENERATION g_") {
			t.Errorf("critic prompt = %q, want a stamped g_<UTC> generation id", cap.prompt)
		}
		if !strings.Contains(cap.prompt, "PLAY "+playID) {
			t.Errorf("critic prompt = %q, want the submitted play id pinned", cap.prompt)
		}
		if cap.tool.playID != playID {
			t.Errorf("write_criticism pin = %q, want the submitted play id", cap.tool.playID)
		}
	})

	t.Run("exhaustion is reviewed honestly", func(t *testing.T) {
		d, _ := newTestDirector(t, &fakeSwarm{}, WithRunDirector(func(_ context.Context, _ directorParams) (string, error) {
			return "", nil // never submits — the empty stage
		}))
		c, cap := newTestCriticWithCapture(t)
		tr := newTestTroupe(t, d, WithGenerationCritic(c))
		if !tr.Prepare(t.Context(), "test") {
			t.Fatal("Prepare must run the generation")
		}
		if !cap.ran {
			t.Fatal("the critic must review the empty stage too")
		}
		if !strings.Contains(cap.prompt, "PLAY none") {
			t.Errorf("critic prompt = %q, want the honest empty-stage review", cap.prompt)
		}
		if cap.tool.playID != "" {
			t.Errorf("write_criticism pin = %q, want empty for the empty stage", cap.tool.playID)
		}
	})

	t.Run("no critic is wired, no review runs", func(t *testing.T) {
		d, _ := newTestDirector(t, &fakeSwarm{}, WithRunDirector(func(_ context.Context, _ directorParams) (string, error) {
			return "", nil
		}))
		tr := newTestTroupe(t, d) // no WithGenerationCritic
		if !tr.Prepare(t.Context(), "test") {
			t.Fatal("Prepare must run the generation")
		}
	})
}

// criticCapture records what one critic run was given: the assembled prompt
// and the pinned write_criticism tool — the facade-level assertion surface
// for the generation review.
type criticCapture struct {
	prompt string
	ran    bool
	tool   *writeCriticismTool
}

// newTestCriticWithCapture builds a critic whose runCritic captures the
// prompt and the pinned gate into a criticCapture.
func newTestCriticWithCapture(t *testing.T) (*Critic, *criticCapture) {
	t.Helper()
	cap := &criticCapture{}
	c, _ := newTestCritic(t, WithRunCritic(func(_ context.Context, p criticParams) (string, error) {
		cap.prompt = p.prompt
		cap.ran = true
		for _, tool := range p.tools {
			if wt, ok := tool.(*writeCriticismTool); ok {
				cap.tool = wt
			}
		}
		return "", nil
	}))
	return c, cap
}
