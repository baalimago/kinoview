package theatre

// Caps and vocabulary shared by every company document. These bound untrusted
// LLM text for context hygiene: the board and the transcript are read back
// into prompts and renderers, so nothing in them may grow without bound.
const (
	// BoardMaxEntries caps the per-generation board worklog. Beyond it the
	// oldest entries drop, because context is read from the tail of the board.
	BoardMaxEntries = 60

	// EntryMaxBody caps a single board entry's body, in runes.
	EntryMaxBody = 240

	// BoardExcerptMax is how many entries AssembleContext renders. Board growth
	// beyond it never grows a prompt.
	BoardExcerptMax = 20

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

// ValidRoles is the theatre's role set. Board entries and transcript events
// only ever name these authors and recipients; anything else is untrusted and
// dropped on load. "stage" is the stage-manager wrapper, which posts notes and
// phase transitions but is never consulted.
var ValidRoles = map[string]bool{
	"director":     true,
	"dramaturg":    true,
	"playwright":   true,
	"scenographer": true,
	"wardrobe":     true,
	"stage":        true,
}

// productionRoles are the roles a consultation may name: the four production
// roles. The director and the stage are never consulted — the director reads
// the board, the stage is the wrapper (decision D4).
var productionRoles = map[string]bool{
	"dramaturg":    true,
	"playwright":   true,
	"scenographer": true,
	"wardrobe":     true,
}

// ValidBoardKinds are the kinds a board entry may carry.
var ValidBoardKinds = map[string]bool{
	"brief":       true,
	"question":    true,
	"answer":      true,
	"note":        true,
	"decision":    true,
	"deliverable": true,
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
	"pinned":    true,
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
// bounded number of short past-tense facts per generation, distilled into the
// repertoire doc at submit (phase 6).
const (
	CanonMaxFacts = 8
	CanonMaxFact  = 120 // runes
)

// CollabMaxRounds is how many collaborations one invocation's wrapper
// resolves. Each round consults the requested role once and re-invokes the
// original role once (decision D4).
const CollabMaxRounds = 2

// Company doc caps (phase 6): every durable company document is trimmed to
// its cap on write, oldest first. The registry is the exception — it is
// fixed (small), because identities enter only by explicit director
// approval: a full book refuses new characters rather than dropping
// canonized ones.
const (
	premisesCap       = 40
	repertoireSumCap  = 30
	repertoireFactCap = 40
	setsCap           = 50
	directorCap       = 30
	bulletinCap       = 40
	registryMax       = 16

	// audienceCap bounds the audience doc, newest first; older notes drop on
	// trim (decision D-3).
	audienceCap = 40

	// lessonMaxLen bounds one critique lesson's text, in runes.
	lessonMaxLen = 240

	// audienceCommentMax bounds one audience note's comment, in runes — the
	// same bound as a board entry body (decision D-3).
	audienceCommentMax = 240

	// variantCap bounds one registry entry's variant list, in entries — a
	// defensive bound against hostile files; every species palette is smaller.
	variantCap = 8
)

// Doc context excerpts (phase 6): the docs grow across generations, but a
// working context only shows the most recent few entries — the past is for
// trimming, not for reading.
const (
	premisesExcerpt  = 8
	factsExcerpt     = 8
	summariesExcerpt = 4
	setsExcerpt      = 6
	lessonsExcerpt   = 6
	bulletinExcerpt  = 8

	// audienceExcerpt caps how many notes a working context shows (decision
	// D-3): the audience's most recent words, never the whole history.
	audienceExcerpt = 8
)

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
