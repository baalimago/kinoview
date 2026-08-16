package theatre

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/agents/theatre/tools"
	"github.com/baalimago/kinoview/internal/model"
)

// The role prompts are the working-context standard's role section (phase 1).
// Each declares its scope explicitly in three sections — what it decides, what
// it asks and when it stops — so a role never drifts outside its remit and
// never burns budget past its deliverable. The four production roles land
// here in full (phase 5); the director's own prompt lives in director.go.

const dramaturgPrompt = `You are the dramaturg. You decide: the production brief — the mood the story should carry, the shape it should take, the 1-3 member cast lineup, and what to avoid repeating. You ask: nothing — you work from the director's notes alone; the playwright and the scenographer build on your brief. You stop: when your final answer is the brief text — deliver it once and stop.`

const playwrightPrompt = `You are the playwright. You decide: the full draft — the title, the beats, the cast usage, the props, and 1-2 canon facts the story leaves behind (short past-tense outcomes, at most 120 characters each, carried in the story's "canon" array). Riff on the canon facts you are told. You ask: the wardrobe, via consult, when a look needs checking against the set. You stop: when your final answer is the complete story as a single JSON object — it is checked against the story schema, so follow it exactly and never guess. The field rules: "cast" is an array of {"id","character","coat","lane","scale","x"} with character one of cat, dog, mouse, bird; "props" is an array of {"id","prop","lane","x"} with prop one of yarn, box, ball, bone, cushion, bowl; "beats" is an array of {"t","actor","action","x","target","ms","from","piece"} with actor a cast id and action one of enter, exit, walkTo, vocalize, sit, stretch, blink, pounce, chase, greet, stareoff, nap, bat, yawn, sniff, jump, setCell, setBackdrop; "scene" is {"backdrop": one of night, livingroom, garden, theatre, sunset, kitchen, forest, rain, "cells": []}; "durationMs" is 1200-10000. Positions: "x" is the stage position as a fraction 0.0-1.0 — 0.0 is far left, 1.0 is far right, 0.5 is centre — never a 0-100 mark; "lane" is 0-2, 0 nearest the viewer, 2 farthest; "t" is the beat's start time in ms and "ms" its duration. Spread the cast across the stage; never give two performers the same x and lane. Every cast member must enter with an enter beat — a character that never enters misses its entrance and stands at its cast mark from the first frame. Dress the stage: aim for 2-4 scene cells and 1-3 props so the set reads as a place, not an empty stage. Write the story once and stop.`

const scenographerPrompt = `You are the scenographer. You decide: the set around the draft's staging — the backdrop, the cells and the prop placements; never put a piece through a performer. You ask: the wardrobe, via consult, when a coat's contrast against a backdrop needs checking. You stop: when the scene is delivered with write_scene — deliver it once and stop.`

const wardrobePrompt = `You are the wardrobe consultant. You decide: nothing — you answer questions about character looks against backdrops, from the fixed cast and its canon looks. You ask: nothing. You stop: when your answer is given with advise — answer once and stop.`

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
// depth: consult for every role except the wardrobe (its "You ask: nothing"
// scope, review 1 R1-02), plus the role's deliverable writer. The author,
// questioner and depth are pinned at construction, so a tool can never act as
// another role or escape its hop depth. The writers validate their artifacts
// at the wrapper boundary (phase 5) before anything enters the working file.
func (r *Runner) roleTools(ctx context.Context, role string, depth int) []models.LLMTool {
	var shared []models.LLMTool
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
		// The dramaturg's brief is its final answer — free text, like the
		// playwright's structured story, so no writer tool is needed; the role
		// also writes the brief into the shared notebook itself (the NOTES
		// contract). It keeps consult.
		return shared
	case "playwright":
		// The playwright's story arrives as its structured final answer (the
		// machine fix, 2026-08-03): no writer tools — the runner persists the
		// final answer into the working file, and the canon facts ride on the
		// story's "canon" array. It keeps consult.
		return shared
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

// writeDraft saves the playwright's draft into the working file. The story is
// parsed and run through model.Story.Validate — the same trust boundary as a
// fresh LLM reply — before it is allowed into the file; the generation id is
// the story id, because the theatre owns the ids (the LLM never does). The
// report, when it is a JSON draft report, is stored beside the draft — its
// acts supersede the derived count — and the canon facts the playwright kept
// are appended to the working file, truncated to the canon cap.
//
// The rejection errors carry the schema field rules: the production
// playwright used to burn its whole budget guessing the story shape from
// bare errors like "no valid cast"; the hint turns the first failure into a
// self-correction roundtrip (machine fix, 2026-08-03).
func (r *Runner) writeDraft(story, report string) (string, error) {
	var s model.Story
	if err := json.Unmarshal([]byte(story), &s); err != nil {
		return "", fmt.Errorf("draft is not valid JSON: %v — the story must be a JSON object: cast an array of {\"id\",\"character\",\"coat\",\"lane\",\"scale\",\"x\"}, props an array of {\"id\",\"prop\",\"lane\",\"x\"}, beats an array of {\"t\",\"actor\",\"action\",\"x\",\"target\",\"ms\",\"from\",\"piece\"} with \"x\" a stage fraction 0.0-1.0 (0.0 far left, 1.0 far right)", err)
	}
	s.ID = r.stage.gen
	s.Origin = "llm"
	if err := s.Validate(); err != nil {
		return "", fmt.Errorf("draft rejected: %v — follow the story schema: cast entries are {\"id\",\"character\",\"coat\",\"lane\",\"scale\",\"x\"} with character one of cat/dog/mouse/bird; beats are {\"t\",\"actor\",\"action\",...} with actor a cast id and action one of enter/exit/walkTo/vocalize/sit/stretch/blink/pounce/chase/greet/stareoff/nap/bat/yawn/sniff/jump; \"x\" is a stage fraction 0.0-1.0 (0.0 far left, 1.0 far right), never a 0-100 mark", err)
	}
	w, err := r.company.LoadWorking()
	if err != nil {
		w = Working{}
	}
	// A fresh draft replaces the previous generation's report: the report is
	// the playwright's own account of THIS draft, and an old one would
	// otherwise leak into the working summary — the 09:53 production carried
	// "The Office S06E06" (13 beats) beside a draft titled S06E09 (9 beats).
	w.Report = nil
	if rep, ok := parseDraftReport(report); ok {
		w.Report = &rep
		for _, f := range rep.Canon {
			if len(w.Canon) >= CanonMaxFacts {
				break
			}
			w.Canon = append(w.Canon, truncateRunes(strings.TrimSpace(f), CanonMaxFact))
		}
	}
	// The canon facts ride on the structured story deliverable: the
	// playwright's story JSON carries a "canon" array (machine fix,
	// 2026-08-03). model.Story does not know the field — it is a
	// soft-continuity seam, not part of the playable story — so the wrapper
	// reads it off the raw JSON before the model.Story unmarshal drops it.
	var canon struct {
		Canon []string `json:"canon"`
	}
	if json.Unmarshal([]byte(story), &canon) == nil {
		for _, f := range canon.Canon {
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
	if err := r.company.SaveWorking(w); err != nil {
		return "", err
	}
	return fmt.Sprintf("draft saved (revision %d)", w.Revision), nil
}

// writeScene dresses the working draft's set. Without a draft there is
// nothing to dress; the scenographer must wait for the playwright. A JSON
// scene report carries the whole dress — backdrop, cells and prop placements,
// validated against the model vocabulary and the draft's own props; a
// free-text report dresses the backdrop only. The working file is the
// authoritative artifact.
func (r *Runner) writeScene(backdrop, report string) (string, error) {
	w, err := r.company.LoadWorking()
	if err != nil {
		return "", fmt.Errorf("no draft in the working file yet — the playwright must write first")
	}
	if sr, ok := parseSceneReport(report); ok {
		backdrop = sr.Backdrop
		if !model.ValidBackdrops[backdrop] {
			return "", fmt.Errorf("unknown backdrop %q", backdrop)
		}
		w.Story.Scene.Backdrop = backdrop
		applyCellPlacements(&w.Story, sr.Cells)
		applyPropPlacements(&w.Story, sr.Props)
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
	if err := r.company.SaveWorking(w); err != nil {
		return "", err
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
