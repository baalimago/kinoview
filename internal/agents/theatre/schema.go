package theatre

import "github.com/baalimago/clai/pkg/text/models"

// playwrightResponseFormat is the structured-output contract for the
// playwright's final answer (machine fix, 2026-08-03): the complete story as
// a single JSON object. It is json_object — not json_schema — because the
// production model (deepseek-v4-flash via OpenRouter) only supports
// json_object. The API therefore guarantees a JSON object but not the story
// shape; the shape is enforced client-side:
//
//   - the playwright prompt carries the field rules (the schema the model
//     must follow),
//   - writeDraft rejects a wrong shape with the field rules in the error,
//   - deliverDraft runs one bounded revision round with that error before
//     the composer floor answers.
//
// Only the production playwright (depth 0) gets it: a consulted playwright
// answers in place with free text, and the other roles deliver through their
// writer tools, not through their final answer.
func playwrightResponseFormat() *models.ResponseFormat {
	return &models.ResponseFormat{Type: "json_object"}
}
