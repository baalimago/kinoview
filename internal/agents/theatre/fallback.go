package theatre

import (
	"encoding/json"
	"fmt"
	"strings"

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

// fallbackBrief is the dramaturg's floor: a brief built from the generation's
// theme and the fixed cast, returned as the deliverable text.
func (r *Runner) fallbackBrief() (string, error) {
	brief := BriefArtifact{
		Mood:   pick(r.rnd, briefMoods),
		Shape:  pick(r.rnd, SceneNames()),
		Lineup: r.castLineup(MaxLineup),
		Theme:  r.theme,
	}
	return marshalArtifact(brief)
}

// fallbackDramaturgAnswer answers a consultation in place: the brief is
// written to the working file by the dramaturg_brief tool, so a consulted
// dramaturg (a consultation, not the brief run) cannot quote it.
func (r *Runner) fallbackDramaturgAnswer(string) (string, error) {
	return "no brief on file — the dramaturg's brief is delivered in its final answer to the director", nil
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
	s := ComposeThemed(r.rnd, r.theme)
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
// valid scene report, exactly like write_scene would.
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
	return marshalArtifact(rep)
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

// fallbackAdvice is the wardrobe's floor: a fixed-cast-grounded answer. A
// question naming a permanent cast member gets its canonical look (the same
// looks the composer draws, floor.go) plus a lane note when a backdrop is
// mentioned; anything else is a clear refusal, because the wardrobe never
// invents a look.
func (r *Runner) fallbackAdvice(question string) (string, error) {
	id := r.knownCharacterIn(question)
	if id == "" {
		return fmt.Sprintf("no known character in the question (the permanent cast is %s)",
			strings.Join(permanentCastIDs(), ", ")), nil
	}
	look := permanentLook(id)
	coat := look.Coat
	if coat == "" {
		coat = "unpinned"
	}
	answer := fmt.Sprintf("the canon look for %s is %s (%s)", id, coat, look.Character)
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

// knownCharacterIn finds the permanent cast member a question names, if any.
func (r *Runner) knownCharacterIn(question string) string {
	q := strings.ToLower(question)
	for _, id := range permanentCastIDs() {
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

// castLineup picks up to n permanent cast ids for the brief's lineup.
func (r *Runner) castLineup(n int) []string {
	ids := permanentCastIDs()
	if len(ids) <= n {
		return ids
	}
	r.rnd.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
	return ids[:n]
}

// permanentCastIDs lists the fixed cast the player can draw — the same cast
// the composer (floor.go) draws.
func permanentCastIDs() []string {
	return []string{ina, freija, mouse, pip}
}

// permanentLook is the canonical look of a fixed cast member — the same looks
// the composer draws (floor.go). The wardrobe's floor answers from these,
// never from a draft.
func permanentLook(id string) model.Cast {
	switch id {
	case ina:
		return model.Cast{ID: ina, Character: "cat", Coat: "ginger"}
	case freija:
		return model.Cast{ID: freija, Character: "dog", Coat: "tan"}
	case mouse:
		return model.Cast{ID: mouse, Character: "mouse"}
	case pip:
		return model.Cast{ID: pip, Character: "bird", Coat: "chaffinch"}
	}
	return model.Cast{}
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
