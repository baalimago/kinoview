package troupe

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Compact valid specs, one per kind, used as the positive controls and as the
// base for the negative tables. The full conformance example lives under
// testdata/ and is exercised by TestParse_ConformanceExample.
const (
	modelSpecBase = `{"bones":[{"id":"root","parent":null,"x":0,"y":0,"rot":0,"length":0}],"attachments":[{"id":"blade","slot":"main","bone":"root","shape":{"type":"ellipse","w":1,"h":3,"color":"#3a7d44"}}],"skins":{"default":{"main":"blade"}}}`
	clipSpecBase  = `{"duration":600,"loop":false,"constraints":[{"type":"reach","effector":"frontLeg","target":{"x":1.5,"y":0},"hint":"front"}]}`
	voiceSpecBase = `{"dur":[0.42,0.60],"f0":[620,770],"amp":[0.50,0.72],"kf":[0.00,0.20,0.55,1.00],"tracks":[[500,1050,900,470],[1150,2150,1600,1000],[3000,3400,3150,2900]],"gains":[1.0,0.45,0.14],"q":[7,10,11],"mouth":[550,4600,2900,780],"pitch":[0.94,1.16,0.82],"vib":[7,10,0.02],"noise":0.02,"pure":0.42,"bursts":[1,2],"gap":[0.06,0.20],"decay":0.80}`
	soundSpecBase = `{"type":"noise","freq":[800,2400],"dur":[0.3,0.9],"amp":[0.2,0.5],"env":{"attack":0.05,"decay":0.4}}`
	gagSpecBase   = `{"clips":["blink@1","pounce@1"]}`
	playSpecBase  = `{"instances":[{"id":"cat","model":"cat@1","role":"actor","scale":1,"x":0.1}],"timeline":[{"at":0,"on":"cat","clip":"walk@1"}]}`
)

// envelope wraps a spec in a valid envelope. version 0 omits the field, which
// is the play form.
func envelope(kind Kind, id string, version int, spec string) string {
	ver := ""
	if version > 0 {
		ver = `,"version":` + strconv.Itoa(version)
	}
	return `{"kind":"` + string(kind) + `","id":"` + id + `"` + ver + `,"status":"draft","author":"fixture","provenance":"fixture","spec":` + spec + `}`
}

// TestParse_ValidBases pins the positive control: each compact base spec
// parses clean, so a failing negative case is caused by the mutation, not a
// broken base.
func TestParse_ValidBases(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		doc      string
	}{
		{"model", "models/leaf@1.json", envelope(KindModel, "leaf", 1, modelSpecBase)},
		{"clip", "clips/pounce@1.json", envelope(KindClip, "pounce", 1, clipSpecBase)},
		{"voice", "voices/cat@1.json", envelope(KindVoice, "cat", 1, voiceSpecBase)},
		{"sound", "sounds/rustle@1.json", envelope(KindSound, "rustle", 1, soundSpecBase)},
		{"gag", "gags/doubletake@1.json", envelope(KindGag, "doubletake", 1, gagSpecBase)},
		{"play", "plays/story_20260820T161500Z.json", envelope(KindPlay, "story_20260820T161500Z", 0, playSpecBase)},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if _, spec, err := Parse(tt.filename, []byte(tt.doc)); err != nil {
				t.Fatalf("Parse: %v", err)
			} else if spec == nil {
				t.Fatal("Parse returned a nil spec")
			}
		})
	}
}

// TestParse_ConformanceExample walks the fixture worktree and pins the
// filename↔identity rule for every file: the directory names the kind, the
// stem carries id@version, and the envelope matches.
func TestParse_ConformanceExample(t *testing.T) {
	want := map[string]struct {
		kind    Kind
		id      string
		version int
	}{
		"models/leaf@1.json":                {KindModel, "leaf", 1},
		"models/branch@1.json":              {KindModel, "branch", 1},
		"models/tree@1.json":                {KindModel, "tree", 1},
		"models/forest@1.json":              {KindModel, "forest", 1},
		"models/cat@1.json":                 {KindModel, "cat", 1},
		"models/dog@1.json":                 {KindModel, "dog", 1},
		"clips/walk@1.json":                 {KindClip, "walk", 1},
		"clips/pounce@1.json":               {KindClip, "pounce", 1},
		"clips/blink@1.json":                {KindClip, "blink", 1},
		"voices/cat@1.json":                 {KindVoice, "cat", 1},
		"voices/dog@1.json":                 {KindVoice, "dog", 1},
		"sounds/rustle@1.json":              {KindSound, "rustle", 1},
		"gags/doubletake@1.json":            {KindGag, "doubletake", 1},
		"plays/story_20260820T161500Z.json": {KindPlay, "story_20260820T161500Z", 0},
	}

	found := map[string]bool{}
	err := filepath.WalkDir("testdata", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		rel, err := filepath.Rel("testdata", path)
		if err != nil {
			return err
		}
		found[rel] = true
		want, ok := want[rel]
		if !ok {
			t.Errorf("%s: unexpected fixture file", rel)
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		env, spec, err := Parse(rel, data)
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			return nil
		}
		if env.Kind != want.kind || env.ID != want.id || env.Version != want.version {
			t.Errorf("%s: identity = %s/%s@%d, want %s/%s@%d", rel, env.Kind, env.ID, env.Version, want.kind, want.id, want.version)
		}
		if spec.kind() != want.kind {
			t.Errorf("%s: spec kind %s, want %s", rel, spec.kind(), want.kind)
		}
		// The spec is preserved byte-for-byte for the served artifact.
		var raw struct {
			Spec json.RawMessage `json:"spec"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		if !bytes.Equal(env.Spec, raw.Spec) {
			t.Errorf("%s: spec bytes were not preserved", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	for rel := range want {
		if !found[rel] {
			t.Errorf("missing fixture %s", rel)
		}
	}
}

// TestParse_RejectsEnvelope covers the filename↔identity rule and the
// uniform-envelope rules.
func TestParse_RejectsEnvelope(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		doc      string
		want     string
	}{
		{
			"id mismatch", "models/leaf@1.json",
			envelope(KindModel, "dog", 1, modelSpecBase), "does not match the filename id",
		},
		{
			"version mismatch", "models/leaf@2.json",
			envelope(KindModel, "leaf", 1, modelSpecBase), "does not match the filename version",
		},
		{
			"kind directory mismatch", "clips/leaf@1.json",
			envelope(KindModel, "leaf", 1, modelSpecBase), "does not match the clip filename directory",
		},
		{
			"bad status", "models/leaf@1.json",
			`{"kind":"model","id":"leaf","version":1,"status":"live","author":"fixture","provenance":"fixture","spec":` + modelSpecBase + `}`, "status",
		},
		{
			"empty author", "models/leaf@1.json",
			`{"kind":"model","id":"leaf","version":1,"status":"draft","author":"","provenance":"fixture","spec":` + modelSpecBase + `}`, "author",
		},
		{
			"empty provenance", "models/leaf@1.json",
			`{"kind":"model","id":"leaf","version":1,"status":"draft","author":"fixture","provenance":"","spec":` + modelSpecBase + `}`, "provenance",
		},
		{
			"unknown envelope field", "models/leaf@1.json",
			`{"kind":"model","id":"leaf","version":1,"status":"draft","author":"fixture","provenance":"fixture","extra":1,"spec":` + modelSpecBase + `}`, "extra",
		},
		{
			"play carries a version", "plays/story_20260820T161500Z.json",
			envelope(KindPlay, "story_20260820T161500Z", 1, playSpecBase), "plays are unversioned",
		},
		{
			"unknown kind", "models/leaf@1.json",
			`{"kind":"prop","id":"leaf","version":1,"status":"draft","author":"fixture","provenance":"fixture","spec":` + modelSpecBase + `}`, "kind",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			assertRejects(t, tt.filename, tt.doc, tt.want)
		})
	}
}

// TestParse_RejectsFilename covers the filename side of the identity rule.
func TestParse_RejectsFilename(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		doc      string
		want     string
	}{
		{"no version in stem", "models/leaf.json", envelope(KindModel, "leaf", 1, modelSpecBase), "must carry id@version"},
		{"zero version", "models/leaf@0.json", envelope(KindModel, "leaf", 1, modelSpecBase), "positive integer"},
		{"uppercase id", "models/Leaf@1.json", envelope(KindModel, "Leaf", 1, modelSpecBase), "must match"},
		{"slash in id", "models/le/af@1.json", envelope(KindModel, "le/af", 1, modelSpecBase), "not a notebook kind directory"},
		{"non-json extension", "models/leaf@1.txt", envelope(KindModel, "leaf", 1, modelSpecBase), "must end in .json"},
		{"roles dir is not a grammar dir", "roles/clown.json", `{}`, "not a notebook kind directory"},
		{"play id not a datetime", "plays/story.json", envelope(KindPlay, "story", 0, playSpecBase), "story_YYYYMMDDTHHMMSSZ"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			assertRejects(t, tt.filename, tt.doc, tt.want)
		})
	}
}

// TestParse_RejectsModel covers the model grammar: bones, art, skins and
// structural verbs.
func TestParse_RejectsModel(t *testing.T) {
	cases := []struct {
		name string
		spec string
		want string
	}{
		{"no bones no structure", `{}`, "at least one bone or one structural verb"},
		{"two roots", `{"bones":[{"id":"a","parent":null},{"id":"b","parent":null}]}`, "exactly one root"},
		{"unknown parent", `{"bones":[{"id":"a","parent":null},{"id":"b","parent":"c"}]}`, "no such bone"},
		{"parent cycle", `{"bones":[{"id":"a","parent":"b"},{"id":"b","parent":"a"}]}`, "cycle"},
		{"duplicate bone id", `{"bones":[{"id":"a","parent":null},{"id":"a","parent":null}]}`, "duplicate bone id"},
		{"negative bone length", `{"bones":[{"id":"a","parent":null,"length":-1}]}`, "non-negative"},
		{"attachment on unknown bone", `{"bones":[{"id":"root","parent":null}],"attachments":[{"id":"x","slot":"main","bone":"nope","shape":{"type":"ellipse","w":1,"h":1,"color":"#ffffff"}}],"skins":{"default":{"main":"x"}}}`, "no such bone"},
		{"attachment missing from default skin", `{"bones":[{"id":"root","parent":null}],"attachments":[{"id":"x","slot":"main","bone":"root","shape":{"type":"ellipse","w":1,"h":1,"color":"#ffffff"}}],"skins":{"default":{}}}`, "not mapped in skins.default"},
		{"skin references unknown attachment", `{"bones":[{"id":"root","parent":null}],"skins":{"default":{"main":"ghost"}}}`, "no such attachment"},
		{"skin slot does not match attachment slot", `{"bones":[{"id":"root","parent":null}],"attachments":[{"id":"x","slot":"main","bone":"root","shape":{"type":"ellipse","w":1,"h":1,"color":"#ffffff"}}],"skins":{"default":{"other":"x"}}}`, "whose slot is"},
		{"attachments without default skin", `{"bones":[{"id":"root","parent":null}],"attachments":[{"id":"x","slot":"main","bone":"root","shape":{"type":"ellipse","w":1,"h":1,"color":"#ffffff"}}],"skins":{"winter":{"main":"x"}}}`, "needs a default skin"},
		{"bad shape color", `{"bones":[{"id":"root","parent":null}],"attachments":[{"id":"x","slot":"main","bone":"root","shape":{"type":"ellipse","w":1,"h":1,"color":"green"}}],"skins":{"default":{"main":"x"}}}`, "#rrggbb"},
		{"rect missing width", `{"bones":[{"id":"root","parent":null}],"attachments":[{"id":"x","slot":"main","bone":"root","shape":{"type":"rect","h":1,"color":"#ffffff"}}],"skins":{"default":{"main":"x"}}}`, "w: required"},
		{"path with two points", `{"bones":[{"id":"root","parent":null}],"attachments":[{"id":"x","slot":"main","bone":"root","shape":{"type":"path","points":[[0,0],[1,0]],"color":"#ffffff"}}],"skins":{"default":{"main":"x"}}}`, "at least 3 vertices"},
		{"unknown shape type", `{"bones":[{"id":"root","parent":null}],"attachments":[{"id":"x","slot":"main","bone":"root","shape":{"type":"spline","color":"#ffffff"}}],"skins":{"default":{"main":"x"}}}`, "rect, ellipse or path"},
		{"attach at unknown bone", `{"bones":[{"id":"root","parent":null}],"structure":[{"type":"attach","model":"branch@1","at":"nope","scale":1}]}`, "no such bone"},
		{"attach with depth", `{"bones":[{"id":"root","parent":null}],"structure":[{"type":"attach","model":"branch@1","at":"root","scale":1,"depth":2}]}`, "not a attach field"},
		{"scatter zero count", `{"structure":[{"type":"scatter","model":"tree@1","count":0,"over":{"type":"band","w":2,"h":2},"seed":1}]}`, "count"},
		{"scatter without region", `{"structure":[{"type":"scatter","model":"tree@1","count":2,"seed":1}]}`, "needs a region"},
		{"scatter unknown region type", `{"structure":[{"type":"scatter","model":"tree@1","count":2,"over":{"type":"blob"},"seed":1}]}`, "band, disc, grid, curve or along"},
		{"scatter negative jitter", `{"structure":[{"type":"scatter","model":"tree@1","count":2,"over":{"type":"band","w":2,"h":2},"jitter":{"scale":-1},"seed":1}]}`, "non-negative"},
		{"recurse zero depth", `{"bones":[{"id":"root","parent":null}],"structure":[{"type":"recurse","model":"branch@1","at":"root","depth":0,"branch":2,"angle":30,"decay":0.7,"tip":"leaf@1","seed":1}]}`, "depth"},
		{"recurse decay over one", `{"bones":[{"id":"root","parent":null}],"structure":[{"type":"recurse","model":"branch@1","at":"root","depth":2,"branch":2,"angle":30,"decay":1.5,"tip":"leaf@1","seed":1}]}`, "in (0, 1]"},
		{"recurse without tip", `{"bones":[{"id":"root","parent":null}],"structure":[{"type":"recurse","model":"branch@1","at":"root","depth":2,"branch":2,"angle":30,"decay":0.7,"seed":1}]}`, "tip"},
		{"bad model ref", `{"bones":[{"id":"root","parent":null}],"structure":[{"type":"attach","model":"branch","at":"root","scale":1}]}`, "id@version"},
		{"unknown verb", `{"bones":[{"id":"root","parent":null}],"structure":[{"type":"grow","model":"branch@1","at":"root"}]}`, "attach, scatter or recurse"},
		{"voice ref not id@version", `{"bones":[{"id":"root","parent":null}],"voice":"cat"}`, "id@version"},
		{"unknown spec field", `{"bones":[{"id":"root","parent":null}],"rig":[]}`, "rig"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			assertRejects(t, "models/leaf@1.json", envelope(KindModel, "leaf", 1, tt.spec), tt.want)
		})
	}
}

// TestParse_RejectsClip covers the clip grammar and the composition boundary:
// a clip references bones only, never a clip.
func TestParse_RejectsClip(t *testing.T) {
	cases := []struct {
		name string
		spec string
		want string
	}{
		{"zero duration", `{"duration":0,"loop":false}`, "duration"},
		{"reach missing target", `{"duration":600,"constraints":[{"type":"reach","effector":"frontLeg","hint":"front"}]}`, "needs a {x,y} coordinate"},
		{"reach bad hint", `{"duration":600,"constraints":[{"type":"reach","effector":"frontLeg","target":{"x":1,"y":1},"hint":"up"}]}`, "front, back, left or right"},
		{"reach with chain", `{"duration":600,"constraints":[{"type":"reach","effector":"frontLeg","target":{"x":1,"y":1},"hint":"front","chain":"spine"}]}`, "reach uses only"},
		{"track target is a coordinate", `{"duration":600,"constraints":[{"type":"track","chain":"spine","target":{"x":1,"y":1}}]}`, "track needs a bone id"},
		{"plant missing at", `{"duration":600,"constraints":[{"type":"plant","bone":"frontLeg"}]}`, "plant needs a {x,y} coordinate"},
		{"unknown constraint type", `{"duration":600,"constraints":[{"type":"jump"}]}`, "reach, look, plant or track"},
		{"bad effector", `{"duration":600,"constraints":[{"type":"reach","effector":"front Leg","target":{"x":1,"y":1},"hint":"front"}]}`, "not a bone id or selector"},
		{"keyframe with no target", `{"duration":600,"keyframes":[{"channel":"rotation","easing":"linear","keys":[{"t":0,"v":0},{"t":100,"v":10}]}]}`, "exactly one of bone/slot"},
		{"keyframe slot on bone-only channel", `{"duration":600,"keyframes":[{"slot":"main","channel":"x","easing":"linear","keys":[{"t":0,"v":0},{"t":100,"v":1}]}]}`, "bone-only"},
		{"keyframe one key", `{"duration":600,"keyframes":[{"bone":"spine","channel":"rotation","easing":"linear","keys":[{"t":0,"v":0}]}]}`, "at least 2 keys"},
		{"keyframe descending times", `{"duration":600,"keyframes":[{"bone":"spine","channel":"rotation","easing":"linear","keys":[{"t":100,"v":0},{"t":0,"v":1}]}]}`, "strictly ascending"},
		{"keyframe bad channel", `{"duration":600,"keyframes":[{"bone":"spine","channel":"spin","easing":"linear","keys":[{"t":0,"v":0},{"t":100,"v":1}]}]}`, "must be x, y, rotation"},
		{"keyframe bad easing", `{"duration":600,"keyframes":[{"bone":"spine","channel":"rotation","easing":"bounce","keys":[{"t":0,"v":0},{"t":100,"v":1}]}]}`, "linear, ease-in"},
		{"oscillation zero freq", `{"duration":600,"oscillations":[{"bone":"spine","channel":"rotation","amp":10,"freq":0,"phase":0}]}`, "freq"},
		{"event with both voice and sound", `{"duration":600,"events":[{"at":100,"voice":true,"sound":"rustle@1"}]}`, "got both"},
		{"event voice false", `{"duration":600,"events":[{"at":100,"voice":false}]}`, "must be true"},
		{"event with no audio", `{"duration":600,"events":[{"at":100}]}`, "exactly one of voice/sound"},
		{"event negative at", `{"duration":600,"events":[{"at":-1,"voice":true}]}`, "non-negative"},
		{"event bad sound ref", `{"duration":600,"events":[{"at":100,"sound":"rustle"}]}`, "id@version"},
		{"clip references a clip (composition boundary)", `{"duration":600,"clip":"walk@1"}`, "clip"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			assertRejects(t, "clips/pounce@1.json", envelope(KindClip, "pounce", 1, tt.spec), tt.want)
		})
	}
}

// TestParse_RejectsVoiceSound covers the formant voice and sound ranges.
func TestParse_RejectsVoiceSound(t *testing.T) {
	cases := []struct {
		name string
		kind Kind
		spec string
		want string
	}{
		{"noise over one", KindVoice, `{"dur":[0.42,0.60],"f0":[620,770],"amp":[0.50,0.72],"kf":[0.00,0.20,0.55,1.00],"tracks":[[500,1050,900,470],[1150,2150,1600,1000],[3000,3400,3150,2900]],"gains":[1.0,0.45,0.14],"q":[7,10,11],"mouth":[550,4600,2900,780],"pitch":[0.94,1.16,0.82],"vib":[7,10,0.02],"noise":1.5,"pure":0.42,"bursts":[1,2],"gap":[0.06,0.20],"decay":0.80}`, "noise"},
		{"noise missing", KindVoice, `{"dur":[0.42,0.60],"f0":[620,770],"amp":[0.50,0.72],"kf":[0.00,0.20,0.55,1.00],"tracks":[[500,1050,900,470],[1150,2150,1600,1000],[3000,3400,3150,2900]],"gains":[1.0,0.45,0.14],"q":[7,10,11],"mouth":[550,4600,2900,780],"pitch":[0.94,1.16,0.82],"vib":[7,10,0.02],"pure":0.42,"bursts":[1,2],"gap":[0.06,0.20],"decay":0.80}`, "noise: required"},
		{"negative dur", KindVoice, `{"dur":[-0.42,0.60],"f0":[620,770],"amp":[0.50,0.72],"kf":[0.00,0.20,0.55,1.00],"tracks":[[500,1050,900,470],[1150,2150,1600,1000],[3000,3400,3150,2900]],"gains":[1.0,0.45,0.14],"q":[7,10,11],"mouth":[550,4600,2900,780],"pitch":[0.94,1.16,0.82],"vib":[7,10,0.02],"noise":0.02,"pure":0.42,"bursts":[1,2],"gap":[0.06,0.20],"decay":0.80}`, "dur"},
		{"amp over one", KindVoice, `{"dur":[0.42,0.60],"f0":[620,770],"amp":[0.50,1.2],"kf":[0.00,0.20,0.55,1.00],"tracks":[[500,1050,900,470],[1150,2150,1600,1000],[3000,3400,3150,2900]],"gains":[1.0,0.45,0.14],"q":[7,10,11],"mouth":[550,4600,2900,780],"pitch":[0.94,1.16,0.82],"vib":[7,10,0.02],"noise":0.02,"pure":0.42,"bursts":[1,2],"gap":[0.06,0.20],"decay":0.80}`, "amp"},
		{"kf descending", KindVoice, `{"dur":[0.42,0.60],"f0":[620,770],"amp":[0.50,0.72],"kf":[1.00,0.20,0.55,0.00],"tracks":[[500,1050,900,470],[1150,2150,1600,1000],[3000,3400,3150,2900]],"gains":[1.0,0.45,0.14],"q":[7,10,11],"mouth":[550,4600,2900,780],"pitch":[0.94,1.16,0.82],"vib":[7,10,0.02],"noise":0.02,"pure":0.42,"bursts":[1,2],"gap":[0.06,0.20],"decay":0.80}`, "ascending"},
		{"tracks wrong row count", KindVoice, `{"dur":[0.42,0.60],"f0":[620,770],"amp":[0.50,0.72],"kf":[0.00,0.20,0.55,1.00],"tracks":[[500,1050,900,470]],"gains":[1.0,0.45,0.14],"q":[7,10,11],"mouth":[550,4600,2900,780],"pitch":[0.94,1.16,0.82],"vib":[7,10,0.02],"noise":0.02,"pure":0.42,"bursts":[1,2],"gap":[0.06,0.20],"decay":0.80}`, "3 formant tracks"},
		{"gains wrong length", KindVoice, `{"dur":[0.42,0.60],"f0":[620,770],"amp":[0.50,0.72],"kf":[0.00,0.20,0.55,1.00],"tracks":[[500,1050,900,470],[1150,2150,1600,1000],[3000,3400,3150,2900]],"gains":[1.0],"q":[7,10,11],"mouth":[550,4600,2900,780],"pitch":[0.94,1.16,0.82],"vib":[7,10,0.02],"noise":0.02,"pure":0.42,"bursts":[1,2],"gap":[0.06,0.20],"decay":0.80}`, "gains"},
		{"negative q", KindVoice, `{"dur":[0.42,0.60],"f0":[620,770],"amp":[0.50,0.72],"kf":[0.00,0.20,0.55,1.00],"tracks":[[500,1050,900,470],[1150,2150,1600,1000],[3000,3400,3150,2900]],"gains":[1.0,0.45,0.14],"q":[7,-10,11],"mouth":[550,4600,2900,780],"pitch":[0.94,1.16,0.82],"vib":[7,10,0.02],"noise":0.02,"pure":0.42,"bursts":[1,2],"gap":[0.06,0.20],"decay":0.80}`, "q"},
		{"bursts single value", KindVoice, `{"dur":[0.42,0.60],"f0":[620,770],"amp":[0.50,0.72],"kf":[0.00,0.20,0.55,1.00],"tracks":[[500,1050,900,470],[1150,2150,1600,1000],[3000,3400,3150,2900]],"gains":[1.0,0.45,0.14],"q":[7,10,11],"mouth":[550,4600,2900,780],"pitch":[0.94,1.16,0.82],"vib":[7,10,0.02],"noise":0.02,"pure":0.42,"bursts":[1],"gap":[0.06,0.20],"decay":0.80}`, "bursts"},
		{"bursts zero", KindVoice, `{"dur":[0.42,0.60],"f0":[620,770],"amp":[0.50,0.72],"kf":[0.00,0.20,0.55,1.00],"tracks":[[500,1050,900,470],[1150,2150,1600,1000],[3000,3400,3150,2900]],"gains":[1.0,0.45,0.14],"q":[7,10,11],"mouth":[550,4600,2900,780],"pitch":[0.94,1.16,0.82],"vib":[7,10,0.02],"noise":0.02,"pure":0.42,"bursts":[0,2],"gap":[0.06,0.20],"decay":0.80}`, "positive integers"},
		{"gap unordered", KindVoice, `{"dur":[0.42,0.60],"f0":[620,770],"amp":[0.50,0.72],"kf":[0.00,0.20,0.55,1.00],"tracks":[[500,1050,900,470],[1150,2150,1600,1000],[3000,3400,3150,2900]],"gains":[1.0,0.45,0.14],"q":[7,10,11],"mouth":[550,4600,2900,780],"pitch":[0.94,1.16,0.82],"vib":[7,10,0.02],"noise":0.02,"pure":0.42,"bursts":[1,2],"gap":[0.20,0.06],"decay":0.80}`, "not an ordered range"},
		{"vib depth over one", KindVoice, `{"dur":[0.42,0.60],"f0":[620,770],"amp":[0.50,0.72],"kf":[0.00,0.20,0.55,1.00],"tracks":[[500,1050,900,470],[1150,2150,1600,1000],[3000,3400,3150,2900]],"gains":[1.0,0.45,0.14],"q":[7,10,11],"mouth":[550,4600,2900,780],"pitch":[0.94,1.16,0.82],"vib":[7,10,2.0],"noise":0.02,"pure":0.42,"bursts":[1,2],"gap":[0.06,0.20],"decay":0.80}`, "depth"},
		{"burstPitch wrong length", KindVoice, `{"dur":[0.42,0.60],"f0":[620,770],"amp":[0.50,0.72],"kf":[0.00,0.20,0.55,1.00],"tracks":[[500,1050,900,470],[1150,2150,1600,1000],[3000,3400,3150,2900]],"gains":[1.0,0.45,0.14],"q":[7,10,11],"mouth":[550,4600,2900,780],"pitch":[0.94,1.16,0.82],"vib":[7,10,0.02],"noise":0.02,"pure":0.42,"bursts":[1,2],"gap":[0.06,0.20],"decay":0.80,"burstPitch":[1]}`, "burstPitch"},
		{"sound bad type", KindSound, `{"type":"bark","freq":[800,2400],"dur":[0.3,0.9],"amp":[0.2,0.5],"env":{"attack":0.05,"decay":0.4}}`, "noise, tone, sweep or burst"},
		{"sound amp over one", KindSound, `{"type":"noise","freq":[800,2400],"dur":[0.3,0.9],"amp":[0.2,1.5],"env":{"attack":0.05,"decay":0.4}}`, "amp"},
		{"sound env decay missing", KindSound, `{"type":"noise","freq":[800,2400],"dur":[0.3,0.9],"amp":[0.2,0.5],"env":{"attack":0.05}}`, "decay"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			filename := "voices/cat@1.json"
			id := "cat"
			if tt.kind == KindSound {
				filename, id = "sounds/rustle@1.json", "rustle"
			}
			assertRejects(t, filename, envelope(tt.kind, id, 1, tt.spec), tt.want)
		})
	}
}

// TestParse_RejectsGagPlay covers the composition layer: gag references clips
// only; play references models/clips/gags and validates instances/timeline.
func TestParse_RejectsGagPlay(t *testing.T) {
	cases := []struct {
		name string
		spec string
		want string
	}{
		{"gag empty", `{"clips":[]}`, "at least one clip"},
		{"gag bad ref", `{"clips":["blink"]}`, "id@version"},
		{"gag references a gag (composition boundary)", `{"gag":"doubletake@1"}`, "gag"},
		{"play no instances", `{"instances":[],"timeline":[]}`, "at least one instance"},
		{"play duplicate instance ids", `{"instances":[{"id":"cat","model":"cat@1","role":"actor","scale":1},{"id":"cat","model":"dog@1","role":"actor","scale":1}],"timeline":[]}`, "duplicate instance id"},
		{"play bad role", `{"instances":[{"id":"cat","model":"cat@1","role":"A ct or","scale":1}],"timeline":[]}`, "role"},
		{"play zero scale", `{"instances":[{"id":"cat","model":"cat@1","role":"actor","scale":0}],"timeline":[]}`, "scale"},
		{"play bad model ref", `{"instances":[{"id":"cat","model":"cat","role":"actor","scale":1}],"timeline":[]}`, "id@version"},
		{"timeline on unknown instance", `{"instances":[{"id":"cat","model":"cat@1","role":"actor","scale":1}],"timeline":[{"at":0,"on":"dog","clip":"walk@1"}]}`, "no such instance"},
		{"timeline no verb", `{"instances":[{"id":"cat","model":"cat@1","role":"actor","scale":1}],"timeline":[{"at":0,"on":"cat"}]}`, "exactly one of clip/gag/tween"},
		{"timeline two verbs", `{"instances":[{"id":"cat","model":"cat@1","role":"actor","scale":1}],"timeline":[{"at":0,"on":"cat","clip":"walk@1","gag":"doubletake@1"}]}`, "exactly one of clip/gag/tween"},
		{"timeline negative at", `{"instances":[{"id":"cat","model":"cat@1","role":"actor","scale":1}],"timeline":[{"at":-1,"on":"cat","clip":"walk@1"}]}`, "non-negative"},
		{"tween zero over", `{"instances":[{"id":"cat","model":"cat@1","role":"actor","scale":1}],"timeline":[{"at":0,"on":"cat","tween":{"to":{"x":0.5},"over":0,"easing":"linear"}}]}`, "over"},
		{"tween empty target", `{"instances":[{"id":"cat","model":"cat@1","role":"actor","scale":1}],"timeline":[{"at":0,"on":"cat","tween":{"to":{},"over":100,"easing":"linear"}}]}`, "exactly one of absolute"},
		{"tween absolute and beside", `{"instances":[{"id":"cat","model":"cat@1","role":"actor","scale":1},{"id":"dog","model":"dog@1","role":"actor","scale":1}],"timeline":[{"at":0,"on":"cat","tween":{"to":{"x":0.5,"beside":"dog","side":"left"},"over":100,"easing":"linear"}}]}`, "exactly one of absolute"},
		{"tween beside unknown instance", `{"instances":[{"id":"cat","model":"cat@1","role":"actor","scale":1}],"timeline":[{"at":0,"on":"cat","tween":{"to":{"beside":"dog","side":"left"},"over":100,"easing":"linear"}}]}`, "no such instance"},
		{"tween beside without side", `{"instances":[{"id":"cat","model":"cat@1","role":"actor","scale":1},{"id":"dog","model":"dog@1","role":"actor","scale":1}],"timeline":[{"at":0,"on":"cat","tween":{"to":{"beside":"dog"},"over":100,"easing":"linear"}}]}`, "side"},
		{"tween side without beside", `{"instances":[{"id":"cat","model":"cat@1","role":"actor","scale":1}],"timeline":[{"at":0,"on":"cat","tween":{"to":{"side":"left"},"over":100,"easing":"linear"}}]}`, "side belongs to a beside"},
		{"tween off up", `{"instances":[{"id":"cat","model":"cat@1","role":"actor","scale":1}],"timeline":[{"at":0,"on":"cat","tween":{"to":{"off":"up"},"over":100,"easing":"linear"}}]}`, "left or right"},
		{"tween bad easing", `{"instances":[{"id":"cat","model":"cat@1","role":"actor","scale":1}],"timeline":[{"at":0,"on":"cat","tween":{"to":{"x":0.5},"over":100,"easing":"bounce"}}]}`, "linear, ease-in"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if strings.Contains(tt.name, "gag") {
				assertRejects(t, "gags/doubletake@1.json", envelope(KindGag, "doubletake", 1, tt.spec), tt.want)
				return
			}
			assertRejects(t, "plays/story_20260820T161500Z.json", envelope(KindPlay, "story_20260820T161500Z", 0, tt.spec), tt.want)
		})
	}
}

// TestParse_RejectsSelector pins the selector grammar.
func TestParse_RejectsSelector(t *testing.T) {
	base := func(effector string) string {
		return `{"duration":600,"constraints":[{"type":"reach","effector":` + strconv.Quote(effector) + `,"target":{"x":1,"y":1},"hint":"front"}]}`
	}
	rejects := []struct {
		name     string
		effector string
		want     string
	}{
		{"model selector without version", "model:leaf", "not a bone id or selector"},
		{"model selector bad id", "model:Leaf@1", "not a bone id or selector"},
		{"wildcard with trailing slash", "tree#*/", "not a bone id or selector"},
		{"wildcard plain segment in the middle", "tree#*/trunk/tip", "not a bone id or selector"},
		{"wildcard negative index", "tree#-1/tip", "not a bone id or selector"},
	}
	for _, tt := range rejects {
		t.Run(tt.name, func(t *testing.T) {
			assertRejects(t, "clips/pounce@1.json", envelope(KindClip, "pounce", 1, base(tt.effector)), tt.want)
		})
	}

	accepts := []struct {
		name     string
		effector string
	}{
		{"wildcard path", "tree#*/branch#*/tip"},
		{"instance-index path", "tree#3/branch#1/tip"},
		{"all instances of a model", "model:leaf@1"},
	}
	for _, tt := range accepts {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := Parse("clips/pounce@1.json", []byte(envelope(KindClip, "pounce", 1, base(tt.effector)))); err != nil {
				t.Fatalf("Parse: %v", err)
			}
		})
	}
}

// TestResolvedPlay_RoundTrip pins the resolved-play schema: the play plus the
// flattened asset table round-trips through JSON.
func TestResolvedPlay_RoundTrip(t *testing.T) {
	playEnv, _, err := Parse("plays/story_20260820T161500Z.json", mustRead(t, "testdata/plays/story_20260820T161500Z.json"))
	if err != nil {
		t.Fatalf("parse play: %v", err)
	}

	assets := ResolvedAssets{
		Models: map[string]Envelope{},
		Voices: map[string]Envelope{},
		Sounds: map[string]Envelope{},
		Clips:  map[string]Envelope{},
		Gags:   map[string]Envelope{},
	}
	files := map[string]map[string]Envelope{
		"models": assets.Models,
		"voices": assets.Voices,
		"sounds": assets.Sounds,
		"clips":  assets.Clips,
		"gags":   assets.Gags,
	}
	err = filepath.WalkDir("testdata", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".json") || strings.Contains(path, "/plays/") {
			return nil
		}
		rel, err := filepath.Rel("testdata", path)
		if err != nil {
			return err
		}
		env, _, err := Parse(rel, mustRead(t, path))
		if err != nil {
			return err
		}
		dir := filepath.Dir(rel)
		files[dir][env.ID+"@"+strconv.Itoa(env.Version)] = env
		return nil
	})
	if err != nil {
		t.Fatalf("collect assets: %v", err)
	}
	for dir, m := range files {
		if len(m) == 0 {
			t.Errorf("no assets collected for %s", dir)
		}
	}

	rp := ResolvedPlay{Play: playEnv, Assets: assets}
	b, err := json.Marshal(rp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ResolvedPlay
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Marshal compacts raw specs, so the byte-exact round trip is checked on
	// the re-marshaled document (map keys marshal sorted, the output is
	// deterministic).
	back, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if !bytes.Equal(back, b) {
		t.Errorf("round trip differs:\ngot  %s\nwant %s", back, b)
	}
	if len(got.Assets.Models) != 6 || len(got.Assets.Voices) != 2 || len(got.Assets.Sounds) != 1 || len(got.Assets.Clips) != 3 || len(got.Assets.Gags) != 1 {
		t.Errorf("asset table sizes = %d/%d/%d/%d/%d, want 6/2/1/3/1",
			len(got.Assets.Models), len(got.Assets.Voices), len(got.Assets.Sounds), len(got.Assets.Clips), len(got.Assets.Gags))
	}
	if _, ok := got.Assets.Models["cat@1"]; !ok {
		t.Error(`assets.models is missing "cat@1"`)
	}
	if _, ok := got.Assets.Voices["cat@1"]; !ok {
		t.Error(`assets.voices is missing "cat@1"`)
	}
}

// TestSTAGEMirrorsGrammar keeps the human-readable note honest: it must exist
// and carry the closed vocabulary the validator enforces, so an edit that
// drifts the note away from the grammar fails loudly.
func TestSTAGEMirrorsGrammar(t *testing.T) {
	b, err := os.ReadFile("STAGE.md")
	if err != nil {
		t.Fatalf("STAGE.md: %v", err)
	}
	doc := string(b)
	for _, kind := range []string{"model", "clip", "voice", "sound", "gag", "play"} {
		if !strings.Contains(doc, kind) {
			t.Errorf("STAGE.md does not mention the %s kind", kind)
		}
	}
	for _, s := range []string{"reach", "look", "plant", "track", "linear", "ease-in-out", "band", "disc", "grid", "curve", "along", "noise", "tone", "sweep", "burst", "story_YYYYMMDDTHHMMSSZ"} {
		if !strings.Contains(doc, s) {
			t.Errorf("STAGE.md does not mention %q", s)
		}
	}
}

// assertRejects parses doc under filename and requires a rejection whose
// message contains want.
func assertRejects(t *testing.T, filename, doc, want string) {
	t.Helper()
	_, _, err := Parse(filename, []byte(doc))
	if err == nil {
		t.Fatalf("Parse(%s) succeeded, want rejection containing %q", filename, want)
	}
	if want != "" && !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not mention %q", err, want)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
