package model

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Story is a short character-driven scene played by the intro splash. It is
// produced ahead of time (by an LLM storyteller, or by the deterministic
// composer) and consumed by the frontend player.
//
// A Story is untrusted data: it may be LLM authored, and it drives animation and
// on-screen text. Always run Validate before handing one to a client.
type Story struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// DurationMs is the intended length of the whole scene.
	DurationMs int `json:"durationMs"`
	// Origin records who made it: "llm" or "composer". Diagnostics only.
	Origin string `json:"origin,omitempty"`
	// Theme is what the play riffs on — normally the last watched title.
	Theme string `json:"theme,omitempty"`
	// Scene is the set the play happens on.
	Scene Scene  `json:"scene"`
	Cast  []Cast `json:"cast"`
	Props []Prop `json:"props,omitempty"`
	Beats []Beat `json:"beats"`
}

// Scene is the set: a backdrop, plus a grid of cells holding set pieces.
//
// The cell grid is how the playwright dresses the stage. A cell is a slot at a
// (row, col) address; a piece occupies it. Because cells are addressed by id,
// a beat can swap what stands in one PART WAY THROUGH the play — the lamp goes
// out, a tree becomes a moon — without disturbing the cast.
type Scene struct {
	// Backdrop selects the sky/ground treatment, e.g. "night", "livingroom".
	Backdrop string `json:"backdrop"`
	// Cells dress the set. Optional; a bare backdrop is a valid set.
	Cells []Cell `json:"cells,omitempty"`
}

// Cell is one addressable slot on the set.
type Cell struct {
	// ID is referenced by "setCell" beats.
	ID string `json:"id"`
	// Row is the depth band: sky, far, mid or near.
	Row string `json:"row"`
	// Col is the horizontal slot, 0..CellCols-1.
	Col int `json:"col"`
	// Piece is what stands here. Empty means the cell is bare.
	Piece string `json:"piece,omitempty"`
}

// Cast is one character on stage.
type Cast struct {
	// ID is referenced by Beat.Actor, e.g. "ina".
	ID string `json:"id"`
	// Character selects the art and voice: "cat", "dog" or "mouse".
	Character string `json:"character"`
	// Coat names a palette within that character. Empty picks at random.
	Coat string `json:"coat,omitempty"`
	// Lane is the depth row; 0 is nearest the camera.
	Lane int `json:"lane"`
	// Scale multiplies the character's natural size.
	Scale float64 `json:"scale,omitempty"`
	// X is the starting mark as a fraction of stage width (0..1).
	X float64 `json:"x"`
}

// Prop is a non-acting object characters can interact with.
type Prop struct {
	ID   string  `json:"id"`
	Prop string  `json:"prop"`
	Lane int     `json:"lane"`
	X    float64 `json:"x"`
}

// Beat is one scheduled action.
type Beat struct {
	// T is milliseconds from the start of the story.
	T int `json:"t"`
	// Actor is a Cast.ID.
	Actor string `json:"actor"`
	// Action is one of ValidActions.
	Action string `json:"action"`
	// X is the destination mark for movement actions (fraction of width).
	X float64 `json:"x,omitempty"`
	// Target is another Cast.ID or Prop.ID, for actions that need one.
	Target string `json:"target,omitempty"`
	// Ms is how long the action should take. Zero lets the player choose.
	Ms int `json:"ms,omitempty"`
	// From is the entry side for "enter"/"exit": "left" or "right".
	From string `json:"from,omitempty"`
	// Piece carries the new value for scene actions: the set piece for
	// "setCell", or the backdrop name for "setBackdrop".
	Piece string `json:"piece,omitempty"`
}

// Limits for a story. These bound both LLM creativity and the animation cost on
// a weak TV.
const (
	MaxStoryDurationMs = 10000
	MinStoryDurationMs = 1200
	MaxCast            = 5
	MaxProps           = 4
	MaxBeats           = 44
	MaxTitleLen        = 64
	MaxLanes           = 3
	MaxCells           = 10
	CellCols           = 6
)

// ValidCharacters are the characters the frontend player can draw.
var ValidCharacters = map[string]bool{
	"cat":   true,
	"dog":   true,
	"mouse": true,
}

// ValidBackdrops are the sets the player can dress the stage with.
var ValidBackdrops = map[string]bool{
	"night":      true,
	"livingroom": true,
	"garden":     true,
	"theatre":    true,
	"sunset":     true,
}

// DefaultBackdrop is used whenever a story does not name a valid one.
const DefaultBackdrop = "night"

// ValidRows are the depth bands a cell can sit in, back to front.
var ValidRows = map[string]bool{
	"sky":  true,
	"far":  true,
	"mid":  true,
	"near": true,
}

// ValidPieces are the set pieces the player can draw into a cell.
var ValidPieces = map[string]bool{
	"tree":   true,
	"bush":   true,
	"fence":  true,
	"cloud":  true,
	"moon":   true,
	"sofa":   true,
	"lamp":   true,
	"plant":  true,
	"window": true,
	"rug":    true,
}

// ValidProps are the props the frontend player can draw.
var ValidProps = map[string]bool{
	"yarn": true,
	"box":  true,
}

// ValidActions is the closed action vocabulary the player implements. Anything
// outside this set is dropped during validation.
var ValidActions = map[string]bool{
	"enter":    true,
	"exit":     true,
	"walkTo":   true,
	"vocalize": true,
	"sit":      true,
	"stretch":  true,
	"blink":    true,
	"pounce":   true,
	"chase":    true,
	"greet":    true,
	"stareoff": true,
	"nap":      true,
	"bat":      true,
	// Scene actions — these dress the set instead of moving a character, and so
	// carry no actor.
	"setCell":     true,
	"setBackdrop": true,
}

// sceneActions act on the set rather than on a character.
var sceneActions = map[string]bool{
	"setCell":     true,
	"setBackdrop": true,
}

// actionNeedsTarget lists actions that are meaningless without a Target.
var actionNeedsTarget = map[string]bool{
	"pounce":   true,
	"chase":    true,
	"greet":    true,
	"stareoff": true,
	"bat":      true,
}

var idRe = regexp.MustCompile(`^[a-z0-9_]{1,24}$`)

// Validate normalises a Story in place and reports whether it is playable.
//
// This is the trust boundary for LLM-authored stories: unknown characters,
// props and actions are dropped rather than passed through, ids are checked
// against a strict pattern, numbers are clamped into the documented limits and
// the title is stripped of control characters. It deliberately repairs what it
// safely can and errors only when nothing playable remains — a partially odd
// story is better than no splash, but a malformed one must never reach the DOM.
func (s *Story) Validate() error {
	s.Title = sanitizeTitle(s.Title)
	// The theme comes from a filename, so it gets the same treatment.
	s.Theme = sanitizeTitle(s.Theme)

	if !idRe.MatchString(s.ID) {
		return fmt.Errorf("story ID %q does not match %v", s.ID, idRe)
	}

	// An unknown backdrop is a naming slip, not a reason to lose the play.
	s.Scene.Backdrop = strings.ToLower(strings.TrimSpace(s.Scene.Backdrop))
	if !ValidBackdrops[s.Scene.Backdrop] {
		s.Scene.Backdrop = DefaultBackdrop
	}

	// Cells: same closed-vocabulary treatment as everything else. Cell ids share
	// the namespace with cast and props so a beat target is never ambiguous.
	cellIDs := map[string]bool{}
	cells := make([]Cell, 0, len(s.Scene.Cells))
	for _, c := range s.Scene.Cells {
		if len(cells) >= MaxCells {
			break
		}
		c.Row = strings.ToLower(strings.TrimSpace(c.Row))
		c.Piece = strings.ToLower(strings.TrimSpace(c.Piece))
		if !idRe.MatchString(c.ID) || cellIDs[c.ID] || !ValidRows[c.Row] {
			continue
		}
		// An unknown piece empties the cell rather than dropping the slot: the
		// address stays valid so a later setCell beat can still fill it.
		if c.Piece != "" && !ValidPieces[c.Piece] {
			c.Piece = ""
		}
		c.Col = clampInt(c.Col, 0, CellCols-1)
		cellIDs[c.ID] = true
		cells = append(cells, c)
	}
	s.Scene.Cells = cells

	if s.DurationMs > MaxStoryDurationMs {
		s.DurationMs = MaxStoryDurationMs
	}
	if s.DurationMs < MinStoryDurationMs {
		s.DurationMs = MinStoryDurationMs
	}

	// Cast: keep only known characters with unique, well formed ids. The id
	// namespace is shared with props and cells so beat targets are unambiguous.
	seen := map[string]bool{}
	for id := range cellIDs {
		seen[id] = true
	}
	cast := make([]Cast, 0, len(s.Cast))
	for _, c := range s.Cast {
		if len(cast) >= MaxCast {
			break
		}
		if !idRe.MatchString(c.ID) || seen[c.ID] || !ValidCharacters[c.Character] {
			continue
		}
		seen[c.ID] = true
		c.Lane = clampInt(c.Lane, 0, MaxLanes-1)
		c.X = clampFloat(c.X, 0.05, 0.95)
		if c.Scale <= 0 {
			c.Scale = 1
		}
		c.Scale = clampFloat(c.Scale, 0.6, 1.6)
		c.Coat = strings.ToLower(strings.TrimSpace(c.Coat))
		if !idRe.MatchString(c.Coat) {
			c.Coat = ""
		}
		cast = append(cast, c)
	}
	s.Cast = cast
	if len(s.Cast) == 0 {
		return fmt.Errorf("story %q has no valid cast", s.ID)
	}

	// Props: same treatment.
	props := make([]Prop, 0, len(s.Props))
	for _, p := range s.Props {
		if len(props) >= MaxProps {
			break
		}
		if !idRe.MatchString(p.ID) || seen[p.ID] || !ValidProps[p.Prop] {
			continue
		}
		seen[p.ID] = true
		p.Lane = clampInt(p.Lane, 0, MaxLanes-1)
		p.X = clampFloat(p.X, 0.05, 0.95)
		props = append(props, p)
	}
	s.Props = props

	// Beats: drop anything referencing an unknown actor/target or action.
	beats := make([]Beat, 0, len(s.Beats))
	for _, b := range s.Beats {
		if len(beats) >= MaxBeats {
			break
		}
		if !ValidActions[b.Action] {
			continue
		}
		b.Piece = strings.ToLower(strings.TrimSpace(b.Piece))

		if sceneActions[b.Action] {
			// Scene beats dress the set and have no actor.
			switch b.Action {
			case "setCell":
				if !cellIDs[b.Target] {
					continue
				}
				if b.Piece != "" && !ValidPieces[b.Piece] {
					continue
				}
			case "setBackdrop":
				if !ValidBackdrops[b.Piece] {
					continue
				}
			}
			b.Actor = ""
			b.T = clampInt(b.T, 0, s.DurationMs)
			b.Ms = clampInt(b.Ms, 0, s.DurationMs)
			beats = append(beats, b)
			continue
		}

		if !seen[b.Actor] {
			continue
		}
		// A beat's actor must be a character, not a prop or a cell.
		if !isCast(s.Cast, b.Actor) {
			continue
		}
		if b.Target != "" && !seen[b.Target] {
			b.Target = ""
		}
		if actionNeedsTarget[b.Action] && b.Target == "" {
			continue
		}
		// A character action never carries a piece.
		b.Piece = ""
		b.T = clampInt(b.T, 0, s.DurationMs)
		b.Ms = clampInt(b.Ms, 0, s.DurationMs)
		b.X = clampFloat(b.X, 0.0, 1.0)
		if b.From != "left" && b.From != "right" {
			b.From = ""
		}
		beats = append(beats, b)
	}
	s.Beats = beats
	if len(s.Beats) == 0 {
		return fmt.Errorf("story %q has no valid beats", s.ID)
	}

	if s.Title == "" {
		s.Title = "A Very Short Story"
	}
	return nil
}

func isCast(cast []Cast, id string) bool {
	for _, c := range cast {
		if c.ID == id {
			return true
		}
	}
	return false
}

// sanitizeTitle makes an LLM-authored title safe and sane to render. The player
// sets it via textContent, so this is about length and stray control characters
// rather than markup escaping.
func sanitizeTitle(t string) string {
	t = strings.TrimSpace(t)
	var b strings.Builder
	for _, r := range t {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteRune(' ')
		case unicode.IsControl(r):
			// drop
		default:
			b.WriteRune(r)
		}
	}
	t = strings.Join(strings.Fields(b.String()), " ")
	if len(t) > MaxTitleLen {
		t = strings.TrimSpace(t[:MaxTitleLen])
	}
	return t
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
