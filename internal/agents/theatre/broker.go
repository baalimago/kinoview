package theatre

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Broker is the consultation layer between the production roles (decision
// D4): it spawns a consulted role as a bounded mini-agent and returns its
// answer. Cycle guards sit at the door:
//
//   - hop cap: a consult at ConsultHopCap is refused, never spawned;
//   - consultation table: a repeat {questioner, role, question} is answered
//     from the table without a second spawn;
//   - budgets: a spawn past the generation's global call cap or wall-clock
//     deadline is refused and the caller is told.
//
// Question and answer land on the transcript, and the ledger records the
// consult and the hop depth it reached.
type Broker struct {
	company *Company
	stage   *Stage
	runner  *Runner

	hopCap int

	mu    sync.Mutex
	table map[string]string
}

// NewBroker builds the consultation broker for a generation. The runner is
// wired after construction (the runner and the broker reference each other:
// the broker spawns through the runner, the runner resolves collaborations
// through the broker).
func NewBroker(company *Company, stage *Stage, runner *Runner) *Broker {
	return &Broker{
		company: company,
		stage:   stage,
		runner:  runner,
		hopCap:  ConsultHopCap,
		table:   make(map[string]string),
	}
}

// Consult answers questioner's question by spawning target as a bounded
// mini-agent (budget DefaultConsultBudget) and returning its answer. Refusals
// are returned as message strings with a nil error, so the calling agent
// reads them and adapts; a spawn failure is a real error.
func (b *Broker) Consult(ctx context.Context, questioner, target, question string, depth int) (string, error) {
	target = strings.ToLower(strings.TrimSpace(target))
	if !productionRoles[target] {
		return fmt.Sprintf("consult refused: %q is not a consultable role (dramaturg, playwright, scenographer, wardrobe)", target), nil
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return "consult refused: empty question", nil
	}

	// Consultation table: a repeat question is answered from the table
	// without a second spawn. The repeat is still accounted — the ledger
	// records the consult and the transcript notes the table hit, so the
	// telemetry counts every consultation; only the spawn is skipped
	// (review 1, R1-03).
	key := consultKey(questioner, target, question)
	b.mu.Lock()
	if answer, ok := b.table[key]; ok {
		b.mu.Unlock()
		b.stage.RecordConsult(questioner, depth)
		b.stage.Emit(TranscriptEvent{Kind: "note", From: "stage", Body: fmt.Sprintf("repeat consult answered from the table: %s→%s", questioner, target)})
		return answer, nil
	}
	b.mu.Unlock()

	// Cycle guards: the hop cap and the generation's budgets. A refusal is
	// still accounted in the ledger, so the telemetry shows how deep the
	// chain tried to go.
	used, max, deadline := b.stage.BudgetSnapshot()
	if depth >= b.hopCap {
		b.stage.RecordConsult(questioner, depth)
		return fmt.Sprintf("consult refused: consultation depth exceeded (max %d)", b.hopCap), nil
	}
	if used >= max {
		b.stage.RecordConsult(questioner, depth)
		return "consult refused: global call budget exhausted", nil
	}
	if !deadline.IsZero() && time.Now().After(deadline) {
		b.stage.RecordConsult(questioner, depth)
		return "consult refused: wall-clock deadline exceeded", nil
	}

	// The question goes on the transcript before the spawn, so the consulted
	// role's task carries it; the transcript records the consult.
	b.stage.Emit(TranscriptEvent{Kind: "consult", From: questioner, To: target, Body: question})

	res, err := b.runner.Run(ctx, Invocation{
		Role:   target,
		Task:   question,
		Budget: DefaultConsultBudget,
		Depth:  depth + 1,
	})
	if err != nil {
		return "", fmt.Errorf("consult %s→%s: %w", questioner, target, err)
	}
	answer := strings.TrimSpace(res.Text)

	// The answer lands on the transcript and the table; the ledger records
	// the consult.
	b.stage.RecordConsult(questioner, depth+1)
	b.stage.Emit(TranscriptEvent{Kind: "answer", From: target, To: questioner, Body: answer})
	b.mu.Lock()
	b.table[key] = answer
	b.mu.Unlock()
	return answer, nil
}

// consultKey identifies a repeat consultation: the questioner, the consulted
// role and the question itself. A repeated question — even one reworded in
// whitespace — answers from the table instead of spawning again.
func consultKey(questioner, target, question string) string {
	sum := sha256.Sum256([]byte(question))
	return questioner + "\x00" + target + "\x00" + fmt.Sprintf("%x", sum)
}
