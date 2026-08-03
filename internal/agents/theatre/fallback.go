package theatre

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/model"
)

// The deterministic floors under the four production roles (decision D11):
// every role answers with a valid artifact of its own schema and does the
// side effects its tools would have done — the dramaturg posts the brief, the
// playwright saves the draft, the scenographer dresses it — so a failing LLM
// can never leave the production without an artifact.

// briefMoods are the moods the deterministic brief can pick from.
var briefMoods = []string{"standoff", "cozy", "chaotic", "wistful", "playful", "quiet"}

// backdropNames is the fixed order backdropIn scans, so a question naming
// several backdrops always names the same one.
var backdropNames = []string{"night", "livingroom", "garden", "theatre", "sunset", "kitchen", "forest", "rain"}

// fallbackFor answers a role invocation deterministically. The injected seam
// (WithFallback, tests) wins when present; the internal dispatcher routes by
// role and carries the invocation depth, so a consulted role answers in place
// instead of running its production side effects over the working file.
func (r *Runner) fallbackFor(role, task string, depth int) (string, error) {
	if r.fallback != nil {
		return r.fallback(role, task)
	}
	return r.roleFallback(role, task, depth)
}

// roleFallback is the internal dispatcher: each role's deterministic artifact
// at depth 0 (its production side effects included), an in-place answer at a
// consult depth — the stage-manager wrapper resolves a consulted role's
// deliverable, and a consult must never rewrite the director's draft.
func (r *Runner) roleFallback(role, task string, depth int) (string, error) {
	switch role {
	case "dramaturg":
		if depth > 0 {
			return r.fallbackDramaturgAnswer(task)
		}
		return r.fallbackBrief()
	case "playwright":
		if depth > 0 {
			return r.fallbackPlaywrightAnswer(task)
		}
		return r.fallbackDraft()
	case "scenographer":
		if depth > 0 {
			return r.fallbackSceneAnswer(task)
		}
		return r.fallbackScene()
	case "wardrobe":
		return r.fallbackAdvice(task)
	default:
		return "", fmt.Errorf("no deterministic fallback for %s", role)
	}
}

// fallbackBrief is the dramaturg's floor: a brief built from the board's
// theme, the registry's cast and the premises ledger (phase 6), posted to
// the board exactly like write_brief would.
func (r *Runner) fallbackBrief() (string, error) {
	brief := BriefArtifact{
		Mood:     pick(r.rnd, briefMoods),
		Shape:    pick(r.rnd, SceneNames()),
		Lineup:   r.registryLineup(MaxLineup),
		Theme:    r.boardTheme(),
		NoRepeat: r.premisesNoRepeat(MaxNoRepeat),
	}
	text, err := marshalArtifact(brief)
	if err != nil {
		return "", err
	}
	if err := r.postToBoard("dramaturg", "brief", "director", text); err != nil {
		return "", fmt.Errorf("dramaturg fallback: post brief: %w", err)
	}
	return text, nil
}

// fallbackDramaturgAnswer answers a consultation in place: the brief on the
// board, when one is posted.
func (r *Runner) fallbackDramaturgAnswer(string) (string, error) {
	board, err := r.company.LoadBoard()
	if err != nil {
		board = Board{}
	}
	for _, e := range board.Entries {
		if e.Kind == "brief" {
			return fmt.Sprintf("the brief is: %s", e.Body), nil
		}
	}
	return "no brief posted yet — ask again once the dramaturg has delivered", nil
}

// fallbackDraft is the playwright's floor: a composer draft saved into the
// working file exactly like write_draft would, plus a valid draft report. The
// draft keeps the canon facts already on the working file — the deterministic
// floor riffs by keeping them. A draft already in the working file under THIS
// generation's id is the playwright's own work; a failed revision must not
// clobber it, so the floor reports the existing draft instead. A stale file
// from an earlier generation (a different id) is overwritten like any other
// missing draft.
func (r *Runner) fallbackDraft() (string, error) {
	if w, err := r.company.LoadWorking(); err == nil && w.Story.ID == r.stage.gen {
		rep := draftReportFrom(w.Story, w.Canon)
		return marshalArtifact(rep)
	}
	s := ComposeThemed(r.rnd, r.boardTheme())
	s.ID = r.stage.gen
	w, err := r.company.LoadWorking()
	if err != nil {
		w = Working{}
	}
	rep := draftReportFrom(s, w.Canon)
	w.Story = s
	w.Report = &rep
	w.Revision++
	w.Status = "draft"
	// A rewritten draft loses the validate_story blessing (review 7, R7-01).
	w.Validated = false
	// Same out-of-band capture as writeDraft (review 3, R3-02): a composer
	// draft is still written from the brief, and it is not dressed yet.
	w.Brief = r.boardBrief()
	w.Dressed = false
	if err := r.company.SaveWorking(w); err != nil {
		return "", fmt.Errorf("playwright fallback: save draft: %w", err)
	}
	return marshalArtifact(rep)
}

// fallbackPlaywrightAnswer answers a consultation in place: the working
// file's shape, without touching the draft.
func (r *Runner) fallbackPlaywrightAnswer(string) (string, error) {
	w, err := r.company.LoadWorking()
	if err != nil {
		return "no draft in the working file yet — ask again once the playwright has written", nil
	}
	return fmt.Sprintf("the draft is %q (%d beats, %d cast, %d props, %s)",
		w.Story.Title, len(w.Story.Beats), len(w.Story.Cast), len(w.Story.Props), w.Status), nil
}

// fallbackScene is the scenographer's floor: the draft's set dressed around
// its cast — the draft's backdrop kept when valid, pieces laid into the
// columns nobody occupies (the staging rules from staging_test.go) — plus a
// valid scene report posted to the board, exactly like write_scene would.
func (r *Runner) fallbackScene() (string, error) {
	w, err := r.company.LoadWorking()
	if err != nil {
		return "", fmt.Errorf("scenographer fallback: no draft to dress")
	}
	scene := DressDraft(r.rnd, w.Story)
	w.Story.Scene = scene
	w.Revision++
	w.Status = "dressed"
	// Dressing rewrites the draft, so it loses the validate_story blessing
	// (review 7, R7-01).
	w.Validated = false
	// The scenographer ran (review 3, R3-02): the out-of-band marker survives
	// a board overflow that trims its deliverable copy.
	w.Dressed = true
	if err := r.company.SaveWorking(w); err != nil {
		return "", fmt.Errorf("scenographer fallback: save scene: %w", err)
	}
	rep := SceneReport{Backdrop: scene.Backdrop, Reason: "deterministic dressing"}
	for _, c := range scene.Cells {
		rep.Cells = append(rep.Cells, CellPlacement{Row: c.Row, Col: c.Col, Piece: c.Piece})
	}
	for _, p := range w.Story.Props {
		rep.Props = append(rep.Props, PropPlacement{ID: p.ID, X: p.X, Lane: p.Lane})
	}
	text, err := marshalArtifact(rep)
	if err != nil {
		return "", err
	}
	if err := r.postToBoard("scenographer", "deliverable", "director", text); err != nil {
		ancli.Errf("theatre: scene report post failed: %v", err)
	}
	return text, nil
}

// fallbackSceneAnswer answers a consultation in place: the set as dressed.
func (r *Runner) fallbackSceneAnswer(string) (string, error) {
	w, err := r.company.LoadWorking()
	if err != nil {
		return "no draft in the working file yet — ask again once the playwright has written", nil
	}
	return fmt.Sprintf("the set is %s with %d cells",
		w.Story.Scene.Backdrop, len(w.Story.Scene.Cells)), nil
}

// fallbackAdvice is the wardrobe's floor: a registry-grounded answer. A
// question naming a known character gets its pinned look — plus a lane note
// when a backdrop is mentioned; anything else is a clear "no registry entry"
// refusal, because the wardrobe never invents a look.
func (r *Runner) fallbackAdvice(question string) (string, error) {
	if r.registry == nil {
		return "no registry entry: the character registry is not wired", nil
	}
	id := r.knownCharacterIn(question)
	if id == "" {
		return fmt.Sprintf("no registry entry: the question names no known character (%s)",
			strings.Join(r.registry.IDs(), ", ")), nil
	}
	look, _ := r.registry.Lookup(id)
	coat := look.Coat
	if coat == "" {
		coat = "unpinned"
	}
	answer := fmt.Sprintf("registry says %s=%s (%s)", id, coat, look.Character)
	if bd := backdropIn(question); bd != "" {
		if look.Character == "bird" {
			// A bird reads by species, not by lane: it perches above the
			// ground line, so the advice is about the perch, not the floor.
			answer += fmt.Sprintf("; on %s keep the bird perched in mid — the %s reads", bd, coat)
		} else {
			answer += fmt.Sprintf("; on %s keep lane ≥ 1", bd)
		}
	}
	return answer, nil
}

// knownCharacterIn finds the registered character a question names, if any.
func (r *Runner) knownCharacterIn(question string) string {
	q := strings.ToLower(question)
	for _, id := range r.registry.IDs() {
		if strings.Contains(q, id) {
			return id
		}
	}
	return ""
}

// backdropIn finds the backdrop a question names, if any, scanning the fixed
// backdrop order so the answer is deterministic.
func backdropIn(question string) string {
	q := strings.ToLower(question)
	for _, b := range backdropNames {
		if strings.Contains(q, b) {
			return b
		}
	}
	return ""
}

// registryLineup picks up to n registered characters for the brief's lineup.
func (r *Runner) registryLineup(n int) []string {
	if r.registry == nil {
		return nil
	}
	ids := r.registry.IDs()
	if len(ids) <= n {
		return ids
	}
	r.rnd.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
	return ids[:n]
}

// premisesNoRepeat lists the themes the company has already worked on, from
// the premises doc, for the brief's no-repeat list (phase 6): the floor
// avoids repeating history even when the LLM is down.
func (r *Runner) premisesNoRepeat(n int) []string {
	premises := r.company.LoadPremises()
	out := make([]string, 0, n)
	for _, p := range premises {
		if len(out) >= n {
			break
		}
		if p.Theme == "" {
			continue
		}
		out = append(out, truncateRunes(p.Theme, MaxNoRepeatLen))
	}
	return out
}

// boardTheme reads the generation's theme from the board; a board read
// failure degrades to the empty theme.
func (r *Runner) boardTheme() string {
	board, err := r.company.LoadBoard()
	if err != nil {
		return ""
	}
	return board.Theme
}

// draftReportFrom summarises a story into its draft report — the playwright
// floor's way of filling the artifact the wrapper validates.
func draftReportFrom(s model.Story, canon []string) DraftReport {
	rep := DraftReport{
		Title:      s.Title,
		Cast:       castIDs(s),
		Props:      propIDs(s),
		BeatsCount: len(s.Beats),
		Canon:      canon,
	}
	if len(s.Beats) > 0 {
		rep.Acts = []Act{{Name: "the whole play", Beats: len(s.Beats), OneLine: s.Title}}
	}
	return rep
}

func castIDs(s model.Story) []string {
	out := make([]string, 0, len(s.Cast))
	for _, c := range s.Cast {
		out = append(out, c.ID)
	}
	return out
}

func propIDs(s model.Story) []string {
	out := make([]string, 0, len(s.Props))
	for _, p := range s.Props {
		out = append(out, p.ID)
	}
	return out
}

// marshalArtifact renders an artifact as its JSON text.
func marshalArtifact(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal artifact: %w", err)
	}
	return string(b), nil
}
