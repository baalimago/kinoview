package theatre

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Ledger is the production's progress state, rewritten at every phase
// transition, tool call and submit. It is the telemetry source ("analyze
// performance later") and the spine of the debug renderer.
type Ledger struct {
	Generation   string    `json:"generation"`
	Phase        string    `json:"phase"`
	PhaseIndex   int       `json:"phaseIndex"`
	PhasesTotal  int       `json:"phasesTotal"`
	Budget       Budget    `json:"budget"`
	Actors       []Actor   `json:"actors"`
	StartedAt    time.Time `json:"startedAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	WallDeadline time.Time `json:"wallDeadline"`
}

// Budget is the per-generation call budget. The director spends DirectorUsed
// within DirectorMax; every budgeted tool call anywhere spends GlobalUsed
// within GlobalMax. The budgets cap WithMaxToolCalls executions — an
// invocation's final answer is not a budgeted call (stage.RecordAnswer,
// review 3 R3-03) — so an actor's Calls and the director budget never exceed
// their caps. GlobalUsed can tail-overshoot GlobalMax by the last admitted
// invocation's budget: the gate refuses new work once the cap is spent but
// lets an invocation already admitted finish its budgeted calls (review 4,
// R4-01). All numbers are flags, tuned later from telemetry (decision D8).
type Budget struct {
	DirectorUsed int `json:"directorUsed"`
	DirectorMax  int `json:"directorMax"`
	GlobalUsed   int `json:"globalUsed"`
	GlobalMax    int `json:"globalMax"`
}

// Actor is one role's production record. Calls, Tokens, Consults and HopDepth
// are the telemetry the ledger is kept for (decision D8: analyzed later);
// Budget is the role's per-invocation call cap.
type Actor struct {
	Role       string `json:"role"`
	Status     string `json:"status"`
	Calls      int    `json:"calls"`
	Budget     int    `json:"budget"`
	Tokens     int    `json:"tokens"`
	Consults   int    `json:"consults"`
	HopDepth   int    `json:"hopDepth"`
	LastAction string `json:"lastAction"`
}

// normalize repairs the ledger in place: negative counters are clamped and
// actors naming unknown roles are dropped. It never fails.
func (l *Ledger) normalize() {
	clamp := func(v *int) {
		if *v < 0 {
			*v = 0
		}
	}
	clamp(&l.PhaseIndex)
	clamp(&l.PhasesTotal)
	clamp(&l.Budget.DirectorUsed)
	clamp(&l.Budget.DirectorMax)
	clamp(&l.Budget.GlobalUsed)
	clamp(&l.Budget.GlobalMax)
	actors := make([]Actor, 0, len(l.Actors))
	for _, a := range l.Actors {
		a.Role = strings.ToLower(strings.TrimSpace(a.Role))
		if !ValidRoles[a.Role] {
			continue
		}
		clamp(&a.Calls)
		clamp(&a.Budget)
		clamp(&a.Tokens)
		clamp(&a.Consults)
		clamp(&a.HopDepth)
		actors = append(actors, a)
	}
	l.Actors = actors
}

// LoadLedger reads the ledger, or the zero ledger if none exists yet. A
// missing file is not an error; a corrupt one is logged and reported.
func (c *Company) LoadLedger() (Ledger, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var l Ledger
	if err := readJSON(c.ledgerPath(), &l); err != nil {
		logLoadFailure("ledger", err)
		return Ledger{}, err
	}
	l.normalize()
	return l, nil
}

// SaveLedger writes the ledger atomically.
func (c *Company) SaveLedger(l Ledger) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	l.normalize()
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal ledger: %w", err)
	}
	return writeFileAtomic(c.ledgerPath(), data)
}
