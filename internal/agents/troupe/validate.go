package troupe

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Identity and shape rules. The grammar is frozen: these patterns are the
// filesystem-safety and reference contracts every authoring note must meet.
var (
	// idRe is the versioned asset and role id charset: filesystem-safe
	// lowercase alnum, _ and -. Asset ids become filenames.
	idRe = regexp.MustCompile(`^[a-z0-9_-]{1,64}$`)
	// intraIDRe is the intra-spec id charset (bones, attachments, slots,
	// skins, play instances): camelCase is allowed because these ids never
	// become filenames.
	intraIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	// playIDRe is the play id exception: plays are unversioned and named
	// story_<UTC>, a lexicographically sortable datetime id.
	playIDRe = regexp.MustCompile(`^story_[0-9]{8}T[0-9]{6}Z$`)
	// refRe is an id@version reference into one of the versioned kinds.
	refRe = regexp.MustCompile(`^[a-z0-9_-]{1,64}@[1-9][0-9]*$`)
	// colorRe is the flat colour: #rrggbb.
	colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	// instanceIndexRe is a generated-instance index inside a selector path.
	instanceIndexRe = regexp.MustCompile(`^[0-9]+$`)
)

// kindDirs maps notebook directory names to their asset kind. A file in any
// other directory is not a grammar file.
var kindDirs = map[string]Kind{
	"models": KindModel,
	"clips":  KindClip,
	"voices": KindVoice,
	"sounds": KindSound,
	"gags":   KindGag,
	"plays":  KindPlay,
}

// Parse validates one notebook file. filename is the identity authority: the
// directory names the kind and the stem carries id@version (versioned kinds)
// or a bare story_<UTC> id (plays); the envelope must match exactly. data is
// the file's JSON content. It returns the envelope — the spec preserved
// byte-for-byte for the served artifact — and the decoded, validated spec.
func Parse(filename string, data []byte) (Envelope, Spec, error) {
	wantKind, wantID, wantVersion, err := filenameIdentity(filename)
	if err != nil {
		return Envelope{}, nil, fmt.Errorf("troupe: %w", err)
	}

	var env Envelope
	if err := decodeStrict(data, &env, filename); err != nil {
		return Envelope{}, nil, fmt.Errorf("troupe: %w", err)
	}

	e := &errs{}
	validateEnvelope(env, wantKind, wantID, wantVersion, e)
	if err := e.err(); err != nil {
		return Envelope{}, nil, fmt.Errorf("troupe: %s: %w", filename, err)
	}

	spec, err := decodeSpec(env.Kind, env.Spec)
	if err != nil {
		return Envelope{}, nil, fmt.Errorf("troupe: %s: %w", filename, err)
	}
	validateSpec(spec, e)
	if err := e.err(); err != nil {
		return Envelope{}, nil, fmt.Errorf("troupe: %s: %w", filename, err)
	}
	return env, spec, nil
}

// filenameIdentity derives kind, id and version from one notebook filename:
// <kinddir>/<id>[@<version>].json. Versioned kinds carry id@version; plays
// carry a bare story_<UTC> id.
func filenameIdentity(filename string) (Kind, string, int, error) {
	dir, base := filepath.Split(filepath.Clean(filename))
	kind, ok := kindDirs[strings.TrimSuffix(dir, string(filepath.Separator))]
	if !ok {
		return "", "", 0, fmt.Errorf("filename %s: %q is not a notebook kind directory (models/clips/voices/sounds/gags/plays)", filename, strings.TrimSuffix(dir, string(filepath.Separator)))
	}
	stem, found := strings.CutSuffix(base, ".json")
	if !found {
		return "", "", 0, fmt.Errorf("filename %s: must end in .json", filename)
	}

	if kind == KindPlay {
		if !playIDRe.MatchString(stem) {
			return "", "", 0, fmt.Errorf("filename %s: play id %q must match story_YYYYMMDDTHHMMSSZ", filename, stem)
		}
		return kind, stem, 0, nil
	}

	id, ver, ok := strings.Cut(stem, "@")
	if !ok {
		return "", "", 0, fmt.Errorf("filename %s: %q must carry id@version", filename, stem)
	}
	if !idRe.MatchString(id) {
		return "", "", 0, fmt.Errorf("filename %s: id %q must match %s", filename, id, idRe)
	}
	version, err := strconv.Atoi(ver)
	if err != nil || version < 1 {
		return "", "", 0, fmt.Errorf("filename %s: version %q must be a positive integer", filename, ver)
	}
	return kind, id, version, nil
}

// decodeStrict decodes data into v and rejects any field the frozen grammar
// does not declare: the grammar is closed, an unknown field is drift.
func decodeStrict(data []byte, v any, path string) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// errs accumulates validation messages so one file reports every violation,
// not just the first — submit_play returns the exact errors to the director.
type errs struct {
	msgs []string
}

func (e *errs) addf(format string, args ...any) {
	e.msgs = append(e.msgs, fmt.Sprintf(format, args...))
}

func (e *errs) err() error {
	if len(e.msgs) == 0 {
		return nil
	}
	return errors.New(strings.Join(e.msgs, "; "))
}

// validateEnvelope checks the uniform envelope against the identity the
// filename declared.
func validateEnvelope(env Envelope, wantKind Kind, wantID string, wantVersion int, e *errs) {
	switch env.Kind {
	case KindModel, KindClip, KindVoice, KindSound, KindGag, KindPlay:
	default:
		e.addf("kind %q: must be one of model/clip/voice/sound/gag/play", env.Kind)
		return
	}
	if env.Kind != wantKind {
		e.addf("kind %q does not match the %s filename directory", env.Kind, wantKind)
	}
	if env.ID != wantID {
		e.addf("id %q does not match the filename id %q", env.ID, wantID)
	}
	if env.Kind == KindPlay {
		if env.Version != 0 {
			e.addf("version %d: plays are unversioned", env.Version)
		}
	} else if env.Version != wantVersion {
		e.addf("version %d does not match the filename version %d", env.Version, wantVersion)
	}
	if env.Status != StatusDraft && env.Status != StatusSubmitted {
		e.addf("status %q: must be draft or submitted", env.Status)
	}
	if env.Author == "" {
		e.addf("author: must not be empty")
	}
	if env.Provenance == "" {
		e.addf("provenance: must not be empty")
	}
}

// decodeSpec decodes the raw spec into the kind's typed spec.
func decodeSpec(kind Kind, raw json.RawMessage) (Spec, error) {
	switch kind {
	case KindModel:
		var s ModelSpec
		if err := decodeStrict(raw, &s, "spec"); err != nil {
			return nil, err
		}
		return &s, nil
	case KindClip:
		var s ClipSpec
		if err := decodeStrict(raw, &s, "spec"); err != nil {
			return nil, err
		}
		return &s, nil
	case KindVoice:
		var s VoiceSpec
		if err := decodeStrict(raw, &s, "spec"); err != nil {
			return nil, err
		}
		return &s, nil
	case KindSound:
		var s SoundSpec
		if err := decodeStrict(raw, &s, "spec"); err != nil {
			return nil, err
		}
		return &s, nil
	case KindGag:
		var s GagSpec
		if err := decodeStrict(raw, &s, "spec"); err != nil {
			return nil, err
		}
		return &s, nil
	case KindPlay:
		var s PlaySpec
		if err := decodeStrict(raw, &s, "spec"); err != nil {
			return nil, err
		}
		return &s, nil
	default:
		return nil, fmt.Errorf("spec: unknown kind %q", kind)
	}
}

// validateSpec dispatches to the kind's validator.
func validateSpec(spec Spec, e *errs) {
	switch s := spec.(type) {
	case *ModelSpec:
		validateModelSpec(s, "spec", e)
	case *ClipSpec:
		validateClipSpec(s, "spec", e)
	case *VoiceSpec:
		validateVoiceSpec(s, "spec", e)
	case *SoundSpec:
		validateSoundSpec(s, "spec", e)
	case *GagSpec:
		validateGagSpec(s, "spec", e)
	case *PlaySpec:
		validatePlaySpec(s, "spec", e)
	}
}

// ── Shared rules ─────────────────────────────────────────────────────────

func validateRef(r Ref, path string, e *errs) {
	if !refRe.MatchString(string(r)) {
		e.addf("%s: %q must be id@version", path, r)
	}
}

func easingOK(es Easing) bool {
	switch es {
	case EasingLinear, EasingIn, EasingOut, EasingInOut:
		return true
	}
	return false
}

func channelOK(c Channel) bool {
	switch c {
	case ChannelX, ChannelY, ChannelRot, ChannelScaleX, ChannelScaleY, ChannelOpacity:
		return true
	}
	return false
}

// isBoneRef reports whether s names a bone: a plain bone id or a selector
// into generated content.
func isBoneRef(s string) bool {
	return intraIDRe.MatchString(s) || isSelector(s)
}

// isSelector reports whether s is a selector: model:<id>@<version> (all
// instances of a model) or a wildcard path of segments joined by "/". A
// segment is id#* (any instance), id#<index> (one instance) or, in the final
// position, a plain id naming a bone inside the instance. Generated content
// is animatable only through selectors; the engine resolves them at render
// time.
func isSelector(s string) bool {
	if rest, ok := strings.CutPrefix(s, "model:"); ok {
		return refRe.MatchString(rest)
	}
	segs := strings.Split(s, "/")
	for i, seg := range segs {
		if seg == "" {
			return false
		}
		if id, idx, ok := strings.Cut(seg, "#"); ok {
			if !intraIDRe.MatchString(id) || (idx != "*" && !instanceIndexRe.MatchString(idx)) {
				return false
			}
			continue
		}
		if i != len(segs)-1 || !intraIDRe.MatchString(seg) {
			return false
		}
	}
	return true
}

func requirePositive(v *float64, path string, e *errs) {
	if v == nil {
		e.addf("%s: required", path)
	} else if *v <= 0 {
		e.addf("%s %v: must be positive", path, *v)
	}
}

// positive and unit are element checks for numeric ranges.
func positive(x float64) bool { return x > 0 }

func unit(x float64) bool { return x >= 0 && x <= 1 }

// validatePair checks a [lo, hi] range: length, element check and ordering.
func validatePair(e *errs, path, field string, v []float64, check func(float64) bool) {
	if len(v) != 2 {
		e.addf("%s.%s: need a [lo, hi] pair, got %d values", path, field, len(v))
		return
	}
	for _, x := range v {
		if !check(x) {
			e.addf("%s.%s: %v out of range", path, field, x)
		}
	}
	if v[0] > v[1] {
		e.addf("%s.%s: [%v, %v] is not an ordered range", path, field, v[0], v[1])
	}
}

// ── Model ────────────────────────────────────────────────────────────────

func validateModelSpec(s *ModelSpec, path string, e *errs) {
	if len(s.Bones) == 0 && len(s.Structure) == 0 {
		e.addf("%s: a model needs at least one bone or one structural verb", path)
	}
	validateBones(s.Bones, path+".bones", e)
	validateAttachments(s.Attachments, s.Bones, s.Skins, path, e)
	validateSkins(s.Skins, s.Attachments, path, e)
	if s.Voice != nil {
		validateRef(*s.Voice, path+".voice", e)
	}
	if s.Sound != nil {
		validateRef(*s.Sound, path+".sound", e)
	}
	for i, st := range s.Structure {
		validateStructure(st, s.Bones, fmt.Sprintf("%s.structure[%d]", path, i), e)
	}
}

func validateBones(bones []Bone, path string, e *errs) {
	if len(bones) == 0 {
		return
	}
	ids := make(map[string]bool, len(bones))
	for i, b := range bones {
		bonePath := fmt.Sprintf("%s[%d]", path, i)
		if !intraIDRe.MatchString(b.ID) {
			e.addf("%s.id %q: must match %s", bonePath, b.ID, intraIDRe)
		}
		if ids[b.ID] {
			e.addf("%s.id %q: duplicate bone id", bonePath, b.ID)
		}
		ids[b.ID] = true
		if b.Scale != nil && *b.Scale <= 0 {
			e.addf("%s.scale %v: must be positive", bonePath, *b.Scale)
		}
		if b.Length < 0 {
			e.addf("%s.length %v: must be non-negative", bonePath, b.Length)
		}
	}

	roots := 0
	for i, b := range bones {
		if b.Parent == nil {
			roots++
			continue
		}
		if !ids[*b.Parent] {
			e.addf("%s[%d].parent %q: no such bone", path, i, *b.Parent)
		}
	}
	if roots != 1 {
		e.addf("%s: exactly one root bone (parent null), got %d", path, roots)
	}

	// A parent chain longer than the bone count is a cycle; the broken-parent
	// case is reported above.
	byID := make(map[string]Bone, len(bones))
	for _, b := range bones {
		byID[b.ID] = b
	}
	for i, b := range bones {
		seen := 0
		for cur := b; cur.Parent != nil; {
			if !ids[*cur.Parent] {
				break
			}
			seen++
			if seen > len(bones) {
				e.addf("%s[%d]: parent chain contains a cycle", path, i)
				break
			}
			cur = byID[*cur.Parent]
		}
	}
}

func validateAttachments(atts []Attachment, bones []Bone, skins Skins, path string, e *errs) {
	boneIDs := make(map[string]bool, len(bones))
	for _, b := range bones {
		boneIDs[b.ID] = true
	}

	attIDs := make(map[string]bool, len(atts))
	for i, a := range atts {
		ap := fmt.Sprintf("%s.attachments[%d]", path, i)
		if !intraIDRe.MatchString(a.ID) {
			e.addf("%s.id %q: must match %s", ap, a.ID, intraIDRe)
		}
		if attIDs[a.ID] {
			e.addf("%s.id %q: duplicate attachment id", ap, a.ID)
		}
		attIDs[a.ID] = true
		if !intraIDRe.MatchString(a.Slot) {
			e.addf("%s.slot %q: must match %s", ap, a.Slot, intraIDRe)
		}
		if !boneIDs[a.Bone] {
			e.addf("%s.bone %q: no such bone", ap, a.Bone)
		}
		validateShape(a.Shape, ap+".shape", e)
	}
}

func validateShape(sh Shape, path string, e *errs) {
	if !colorRe.MatchString(sh.Color) {
		e.addf("%s.color %q: must be #rrggbb", path, sh.Color)
	}
	switch sh.Type {
	case ShapeRect:
		requirePositive(sh.W, path+".w", e)
		requirePositive(sh.H, path+".h", e)
		if sh.Radius != nil && *sh.Radius < 0 {
			e.addf("%s.radius %v: must be non-negative", path, *sh.Radius)
		}
	case ShapeEllipse:
		requirePositive(sh.W, path+".w", e)
		requirePositive(sh.H, path+".h", e)
	case ShapePath:
		if len(sh.Points) < 3 {
			e.addf("%s.points: a path needs at least 3 vertices, got %d", path, len(sh.Points))
		}
	default:
		e.addf("%s.type %q: must be rect, ellipse or path", path, sh.Type)
	}
}

func validateSkins(skins Skins, atts []Attachment, path string, e *errs) {
	if len(atts) == 0 {
		if len(skins) > 0 {
			e.addf("%s.skins: a model without attachments has nothing to skin", path)
		}
	} else if _, ok := skins["default"]; !ok {
		e.addf("%s.skins: a model with attachments needs a default skin", path)
	}

	attByID := make(map[string]Attachment, len(atts))
	for _, a := range atts {
		attByID[a.ID] = a
	}
	for name, skin := range skins {
		if !intraIDRe.MatchString(name) {
			e.addf("%s.skins: skin name %q must match %s", path, name, intraIDRe)
		}
		for slot, attID := range skin {
			a, ok := attByID[attID]
			if !ok {
				e.addf("%s.skins.%s: attachment %q: no such attachment", path, name, attID)
				continue
			}
			if a.Slot != slot {
				e.addf("%s.skins.%s: slot %q holds %q whose slot is %q", path, name, slot, attID, a.Slot)
			}
		}
	}

	// The default skin is the render baseline: every attachment must be
	// reachable through it; alternatives may add more mappings.
	if len(atts) > 0 {
		def := skins["default"]
		for _, a := range atts {
			if def[a.Slot] != a.ID {
				e.addf("%s.skins.default: attachment %q (slot %q) is not mapped in skins.default", path, a.ID, a.Slot)
			}
		}
	}
}

func validateStructure(st Structure, bones []Bone, path string, e *errs) {
	boneIDs := make(map[string]bool, len(bones))
	for _, b := range bones {
		boneIDs[b.ID] = true
	}

	switch st.Type {
	case StructureAttach:
		validateRef(st.Model, path+".model", e)
		if !boneIDs[st.At] {
			e.addf("%s.at %q: no such bone", path, st.At)
		}
		if st.Scale != nil && *st.Scale <= 0 {
			e.addf("%s.scale %v: must be positive", path, *st.Scale)
		}
		forbidStructureExtras(st, path, e, "at", "scale", "rot")
	case StructureScatter:
		validateRef(st.Model, path+".model", e)
		if st.Count < 1 {
			e.addf("%s.count %d: must be positive", path, st.Count)
		}
		if st.Over == nil {
			e.addf("%s.over: scatter needs a region", path)
		} else {
			validateRegion(*st.Over, bones, path+".over", e)
		}
		if st.Jitter != nil {
			validateJitter(*st.Jitter, path+".jitter", e)
		}
		forbidStructureExtras(st, path, e, "count", "over", "jitter", "seed")
	case StructureRecurse:
		validateRef(st.Model, path+".model", e)
		if !boneIDs[st.At] {
			e.addf("%s.at %q: no such bone", path, st.At)
		}
		if st.Depth < 1 {
			e.addf("%s.depth %d: must be positive", path, st.Depth)
		}
		if st.Branch < 1 {
			e.addf("%s.branch %d: must be positive", path, st.Branch)
		}
		if st.Decay <= 0 || st.Decay > 1 {
			e.addf("%s.decay %v: must be in (0, 1]", path, st.Decay)
		}
		if st.Tip == nil {
			e.addf("%s.tip: recurse needs a terminal model", path)
		} else {
			validateRef(*st.Tip, path+".tip", e)
		}
		forbidStructureExtras(st, path, e, "at", "depth", "branch", "angle", "decay", "tip", "seed")
	default:
		e.addf("%s.type %q: must be attach, scatter or recurse", path, st.Type)
	}
}

// forbidStructureExtras rejects any field outside the verb's declared set:
// the grammar is frozen, attach does not silently accept depth.
func forbidStructureExtras(st Structure, path string, e *errs, allowed ...string) {
	set := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		set[a] = true
	}
	fields := []struct {
		name string
		set  bool
	}{
		{"at", st.At != ""},
		{"scale", st.Scale != nil},
		{"rot", st.Rot != nil},
		{"count", st.Count != 0},
		{"over", st.Over != nil},
		{"jitter", st.Jitter != nil},
		{"seed", st.Seed != nil},
		{"depth", st.Depth != 0},
		{"branch", st.Branch != 0},
		{"angle", st.Angle != 0},
		{"decay", st.Decay != 0},
		{"tip", st.Tip != nil},
	}
	for _, f := range fields {
		if f.set && !set[f.name] {
			e.addf("%s.%s: not a %s field", path, f.name, st.Type)
		}
	}
}

func validateRegion(r Region, bones []Bone, path string, e *errs) {
	boneIDs := make(map[string]bool, len(bones))
	for _, b := range bones {
		boneIDs[b.ID] = true
	}

	switch r.Type {
	case RegionBand:
		requirePositive(r.W, path+".w", e)
		requirePositive(r.H, path+".h", e)
	case RegionDisc:
		requirePositive(r.R, path+".r", e)
	case RegionGrid:
		if r.Cols < 1 {
			e.addf("%s.cols %d: must be positive", path, r.Cols)
		}
		if r.Rows < 1 {
			e.addf("%s.rows %d: must be positive", path, r.Rows)
		}
		requirePositive(r.Cell, path+".cell", e)
	case RegionCurve:
		if len(r.Points) < 2 {
			e.addf("%s.points: a curve needs at least 2 vertices, got %d", path, len(r.Points))
		}
	case RegionAlong:
		if !boneIDs[r.Bone] {
			e.addf("%s.bone %q: no such bone", path, r.Bone)
		}
	default:
		e.addf("%s.type %q: must be band, disc, grid, curve or along", path, r.Type)
	}
}

func validateJitter(j Jitter, path string, e *errs) {
	if j.Scale < 0 {
		e.addf("%s.scale %v: must be non-negative", path, j.Scale)
	}
	if j.Rot < 0 {
		e.addf("%s.rot %v: must be non-negative", path, j.Rot)
	}
}

// ── Clip ─────────────────────────────────────────────────────────────────

func validateClipSpec(s *ClipSpec, path string, e *errs) {
	if s.Duration <= 0 {
		e.addf("%s.duration %d: must be positive", path, s.Duration)
	}
	for i, c := range s.Constraints {
		validateConstraint(c, fmt.Sprintf("%s.constraints[%d]", path, i), e)
	}
	for i, k := range s.Keyframes {
		validateKeyframe(k, fmt.Sprintf("%s.keyframes[%d]", path, i), e)
	}
	for i, o := range s.Oscillations {
		validateOscillation(o, fmt.Sprintf("%s.oscillations[%d]", path, i), e)
	}
	for i, ev := range s.Events {
		validateClipEvent(ev, fmt.Sprintf("%s.events[%d]", path, i), e)
	}
}

func validateConstraint(c Constraint, path string, e *errs) {
	coord := c.Target.Coord != nil
	bone := c.Target.Bone != ""
	switch c.Type {
	case ConstraintReach:
		if !isBoneRef(c.Effector) {
			e.addf("%s.effector %q: not a bone id or selector", path, c.Effector)
		}
		if !coord {
			e.addf("%s.target: reach needs a {x,y} coordinate", path)
		}
		if c.Hint == nil || (*c.Hint != HintFront && *c.Hint != HintBack && *c.Hint != HintLeft && *c.Hint != HintRight) {
			e.addf("%s.hint %v: must be front, back, left or right", path, c.Hint)
		}
		if c.Chain != "" || c.Bone != "" || c.At != nil {
			e.addf("%s: reach uses only effector, target and hint", path)
		}
	case ConstraintLook:
		if !isBoneRef(c.Chain) {
			e.addf("%s.chain %q: not a bone id or selector", path, c.Chain)
		}
		if !coord {
			e.addf("%s.target: look needs a {x,y} coordinate", path)
		}
		if c.Effector != "" || c.Hint != nil || c.Bone != "" || c.At != nil {
			e.addf("%s: look uses only chain and target", path)
		}
	case ConstraintPlant:
		if !isBoneRef(c.Bone) {
			e.addf("%s.bone %q: not a bone id or selector", path, c.Bone)
		}
		if c.At == nil {
			e.addf("%s.at: plant needs a {x,y} coordinate", path)
		}
		if c.Effector != "" || c.Hint != nil || c.Chain != "" {
			e.addf("%s: plant uses only bone and at", path)
		}
	case ConstraintTrack:
		if !isBoneRef(c.Chain) {
			e.addf("%s.chain %q: not a bone id or selector", path, c.Chain)
		}
		if !bone {
			e.addf("%s.target: track needs a bone id", path)
		}
		if c.Effector != "" || c.Hint != nil || c.Bone != "" || c.At != nil {
			e.addf("%s: track uses only chain and target", path)
		}
	default:
		e.addf("%s.type %q: must be reach, look, plant or track", path, c.Type)
	}
}

func validateKeyframe(k Keyframe, path string, e *errs) {
	hasBone, hasSlot := k.Bone != "", k.Slot != ""
	if hasBone == hasSlot {
		e.addf("%s: exactly one of bone/slot, got bone=%q slot=%q", path, k.Bone, k.Slot)
	}
	if hasBone && !isBoneRef(k.Bone) {
		e.addf("%s.bone %q: not a bone id or selector", path, k.Bone)
	}
	if hasSlot && !intraIDRe.MatchString(k.Slot) {
		e.addf("%s.slot %q: must match %s", path, k.Slot, intraIDRe)
	}
	if !channelOK(k.Channel) {
		e.addf("%s.channel %q: must be x, y, rotation, scaleX, scaleY or opacity", path, k.Channel)
	}
	if hasSlot && (k.Channel == ChannelX || k.Channel == ChannelY) {
		e.addf("%s.channel %q: x/y are bone-only channels; a slot is rigidly skinned", path, k.Channel)
	}
	if !easingOK(k.Easing) {
		e.addf("%s.easing %q: must be linear, ease-in, ease-out or ease-in-out", path, k.Easing)
	}
	if len(k.Keys) < 2 {
		e.addf("%s.keys: need at least 2 keys, got %d", path, len(k.Keys))
	}
	last := int64(-1)
	for i, key := range k.Keys {
		if key.T < 0 {
			e.addf("%s.keys[%d].t %d: must be non-negative", path, i, key.T)
		}
		if key.T <= last {
			e.addf("%s.keys[%d].t %d: times must be strictly ascending", path, i, key.T)
		}
		last = key.T
	}
}

func validateOscillation(o Oscillation, path string, e *errs) {
	if !isBoneRef(o.Bone) {
		e.addf("%s.bone %q: not a bone id or selector", path, o.Bone)
	}
	if !channelOK(o.Channel) {
		e.addf("%s.channel %q: must be x, y, rotation, scaleX, scaleY or opacity", path, o.Channel)
	}
	if o.Amp < 0 {
		e.addf("%s.amp %v: must be non-negative", path, o.Amp)
	}
	if o.Freq <= 0 {
		e.addf("%s.freq %v: must be positive", path, o.Freq)
	}
}

func validateClipEvent(ev ClipEvent, path string, e *errs) {
	if ev.At < 0 {
		e.addf("%s.at %d: must be non-negative", path, ev.At)
	}
	switch {
	case ev.Voice == nil && ev.Sound == nil:
		e.addf("%s: exactly one of voice/sound", path)
	case ev.Voice != nil && ev.Sound != nil:
		e.addf("%s: exactly one of voice/sound, got both", path)
	case ev.Voice != nil && !*ev.Voice:
		e.addf("%s.voice: must be true", path)
	case ev.Sound != nil:
		validateRef(*ev.Sound, path+".sound", e)
	}
}

// ── Gag ──────────────────────────────────────────────────────────────────

func validateGagSpec(s *GagSpec, path string, e *errs) {
	if len(s.Clips) == 0 {
		e.addf("%s.clips: a gag needs at least one clip", path)
	}
	for i, c := range s.Clips {
		validateRef(c, fmt.Sprintf("%s.clips[%d]", path, i), e)
	}
}

// ── Voice ────────────────────────────────────────────────────────────────

// The fixed-length arrays map 1:1 to the formant synth: two-value ranges, a
// four-point ramp (kf, mouth, formant track points) and three-value
// per-formant/per-burst curves.
const (
	voicePairLen  = 2
	voiceRampLen  = 4
	voiceTracks   = 3
	voiceCurveLen = 3
)

func validateVoiceSpec(s *VoiceSpec, path string, e *errs) {
	validatePair(e, path, "dur", s.Dur, positive)
	validatePair(e, path, "f0", s.F0, positive)
	validatePair(e, path, "amp", s.Amp, unit)

	if len(s.Kf) != voiceRampLen {
		e.addf("%s.kf: need %d ramp fractions, got %d", path, voiceRampLen, len(s.Kf))
	} else {
		last := -1.0
		for i, x := range s.Kf {
			if x < 0 || x > 1 {
				e.addf("%s.kf[%d] %v: must be in [0, 1]", path, i, x)
			}
			if i > 0 && x <= last {
				e.addf("%s.kf: fractions must be strictly ascending", path)
			}
			last = x
		}
	}

	if len(s.Tracks) != voiceTracks {
		e.addf("%s.tracks: need %d formant tracks, got %d", path, voiceTracks, len(s.Tracks))
	} else {
		for i, track := range s.Tracks {
			if len(track) != voiceRampLen {
				e.addf("%s.tracks[%d]: need %d points, got %d", path, i, voiceRampLen, len(track))
				continue
			}
			for k, x := range track {
				if x <= 0 {
					e.addf("%s.tracks[%d][%d] %v: must be positive", path, i, k, x)
				}
			}
		}
	}

	validateCurve(e, path, "gains", s.Gains, unit)
	validateCurve(e, path, "q", s.Q, positive)
	validateRamp(e, path, "mouth", s.Mouth, positive)
	validateCurve(e, path, "pitch", s.Pitch, positive)
	if len(s.Vib) != voiceCurveLen {
		e.addf("%s.vib: need [rateLo, rateHi, depth], got %d values", path, len(s.Vib))
	} else {
		if s.Vib[0] <= 0 || s.Vib[1] <= 0 {
			e.addf("%s.vib: rates must be positive", path)
		}
		if s.Vib[0] > s.Vib[1] {
			e.addf("%s.vib: [%v, %v] is not an ordered rate range", path, s.Vib[0], s.Vib[1])
		}
		if s.Vib[2] < 0 || s.Vib[2] > 1 {
			e.addf("%s.vib[2] %v: depth must be in [0, 1]", path, s.Vib[2])
		}
	}

	validateScalar(e, path, "noise", s.Noise)
	validateScalar(e, path, "pure", s.Pure)
	validateScalar(e, path, "decay", s.Decay)

	if len(s.Bursts) != voicePairLen {
		e.addf("%s.bursts: need [lo, hi] burst counts, got %d values", path, len(s.Bursts))
	} else {
		if s.Bursts[0] < 1 || s.Bursts[1] < 1 {
			e.addf("%s.bursts: counts must be positive integers", path)
		}
		if s.Bursts[0] > s.Bursts[1] {
			e.addf("%s.bursts: [%d, %d] is not an ordered range", path, s.Bursts[0], s.Bursts[1])
		}
	}

	validatePair(e, path, "gap", s.Gap, positive)
	if len(s.BurstPitch) != 0 {
		validateCurve(e, path, "burstPitch", s.BurstPitch, positive)
	}
}

// validateCurve checks a fixed-length three-value synth curve.
func validateCurve(e *errs, path, field string, v []float64, check func(float64) bool) {
	if len(v) != voiceCurveLen {
		e.addf("%s.%s: need %d values, got %d", path, field, voiceCurveLen, len(v))
		return
	}
	for i, x := range v {
		if !check(x) {
			e.addf("%s.%s[%d] %v: out of range", path, field, i, x)
		}
	}
}

// validateRamp checks a fixed-length four-value synth ramp (mouth is a vowel
// path of four frequencies, not a monotonic curve).
func validateRamp(e *errs, path, field string, v []float64, check func(float64) bool) {
	if len(v) != voiceRampLen {
		e.addf("%s.%s: need %d values, got %d", path, field, voiceRampLen, len(v))
		return
	}
	for i, x := range v {
		if !check(x) {
			e.addf("%s.%s[%d] %v: out of range", path, field, i, x)
		}
	}
}

// validateScalar checks a required 0–1 voice scalar; a missing field is an
// error, never a silent zero.
func validateScalar(e *errs, path, field string, v *float64) {
	if v == nil {
		e.addf("%s.%s: required", path, field)
	} else if *v < 0 || *v > 1 {
		e.addf("%s.%s %v: must be in [0, 1]", path, field, *v)
	}
}

// ── Sound ────────────────────────────────────────────────────────────────

func validateSoundSpec(s *SoundSpec, path string, e *errs) {
	switch s.Type {
	case SoundNoise, SoundTone, SoundSweep, SoundBurst:
	default:
		e.addf("%s.type %q: must be noise, tone, sweep or burst", path, s.Type)
	}
	validatePair(e, path, "freq", s.Freq, positive)
	validatePair(e, path, "dur", s.Dur, positive)
	validatePair(e, path, "amp", s.Amp, unit)
	if s.Env.Attack == nil || *s.Env.Attack < 0 {
		e.addf("%s.env.attack: must be non-negative", path)
	}
	if s.Env.Decay == nil || *s.Env.Decay < 0 {
		e.addf("%s.env.decay: must be non-negative", path)
	}
}

// ── Play ─────────────────────────────────────────────────────────────────

func validatePlaySpec(s *PlaySpec, path string, e *errs) {
	if len(s.Instances) == 0 {
		e.addf("%s.instances: a play needs at least one instance", path)
	}
	ids := make(map[string]bool, len(s.Instances))
	for i, inst := range s.Instances {
		ip := fmt.Sprintf("%s.instances[%d]", path, i)
		if !idRe.MatchString(inst.ID) {
			e.addf("%s.id %q: must match %s", ip, inst.ID, idRe)
		}
		if ids[inst.ID] {
			e.addf("%s.id %q: duplicate instance id", ip, inst.ID)
		}
		ids[inst.ID] = true
		validateRef(inst.Model, ip+".model", e)
		if !idRe.MatchString(inst.Role) {
			e.addf("%s.role %q: must match %s", ip, inst.Role, idRe)
		}
		if !intraIDRe.MatchString(inst.ID) {
			e.addf("%s.id %q: must match %s", ip, inst.ID, intraIDRe)
		}
		if inst.Scale <= 0 {
			e.addf("%s.scale %v: must be positive", ip, inst.Scale)
		}
		if inst.Voice != nil {
			validateRef(*inst.Voice, ip+".voice", e)
		}
		if inst.Sound != nil {
			validateRef(*inst.Sound, ip+".sound", e)
		}
	}

	for i, entry := range s.Timeline {
		tp := fmt.Sprintf("%s.timeline[%d]", path, i)
		if entry.At < 0 {
			e.addf("%s.at %d: must be non-negative", tp, entry.At)
		}
		if !ids[entry.On] {
			e.addf("%s.on %q: no such instance", tp, entry.On)
		}
		verbs := 0
		if entry.Clip != nil {
			verbs++
			validateRef(*entry.Clip, tp+".clip", e)
		}
		if entry.Gag != nil {
			verbs++
			validateRef(*entry.Gag, tp+".gag", e)
		}
		if entry.Tween != nil {
			verbs++
			validateTween(*entry.Tween, ids, tp+".tween", e)
		}
		if verbs != 1 {
			e.addf("%s: exactly one of clip/gag/tween, got %d verbs", tp, verbs)
		}
	}
}

func validateTween(t Tween, instanceIDs map[string]bool, path string, e *errs) {
	if t.Over <= 0 {
		e.addf("%s.over %d: must be positive", path, t.Over)
	}
	if !easingOK(t.Easing) {
		e.addf("%s.easing %q: must be linear, ease-in, ease-out or ease-in-out", path, t.Easing)
	}

	target := t.To
	abs := target.X != nil || target.Y != nil || target.Rot != nil || target.Scale != nil
	beside := target.Beside != ""
	off := target.Off != nil
	if target.Side != nil && !beside {
		e.addf("%s.to.side: side belongs to a beside target", path)
	}
	if count := bools(abs, beside, off); count != 1 {
		e.addf("%s.to: exactly one of absolute x/y/rot/scale, beside or off, got %d", path, count)
		return
	}

	if beside {
		if target.Side == nil || (*target.Side != SideLeft && *target.Side != SideRight && *target.Side != SideFront && *target.Side != SideBack) {
			e.addf("%s.to.side %v: must be left, right, front or back", path, target.Side)
		}
		if !instanceIDs[target.Beside] {
			e.addf("%s.to.beside %q: no such instance", path, target.Beside)
		}
	} else if target.Side != nil {
		e.addf("%s.to.side: side belongs to a beside target", path)
	}
	if off && *target.Off != SideLeft && *target.Off != SideRight {
		e.addf("%s.to.off %q: must be left or right", path, *target.Off)
	}
}

// bools counts how many of its arguments are true.
func bools(vs ...bool) int {
	n := 0
	for _, v := range vs {
		if v {
			n++
		}
	}
	return n
}
