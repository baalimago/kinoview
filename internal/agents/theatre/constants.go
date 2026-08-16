package theatre

// Caps and vocabulary shared by the company's paperwork. These bound untrusted
// LLM text for context hygiene: the transcript is read back into renderers,
// so nothing in it may grow without bound.
const (
	// TranscriptMaxBody caps a single transcript event's body, in runes. The
	// transcript is a file, not a prompt, so its cap is generous; the line cap
	// below is the real bound.
	TranscriptMaxBody = 2000

	// TranscriptMaxTo caps an event's addressee, in runes. Roles are short;
	// the only free-form addressee is a deliver's artifact name.
	TranscriptMaxTo = 240

	// TranscriptMaxLines caps the transcript file; older lines are trimmed on
	// the next append.
	TranscriptMaxLines = 2000
)

// ValidRoles is the theatre's role set. Transcript events only ever name
// these authors and recipients; anything else is untrusted and dropped on
// load. "stage" is the stage-manager wrapper, which posts notes and phase
// transitions but is never consulted.
var ValidRoles = map[string]bool{
	"director":     true,
	"dramaturg":    true,
	"playwright":   true,
	"scenographer": true,
	"wardrobe":     true,
	"stage":        true,
}

// productionRoles are the roles a consultation may name: the four production
// roles. The director and the stage are never consulted — the director
// decides, the stage is the wrapper (decision D4).
var productionRoles = map[string]bool{
	"dramaturg":    true,
	"playwright":   true,
	"scenographer": true,
	"wardrobe":     true,
}

// ValidTranscriptKinds are the kinds an inter-agent event may carry.
var ValidTranscriptKinds = map[string]bool{
	"post":    true,
	"consult": true,
	"answer":  true,
	"deliver": true,
	"note":    true,
	"phase":   true,
	"submit":  true,
	"fail":    true,
}

// ValidLevels are the feed's severity labels. An event without a level (or
// with an unknown one) prints at the level its kind implies: submit succeeds,
// fail fails, everything else is a notice.
var ValidLevels = map[string]bool{
	"notice":  true,
	"ok":      true,
	"warning": true,
	"error":   true,
}

// ValidWorkingStatuses are the stages a draft passes through, in order.
var ValidWorkingStatuses = map[string]bool{
	"brief":     true,
	"draft":     true,
	"dressed":   true,
	"validated": true,
	"submitted": true,
}

// Subagent call budgets (decision D8, tuned later from telemetry): a role
// invocation spends at most DefaultSubagentBudget calls, a consultation spawn
// at most DefaultConsultBudget, and every consult chain is capped at
// ConsultHopCap hops — a consult at that depth is refused instead of spawned.
const (
	DefaultSubagentBudget = 8
	DefaultConsultBudget  = 4
	ConsultHopCap         = 2
)

// Canon caps bound the playwright's canon facts (soft continuity, D6): a
// bounded number of short past-tense facts per generation, carried in the
// working file and read back into the working summary.
const (
	CanonMaxFacts = 8
	CanonMaxFact  = 120 // runes
)

// CollabMaxRounds is how many collaborations one invocation's wrapper
// resolves. Each round consults the requested role once and re-invokes the
// original role once (decision D4).
const CollabMaxRounds = 2

// truncateRunes caps a string at n runes. Bodies are LLM text, so cutting on
// runes rather than bytes keeps the truncation at a character boundary.
func truncateRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
