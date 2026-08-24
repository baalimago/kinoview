package troupe

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
)

// generationCooldown is the hardcoded minimum wall time between two
// generations (decision 13: the cooldown is hardcoded, never a flag). A
// generation is an expensive swarm, and a handful of page refreshes must not
// each trigger one; the cadence matches the concierge's autonomous interval
// (6h), the sibling heavy agent on the same server.
const generationCooldown = 6 * time.Hour

// generationIDPrefix distinguishes the generation id the facade stamps into
// the critic's review from the play ids the director authors: g_<UTC> for a
// generation, story_<UTC> for a play. The critic's criticism note names the
// generation it reviewed through this id; the second-precision stamp is
// unambiguous because the hardcoded cooldown (6h) separates generations in
// production.
const generationIDPrefix = "g_"

// Troupe is the facade the server talks to: Prepare triggers at most one
// generation (hardcoded cooldown + single-flight) and then runs the critic
// over the generation's outcome; Warm materialises the notebook (no seed, no
// composer); and the served play is always the newest submitted play read
// from disk — there is no in-memory current story, and an empty stage (no
// submitted play) is the signal to investigate, never a generated fallback.
type Troupe struct {
	director *Director
	// critic is the fixed advisory pole: run after a generation with the
	// generation's id and outcome (phase 8). Nil disables the review — the
	// generation runs, nothing comments on it.
	critic   *Critic
	cooldown time.Duration

	// materialise pulls the shared notebook into the worktree — Warm's only
	// job. Production wires the slivingdoc pull over the shared worktree;
	// tests inject a recording fake. Unset, Warm is a no-op.
	materialise func(ctx context.Context) error

	mu       sync.Mutex
	lastGen  time.Time
	inFlight bool
}

// TroupeOption configures one Troupe.
type TroupeOption func(*Troupe)

// WithGenerationDirector sets the director that runs one generation.
func WithGenerationDirector(d *Director) TroupeOption {
	return func(t *Troupe) { t.director = d }
}

// WithGenerationCritic sets the fixed advisory pole that reviews each
// generation after it runs (phase 8's wiring): Prepare runs the director,
// then the critic with the generation's stamped id and the outcome — the
// submitted play id, or the honest empty stage. Nil leaves the generation
// unreviewed.
func WithGenerationCritic(c *Critic) TroupeOption {
	return func(t *Troupe) { t.critic = c }
}

// WithMaterialise sets how Warm materialises the notebook: production passes
// the slivingdoc pull over the shared worktree; tests inject a recording
// fake. Unset, Warm materialises nothing.
func WithMaterialise(fn func(ctx context.Context) error) TroupeOption {
	return func(t *Troupe) { t.materialise = fn }
}

// WithGenerationCooldown overrides the hardcoded generation cooldown — a
// test seam, never an operator flag (decision 13).
func WithGenerationCooldown(d time.Duration) TroupeOption {
	return func(t *Troupe) { t.cooldown = d }
}

// NewTroupe builds the facade. The director is required; the cooldown
// defaults to the hardcoded generationCooldown.
func NewTroupe(opts ...TroupeOption) (*Troupe, error) {
	t := &Troupe{cooldown: generationCooldown}
	for _, o := range opts {
		o(t)
	}
	if t.director == nil {
		return nil, errors.New("troupe: facade: director can't be nil")
	}
	return t, nil
}

// Prepare triggers at most one generation: single-flight (a generation
// already in flight refuses new triggers) and the hardcoded cooldown (a
// generation that started recently refuses new triggers). Returns whether a
// generation ran; the generation's outcome — a submitted play or exhaustion
// — is on disk, read back by the play API, never held in memory. When a
// generation ran and the facade has a critic, the critic then reviews the
// generation — the submitted play, or the honest empty stage — outside the
// generation budget (phase 8). An error in the generation itself is logged
// and still counts as a run: the gates guard starts, not outcomes.
func (t *Troupe) Prepare(ctx context.Context, reason string) bool {
	t.mu.Lock()
	if t.inFlight {
		t.mu.Unlock()
		return false
	}
	if since := time.Since(t.lastGen); !t.lastGen.IsZero() && since < t.cooldown {
		t.mu.Unlock()
		ancli.Noticef("troupe: skipping generation (%v, %v left of cooldown)",
			reason, (t.cooldown - since).Truncate(time.Second))
		return false
	}
	t.inFlight = true
	t.lastGen = time.Now()
	t.mu.Unlock()

	defer func() {
		t.mu.Lock()
		t.inFlight = false
		t.mu.Unlock()
	}()

	generation := t.generationID(time.Now())

	// A fresh budget per generation: the termination authority bounds one
	// generation, never the accumulated history of many. Without this the
	// hardcoded call max and the token stoploss would be spent permanently
	// after the first few generations, and every later generation would be
	// refused before it starts.
	t.director.ResetBudget()
	outcome, err := t.director.Run(ctx)
	switch {
	case err != nil:
		ancli.Errf("troupe: generation %v failed: %v", reason, err)
	case outcome.Submitted:
		ancli.Okf("troupe: generation %v submitted %s", reason, outcome.PlayID)
	default:
		ancli.Noticef("troupe: generation %v exhausted — nothing submitted; the stage stays empty", reason)
	}
	if t.critic != nil {
		if cerr := t.critic.Run(ctx, generation, outcome); cerr != nil {
			ancli.Errf("troupe: critic: %v", cerr)
		}
	}
	return true
}

// generationID stamps the id the critic's review names: g_<UTC> in the
// compact play-id form. The id is stamped when the generation starts — the
// review is of the run the gates admitted, never of a skipped trigger.
func (t *Troupe) generationID(now time.Time) string {
	return generationIDPrefix + now.UTC().Format("20060102T150405Z")
}

// Cooldown reports the configured generation cooldown — the minimum wall
// time between two generations. The server's generation loop ticks on this
// period, so the cadence lives in one place: the facade owns the gate, and
// the loop just calls Prepare.
func (t *Troupe) Cooldown() time.Duration {
	return t.cooldown
}

// Warm materialises the notebook without authoring content: the shared
// worktree is pulled into place so the troupe starts from the repertoire the
// last generation left. There is no seed and no composer floor — Warm never
// authors anything, and a missing play is an empty stage, the signal to
// investigate, never a generated fallback.
func (t *Troupe) Warm(ctx context.Context) {
	if t.materialise == nil {
		return
	}
	if err := t.materialise(ctx); err != nil {
		ancli.Errf("troupe: warm: materialise notebook: %v", err)
	}
}
