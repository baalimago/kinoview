package theatre

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/agents/theatre/tools"
	"github.com/baalimago/kinoview/internal/model"
)

// The role prompts are the working-context standard's role section (phase 1).
// Each declares its scope explicitly in three sections — what it decides, what
// it asks and when it stops — so a role never drifts outside its remit and
// never burns budget past its deliverable. The four production roles land
// here in full (phase 5); the director's own prompt lives in director.go.

const dramaturgPrompt = `You are the dramaturg. You decide: the production brief — the mood the story should carry, the shape it should take, the 1-3 member cast lineup, and what to avoid repeating. You ask: nothing — you work from the board and the character registry alone; the playwright and the scenographer build on your brief. You stop: when the brief is delivered with write_brief — deliver it once and stop.`

const playwrightPrompt = `You are the playwright. You decide: the full draft — the title, the beats, the cast usage, the props, and 1-2 canon facts the story leaves behind (short past-tense outcomes, at most 120 characters each). Riff on the canon facts you are told; never contradict the pinned registry (a cat stays a cat, a pinned coat stays pinned). You ask: the wardrobe, via consult, when a look needs checking against the set. You stop: when the draft is written with write_draft (the full story JSON) — write it once and stop.`

const scenographerPrompt = `You are the scenographer. You decide: the set around the draft's staging — the backdrop, the cells and the prop placements; never put a piece through a performer. You ask: the wardrobe, via consult, when a coat's contrast against a backdrop needs checking. You stop: when the scene is delivered with write_scene — deliver it once and stop.`

const wardrobePrompt = `You are the wardrobe consultant. You decide: nothing — you answer questions about character looks against backdrops, grounded in the character registry. You ask: nothing. You stop: when your answer is given with advise — answer once and stop.`

// The role prompts are constants — the working-context standard's role
// section can never be swapped at runtime. The len checks are compile-time
// proof: a var would not be usable in a constant expression.
const (
	_ = len(dramaturgPrompt)
	_ = len(playwrightPrompt)
	_ = len(scenographerPrompt)
	_ = len(wardrobePrompt)
)

// RolePrompt returns a role's system prompt for the working-context standard.
// Unknown roles get an empty prompt: they are never invoked with one.
func RolePrompt(role string) string {
	switch role {
	case "director":
		return directorPrompt
	case "dramaturg":
		return dramaturgPrompt
	case "playwright":
		return playwrightPrompt
	case "scenographer":
		return scenographerPrompt
	case "wardrobe":
		return wardrobePrompt
	default:
		return ""
	}
}

// artifactName is the deliverable's name on the transcript — the free-form
// addressee of a deliver event (phase 2, decision D-P2-5).
func artifactName(role string) string {
	switch role {
	case "dramaturg":
		return "brief"
	case "playwright":
		return "draft"
	case "scenographer":
		return "scene"
	case "wardrobe":
		return "answer"
	default:
		return "deliverable"
	}
}

// roleTools builds the tool set for one role invocation at the given hop
// depth: the shared tools every role carries — post_to_board and read_board —
// plus consult for every role except the wardrobe (its "You ask: nothing"
// scope, review 1 R1-02), plus the role's deliverable writer. The author,
// questioner and depth are pinned at construction, so a tool can never act as
// another role or escape its hop depth. The writers validate their artifacts
// at the wrapper boundary (phase 5) before anything enters the board or the
// working file.
func (r *Runner) roleTools(ctx context.Context, role string, depth int) []models.LLMTool {
	shared := []models.LLMTool{
		tools.NewPostToBoard(func(kind, to, body string) error {
			return r.postToBoard(role, kind, to, body)
		}),
		tools.NewReadBoard(func() (string, error) {
			return r.readBoardExcerpt(), nil
		}),
	}
	if role != "wardrobe" {
		shared = append(shared, tools.NewConsult(func(target, question string) (string, error) {
			return r.consult(ctx, role, target, question, depth)
		}))
	}
	switch role {
	case "director":
		if r.directorTools == nil {
			return shared
		}
		return r.directorTools(ctx)
	case "dramaturg":
		return append(shared, tools.NewWriteBrief(func(brief string) error {
			return r.writeBrief(brief)
		}))
	case "playwright":
		shared = append(shared,
			tools.NewWriteDraft(func(story, report string) (string, error) {
				return r.writeDraft(story, report)
			}),
			tools.NewAppendCanon(func(fact string) error {
				return r.appendCanon(fact)
			}),
		)
	case "scenographer":
		shared = append(shared, tools.NewWriteScene(func(backdrop, report string) (string, error) {
			return r.writeScene(backdrop, report)
		}))
	case "wardrobe":
		shared = append(shared, tools.NewAdvise(func(answer string) (string, error) {
			return strings.TrimSpace(answer), nil
		}))
	}
	return shared
}

// consult routes a consult tool call into the broker. The broker is wired
// after construction (NewRunner + WireBroker), so a nil broker refuses with a
// clear message rather than panicking.
func (r *Runner) consult(ctx context.Context, questioner, target, question string, depth int) (string, error) {
	if r.broker == nil {
		return "consult refused: broker not wired", nil
	}
	return r.broker.Consult(ctx, questioner, target, question, depth)
}

// postToBoard appends a validated entry on the role's behalf and records the
// post event. Both the kind and the addressee are checked here so the tool
// can tell the model that a rejected entry was rejected: the board gate
// would silently drop an unknown kind and clear an unknown addressee, and
// the transcript would drop the same event — the two records must agree on
// every accepted post (review 1, R1-04).
func (r *Runner) postToBoard(author, kind, to, body string) error {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if !ValidBoardKinds[kind] {
		return fmt.Errorf("unknown kind %q (one of brief, question, answer, note, decision, deliverable)", kind)
	}
	to = strings.ToLower(strings.TrimSpace(to))
	if to != "" && !ValidRoles[to] {
		return fmt.Errorf("unknown addressee %q (a role, or empty for the company)", to)
	}
	if err := appendBoardEntry(r.company, Entry{Author: author, Kind: kind, To: to, Body: body}); err != nil {
		return fmt.Errorf("board write: %w", err)
	}
	r.stage.Emit(TranscriptEvent{Kind: "post", From: author, To: to, Body: body})
	return nil
}

// boardBrief returns the newest brief entry's body — the brief the draft is
// written from. The playwright's writer and floor capture it into the working
// file at draft-write time (review 3, R3-02), so the premise survives a board
// overflow after the draft is written. The window BEFORE the draft write is
// not budget-closed: consults post question + answer and consulted roles post
// up to their budget, so a chatty generation can still trim the brief before
// the playwright writes (review 5, R5-01) — the out-of-band capture should
// happen at brief-post time, with this scan as the fallback. A board read
// failure degrades to the empty brief.
func (r *Runner) boardBrief() string {
	board, err := r.company.LoadBoard()
	if err != nil {
		return ""
	}
	for i := len(board.Entries) - 1; i >= 0; i-- {
		if board.Entries[i].Kind == "brief" {
			return board.Entries[i].Body
		}
	}
	return ""
}

// readBoardExcerpt renders the board excerpt for the read_board tool. A board
// read failure degrades to the empty excerpt — the generation continues.
func (r *Runner) readBoardExcerpt() string {
	board, err := r.company.LoadBoard()
	if err != nil {
		board = Board{}
	}
	var b strings.Builder
	for _, e := range board.Excerpt(BoardExcerptMax) {
		to := e.To
		if to == "" {
			to = "company"
		}
		fmt.Fprintf(&b, "[%d] %s (%s) → %s: %s\n", e.Seq, e.Author, e.Kind, to, e.Body)
	}
	return b.String()
}

// writeBrief posts the dramaturg's brief to the board. A JSON brief is
// validated at the wrapper boundary — unknown lineup ids dropped, lengths
// capped — before it enters the board; a free-text brief passes through
// untouched.
func (r *Runner) writeBrief(brief string) error {
	normalized, ok := r.validateBrief(brief)
	if !ok {
		normalized = brief
	}
	return r.postToBoard("dramaturg", "brief", "director", normalized)
}

// validateBrief parses and normalises a brief artifact. It reports whether
// the text was a JSON brief at all; the returned string is the normalized
// artifact (or the input, when it was not an artifact).
func (r *Runner) validateBrief(text string) (string, bool) {
	var ba BriefArtifact
	if !parseArtifact(text, &ba) {
		return text, false
	}
	var known func(string) bool
	if r.registry != nil {
		known = r.registry.Known
	}
	normalizeBrief(&ba, known)
	out, err := marshalArtifact(ba)
	if err != nil {
		return text, false
	}
	return out, true
}

// writeDraft saves the playwright's draft into the working file. The story is
// parsed and run through model.Story.Validate — the same trust boundary as a
// fresh LLM reply — before it is allowed into the file; the generation id is
// the story id, because the theatre owns the ids (the LLM never does). The
// report, when it is a JSON draft report, is stored beside the draft — its
// acts supersede the derived count — and the canon facts the playwright kept
// are appended to the working file, truncated to the canon cap.
func (r *Runner) writeDraft(story, report string) (string, error) {
	var s model.Story
	if err := json.Unmarshal([]byte(story), &s); err != nil {
		return "", fmt.Errorf("draft is not valid JSON: %v", err)
	}
	s.ID = r.stage.gen
	s.Origin = "llm"
	w, err := r.company.LoadWorking()
	if err != nil {
		w = Working{}
	}
	if rep, ok := parseDraftReport(report); ok {
		w.Report = &rep
		for _, f := range rep.Canon {
			if len(w.Canon) >= CanonMaxFacts {
				break
			}
			w.Canon = append(w.Canon, truncateRunes(strings.TrimSpace(f), CanonMaxFact))
		}
	}
	w.Story = s
	w.Revision++
	w.Status = "draft"
	// A rewritten draft loses the validate_story blessing (review 7, R7-01):
	// the exhaustion path ships only the exact content that passed the gate.
	w.Validated = false
	// The brief rides out of band (review 3, R3-02): the draft is written
	// from it, and the premise must survive a board overflow. A fresh draft
	// is not dressed yet.
	w.Brief = r.boardBrief()
	w.Dressed = false
	if err := r.company.SaveWorking(w); err != nil {
		return "", err
	}
	return fmt.Sprintf("draft saved (revision %d)", w.Revision), nil
}

// writeScene dresses the working draft's set. Without a draft there is
// nothing to dress; the scenographer must wait for the playwright. A JSON
// scene report carries the whole dress — backdrop, cells and prop placements,
// validated against the model vocabulary and the draft's own props; a
// free-text report dresses the backdrop only. The scene report lands on the
// board (integration contract); the working file stays the authoritative
// artifact, so a board write failure is logged, not fatal.
func (r *Runner) writeScene(backdrop, report string) (string, error) {
	w, err := r.company.LoadWorking()
	if err != nil {
		return "", fmt.Errorf("no draft in the working file yet — the playwright must write first")
	}
	post := fmt.Sprintf("backdrop: %s", strings.ToLower(strings.TrimSpace(backdrop)))
	if sr, ok := parseSceneReport(report); ok {
		backdrop = sr.Backdrop
		if !model.ValidBackdrops[backdrop] {
			return "", fmt.Errorf("unknown backdrop %q", backdrop)
		}
		w.Story.Scene.Backdrop = backdrop
		applyCellPlacements(&w.Story, sr.Cells)
		applyPropPlacements(&w.Story, sr.Props)
		if b, merr := marshalArtifact(sr); merr == nil {
			post = b
		}
	} else {
		backdrop = strings.ToLower(strings.TrimSpace(backdrop))
		if !model.ValidBackdrops[backdrop] {
			return "", fmt.Errorf("unknown backdrop %q", backdrop)
		}
		w.Story.Scene.Backdrop = backdrop
	}
	w.Revision++
	w.Status = "dressed"
	// Dressing rewrites the draft, so it loses the validate_story blessing
	// (review 7, R7-01): the exhaustion path ships only the exact content
	// that passed the gate.
	w.Validated = false
	// The scenographer ran: the out-of-band marker survives a board overflow
	// that trims its deliverable copy (review 3, R3-02).
	w.Dressed = true
	if err := r.company.SaveWorking(w); err != nil {
		return "", err
	}
	if err := r.postToBoard("scenographer", "deliverable", "director", post); err != nil {
		ancli.Errf("theatre: scene report post failed: %v", err)
	}
	return fmt.Sprintf("scene saved (revision %d, backdrop %s)", w.Revision, backdrop), nil
}

// applyCellPlacements merges the scenographer's cells into the draft's set: a
// placement at a slot the draft already has updates that cell's piece
// (keeping its id, so the playwright's setCell beats stay addressable); a
// placement at a new slot gets a fresh id. Placements past the model's cell
// cap are dropped.
func applyCellPlacements(s *model.Story, placements []CellPlacement) {
	bySlot := map[string]int{} // row:col → index into s.Scene.Cells
	for i, c := range s.Scene.Cells {
		bySlot[c.Row+":"+cellColKey(c.Col)] = i
	}
	used := map[string]bool{}
	for _, c := range s.Scene.Cells {
		used[c.ID] = true
	}
	next := 0
	for _, p := range placements {
		slot := p.Row + ":" + cellColKey(p.Col)
		if i, ok := bySlot[slot]; ok {
			s.Scene.Cells[i].Piece = p.Piece
			continue
		}
		if len(s.Scene.Cells) >= model.MaxCells {
			break
		}
		s.Scene.Cells = append(s.Scene.Cells, model.Cell{
			ID: nextCellID(used, &next), Row: p.Row, Col: p.Col, Piece: p.Piece,
		})
		bySlot[slot] = len(s.Scene.Cells) - 1
	}
}

// nextCellID hands out fresh cell ids (set_a, set_b, …) that do not collide
// with the draft's own cells.
func nextCellID(used map[string]bool, next *int) string {
	for {
		id := "set_" + string(rune('a'+*next%26))
		*next++
		if !used[id] {
			used[id] = true
			return id
		}
	}
}

// cellColKey renders a column for slot addressing.
func cellColKey(col int) string { return string(rune('0' + col)) }

// applyPropPlacements moves the draft's props to the scenographer's marks. A
// placement naming a prop the draft does not have is dropped — the scene is
// validated against the draft (cross-schema contract).
func applyPropPlacements(s *model.Story, placements []PropPlacement) {
	byID := map[string]int{}
	for i, p := range s.Props {
		byID[p.ID] = i
	}
	for _, p := range placements {
		i, ok := byID[p.ID]
		if !ok {
			continue
		}
		s.Props[i].X = p.X
		s.Props[i].Lane = p.Lane
	}
}

// appendCanon appends a canon fact to the working file, capped in count and
// length (soft continuity, D6).
func (r *Runner) appendCanon(fact string) error {
	w, err := r.company.LoadWorking()
	if err != nil {
		return fmt.Errorf("no draft in the working file yet — the playwright must write first")
	}
	if len(w.Canon) >= CanonMaxFacts {
		return fmt.Errorf("canon full (%d facts)", CanonMaxFacts)
	}
	w.Canon = append(w.Canon, truncateRunes(strings.TrimSpace(fact), CanonMaxFact))
	if err := r.company.SaveWorking(w); err != nil {
		return err
	}
	return nil
}
