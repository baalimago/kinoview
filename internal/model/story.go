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
	Cast   []Cast `json:"cast"`
	Props  []Prop `json:"props,omitempty"`
	Beats  []Beat `json:"beats"`
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
}

// Limits for a story. These bound both LLM creativity and the animation cost on
// a weak TV.
const (
	MaxStoryDurationMs = 5000
	MinStoryDurationMs = 1200
	MaxCast            = 4
	MaxProps           = 3
	MaxBeats           = 24
	MaxTitleLen        = 64
	MaxLanes           = 3
)

// ValidCharacters are the characters the frontend player can draw.
var ValidCharacters = map[string]bool{
	"cat":   true,
	"dog":   true,
	"mouse": true,
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

	if !idRe.MatchString(s.ID) {
		return fmt.Errorf("story ID %q does not match %v", s.ID, idRe)
	}

	if s.DurationMs > MaxStoryDurationMs {
		s.DurationMs = MaxStoryDurationMs
	}
	if s.DurationMs < MinStoryDurationMs {
		s.DurationMs = MinStoryDurationMs
	}

	// Cast: keep only known characters with unique, well formed ids.
	seen := map[string]bool{}
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
		if !ValidActions[b.Action] || !seen[b.Actor] {
			continue
		}
		// A beat's actor must be a character, not a prop.
		if !isCast(s.Cast, b.Actor) {
			continue
		}
		if b.Target != "" && !seen[b.Target] {
			b.Target = ""
		}
		if actionNeedsTarget[b.Action] && b.Target == "" {
			continue
		}
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
