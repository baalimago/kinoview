package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/baalimago/clai/pkg/text/models"
)

// allTools builds one instance of every tool with spy callbacks, so the
// spec-shape tests cover the whole set at once.
func allTools(t *testing.T) map[string]models.LLMTool {
	t.Helper()
	spy := func(string) error { return nil }
	spy1 := func(string) (string, error) { return "ok", nil }
	spy2 := func(string, string) (string, error) { return "ok", nil }
	spy3 := func() (string, error) { return "ok", nil }
	return map[string]models.LLMTool{
		"consult":         NewConsult(func(role, question string) (string, error) { return "answer", nil }),
		"write_draft":     NewWriteDraft(spy2),
		"append_canon":    NewAppendCanon(spy),
		"write_scene":     NewWriteScene(spy2),
		"advise":          NewAdvise(spy1),
		"dramaturg_brief": NewDramaturgBrief(spy1),
		"draft_story":     NewDraftStory(spy1),
		"dress_set":       NewDressSet(spy1),
		"read_story":      NewReadStory(spy1),
		"validate_story":  NewValidateStory(spy3),
		"submit_story":    NewSubmitStory(spy3),
	}
}

// Every tool spec validates: a snake_case name, a non-empty description and
// an input schema whose required fields all have properties. The consult
// tool's role input is restricted to the four production roles, so a subagent
// can never consult the director (decision D4).
func TestTools_SpecificationsValid(t *testing.T) {
	for name, tool := range allTools(t) {
		t.Run(name, func(t *testing.T) {
			spec := tool.Specification()
			if spec.Name != name {
				t.Errorf("spec name = %q, want %q", spec.Name, name)
			}
			if !isSnakeCase(spec.Name) {
				t.Errorf("spec name %q is not snake_case", spec.Name)
			}
			if strings.TrimSpace(spec.Description) == "" {
				t.Error("spec description is empty")
			}
			if spec.Inputs == nil {
				t.Fatal("spec inputs are nil")
			}
			if !spec.Inputs.IsOk() {
				t.Error("spec inputs are not well formed")
			}
			for _, req := range spec.Inputs.Required {
				if _, ok := spec.Inputs.Properties[req]; !ok {
					t.Errorf("required input %q has no property", req)
				}
			}
			if name == "consult" {
				role, ok := spec.Inputs.Properties["role"]
				if !ok {
					t.Fatal("consult spec lacks a 'role' input")
				}
				if role.Enum == nil {
					t.Fatal("consult role input has no enum")
				}
				for _, r := range *role.Enum {
					if !productionRole(r) {
						t.Errorf("consult role enum allows %q — the director and the stage are never consultable", r)
					}
				}
			}
		})
	}
}

func isSnakeCase(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r >= 'A' && r <= 'Z' || r == ' ' || r == '-' {
			return false
		}
	}
	return true
}

func productionRole(r string) bool {
	switch r {
	case "dramaturg", "playwright", "scenographer", "wardrobe":
		return true
	}
	return false
}

// Malformed input returns a message string with a nil error, so the agent
// loop continues and the model can read the refusal and adapt — never a hard
// failure that would trip the deterministic floor for a fixable slip.
func TestTools_MalformedInputReturnsMessage(t *testing.T) {
	cases := []struct {
		name  string
		tool  models.LLMTool
		input models.Input
	}{
		{"consult", NewConsult(func(string, string) (string, error) { return "", nil }), models.Input{"role": "director"}},
		{"write_draft", NewWriteDraft(func(string, string) (string, error) { return "", nil }), models.Input{"story": 42}},
		{"append_canon", NewAppendCanon(func(string) error { return nil }), models.Input{"fact": nil}},
		{"write_scene", NewWriteScene(func(string, string) (string, error) { return "", nil }), models.Input{"backdrop": true}},
		{"advise", NewAdvise(func(string) (string, error) { return "", nil }), models.Input{}},
		{"dramaturg_brief", NewDramaturgBrief(func(string) (string, error) { return "", nil }), models.Input{"notes": 1}},
		{"draft_story", NewDraftStory(func(string) (string, error) { return "", nil }), models.Input{"notes": nil}},
		{"dress_set", NewDressSet(func(string) (string, error) { return "", nil }), models.Input{"notes": true}},
		{"read_story", NewReadStory(func(string) (string, error) { return "", nil }), models.Input{"part": 42}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			out, err := tt.tool.Call(tt.input)
			if err != nil {
				t.Fatalf("Call returned a hard error, want a message string: %v", err)
			}
			if !strings.Contains(out, tt.name) {
				t.Errorf("message %q does not name the tool", out)
			}
		})
	}
}

// Each tool routes its parsed input into its callback and returns the
// callback's outcome to the agent.
func TestTools_CallBehavior(t *testing.T) {
	t.Run("consult returns the answer", func(t *testing.T) {
		var gotRole, gotQ string
		tool := NewConsult(func(role, question string) (string, error) {
			gotRole, gotQ = role, question
			return "silver reads", nil
		})
		out, err := tool.Call(models.Input{"role": " Wardrobe ", "question": "does silver read?"})
		if err != nil || out != "silver reads" {
			t.Fatalf("out = %q, err = %v", out, err)
		}
		if gotRole != "wardrobe" {
			t.Errorf("questioner role %q was not lowercased and trimmed", gotRole)
		}
		if gotQ != "does silver read?" {
			t.Errorf("question = %q", gotQ)
		}
	})

	t.Run("write_draft returns the confirmation", func(t *testing.T) {
		tool := NewWriteDraft(func(story, report string) (string, error) {
			if story != `{"title":"x"}` {
				t.Errorf("story = %q", story)
			}
			return "draft saved (revision 2)", nil
		})
		out, err := tool.Call(models.Input{"story": `{"title":"x"}`, "report": "2 beats"})
		if err != nil || !strings.Contains(out, "revision 2") {
			t.Fatalf("out = %q, err = %v", out, err)
		}
	})

	t.Run("advise passes the answer through", func(t *testing.T) {
		tool := NewAdvise(func(answer string) (string, error) { return answer, nil })
		out, err := tool.Call(models.Input{"answer": "keep ina lane 1"})
		if err != nil || out != "keep ina lane 1" {
			t.Fatalf("out = %q, err = %v", out, err)
		}
	})

	t.Run("optional-string tools forward their input", func(t *testing.T) {
		cases := []struct {
			name  string
			tool  func(func(string) (string, error)) models.LLMTool
			input models.Input
			want  string
			forw  string
		}{
			{"dramaturg_brief forwards the notes", NewDramaturgBrief, models.Input{"notes": "keep it dry"}, "mood=standoff, lineup=3", "keep it dry"},
			{"read_story forwards the part", NewReadStory, models.Input{"part": "cast"}, "cast json", "cast"},
		}
		for _, tt := range cases {
			t.Run(tt.name, func(t *testing.T) {
				var got string
				tool := tt.tool(func(v string) (string, error) {
					got = v
					return tt.want, nil
				})
				out, err := tool.Call(tt.input)
				if err != nil || out != tt.want {
					t.Fatalf("out = %q, err = %v", out, err)
				}
				if got != tt.forw {
					t.Errorf("forwarded %q, want %q", got, tt.forw)
				}
			})
		}
	})

	t.Run("draft_story tolerates missing notes", func(t *testing.T) {
		tool := NewDraftStory(func(notes string) (string, error) { return "16 beats / 3 acts", nil })
		out, err := tool.Call(models.Input{})
		if err != nil || !strings.Contains(out, "16 beats") {
			t.Fatalf("out = %q, err = %v", out, err)
		}
	})

	t.Run("submit_story returns the summary", func(t *testing.T) {
		tool := NewSubmitStory(func() (string, error) {
			return "submitted \"T\" (12 beats, 3 cast, 1 props)", nil
		})
		out, err := tool.Call(models.Input{})
		if err != nil || !strings.Contains(out, "submitted") {
			t.Fatalf("out = %q, err = %v", out, err)
		}
	})
}

// A callback failure is a message to the agent, never a hard error: the loop
// continues and the model can read the refusal and adapt.
func TestTools_CallbackErrorIsMessage(t *testing.T) {
	cases := []struct {
		name string
		tool models.LLMTool
		in   models.Input
	}{
		{"consult", NewConsult(func(string, string) (string, error) { return "", fmt.Errorf("consult boom") }), models.Input{"role": "wardrobe", "question": "q"}},
		{"write_draft", NewWriteDraft(func(string, string) (string, error) { return "", fmt.Errorf("draft boom") }), models.Input{"story": "{}"}},
		{"append_canon", NewAppendCanon(func(string) error { return fmt.Errorf("canon boom") }), models.Input{"fact": "f"}},
		{"write_scene", NewWriteScene(func(string, string) (string, error) { return "", fmt.Errorf("scene boom") }), models.Input{"backdrop": "night"}},
		{"advise", NewAdvise(func(string) (string, error) { return "", fmt.Errorf("advise boom") }), models.Input{"answer": "a"}},
		{"dramaturg_brief", NewDramaturgBrief(func(string) (string, error) { return "", fmt.Errorf("brief boom") }), models.Input{"notes": "n"}},
		{"draft_story", NewDraftStory(func(string) (string, error) { return "", fmt.Errorf("draft boom") }), models.Input{"notes": "n"}},
		{"dress_set", NewDressSet(func(string) (string, error) { return "", fmt.Errorf("scene boom") }), models.Input{"notes": "n"}},
		{"read_story", NewReadStory(func(string) (string, error) { return "", fmt.Errorf("read boom") }), models.Input{"part": "cast"}},
		{"validate_story", NewValidateStory(func() (string, error) { return "", fmt.Errorf("validate boom") }), models.Input{}},
		{"submit_story", NewSubmitStory(func() (string, error) { return "", fmt.Errorf("submit boom") }), models.Input{}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			out, err := tt.tool.Call(tt.in)
			if err != nil {
				t.Fatalf("Call returned a hard error, want a message: %v", err)
			}
			if !strings.Contains(out, "boom") {
				t.Errorf("out = %q, want the callback error surfaced", out)
			}
		})
	}
}

// Every tool's input schema must marshal to a wire shape the model vendors
// accept. The openai vendor sends spec.Inputs straight into the request body,
// and clai's models.InputSchema marshals 'required' and 'properties'
// unconditionally — nil slices/maps become JSON null, which the DeepSeek /
// OpenRouter schema validator rejects with
// "Invalid schema for function 'X': null is not of type \"array\""
// (observed in prod, generation stry_r6697buw, director tool dramaturg_brief).
// required must be a JSON array and properties a JSON object on the wire,
// never null.
func TestTools_SpecWireShape(t *testing.T) {
	for name, tool := range allTools(t) {
		t.Run(name, func(t *testing.T) {
			spec := tool.Specification()
			if spec.Inputs == nil {
				t.Fatal("spec inputs are nil")
			}
			raw, err := json.Marshal(spec.Inputs)
			if err != nil {
				t.Fatalf("marshal inputs: %v", err)
			}
			var wire struct {
				Required   json.RawMessage `json:"required"`
				Properties json.RawMessage `json:"properties"`
			}
			if err := json.Unmarshal(raw, &wire); err != nil {
				t.Fatalf("unmarshal wire: %v", err)
			}
			if len(wire.Required) == 0 || string(wire.Required) == "null" {
				t.Errorf("required is %s, want a JSON array", wire.Required)
			} else if err := json.Unmarshal(wire.Required, &[]string{}); err != nil {
				t.Errorf("required %s is not a JSON array: %v", wire.Required, err)
			}
			if len(wire.Properties) == 0 || string(wire.Properties) == "null" {
				t.Errorf("properties is %s, want a JSON object", wire.Properties)
			} else if err := json.Unmarshal(wire.Properties, &map[string]json.RawMessage{}); err != nil {
				t.Errorf("properties %s is not a JSON object: %v", wire.Properties, err)
			}
		})
	}
}
