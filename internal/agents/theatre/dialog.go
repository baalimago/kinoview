package theatre

import (
	"fmt"
	"os"
	"strings"
)

// RenderDialog renders one generation's transcript and ledger as a readable
// script: phase markers, role lines with arrows and the final summary. It
// never modifies company files. An unknown generation — nothing in the
// transcript and no matching ledger — is an error; a partial or corrupt
// transcript renders its readable events and warns.
func RenderDialog(c *Company, gen string) (string, error) {
	all, bad, err := scanTranscript(c.transcriptPath())
	if os.IsNotExist(err) {
		all, bad, err = nil, 0, nil
	}

	var events []TranscriptEvent
	for _, ev := range all {
		if ev.Gen == gen {
			events = append(events, ev)
		}
	}
	ledger, lerr := c.LoadLedger()
	if len(events) == 0 && (lerr != nil || ledger.Generation != gen) {
		return "", fmt.Errorf("no such generation %q", gen)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "── production %s ──\n", gen)
	for _, ev := range events {
		fmt.Fprintf(&b, "[%d] %s\n", ev.Seq, FormatEventLine(ev))
	}
	switch {
	case lerr == nil && ledger.Generation == gen:
		b.WriteString(renderLedgerSummary(ledger))
	case lerr == nil && ledger.Generation != "":
		fmt.Fprintf(&b, "(ledger belongs to generation %s)\n", ledger.Generation)
	}
	switch {
	case err != nil:
		fmt.Fprintf(&b, "(warning: transcript unreadable: %v)\n", err)
	case bad > 0:
		fmt.Fprintf(&b, "(warning: %d unreadable transcript line(s) dropped)\n", bad)
	}
	return b.String(), nil
}

// renderLedgerSummary renders the ledger's final state: the phase, the
// budgets and each actor's telemetry.
func renderLedgerSummary(l Ledger) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\nledger: phase %s (%d/%d) · director %d/%d calls · global %d/%d calls\n",
		l.Phase, l.PhaseIndex, l.PhasesTotal,
		l.Budget.DirectorUsed, l.Budget.DirectorMax,
		l.Budget.GlobalUsed, l.Budget.GlobalMax)
	if len(l.Actors) == 0 {
		b.WriteString("  (no actors)\n")
		return b.String()
	}
	for _, a := range l.Actors {
		fmt.Fprintf(&b, "  %s: %d calls · %d tokens · %d consults · hop %d\n",
			a.Role, a.Calls, a.Tokens, a.Consults, a.HopDepth)
	}
	return b.String()
}
