// Package troupe is the fixed, human-owned stage: the frozen grammar, its
// validator, the resolver, the closed tool registry, the spawn runner and —
// in later phases — the two fixed roles. Nothing here is ever agent-authored:
// the troupe authors instances of these types as notes in the slivingdoc
// notebook, and the stage interprets them. The grammar is frozen; later
// phases extend the validator, never relax it.
//
// Phase 1 ships the grammar itself: the atom types, the uniform asset
// envelope and the validator that keeps the notebook and the grammar from
// drifting. Phase 3 ships the resolver: it walks a worktree snapshot,
// validates every file, closes the typed id@version references of one play
// and emits the resolved play. Phase 4 ships the role notes, the closed
// name→tool registry and the recursive spawn runner: roles are notes the
// stage reads and executes by name, each spawn a bounded agent with exactly
// the tools its note selects. Phase 5 ships the termination authority: one
// generation budget with the token stoploss (the only operator flag), the
// hardcoded global call max and the depth cap, enforced on the spawn runner
// and accounted through every spawned agent's usage recorder. Phase 6 ships
// the submit_play gate: the single-writer persistence boundary that
// validates and resolves the named play from the worktree, stamps it
// submitted and durably persists it with its plays/index.json metadata
// entry. Phase 7 ships the director and the swarm: the fixed sovereign role
// (director.go) runs one generation per Director.Run — feedback-read through
// decompose → swarm → assemble → submit, or exhaustion — spawning sub-agents
// through the Swarm seam (swarm.go, the Spawner) and submitting through the
// submit_play gate, behind the facade (troupe.go) that guards generations
// with the hardcoded cooldown and single-flight. Phase 8 ships the critic:
// the fixed advisory role (critic.go) that reviews each generation after it
// runs — reading viewer feedback, the submitted play and the notes the swarm
// left — and appends one evidence-cited criticism note to feedback/ through
// the critic-only write_criticism gate (never vetoing, never editing, and
// reviewing the empty stage honestly when a generation shipped nothing). The
// human-readable mirror is STAGE.md; the Go validator is the authority.
package troupe

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Kind is one of the six closed asset kinds. A new kind is a human-gated
// grammar change, never an agent-authored one.
type Kind string

const (
	KindModel Kind = "model"
	KindClip  Kind = "clip"
	KindVoice Kind = "voice"
	KindSound Kind = "sound"
	KindGag   Kind = "gag"
	KindPlay  Kind = "play"
)

// Status is the closed asset status enum. Assets stay draft while the swarm
// works; submit_play sets submitted on the play it persists.
type Status string

const (
	StatusDraft     Status = "draft"
	StatusSubmitted Status = "submitted"
)

// Ref is an id@version reference into one of the versioned asset kinds. The
// referenced kind is carried by the field that holds the ref (model resolves
// into models/, voice into voices/, …), never by the string itself.
type Ref string

// Envelope is the uniform asset envelope. The kind-specific content stays raw
// under Spec so the served resolved play never re-marshals it. The filename
// is the only authority for id/version; the envelope must match it exactly or
// the file is rejected. Plays are the exception: no version, a story_<UTC> id.
type Envelope struct {
	Kind       Kind            `json:"kind"`
	ID         string          `json:"id"`
	Version    int             `json:"version,omitempty"`
	Status     Status          `json:"status"`
	Author     string          `json:"author"`
	Provenance string          `json:"provenance"`
	Spec       json.RawMessage `json:"spec"`
}

// Spec is one kind's validated content. Only the six spec types implement it;
// a new kind is a human-gated grammar change.
type Spec interface {
	kind() Kind
}

func (*ModelSpec) kind() Kind { return KindModel }
func (*ClipSpec) kind() Kind  { return KindClip }
func (*VoiceSpec) kind() Kind { return KindVoice }
func (*SoundSpec) kind() Kind { return KindSound }
func (*GagSpec) kind() Kind   { return KindGag }
func (*PlaySpec) kind() Kind  { return KindPlay }

// ── Structure: the rig and its art ────────────────────────────────────────

// Bone is a transform node: a joint (where it rotates) and a segment (its far
// end is where children attach). Bones form a tree — exactly one root with
// parent null.
type Bone struct {
	ID     string   `json:"id"`
	Parent *string  `json:"parent"`
	X      float64  `json:"x"`
	Y      float64  `json:"y"`
	Rot    float64  `json:"rot"`
	Scale  *float64 `json:"scale,omitempty"` // default 1; scale compounds through the rig
	Length float64  `json:"length"`
}

// ShapeType is the closed flat-shape vocabulary: rect (rounded), ellipse and
// path. Flat colour only — no lighting, no gradients.
type ShapeType string

const (
	ShapeRect    ShapeType = "rect"
	ShapeEllipse ShapeType = "ellipse"
	ShapePath    ShapeType = "path"
)

// PathPoint is one vertex of a path shape or a curve region: a compact [x, y]
// pair.
type PathPoint [2]float64

// Shape is one flat attachment shape bound to exactly one bone (rigid
// skinning).
type Shape struct {
	Type   ShapeType   `json:"type"`
	W      *float64    `json:"w,omitempty"`      // rect/ellipse width
	H      *float64    `json:"h,omitempty"`      // rect/ellipse height
	Radius *float64    `json:"radius,omitempty"` // rect corner radius
	Points []PathPoint `json:"points,omitempty"` // path vertices
	Color  string      `json:"color"`
}

// Attachment is a flat shape bound to exactly one bone, with a local
// transform and a flat colour.
type Attachment struct {
	ID    string  `json:"id"`
	Slot  string  `json:"slot"`
	Bone  string  `json:"bone"`
	X     float64 `json:"x,omitempty"`
	Y     float64 `json:"y,omitempty"`
	Rot   float64 `json:"rot,omitempty"`
	Shape Shape   `json:"shape"`
}

// Skin maps slot ids to attachment ids for one named look (a coat is a skin).
// The default skin must cover every attachment a model declares.
type Skin map[string]string

// Skins maps skin names to skins. The default skin is required whenever a
// model declares attachments.
type Skins map[string]Skin

// ModelSpec is a model: a skeleton (bones) plus slots/attachments (art), an
// optional voice/sound pair and optional structural verbs. Characters, props,
// pieces and backdrops are all models, differing only in scale, stage
// position and the role a play assigns.
type ModelSpec struct {
	Bones       []Bone       `json:"bones,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
	Skins       Skins        `json:"skins,omitempty"`
	Voice       *Ref         `json:"voice,omitempty"`
	Sound       *Ref         `json:"sound,omitempty"`
	Structure   []Structure  `json:"structure,omitempty"`
}

// StructureType is the closed procedural-structure vocabulary.
type StructureType string

const (
	StructureAttach  StructureType = "attach"
	StructureScatter StructureType = "scatter"
	StructureRecurse StructureType = "recurse"
)

// Structure is one structural verb. The validator enforces each verb's field
// set strictly: attach uses model/at/scale/rot, scatter uses
// model/count/over/jitter/seed, recurse uses model/at/depth/branch/angle/
// decay/tip/seed.
type Structure struct {
	Type   StructureType `json:"type"`
	Model  Ref           `json:"model"`
	At     string        `json:"at,omitempty"`     // attach/recurse: the bone instances mount on
	Scale  *float64      `json:"scale,omitempty"`  // attach: relative scale, default 1
	Rot    *float64      `json:"rot,omitempty"`    // attach: rotation offset, degrees
	Count  int           `json:"count,omitempty"`  // scatter: instance count
	Over   *Region       `json:"over,omitempty"`   // scatter: the placement region
	Jitter *Jitter       `json:"jitter,omitempty"` // scatter: placement jitter
	Seed   *uint64       `json:"seed,omitempty"`   // scatter/recurse; inherits the play seed when absent
	Depth  int           `json:"depth,omitempty"`  // recurse: nesting depth
	Branch int           `json:"branch,omitempty"` // recurse: children per level
	Angle  float64       `json:"angle,omitempty"`  // recurse: spread per level, degrees
	Decay  float64       `json:"decay,omitempty"`  // recurse: scale per level
	Tip    *Ref          `json:"tip,omitempty"`    // recurse: the terminal model
}

// RegionType is the closed scatter region vocabulary.
type RegionType string

const (
	RegionBand  RegionType = "band"  // a w×h rectangle
	RegionDisc  RegionType = "disc"  // a circle of radius r
	RegionGrid  RegionType = "grid"  // cols×rows cells of size cell
	RegionCurve RegionType = "curve" // a polyline through points
	RegionAlong RegionType = "along" // along a bone of the containing model
)

// Region is one closed scatter placement region.
type Region struct {
	Type   RegionType  `json:"type"`
	W      *float64    `json:"w,omitempty"`      // band
	H      *float64    `json:"h,omitempty"`      // band
	R      *float64    `json:"r,omitempty"`      // disc
	Cols   int         `json:"cols,omitempty"`   // grid
	Rows   int         `json:"rows,omitempty"`   // grid
	Cell   *float64    `json:"cell,omitempty"`   // grid
	Points []PathPoint `json:"points,omitempty"` // curve
	Bone   string      `json:"bone,omitempty"`   // along
}

// Jitter is the seeded placement jitter of a scatter: a fractional scale
// spread and a rotation spread in degrees.
type Jitter struct {
	Scale float64 `json:"scale,omitempty"`
	Rot   float64 `json:"rot,omitempty"`
}

// ── Motion: constraints, keyframes, oscillation ──────────────────────────

// ConstraintType is the closed constraint vocabulary, solved by closed-form
// 2-bone IK in the engine.
type ConstraintType string

const (
	ConstraintReach ConstraintType = "reach" // effector's far end reaches a coordinate
	ConstraintLook  ConstraintType = "look"  // chain faces a coordinate
	ConstraintPlant ConstraintType = "plant" // bone stays at a coordinate while the rest moves
	ConstraintTrack ConstraintType = "track" // chain continuously follows another bone
)

// Hint is the closed pole-direction vocabulary of a reach constraint.
type Hint string

const (
	HintFront Hint = "front"
	HintBack  Hint = "back"
	HintLeft  Hint = "left"
	HintRight Hint = "right"
)

// Coord is a local coordinate target {x, y}.
type Coord struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// ConstraintTarget is a constraint's target: a local coordinate for
// reach/look/plant, or a bone id for track.
type ConstraintTarget struct {
	Coord *Coord
	Bone  string
}

// UnmarshalJSON accepts either shape: {"x":..,"y":..} or "boneId".
func (t *ConstraintTarget) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil
	}
	switch trimmed[0] {
	case '{':
		dec := json.NewDecoder(bytes.NewReader(trimmed))
		dec.DisallowUnknownFields()
		var c Coord
		if err := dec.Decode(&c); err != nil {
			return err
		}
		t.Coord, t.Bone = &c, ""
	case '"':
		if err := json.Unmarshal(trimmed, &t.Bone); err != nil {
			return err
		}
		t.Coord = nil
	default:
		return fmt.Errorf("target must be a {x,y} coordinate or a bone id")
	}
	return nil
}

// Constraint is one authored goal. The engine solves it analytically; the
// author never writes joint angles.
type Constraint struct {
	Type     ConstraintType   `json:"type"`
	Effector string           `json:"effector,omitempty"` // reach
	Target   ConstraintTarget `json:"target"`             // reach/look: coordinate; track: bone id
	Hint     *Hint            `json:"hint,omitempty"`     // reach: pole direction
	Chain    string           `json:"chain,omitempty"`    // look/track: the chain root
	Bone     string           `json:"bone,omitempty"`     // plant
	At       *Coord           `json:"at,omitempty"`       // plant
}

// Channel is a keyframable/oscillating transform channel.
type Channel string

const (
	ChannelX       Channel = "x"
	ChannelY       Channel = "y"
	ChannelRot     Channel = "rotation"
	ChannelScaleX  Channel = "scaleX"
	ChannelScaleY  Channel = "scaleY"
	ChannelOpacity Channel = "opacity"
)

// Easing is the one easing enum, shared by keyframes and tweens.
type Easing string

const (
	EasingLinear Easing = "linear"
	EasingIn     Easing = "ease-in"
	EasingOut    Easing = "ease-out"
	EasingInOut  Easing = "ease-in-out"
)

// Key is one keyframe sample: a time in milliseconds and a channel value.
type Key struct {
	T int64   `json:"t"`
	V float64 `json:"v"`
}

// Keyframe is the FK escape hatch: a channel keyframed over time on a bone or
// a slot. x/y are bone-only channels — a slot is rigidly skinned and never
// slides on its bone; it keyframes rotation/scaleX/scaleY/opacity.
type Keyframe struct {
	Bone    string  `json:"bone,omitempty"`
	Slot    string  `json:"slot,omitempty"`
	Channel Channel `json:"channel"`
	Easing  Easing  `json:"easing"`
	Keys    []Key   `json:"keys"`
}

// Oscillation is a periodic channel (sine with amplitude/frequency/phase), for
// a tail wag or a body bob. Oscillations target bones only.
type Oscillation struct {
	Bone    string  `json:"bone"`
	Channel Channel `json:"channel"`
	Amp     float64 `json:"amp"`
	Freq    float64 `json:"freq"`
	Phase   float64 `json:"phase"`
}

// ClipEvent is a timed audio event inside a clip: exactly one of voice:true
// (vocalize with the instance's resolved voice) or sound: "id@version".
type ClipEvent struct {
	At    int64 `json:"at"`
	Voice *bool `json:"voice,omitempty"`
	Sound *Ref  `json:"sound,omitempty"`
}

// ClipSpec is motion in the model's own frame — relative to the model's root.
// It animates bones only (never clips) and never moves the root across the
// stage; crossing the stage is a play tween's job.
type ClipSpec struct {
	Duration     int64         `json:"duration"`
	Loop         bool          `json:"loop"`
	Constraints  []Constraint  `json:"constraints,omitempty"`
	Keyframes    []Keyframe    `json:"keyframes,omitempty"`
	Oscillations []Oscillation `json:"oscillations,omitempty"`
	Events       []ClipEvent   `json:"events,omitempty"`
}

// ── Audio ────────────────────────────────────────────────────────────────

// VoiceSpec is the formant vocalization, mapping 1:1 to the engine's formant
// synth: fixed-length numeric arrays and 0–1 scalars.
type VoiceSpec struct {
	Dur        []float64   `json:"dur"`                  // [2] burst duration range, seconds, positive
	F0         []float64   `json:"f0"`                   // [2] fundamental range, Hz, positive
	Amp        []float64   `json:"amp"`                  // [2] amplitude range, 0–1
	Kf         []float64   `json:"kf"`                   // [4] ramp fractions of the duration, 0–1 ascending
	Tracks     [][]float64 `json:"tracks"`               // [3][4] formant tracks, Hz, positive
	Gains      []float64   `json:"gains"`                // [3] per-formant gain, 0–1
	Q          []float64   `json:"q"`                    // [3] per-formant Q, positive
	Mouth      []float64   `json:"mouth"`                // [4] vowel path, Hz, positive
	Pitch      []float64   `json:"pitch"`                // [3] pitch curve multipliers, positive
	Vib        []float64   `json:"vib"`                  // [3] vibrato: rate range and depth fraction of f0
	Noise      *float64    `json:"noise"`                // 0–1
	Pure       *float64    `json:"pure"`                 // 0–1
	Bursts     []int       `json:"bursts"`               // [2] burst count range, positive integers
	Gap        []float64   `json:"gap"`                  // [2] gap between bursts, seconds, positive
	Decay      *float64    `json:"decay"`                // 0–1
	BurstPitch []float64   `json:"burstPitch,omitempty"` // [3] optional per-burst pitch multipliers
}

// SoundType is the closed environmental sound vocabulary.
type SoundType string

const (
	SoundNoise SoundType = "noise"
	SoundTone  SoundType = "tone"
	SoundSweep SoundType = "sweep"
	SoundBurst SoundType = "burst"
)

// SoundEnv is the amplitude envelope of a sound effect.
type SoundEnv struct {
	Attack *float64 `json:"attack"`
	Decay  *float64 `json:"decay"`
}

// SoundSpec is an environmental (non-vocal) sound effect: one of the four
// synthesis types with frequency/duration/amplitude ranges and an envelope.
type SoundSpec struct {
	Type SoundType `json:"type"`
	Freq []float64 `json:"freq"` // [2] Hz range, positive
	Dur  []float64 `json:"dur"`  // [2] seconds range, positive
	Amp  []float64 `json:"amp"`  // [2] 0–1 range
	Env  SoundEnv  `json:"env"`
}

// ── Composition ──────────────────────────────────────────────────────────

// GagSpec is a named sequence of clips, played sequentially by their own
// durations. A gag references clips only — never another gag.
type GagSpec struct {
	Clips []Ref `json:"clips"`
}

// Side is a stage-relative side. beside accepts all four; off accepts
// left/right only.
type Side string

const (
	SideLeft  Side = "left"
	SideRight Side = "right"
	SideFront Side = "front"
	SideBack  Side = "back"
)

// Tween is a root-bone stage motion on one play instance: the instance's root
// moves from its current pose to a target over over milliseconds with an
// easing. It moves the whole instance — it is how a character crosses the
// stage or meets another.
type Tween struct {
	To     TweenTarget `json:"to"`
	Over   int64       `json:"over"`
	Easing Easing      `json:"easing"`
}

// TweenTarget is exactly one of: absolute coordinates (any subset of
// x/y/rot/scale), a beside reference to another instance, or an off exit
// side.
type TweenTarget struct {
	X      *float64 `json:"x,omitempty"`
	Y      *float64 `json:"y,omitempty"`
	Rot    *float64 `json:"rot,omitempty"`
	Scale  *float64 `json:"scale,omitempty"`
	Beside string   `json:"beside,omitempty"` // instance id to meet
	Side   *Side    `json:"side,omitempty"`   // beside side
	Off    *Side    `json:"off,omitempty"`    // exit side, left/right
}

// PlayInstance is one model instantiation: an id, the model, a role, a stage
// position, and optional voice/sound overrides.
type PlayInstance struct {
	ID    string   `json:"id"`
	Model Ref      `json:"model"`
	Role  string   `json:"role"`
	Scale float64  `json:"scale"`
	X     *float64 `json:"x,omitempty"`
	Y     *float64 `json:"y,omitempty"`
	Voice *Ref     `json:"voice,omitempty"`
	Sound *Ref     `json:"sound,omitempty"`
}

// TimelineEntry is one timed play verb on one instance: exactly one of
// clip/gag/tween. Entries sharing the same at run concurrently.
type TimelineEntry struct {
	At    int64  `json:"at"`
	On    string `json:"on"`
	Clip  *Ref   `json:"clip,omitempty"`
	Gag   *Ref   `json:"gag,omitempty"`
	Tween *Tween `json:"tween,omitempty"`
}

// PlaySpec is model instantiations plus a timed timeline. The optional seed
// seeds every structural verb that does not carry its own.
type PlaySpec struct {
	Instances []PlayInstance  `json:"instances"`
	Timeline  []TimelineEntry `json:"timeline"`
	Seed      *uint64         `json:"seed,omitempty"`
}

// ── The resolved play (the served artifact) ──────────────────────────────

// ResolvedPlay is the single served artifact: the play with its references
// intact plus a flattened, transitively-closed asset table. The engine
// consumes this format, never the authoring surface.
type ResolvedPlay struct {
	Play   Envelope       `json:"play"`
	Assets ResolvedAssets `json:"assets"`
}

// ResolvedAssets is the flattened asset table, keyed by id@version within
// each kind — cat@1 the model and cat@1 the voice are distinct.
type ResolvedAssets struct {
	Models map[string]Envelope `json:"models"`
	Voices map[string]Envelope `json:"voices"`
	Sounds map[string]Envelope `json:"sounds"`
	Clips  map[string]Envelope `json:"clips"`
	Gags   map[string]Envelope `json:"gags"`
}
