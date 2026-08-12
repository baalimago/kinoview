package theatre

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/agents/theatre/tools"
	"github.com/baalimago/kinoview/internal/model"
)

// directorPrompt is the director's system prompt (phase 4): the production
// flow as guidance, not law (decision D1). The director orchestrates the
// company through its tools, works from the stage-manager reports and reads
// script pages only when scrutiny is needed. The working-context standard
// (AssembleContext) carries the generation, the theme, the board excerpt and
// the working summary around this prompt.
const directorPrompt = `You are the director of a tiny wordless theatre company. The company produces
one short slapstick scene for a media-server splash screen, acted out by simple
cartoon animals, then the app opens.

The permanent cast:
  - "ina"    : a cat  (character "cat")
  - "freija" : a dog  (character "dog")
  - "mouse1" : a mouse they like to hunt (character "mouse")
  - "pip"    : a bird that perches above them (character "bird")

You do not have to use all of them. Two characters is usually funnier than
three.

The company works through you:

  * dramaturg_brief — the dramaturg decides the production brief (mood, shape,
    cast lineup, what to avoid) and posts it to the board.
  * draft_story — the playwright writes the full story into the working file:
    title, cast, props, beats, canon facts.
  * dress_set — the scenographer dresses the draft's set.
  * read_story — read the working draft, or one part of it (cast, beats,
    scene, title), when you need to scrutinise the script.
  * validate_story — run the playability gate; it reports exact errors when
    the draft cannot be staged.
  * pin_identity — pin the canonical looks so characters never drift.
  * post_to_board — share notes with the company.
  * consult — ask a production role (dramaturg, playwright, scenographer,
    wardrobe) a focused question; the question and its answer land on the
    board.
  * submit_story — submit the production: validate, persist, end the run.
    Optionally pass 'notes' (critique lessons for your memory, one per line)
    and 'characters' (a JSON array of newly approved cast members to canonize
    in the registry, e.g. [{"id":"mouse2","species":"mouse","coat":"white"}] —
    only characters in the draft may be canonized).

Suggested flow: brief → draft → dress → validate → pin → iterate on your notes
→ submit. This is guidance, not law: you may deviate, revisit or consult at
will. Work from the reports the roles deliver; read script pages only when
scrutiny is needed. Submit as soon as the piece is good — do not burn budget.

The company's memory is a springboard, not a script. The bulletin, the
lessons, the audience notes and the earlier productions in your context are
guidance: never submit a play that repeats an earlier production's title,
beat skeleton or backdrop. If the audience disliked recent shows or asked
for something new, change the shape, the cast or the set — do not polish the
same play again. A repeated play is a failure, not a success.`

// production is one generation's run: the company paperwork, the stage, the
// runner and the broker, plus the director's own bookkeeping. It exists for
// the duration of one generation and is never shared.
type production struct {
	theatre *Theatre
	company *Company
	stage   *Stage
	runner  *Runner
	broker  *Broker
	gen     string

	// lessons are the director's critique lessons, carried by the submit
	// call and distilled into the director doc (phase 6).
	lessons []string

	// submitted is set by submit_story: the story is persisted and a second
	// submit is refused. The director's loop is single-threaded, so a plain
	// field is safe.
	submitted bool
}

// openProduction wires one generation: the company, the stage (with the
// generation's budgets, wall-clock deadline and log sink), the runner (model,
// config dir, cache dir and the LLM seam when a test injected one) and the
// consultation broker. The board is seeded with the generation and theme, so
// the working-context standard carries them into every agent.
func (t *Theatre) openProduction(theme string) *production {
	gen := t.newGenID()
	company := Open(t.cacheDir)
	stage := OpenStage(company, gen,
		WithBudgets(t.directorMax, t.globalMax),
		WithWallDeadline(t.wallClock),
		WithLogSink(t.logSink),
	)
	if err := company.SaveBoard(Board{Generation: gen, Theme: theme}); err != nil {
		ancli.Errf("theatre: board seed failed: %v", err)
	}
	// A generation starts with no draft: the previous generation's submitted
	// story must not prime the new one's working-context — its title, cast
	// and set anchored every role on the same play, generation after
	// generation (the cold-case loop).
	if err := company.ResetWorking(); err != nil {
		ancli.Errf("theatre: working reset failed: %v", err)
	}
	runner := NewRunner(company, stage,
		WithModel(t.model),
		WithConfigDir(t.configDir),
		WithCacheDir(t.cacheDir),
		WithRegistry(t.registry),
	)
	if t.runLLM != nil {
		runner.runLLM = t.runLLM
	}
	broker := NewBroker(company, stage, runner)
	runner.WireBroker(broker)

	p := &production{
		theatre: t,
		company: company,
		stage:   stage,
		runner:  runner,
		broker:  broker,
		gen:     gen,
	}
	// The director's tools close over the production; the runner is wired
	// after construction, exactly like the broker.
	runner.directorTools = func(ctx context.Context) []models.LLMTool { return p.directorTools(ctx) }
	return p
}

// runProduction runs one generation to completion: the director's bounded
// agent loop over the company, then the resolution of whatever it left
// behind. The director's exit is never the point — the working file is: a
// submitted story, the last validated draft, or nothing (the composer floor
// answers, through the caller).
func (t *Theatre) runProduction(ctx context.Context, theme string) (model.Story, error) {
	p := t.openProduction(theme)
	defer p.stage.Close()

	// The director's loop is bounded by the generation's wall clock: the
	// broker refuses spawns past the deadline, and the loop itself is
	// cancelled when the deadline expires mid-LLM-call.
	dctx, cancel := context.WithTimeout(ctx, t.wallClock)
	defer cancel()

	task := fmt.Sprintf("Direct the production for generation %s: produce the best story the company can within its budget, then submit it.", p.gen)
	_, err := p.runner.Run(dctx, Invocation{
		Role:   "director",
		Task:   task,
		Budget: t.directorMax,
		Depth:  0,
	})
	return p.finish(err)
}

// finish resolves what a generation produced. The working file is the single
// source of truth (decision D3): a submitted story ships as-is; a validated
// draft ships on exhaustion; anything else — no draft, a corrupt one, or a
// playable draft that never passed the playability gate (review 7, R7-01) —
// is an error the caller answers with the composer floor. An invalid draft
// can never reach this point — SaveWorking and LoadWorking run the same gate.
func (p *production) finish(dirErr error) (model.Story, error) {
	w, werr := p.company.LoadWorking()
	switch {
	case werr == nil && w.Status == "submitted":
		// submit_story already validated and persisted the story; the stage
		// just rings the bell.
		p.stage.Submit(w.Story.Title)
		return w.Story, nil
	case werr == nil && w.Validated:
		// The director ended without submitting: budget or wall-clock
		// exhaustion. The last validated draft ships; the ledger records the
		// exhaustion as a warning note.
		p.stage.Emit(TranscriptEvent{
			Kind: "note", From: "stage",
			Body: "director ended without submitting — shipping the last validated draft", Level: "warning",
		})
		s := w.Story
		if err := p.theatre.saveStory(s); err != nil {
			ancli.Errf("theatre: persist last validated draft: %v", err)
		}
		p.stage.Submit(s.Title)
		return s, nil
	default:
		// Nothing shippable: the working file is missing or corrupt, or it
		// holds a playable draft that never passed validate_story (R7-01).
		// The composer floor answers; the director's own failure (when there
		// is one) is the more specific cause.
		cause := fmt.Errorf("no playable draft: %v", werr)
		if werr == nil {
			p.stage.Emit(TranscriptEvent{
				Kind: "note", From: "stage",
				Body: "director ended without submitting — the draft never passed validate_story; the composer floor answers", Level: "warning",
			})
			cause = errors.New("the draft was never validated")
		}
		p.stage.Fail(dirErr)
		if dirErr != nil {
			return model.Story{}, fmt.Errorf("production %s: %w", p.gen, dirErr)
		}
		return model.Story{}, fmt.Errorf("production %s: %w", p.gen, cause)
	}
}

// directorTools is the director's instrument set: the three role spawns, the
// two working-file gates, the deterministic pin, the shared board post and
// consult (author and questioner pinned to the director, depth 0), and the
// final submit gate — nine tools, the spec's table.
func (p *production) directorTools(ctx context.Context) []models.LLMTool {
	return []models.LLMTool{
		tools.NewDramaturgBrief(func(notes string) (string, error) {
			res, err := p.runner.Run(ctx, Invocation{
				Role: "dramaturg", Task: p.roleTask("Write the production brief.", notes),
				Budget: DefaultSubagentBudget, Depth: 0,
			})
			if err != nil {
				return "", err
			}
			p.stage.SetPhase("brief")
			return res.Text, nil
		}),
		tools.NewDraftStory(func(notes string) (string, error) {
			res, err := p.runner.Run(ctx, Invocation{
				Role: "playwright", Task: p.roleTask("Write the full draft story: the beats, the cast usage, the props, the title and the canon facts. Your final answer is the complete story JSON — a single JSON object checked against the story schema.", notes),
				Budget: DefaultSubagentBudget, Depth: 0,
			})
			if err != nil {
				return "", err
			}
			p.stage.SetPhase("draft")
			return res.Text, nil
		}),
		tools.NewDressSet(func(notes string) (string, error) {
			res, err := p.runner.Run(ctx, Invocation{
				Role: "scenographer", Task: p.roleTask("Dress the draft's set: choose the backdrop that suits the brief and the story. Deliver it with write_scene.", notes),
				Budget: DefaultSubagentBudget, Depth: 0,
			})
			if err != nil {
				return "", err
			}
			p.stage.SetPhase("dress")
			return res.Text, nil
		}),
		tools.NewReadStory(func(part string) (string, error) { return p.readStory(part) }),
		tools.NewValidateStory(func() (string, error) { return p.validateStory() }),
		tools.NewPinIdentity(func() (string, error) { return p.pinIdentity() }),
		tools.NewPostToBoard(func(kind, to, body string) error {
			return p.runner.postToBoard("director", kind, to, body)
		}),
		tools.NewConsult(func(target, question string) (string, error) {
			return p.broker.Consult(ctx, "director", target, question, 0)
		}),
		tools.NewSubmitStory(func(notes, characters string) (string, error) {
			return p.submitStory(notes, characters)
		}),
	}
}

// roleTask appends the director's notes to a role's task. The working-context
// standard carries the rest: the board excerpt (the brief), the working
// summary and the role prompt.
func (p *production) roleTask(task, notes string) string {
	notes = strings.TrimSpace(notes)
	if notes == "" {
		return task
	}
	return task + "\n\nThe director's notes: " + notes
}

// readStory returns the working draft, or one requested part of it. Without a
// draft there is nothing to read; the playwright must write first.
func (p *production) readStory(part string) (string, error) {
	w, err := p.company.LoadWorking()
	if err != nil {
		return "", fmt.Errorf("no draft in the working file yet — the playwright must write first")
	}
	switch strings.ToLower(strings.TrimSpace(part)) {
	case "", "story":
		b, err := json.MarshalIndent(w.Story, "", "  ")
		if err != nil {
			return "", err
		}
		return string(b), nil
	case "cast":
		b, err := json.MarshalIndent(w.Story.Cast, "", "  ")
		if err != nil {
			return "", err
		}
		return string(b), nil
	case "beats":
		b, err := json.MarshalIndent(w.Story.Beats, "", "  ")
		if err != nil {
			return "", err
		}
		return string(b), nil
	case "scene":
		b, err := json.MarshalIndent(w.Story.Scene, "", "  ")
		if err != nil {
			return "", err
		}
		return string(b), nil
	case "title":
		return w.Story.Title, nil
	default:
		return "", fmt.Errorf("unknown part %q (story, cast, beats, scene, title)", part)
	}
}

// validateStory runs the playability gate on the working draft (the same
// trust boundary as a fresh LLM reply) and marks the draft validated — the
// blessing that qualifies it for the exhaustion path (review 7, R7-01).
// Exact errors return to the director — an invalid draft must never reach
// submit.
func (p *production) validateStory() (string, error) {
	w, err := p.company.LoadWorking()
	if err != nil {
		return "", fmt.Errorf("draft rejected: %v", err)
	}
	w.Status = "validated"
	w.Validated = true
	if err := p.company.SaveWorking(w); err != nil {
		return "", err
	}
	p.stage.SetPhase("validate")
	b, err := json.MarshalIndent(w.Story, "", "  ")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("story validated: %s", b), nil
}

// pinIdentity pins the canonical coat and character per cast id (decision
// D7): a registered id is stamped from the book — the permanent cast's
// canonical defaults, whatever the draft says — so characters never drift.
// An unregistered id is a guest and is left as-is. Phase 6 made the book
// durable: it round-trips through registry.json and new characters enter
// only by director approval at submit.
func (p *production) pinIdentity() (string, error) {
	w, err := p.company.LoadWorking()
	if err != nil {
		return "", fmt.Errorf("no draft in the working file yet — the playwright must write first")
	}
	applied := p.theatre.registry.PinAndApply(w.Story.Cast)
	w.Status = "pinned"
	if err := p.company.SaveWorking(w); err != nil {
		return "", err
	}
	p.stage.SetPhase("pin")
	return fmt.Sprintf("pinned %d identities (%d in the registry)", applied, p.theatre.registry.Size()), nil
}

// submitStory is the final gate: the working draft is validated once more
// (belt and braces — SaveWorking already refuses unplayable drafts), the
// story is persisted to intro_story.json, the working file is marked
// submitted and the production is done. The submit call also carries the
// director's final word — critique lessons and newly approved characters —
// which the distillation folds into the company library AFTER the story is
// persisted (the integration contract: docs never precede the story). A
// second submit for the same generation is refused; the story is persisted
// exactly once.
func (p *production) submitStory(notes, characters string) (string, error) {
	if p.submitted {
		return "", fmt.Errorf("submit refused: the production is already submitted")
	}
	w, err := p.company.LoadWorking()
	if err != nil {
		return "", fmt.Errorf("submit refused: no playable draft — %v", err)
	}
	// The theatre owns the ids: the generation id is the story id (the
	// playwright's write_draft set it), so the persisted story is traceable
	// back to its production. The story must be durably on disk before the
	// paperwork claims a submission: a persistence failure aborts the submit
	// and leaves the working state and the library untouched (review 7,
	// R7-02).
	if err := p.theatre.saveStory(w.Story); err != nil {
		return "", fmt.Errorf("submit refused: story not persisted: %w", err)
	}
	w.Status = "submitted"
	if err := p.company.SaveWorking(w); err != nil {
		return "", err
	}
	p.submitted = true
	p.stage.SetPhase("submit")

	// The director's final word, distilled after the story is safe. A
	// distillation failure is logged and never fails the submit — the story
	// is already persisted, and the next submit writes the docs again (the
	// error table).
	p.lessons = splitLessons(notes)
	approved, refused := 0, []string(nil)
	if entries, perr := parseCanonizations(characters); perr != nil {
		refused = append(refused, perr.Error())
	} else {
		approved, refused = p.theatre.registry.Canonize(entries, castIDs(w.Story))
	}
	if err := p.distill(); err != nil {
		ancli.Errf("theatre: distillation failed: %v", err)
		p.stage.Emit(TranscriptEvent{Kind: "note", From: "stage", Body: "distillation failed — the company library was not updated", Level: "warning"})
	}

	msg := fmt.Sprintf("submitted %q (%d beats, %d cast, %d props)",
		w.Story.Title, len(w.Story.Beats), len(w.Story.Cast), len(w.Story.Props))
	if approved > 0 {
		msg += fmt.Sprintf(", canonized %d characters", approved)
	}
	if len(refused) > 0 {
		msg += fmt.Sprintf(", refused: %s", strings.Join(refused, "; "))
	}
	return msg, nil
}
