package theatre

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/baalimago/clai/pkg/text/models"
)

// Report is the deliverable envelope the wrapper parses out of an agent's
// final text: the compact report plus the collaborations the role requests
// (decision D4). Phase 5 refines the per-role artifact schemas; this is the
// minimal shared shape the wrapper needs to resolve collaborations.
type Report struct {
	Report         string          `json:"report"`
	Collaborations []Collaboration `json:"collaborations,omitempty"`
}

// Collaboration is one consultation request riding on a deliverable: a role
// and a focused question for it.
type Collaboration struct {
	Role     string `json:"role"`
	Question string `json:"question"`
}

// parseReport splits an agent's final text into the report and the
// collaborations it requests. A deliverable without a parseable envelope is
// the report itself, with no collaborations — lenient, because the LLM is
// untrusted and the deterministic floor answers regardless.
func parseReport(text string) (string, []Collaboration) {
	raw := extractJSON(text)
	if raw == "" {
		return text, nil
	}
	var rep Report
	if err := json.Unmarshal([]byte(raw), &rep); err != nil {
		return text, nil
	}
	report := strings.TrimSpace(rep.Report)
	if report == "" {
		report = text
	}
	collabs := make([]Collaboration, 0, len(rep.Collaborations))
	for _, c := range rep.Collaborations {
		c.Role = strings.ToLower(strings.TrimSpace(c.Role))
		c.Question = strings.TrimSpace(c.Question)
		if c.Role == "" || c.Question == "" || !productionRoles[c.Role] {
			continue
		}
		collabs = append(collabs, c)
	}
	return report, collabs
}

// resolveCollaborations resolves the collaborations a deliverable requests
// (decision D4): at most CollabMaxRounds rounds, each consulting the
// requested role once through the broker — which posts the question and the
// answer — and re-invoking the original role once with the
// answer injected into its task. A failed consult or revision never loses
// the deliverable: the last good text stands.
func (r *Runner) resolveCollaborations(ctx context.Context, inv Invocation, prompt string, tools []models.LLMTool, out io.Writer, res Result) (string, error) {
	if r.broker == nil {
		return res.Text, nil
	}
	report, collabs := parseReport(res.Text)
	text := report
	for i, c := range collabs {
		if i >= CollabMaxRounds {
			break
		}
		answer, err := r.broker.Consult(ctx, inv.Role, c.Role, c.Question, inv.Depth)
		if err != nil {
			r.stage.Emit(TranscriptEvent{Kind: "note", From: "stage", Body: fmt.Sprintf("collaboration failed: %s→%s: %v", inv.Role, c.Role, err), Level: "warning"})
			continue
		}
		revised, fallback, llmErr := r.runOnce(ctx, inv.Role,
			inv.Task+"\n\nThe consulted "+c.Role+" answered: "+answer+"\nRevise your deliverable accordingly and reply again.",
			prompt+"\n\nThe consulted "+c.Role+" answered: "+answer+"\nRevise your deliverable accordingly and reply again.",
			tools, inv.Budget, out, inv.Depth)
		if llmErr != nil && !fallback {
			// Both the LLM and the fallback failed on the revision; the
			// pre-collaboration deliverable still stands.
			r.stage.Emit(TranscriptEvent{Kind: "note", From: "stage", Body: fmt.Sprintf("collaboration revision failed for %s: %v", inv.Role, llmErr), Level: "warning"})
			continue
		}
		text = revised
	}
	final, _ := parseReport(text)
	return final, nil
}

// extractJSON pulls the first balanced {...} out of a reply, tolerating code
// fences and any stray prose an LLM decides to add. Migrated here with the
// mini-agent runner (phase 3); the composer-only floor never needs it.
func extractJSON(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
		case c == '\\' && inStr:
			esc = true
		case c == '"':
			inStr = !inStr
		case inStr:
			// skip
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}
