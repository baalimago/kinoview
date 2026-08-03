package tools

import (
	"github.com/baalimago/clai/pkg/text/models"
)

// The director's tools (phase 4): the superagent's instrument set. Each tool
// is a thin adapter over a callback supplied by the theatre package — the
// same contract as the mini-agent tools: malformed input and callback
// failures are message strings with nil error, so the director's loop
// continues and the model can read the refusal and adapt (decision D11).
//
// The director's post_to_board and consult reuse the shared tools with the
// author and questioner pinned to "director"; the seven tools below are the
// director's own.

// optString reads an optional string input: an absent key is the zero value
// (the director's notes and read_story's part are optional), while a present
// non-string is malformed input the tool must report.
func optString(input models.Input, key string) (string, bool) {
	raw, present := input[key]
	if !present {
		return "", true
	}
	v, ok := raw.(string)
	return v, ok
}

// dramaturgBriefTool runs the dramaturg mini-agent and returns its brief
// report.
type dramaturgBriefTool struct {
	run func(notes string) (string, error)
}

// NewDramaturgBrief builds the dramaturg_brief tool.
func NewDramaturgBrief(run func(notes string) (string, error)) models.LLMTool {
	return &dramaturgBriefTool{run: run}
}

func (t *dramaturgBriefTool) Call(input models.Input) (string, error) {
	notes, ok := optString(input, "notes")
	if !ok {
		return "dramaturg_brief: 'notes' must be a string", nil
	}
	out, err := t.run(notes)
	if err != nil {
		return "dramaturg_brief: " + err.Error(), nil
	}
	return out, nil
}

func (t *dramaturgBriefTool) Specification() models.Specification {
	return models.Specification{
		Name:        "dramaturg_brief",
		Description: "Run the dramaturg: the production brief (mood, shape, cast lineup, what to avoid) is decided and posted to the board. Returns the brief report.",
		Inputs: &models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterObject{
				"notes": {Type: "string", Description: "Your direction for the dramaturg, or empty"},
			},
		},
	}
}

// draftStoryTool runs the playwright mini-agent and returns its compact
// report; the full draft lands in the working file.
type draftStoryTool struct {
	run func(notes string) (string, error)
}

// NewDraftStory builds the draft_story tool.
func NewDraftStory(run func(notes string) (string, error)) models.LLMTool {
	return &draftStoryTool{run: run}
}

func (t *draftStoryTool) Call(input models.Input) (string, error) {
	notes, ok := optString(input, "notes")
	if !ok {
		return "draft_story: 'notes' must be a string", nil
	}
	out, err := t.run(notes)
	if err != nil {
		return "draft_story: " + err.Error(), nil
	}
	return out, nil
}

func (t *draftStoryTool) Specification() models.Specification {
	return models.Specification{
		Name:        "draft_story",
		Description: "Run the playwright: the full draft (title, cast, props, beats, canon facts) is written into the working file. Returns the compact report.",
		Inputs: &models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterObject{
				"notes": {Type: "string", Description: "Your direction for the playwright: the brief summary and any notes, or empty"},
			},
		},
	}
}

// dressSetTool runs the scenographer mini-agent and returns its dressing
// report; the set lands in the working file.
type dressSetTool struct {
	run func(notes string) (string, error)
}

// NewDressSet builds the dress_set tool.
func NewDressSet(run func(notes string) (string, error)) models.LLMTool {
	return &dressSetTool{run: run}
}

func (t *dressSetTool) Call(input models.Input) (string, error) {
	notes, ok := optString(input, "notes")
	if !ok {
		return "dress_set: 'notes' must be a string", nil
	}
	out, err := t.run(notes)
	if err != nil {
		return "dress_set: " + err.Error(), nil
	}
	return out, nil
}

func (t *dressSetTool) Specification() models.Specification {
	return models.Specification{
		Name:        "dress_set",
		Description: "Run the scenographer: the draft's set is dressed (backdrop chosen) in the working file. Returns the dressing report.",
		Inputs: &models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterObject{
				"notes": {Type: "string", Description: "Your direction for the scenographer: the desired mood and backdrop, or empty"},
			},
		},
	}
}

// readStoryTool reads the working draft, or one part of it.
type readStoryTool struct {
	read func(part string) (string, error)
}

// NewReadStory builds the read_story tool.
func NewReadStory(read func(part string) (string, error)) models.LLMTool {
	return &readStoryTool{read: read}
}

func (t *readStoryTool) Call(input models.Input) (string, error) {
	part, ok := optString(input, "part")
	if !ok {
		return "read_story: 'part' must be a string", nil
	}
	out, err := t.read(part)
	if err != nil {
		return "read_story: " + err.Error(), nil
	}
	return out, nil
}

func (t *readStoryTool) Specification() models.Specification {
	parts := []string{"story", "cast", "beats", "scene", "title"}
	return models.Specification{
		Name:        "read_story",
		Description: "Read the working draft, or one part of it (cast, beats, scene, title). Use it only when you need to scrutinise the script — the roles' reports are the cheap source.",
		Inputs: &models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterObject{
				"part": {
					Type:        "string",
					Description: "Which part to read: story (default), cast, beats, scene or title",
					Enum:        &parts,
				},
			},
		},
	}
}

// validateStoryTool runs the playability gate on the working draft.
type validateStoryTool struct {
	validate func() (string, error)
}

// NewValidateStory builds the validate_story tool.
func NewValidateStory(validate func() (string, error)) models.LLMTool {
	return &validateStoryTool{validate: validate}
}

func (t *validateStoryTool) Call(models.Input) (string, error) {
	out, err := t.validate()
	if err != nil {
		return "validate_story: " + err.Error(), nil
	}
	return out, nil
}

func (t *validateStoryTool) Specification() models.Specification {
	return models.Specification{
		Name:        "validate_story",
		Description: "Run the playability gate on the working draft: returns the normalized story, or the exact errors when it cannot be staged. An invalid draft must never reach submit_story.",
		Inputs:      &models.InputSchema{Type: "object"},
	}
}

// pinIdentityTool pins the canonical looks so characters never drift.
type pinIdentityTool struct {
	pin func() (string, error)
}

// NewPinIdentity builds the pin_identity tool.
func NewPinIdentity(pin func() (string, error)) models.LLMTool {
	return &pinIdentityTool{pin: pin}
}

func (t *pinIdentityTool) Call(models.Input) (string, error) {
	out, err := t.pin()
	if err != nil {
		return "pin_identity: " + err.Error(), nil
	}
	return out, nil
}

func (t *pinIdentityTool) Specification() models.Specification {
	return models.Specification{
		Name:        "pin_identity",
		Description: "Pin the canonical coat and character per cast id in the registry, so a character's look never drifts between generations. Deterministic — run it after the draft is validated.",
		Inputs:      &models.InputSchema{Type: "object"},
	}
}

// submitStoryTool is the final gate: validate, persist the story, fold the
// generation into the company library and end the run. The optional inputs
// carry the director's final word: critique lessons for the director doc and
// newly approved characters for the registry (phase 6).
type submitStoryTool struct {
	submit func(notes, characters string) (string, error)
}

// NewSubmitStory builds the submit_story tool.
func NewSubmitStory(submit func(notes, characters string) (string, error)) models.LLMTool {
	return &submitStoryTool{submit: submit}
}

func (t *submitStoryTool) Call(input models.Input) (string, error) {
	notes, ok := optString(input, "notes")
	if !ok {
		return "submit_story: 'notes' must be a string", nil
	}
	characters, ok := optString(input, "characters")
	if !ok {
		return "submit_story: 'characters' must be a string", nil
	}
	out, err := t.submit(notes, characters)
	if err != nil {
		return "submit_story: " + err.Error(), nil
	}
	return out, nil
}

func (t *submitStoryTool) Specification() models.Specification {
	return models.Specification{
		Name:        "submit_story",
		Description: "Submit the production: the working draft is validated once more, persisted as the intro story, the generation's work is distilled into the company library and the generation ends. Call it when the piece is good — a second call is refused.",
		Inputs: &models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterObject{
				"notes": {
					Type:        "string",
					Description: "Optional critique lessons for your memory, one per line (\"two stares in a row is dead air\")",
				},
				"characters": {
					Type:        "string",
					Description: "Optional JSON array of newly approved characters to canonize in the registry, e.g. [{\"id\":\"mouse2\",\"species\":\"mouse\",\"coat\":\"white\"}] — only characters in the draft may be canonized",
				},
			},
		},
	}
}
