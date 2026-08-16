// Package tools holds the theatre's mini-agent tools. Each tool is a thin
// adapter over a callback supplied by the theatre package: the tools
// themselves know nothing about the broker or the stage, so the import graph
// stays one-way (theatre → tools) and every tool is testable in isolation
// with a spy callback.
//
// The Call contract follows internal/agents/tools: Call returns the tool's
// output as a string. Malformed input and callback failures are returned as
// message strings with a nil error, so the agent loop continues and the model
// can read the refusal and adapt — the deterministic floor stands below
// everything (decision D11).
package tools

import (
	"strings"

	"github.com/baalimago/clai/pkg/text/models"
)

// consultTool asks another production role a question through the
// consultation broker.
type consultTool struct {
	consult func(role, question string) (string, error)
}

// NewConsult builds the consult tool. The questioner and the hop depth are
// pinned by the theatre package at construction; the role input is restricted
// to the four production roles, so a subagent can never consult the director
// (decision D4).
func NewConsult(consult func(role, question string) (string, error)) models.LLMTool {
	return &consultTool{consult: consult}
}

func (t *consultTool) Call(input models.Input) (string, error) {
	role, ok := input["role"].(string)
	if !ok {
		return "consult: 'role' must be a string", nil
	}
	question, ok := input["question"].(string)
	if !ok {
		return "consult: 'question' must be a string", nil
	}
	answer, err := t.consult(strings.ToLower(strings.TrimSpace(role)), question)
	if err != nil {
		return "consult: " + err.Error(), nil
	}
	return answer, nil
}

func (t *consultTool) Specification() models.Specification {
	roles := []string{"dramaturg", "playwright", "scenographer", "wardrobe"}
	return models.Specification{
		Name:        "consult",
		Description: "Ask another production role a focused question and get its answer. Use it when another role's expertise would improve your deliverable.",
		Inputs: &models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterObject{
				"role": {
					Type:        "string",
					Description: "The role to consult",
					Enum:        &roles,
				},
				"question": {
					Type:        "string",
					Description: "The question, as focused as possible",
				},
			},
			Required: []string{"role", "question"},
		},
	}
}

// writeDraftTool saves the playwright's draft into the working file.
type writeDraftTool struct {
	write func(story, report string) (string, error)
}

// NewWriteDraft builds the write_draft tool, the playwright's deliverable
// writer: it writes the full story JSON to the working file and returns a
// confirmation the playwright can report.
func NewWriteDraft(write func(story, report string) (string, error)) models.LLMTool {
	return &writeDraftTool{write: write}
}

func (t *writeDraftTool) Call(input models.Input) (string, error) {
	story, ok := input["story"].(string)
	if !ok {
		return "write_draft: 'story' must be a string containing the draft JSON", nil
	}
	report, _ := input["report"].(string)
	out, err := t.write(story, report)
	if err != nil {
		return "write_draft: " + err.Error(), nil
	}
	return out, nil
}

func (t *writeDraftTool) Specification() models.Specification {
	return models.Specification{
		Name:        "write_draft",
		Description: "Write the full draft story into the working file: pass the complete story as JSON in 'story' and a compact report in 'report'. This is your final deliverable — call it once, then stop.",
		Inputs: &models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterObject{
				"story": {
					Type:        "string",
					Description: "The complete draft story as a JSON object: title, durationMs, scene.backdrop, cast, props, beats",
				},
				"report": {
					Type:        "string",
					Description: "A compact report of the draft: beats, acts, title",
				},
			},
			Required: []string{"story"},
		},
	}
}

// appendCanonTool appends a canon fact to the working file (soft continuity,
// D6).
type appendCanonTool struct {
	appendFact func(fact string) error
}

// NewAppendCanon builds the append_canon tool, the playwright's canon writer.
func NewAppendCanon(appendFact func(fact string) error) models.LLMTool {
	return &appendCanonTool{appendFact: appendFact}
}

func (t *appendCanonTool) Call(input models.Input) (string, error) {
	fact, ok := input["fact"].(string)
	if !ok {
		return "append_canon: 'fact' must be a string", nil
	}
	if err := t.appendFact(fact); err != nil {
		return "append_canon: " + err.Error(), nil
	}
	return "canon fact appended", nil
}

func (t *appendCanonTool) Specification() models.Specification {
	return models.Specification{
		Name:        "append_canon",
		Description: "Append a short canon fact: a past-tense outcome statement (\"the mouse got away\") that future generations will riff on. At most 120 characters.",
		Inputs: &models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterObject{
				"fact": {
					Type:        "string",
					Description: "The canon fact, at most 120 characters",
				},
			},
			Required: []string{"fact"},
		},
	}
}

// writeSceneTool dresses the working draft's set.
type writeSceneTool struct {
	write func(backdrop, report string) (string, error)
}

// NewWriteScene builds the write_scene tool, the scenographer's deliverable
// writer: it dresses the working draft's set and returns a confirmation.
func NewWriteScene(write func(backdrop, report string) (string, error)) models.LLMTool {
	return &writeSceneTool{write: write}
}

func (t *writeSceneTool) Call(input models.Input) (string, error) {
	backdrop, ok := input["backdrop"].(string)
	if !ok {
		return "write_scene: 'backdrop' must be a string", nil
	}
	report, _ := input["report"].(string)
	out, err := t.write(backdrop, report)
	if err != nil {
		return "write_scene: " + err.Error(), nil
	}
	return out, nil
}

func (t *writeSceneTool) Specification() models.Specification {
	return models.Specification{
		Name:        "write_scene",
		Description: "Dress the draft's set: choose the backdrop that suits the brief and the draft. This is your final deliverable — call it once, then stop.",
		Inputs: &models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterObject{
				"backdrop": {
					Type:        "string",
					Description: "One of: night, livingroom, garden, theatre, sunset, kitchen, forest, rain",
				},
				"report": {
					Type:        "string",
					Description: "A compact report of the scene and the reasoning",
				},
			},
			Required: []string{"backdrop"},
		},
	}
}

// adviseTool is the wardrobe consultant's answer tool: it confirms the
// answer text in-line. The wardrobe has no deliverable writer — its answer is
// the text itself.
type adviseTool struct {
	advise func(answer string) (string, error)
}

// NewAdvise builds the advise tool.
func NewAdvise(advise func(answer string) (string, error)) models.LLMTool {
	return &adviseTool{advise: advise}
}

func (t *adviseTool) Call(input models.Input) (string, error) {
	answer, ok := input["answer"].(string)
	if !ok {
		return "advise: 'answer' must be a string", nil
	}
	out, err := t.advise(answer)
	if err != nil {
		return "advise: " + err.Error(), nil
	}
	return out, nil
}

func (t *adviseTool) Specification() models.Specification {
	return models.Specification{
		Name:        "advise",
		Description: "Give your answer to the question you were consulted about. The answer text is what the questioner receives.",
		Inputs: &models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterObject{
				"answer": {
					Type:        "string",
					Description: "Your answer, at most 240 characters",
				},
			},
			Required: []string{"answer"},
		},
	}
}
