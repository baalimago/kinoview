package tools

import (
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
	spy4 := func(string, string) (string, error) { return "ok", nil }
	return map[string]models.LLMTool{
		"post_to_board":   NewPostToBoard(func(kind, to, body string) error { return nil }),
		"read_board":      NewReadBoard(func() (string, error) { return "[1] stage (note) → company: hi", nil }),
		"consult":         NewConsult(func(role, question string) (string, error) { return "answer", nil }),
		"write_brief":     NewWriteBrief(spy),
		"write_draft":     NewWriteDraft(spy2),
		"append_canon":    NewAppendCanon(spy),
		"write_scene":     NewWriteScene(spy2),
		"advise":          NewAdvise(spy1),
		"dramaturg_brief": NewDramaturgBrief(spy1),
		"draft_story":     NewDraftStory(spy1),
		"dress_set":       NewDressSet(spy1),
		"read_story":      NewReadStory(spy1),
		"validate_story":  NewValidateStory(spy3),
		"pin_identity":    NewPinIdentity(spy3),
		"submit_story":    NewSubmitStory(spy4),
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
		{"post_to_board", NewPostToBoard(func(string, string, string) error { return nil }), models.Input{"kind": 1}},
		{"consult", NewConsult(func(string, string) (string, error) { return "", nil }), models.Input{"role": "director"}},
		{"write_brief", NewWriteBrief(func(string) error { return nil }), models.Input{"brief": 42}},
		{"write_draft", NewWriteDraft(func(string, string) (string, error) { return "", nil }), models.Input{"story": 42}},
		{"append_canon", NewAppendCanon(func(string) error { return nil }), models.Input{"fact": nil}},
		{"write_scene", NewWriteScene(func(string, string) (string, error) { return "", nil }), models.Input{"backdrop": true}},
		{"advise", NewAdvise(func(string) (string, error) { return "", nil }), models.Input{}},
		{"dramaturg_brief", NewDramaturgBrief(func(string) (string, error) { return "", nil }), models.Input{"notes": 1}},
		{"draft_story", NewDraftStory(func(string) (string, error) { return "", nil }), models.Input{"notes": nil}},
		{"dress_set", NewDressSet(func(string) (string, error) { return "", nil }), models.Input{"notes": true}},
		{"read_story", NewReadStory(func(string) (string, error) { return "", nil }), models.Input{"part": 42}},
		{"submit_story", NewSubmitStory(func(string, string) (string, error) { return "", nil }), models.Input{"notes": 42}},
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
	t.Run("post_to_board forwards kind, to and body", func(t *testing.T) {
		var gotKind, gotTo, gotBody string
		tool := NewPostToBoard(func(kind, to, body string) error {
			gotKind, gotTo, gotBody = kind, to, body
			return nil
		})
		out, err := tool.Call(models.Input{"kind": "note", "to": "director", "body": "hello"})
		if err != nil || !strings.Contains(out, "posted") {
			t.Fatalf("out = %q, err = %v", out, err)
		}
		if gotKind != "note" || gotTo != "director" || gotBody != "hello" {
			t.Errorf("callback got (%q, %q, %q)", gotKind, gotTo, gotBody)
		}
	})

	t.Run("post_to_board callback error is a message, not a hard error", func(t *testing.T) {
		tool := NewPostToBoard(func(string, string, string) error { return fmt.Errorf("board write: boom") })
		out, err := tool.Call(models.Input{"kind": "note", "to": "", "body": "x"})
		if err != nil {
			t.Fatalf("want a message string, got hard error %v", err)
		}
		if !strings.Contains(out, "boom") {
			t.Errorf("out = %q, want the callback error surfaced", out)
		}
	})

	t.Run("read_board returns the excerpt", func(t *testing.T) {
		tool := NewReadBoard(func() (string, error) { return "[1] stage (note) → company: hi", nil })
		out, err := tool.Call(models.Input{})
		if err != nil || !strings.Contains(out, "[1] stage") {
			t.Fatalf("out = %q, err = %v", out, err)
		}
	})

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

	t.Run("submit_story forwards notes and characters and returns the summary", func(t *testing.T) {
		var gotNotes, gotChars string
		tool := NewSubmitStory(func(notes, characters string) (string, error) {
			gotNotes, gotChars = notes, characters
			return "submitted \"T\" (12 beats, 3 cast, 1 props)", nil
		})
		out, err := tool.Call(models.Input{"notes": "two stares in a row", "characters": `[{"id":"m2"}]`})
		if err != nil || !strings.Contains(out, "submitted") {
			t.Fatalf("out = %q, err = %v", out, err)
		}
		if gotNotes != "two stares in a row" || gotChars != `[{"id":"m2"}]` {
			t.Errorf("callback got notes %q, characters %q", gotNotes, gotChars)
		}
	})

	t.Run("submit_story tolerates missing optional inputs", func(t *testing.T) {
		tool := NewSubmitStory(func(notes, characters string) (string, error) {
			if notes != "" || characters != "" {
				t.Errorf("got notes %q, characters %q, want empty", notes, characters)
			}
			return "submitted", nil
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
		{"read_board", NewReadBoard(func() (string, error) { return "", fmt.Errorf("read boom") }), models.Input{}},
		{"consult", NewConsult(func(string, string) (string, error) { return "", fmt.Errorf("consult boom") }), models.Input{"role": "wardrobe", "question": "q"}},
		{"write_brief", NewWriteBrief(func(string) error { return fmt.Errorf("brief boom") }), models.Input{"brief": "b"}},
		{"write_draft", NewWriteDraft(func(string, string) (string, error) { return "", fmt.Errorf("draft boom") }), models.Input{"story": "{}"}},
		{"append_canon", NewAppendCanon(func(string) error { return fmt.Errorf("canon boom") }), models.Input{"fact": "f"}},
		{"write_scene", NewWriteScene(func(string, string) (string, error) { return "", fmt.Errorf("scene boom") }), models.Input{"backdrop": "night"}},
		{"advise", NewAdvise(func(string) (string, error) { return "", fmt.Errorf("advise boom") }), models.Input{"answer": "a"}},
		{"dramaturg_brief", NewDramaturgBrief(func(string) (string, error) { return "", fmt.Errorf("brief boom") }), models.Input{"notes": "n"}},
		{"draft_story", NewDraftStory(func(string) (string, error) { return "", fmt.Errorf("draft boom") }), models.Input{"notes": "n"}},
		{"dress_set", NewDressSet(func(string) (string, error) { return "", fmt.Errorf("scene boom") }), models.Input{"notes": "n"}},
		{"read_story", NewReadStory(func(string) (string, error) { return "", fmt.Errorf("read boom") }), models.Input{"part": "cast"}},
		{"validate_story", NewValidateStory(func() (string, error) { return "", fmt.Errorf("validate boom") }), models.Input{}},
		{"pin_identity", NewPinIdentity(func() (string, error) { return "", fmt.Errorf("pin boom") }), models.Input{}},
		{"submit_story", NewSubmitStory(func(string, string) (string, error) { return "", fmt.Errorf("submit boom") }), models.Input{}},
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
