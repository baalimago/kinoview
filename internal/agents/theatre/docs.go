package theatre

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/baalimago/kinoview/internal/model"
)

// The company's durable memory (phase 6): seven documents under
// intro/company/, each atomic-written, validated on load and trimmed to its
// cap. They grow across generations — distillation at submit appends the
// current generation's work — and they are injected back into the relevant
// role's context, so the company develops itself. The deterministic floor
// stands under them: a corrupt document degrades to the empty one, never a
// crash. The audience doc is the exception to the distillation rule: only
// the audience writes it (decision D-5).

// Premise records one production brief's theme and shape, dated.
type Premise struct {
	Theme string `json:"theme"`
	Shape string `json:"shape,omitempty"`
	Date  string `json:"date,omitempty"`
}

// PremisesDoc is the dramaturg's memory: the themes and shapes already used,
// so the no-repeat list is grounded in history rather than memory.
type PremisesDoc []Premise

// PlaySummary is one past production's footprint: the title and the shape of
// the play.
type PlaySummary struct {
	Title string `json:"title"`
	Acts  int    `json:"acts,omitempty"`
	Beats int    `json:"beats,omitempty"`
	Date  string `json:"date,omitempty"`
}

// RepertoireDoc is the playwright's memory (soft continuity, D6): story
// summaries and the canon facts earlier productions left behind.
type RepertoireDoc struct {
	Summaries []PlaySummary `json:"summaries"`
	Facts     []string      `json:"facts"`
}

// SetRecipe is one backdrop dressing the scenographer has used: the backdrop,
// the cells and the prop placements, with a usage count. A recipe's identity
// is the backdrop plus the layout, so the same dress again is a count bump,
// not a duplicate entry.
type SetRecipe struct {
	Backdrop string          `json:"backdrop"`
	Cells    []CellPlacement `json:"cells,omitempty"`
	Props    []PropPlacement `json:"props,omitempty"`
	Count    int             `json:"count"`
	Date     string          `json:"date,omitempty"`
}

// SetsDoc is the scenographer's memory: the dressing recipes in use, so a
// set is varied rather than repeated.
type SetsDoc []SetRecipe

// CharacterEntry is one row of the character registry: id → species,
// canonical coat, wardrobe variants and a note. The registry is the only
// place identities are born (decision D7).
type CharacterEntry struct {
	ID       string   `json:"id"`
	Species  string   `json:"species"`
	Coat     string   `json:"coat"`
	Variants []string `json:"variants,omitempty"`
	Notes    string   `json:"notes,omitempty"`
}

// RegistryDoc is the costumer's book: the durable character registry. Fixed
// and small — identities enter only by explicit director approval, never by
// LLM text.
type RegistryDoc []CharacterEntry

// Lesson is one critique the director learned from a production.
type Lesson struct {
	Text string `json:"text"`
	Date string `json:"date,omitempty"`
}

// DirectorDoc is the director's memory: critique lessons from earlier
// productions ("two stares in a row is dead air").
type DirectorDoc []Lesson

// Notice is one durable cross-role announcement.
type Notice struct {
	Author string `json:"author"`
	Kind   string `json:"kind"`
	Body   string `json:"body"`
	Date   string `json:"date,omitempty"`
}

// BulletinDoc is the company bulletin: durable announcements any role may
// contribute to — through the board; the LLM never writes the docs directly.
type BulletinDoc []Notice

// AudienceNote is one piece of audience feedback on a story.
type AudienceNote struct {
	StoryID string `json:"storyId"`
	Rating  int    `json:"rating"` // +1 thumbs up, -1 thumbs down
	Comment string `json:"comment,omitempty"`
	Date    string `json:"date,omitempty"`
}

// AudienceDoc is the audience's memory: what viewers thought of recent
// productions, so the next generation can improve. Newest note first.
type AudienceDoc []AudienceNote

// Library is the company's seven durable documents as one loadable, savable
// unit. The audience doc is read here like the rest; only SaveLibrary never
// writes it (decision D-5).
type Library struct {
	Premises   PremisesDoc
	Repertoire RepertoireDoc
	Sets       SetsDoc
	Registry   RegistryDoc
	Director   DirectorDoc
	Bulletin   BulletinDoc
	Audience   AudienceDoc
}

// The trim functions repair a document and trim it to its cap, oldest first.
// Load and save run the same gate, so a document that could not be read back
// is never written. A document over its cap drops the oldest entries on the
// next write (the acceptance criterion).

func trimPremises(d PremisesDoc) PremisesDoc {
	out := make(PremisesDoc, 0, premisesCap)
	seen := map[string]bool{}
	for _, p := range d {
		p.Theme = truncateRunes(strings.TrimSpace(p.Theme), model.MaxTitleLen)
		p.Shape = truncateRunes(strings.TrimSpace(p.Shape), MaxShapeLen)
		p.Date = trimDate(p.Date)
		if p.Theme == "" || seen[p.Theme] {
			continue
		}
		seen[p.Theme] = true
		if len(out) >= premisesCap {
			break
		}
		out = append(out, p)
	}
	return out
}

func trimRepertoire(d RepertoireDoc) RepertoireDoc {
	d.Summaries = trimSummaries(d.Summaries)
	d.Facts = trimFacts(d.Facts)
	return d
}

func trimSummaries(d []PlaySummary) []PlaySummary {
	out := make([]PlaySummary, 0, repertoireSumCap)
	seen := map[string]bool{}
	for _, s := range d {
		s.Title = truncateRunes(strings.TrimSpace(s.Title), model.MaxTitleLen)
		s.Date = trimDate(s.Date)
		if s.Title == "" || seen[s.Title] {
			continue
		}
		seen[s.Title] = true
		if len(out) >= repertoireSumCap {
			break
		}
		out = append(out, s)
	}
	return out
}

func trimFacts(d []string) []string {
	out := make([]string, 0, repertoireFactCap)
	seen := map[string]bool{}
	for _, f := range d {
		f = truncateRunes(strings.TrimSpace(f), CanonMaxFact)
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		if len(out) >= repertoireFactCap {
			break
		}
		out = append(out, f)
	}
	return out
}

func trimSets(d SetsDoc) SetsDoc {
	out := make(SetsDoc, 0, setsCap)
	seen := map[string]bool{}
	for _, r := range d {
		r.Backdrop = strings.ToLower(strings.TrimSpace(r.Backdrop))
		if !model.ValidBackdrops[r.Backdrop] {
			continue
		}
		r.Cells = normalizeCells(r.Cells)
		r.Props = normalizeProps(r.Props)
		r.Date = trimDate(r.Date)
		if r.Count < 1 {
			r.Count = 1
		}
		key := setRecipeKey(r)
		if seen[key] {
			continue
		}
		seen[key] = true
		if len(out) >= setsCap {
			break
		}
		out = append(out, r)
	}
	return out
}

func trimRegistry(d RegistryDoc) RegistryDoc {
	out := make(RegistryDoc, 0, registryMax)
	seen := map[string]bool{}
	for _, e := range d {
		e.ID = strings.ToLower(strings.TrimSpace(e.ID))
		e.Species = strings.ToLower(strings.TrimSpace(e.Species))
		e.Coat = strings.ToLower(strings.TrimSpace(e.Coat))
		if !artifactIDRe.MatchString(e.ID) || seen[e.ID] || !model.ValidCharacters[e.Species] {
			continue
		}
		// Load and canonize agree on what a coat may be (review 3, R3-04):
		// Canonize refuses coats and variants outside the species palette, so
		// the load gate drops them too — a hand-edited file can never surface
		// a coat the player cannot draw.
		palette := speciesVariants(e.Species)
		if e.Coat != "" && !slices.Contains(palette, e.Coat) {
			e.Coat = ""
		}
		e.Variants = filterVariants(e.Variants, palette)
		e.Notes = truncateRunes(strings.TrimSpace(e.Notes), MaxReasonLen)
		seen[e.ID] = true
		if len(out) >= registryMax {
			break
		}
		out = append(out, e)
	}
	return out
}

func trimLessons(d DirectorDoc) DirectorDoc {
	out := make(DirectorDoc, 0, directorCap)
	seen := map[string]bool{}
	for _, l := range d {
		l.Text = truncateRunes(strings.TrimSpace(l.Text), lessonMaxLen)
		l.Date = trimDate(l.Date)
		if l.Text == "" || seen[l.Text] {
			continue
		}
		seen[l.Text] = true
		if len(out) >= directorCap {
			break
		}
		out = append(out, l)
	}
	return out
}

func trimNotices(d BulletinDoc) BulletinDoc {
	out := make(BulletinDoc, 0, bulletinCap)
	seen := map[string]bool{}
	for _, n := range d {
		n.Author = strings.ToLower(strings.TrimSpace(n.Author))
		n.Kind = strings.ToLower(strings.TrimSpace(n.Kind))
		n.Body = truncateRunes(strings.TrimSpace(n.Body), EntryMaxBody)
		n.Date = trimDate(n.Date)
		if !ValidRoles[n.Author] || !ValidBoardKinds[n.Kind] || n.Body == "" {
			continue
		}
		key := n.Author + "\x00" + n.Kind + "\x00" + n.Body
		if seen[key] {
			continue
		}
		seen[key] = true
		if len(out) >= bulletinCap {
			break
		}
		out = append(out, n)
	}
	return out
}

// trimAudience repairs the audience doc: a note needs a valid story id and a
// +1/-1 rating; the comment is truncated to its cap (never rejected — a long
// comment is clipped, not lost), the date is trimmed to YYYY-MM-DD, and
// duplicates (same story, rating and comment) collapse. The doc is kept
// newest first, so the cap drops the oldest notes (decision D-3).
func trimAudience(d AudienceDoc) AudienceDoc {
	out := make(AudienceDoc, 0, audienceCap)
	seen := map[string]bool{}
	for _, n := range d {
		n.StoryID = strings.TrimSpace(n.StoryID)
		n.Comment = truncateRunes(strings.TrimSpace(n.Comment), audienceCommentMax)
		n.Date = trimDate(n.Date)
		if !artifactIDRe.MatchString(n.StoryID) || (n.Rating != 1 && n.Rating != -1) {
			continue
		}
		key := fmt.Sprintf("%s\x00%d\x00%s", n.StoryID, n.Rating, n.Comment)
		if seen[key] {
			continue
		}
		seen[key] = true
		if len(out) >= audienceCap {
			break
		}
		out = append(out, n)
	}
	return out
}

// normalizeCells and normalizeProps bound a recipe's layout — the same gates
// the scene report's own normalization applies.
func normalizeCells(cells []CellPlacement) []CellPlacement {
	out := make([]CellPlacement, 0, len(cells))
	for _, c := range cells {
		c.Row = strings.ToLower(strings.TrimSpace(c.Row))
		c.Piece = strings.ToLower(strings.TrimSpace(c.Piece))
		if !model.ValidRows[c.Row] {
			continue
		}
		if c.Piece != "" && !model.ValidPieces[c.Piece] {
			continue
		}
		c.Col = clampArtifactInt(c.Col, 0, model.CellCols-1)
		out = append(out, c)
	}
	return out
}

func normalizeProps(props []PropPlacement) []PropPlacement {
	out := make([]PropPlacement, 0, len(props))
	for _, p := range props {
		p.ID = strings.ToLower(strings.TrimSpace(p.ID))
		if !artifactIDRe.MatchString(p.ID) {
			continue
		}
		p.X = clampArtifactFloat(p.X, 0.05, 0.95)
		p.Lane = clampArtifactInt(p.Lane, 0, model.MaxLanes-1)
		out = append(out, p)
	}
	return out
}

// setRecipeKey is a recipe's identity: the backdrop plus the canonical cell
// and prop layout, so two identical dresses are one recipe.
func setRecipeKey(r SetRecipe) string {
	cells := make([]string, 0, len(r.Cells))
	for _, c := range r.Cells {
		cells = append(cells, fmt.Sprintf("%s:%d=%s", c.Row, c.Col, c.Piece))
	}
	slices.Sort(cells)
	props := make([]string, 0, len(r.Props))
	for _, p := range r.Props {
		props = append(props, fmt.Sprintf("%s@%d:%.2f", p.ID, p.Lane, p.X))
	}
	slices.Sort(props)
	return r.Backdrop + "|" + strings.Join(cells, ",") + "|" + strings.Join(props, ",")
}

// trimDate caps a date stamp at 10 runes (YYYY-MM-DD).
func trimDate(s string) string { return truncateRunes(strings.TrimSpace(s), 10) }

// dateStamp renders today as YYYY-MM-DD.
func dateStamp(now time.Time) string { return now.Format("2006-01-02") }

// loadDoc reads and repairs one company document. A missing file is the
// empty document — nothing has been produced yet; a corrupt one is logged
// and degrades to the empty document, so a corrupt doc never blocks startup
// (the acceptance criterion).
func loadDoc[T any](c *Company, path, name string, repair func(T) T) T {
	c.mu.Lock()
	defer c.mu.Unlock()
	return loadDocLocked(c, path, name, repair)
}

// loadDocLocked is loadDoc with the company mutex already held — the
// compound append path runs its read-modify-write under one lock.
func loadDocLocked[T any](c *Company, path, name string, repair func(T) T) T {
	var d T
	if err := readJSON(path, &d); err != nil {
		logLoadFailure(name, err)
		return d
	}
	return repair(d)
}

// saveDoc repairs and writes one company document atomically, running the
// same gate as load so a document that could not be read back is never
// written in the first place.
func saveDoc[T any](c *Company, path, name string, d T, repair func(T) T) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return saveDocLocked(c, path, name, d, repair)
}

// saveDocLocked is saveDoc with the company mutex already held.
func saveDocLocked[T any](c *Company, path, name string, d T, repair func(T) T) error {
	d = repair(d)
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", name, err)
	}
	return writeFileAtomic(path, data)
}

func (c *Company) premisesPath() string   { return filepath.Join(c.dir, premisesFileName) }
func (c *Company) repertoirePath() string { return filepath.Join(c.dir, repertoireFileName) }
func (c *Company) setsPath() string       { return filepath.Join(c.dir, setsFileName) }
func (c *Company) registryDocPath() string {
	return filepath.Join(c.dir, registryFileName)
}
func (c *Company) directorPath() string { return filepath.Join(c.dir, directorFileName) }
func (c *Company) bulletinPath() string { return filepath.Join(c.dir, bulletinFileName) }
func (c *Company) audiencePath() string { return filepath.Join(c.dir, audienceFileName) }

// The seven document accessors. Each loads independently: a corrupt document
// degrades to the empty one with an error log, and the rest stand.

func (c *Company) LoadPremises() PremisesDoc {
	return loadDoc(c, c.premisesPath(), "premises", trimPremises)
}

func (c *Company) SavePremises(d PremisesDoc) error {
	return saveDoc(c, c.premisesPath(), "premises", d, trimPremises)
}

func (c *Company) LoadRepertoire() RepertoireDoc {
	return loadDoc(c, c.repertoirePath(), "repertoire", trimRepertoire)
}

func (c *Company) SaveRepertoire(d RepertoireDoc) error {
	return saveDoc(c, c.repertoirePath(), "repertoire", d, trimRepertoire)
}

func (c *Company) LoadSets() SetsDoc {
	return loadDoc(c, c.setsPath(), "sets", trimSets)
}

func (c *Company) SaveSets(d SetsDoc) error {
	return saveDoc(c, c.setsPath(), "sets", d, trimSets)
}

func (c *Company) LoadRegistryDoc() RegistryDoc {
	return loadDoc(c, c.registryDocPath(), "registry", trimRegistry)
}

func (c *Company) SaveRegistryDoc(d RegistryDoc) error {
	return saveDoc(c, c.registryDocPath(), "registry", d, trimRegistry)
}

func (c *Company) LoadDirector() DirectorDoc {
	return loadDoc(c, c.directorPath(), "director", trimLessons)
}

func (c *Company) SaveDirector(d DirectorDoc) error {
	return saveDoc(c, c.directorPath(), "director", d, trimLessons)
}

func (c *Company) LoadBulletin() BulletinDoc {
	return loadDoc(c, c.bulletinPath(), "bulletin", trimNotices)
}

func (c *Company) SaveBulletin(d BulletinDoc) error {
	return saveDoc(c, c.bulletinPath(), "bulletin", d, trimNotices)
}

func (c *Company) LoadAudience() AudienceDoc {
	return loadDoc(c, c.audiencePath(), "audience", trimAudience)
}

func (c *Company) SaveAudience(d AudienceDoc) error {
	return saveDoc(c, c.audiencePath(), "audience", d, trimAudience)
}

// AppendAudience records one audience note — the doc's single write path
// (decision D-5): the load, prepend, trim and save run under the company's
// single mutex, so two concurrent appends lose no note. Distillation never
// writes audience.json, so a submit can never overwrite a fresh note with a
// stale in-memory copy.
func (c *Company) AppendAudience(note AudienceNote) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	doc := loadDocLocked(c, c.audiencePath(), "audience", trimAudience)
	doc = append(AudienceDoc{note}, doc...)
	return saveDocLocked(c, c.audiencePath(), "audience", doc, trimAudience)
}

// LoadLibrary reads the seven durable documents as one unit. Each document
// loads independently — a corrupt one degrades to the empty document with an
// error log, and the rest stand. The audience doc is read like the rest;
// only SaveLibrary never writes it (decision D-5).
func (c *Company) LoadLibrary() Library {
	return Library{
		Premises:   c.LoadPremises(),
		Repertoire: c.LoadRepertoire(),
		Sets:       c.LoadSets(),
		Registry:   c.LoadRegistryDoc(),
		Director:   c.LoadDirector(),
		Bulletin:   c.LoadBulletin(),
		Audience:   c.LoadAudience(),
	}
}

// SaveLibrary writes the six distilled documents. It deliberately never
// persists the audience doc (decision D-5): audience.json is single-writer —
// only Theatre.Feedback appends it — so distillation cannot overwrite a
// fresh note with a stale in-memory copy. Every document is attempted — a
// write failure on one never skips the others — and the joined error reports
// what failed. The caller logs it: the story is already persisted by the time
// distillation runs, so a failed doc never loses the show.
func (c *Company) SaveLibrary(l Library) error {
	var errs []error
	if err := c.SavePremises(l.Premises); err != nil {
		errs = append(errs, err)
	}
	if err := c.SaveRepertoire(l.Repertoire); err != nil {
		errs = append(errs, err)
	}
	if err := c.SaveSets(l.Sets); err != nil {
		errs = append(errs, err)
	}
	if err := c.SaveRegistryDoc(l.Registry); err != nil {
		errs = append(errs, err)
	}
	if err := c.SaveDirector(l.Director); err != nil {
		errs = append(errs, err)
	}
	if err := c.SaveBulletin(l.Bulletin); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// The context excerpts: a role reads the most recent entries of its own doc
// (and everyone reads the bulletin), never the whole history — the docs grow
// across generations, a prompt must not.

func (d PremisesDoc) context() string {
	excerpt := d
	if len(excerpt) > premisesExcerpt {
		excerpt = excerpt[:premisesExcerpt]
	}
	if len(excerpt) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nPremises already used (do not repeat these themes):\n")
	for _, p := range excerpt {
		fmt.Fprintf(&b, "  - %s", p.Theme)
		if p.Shape != "" {
			fmt.Fprintf(&b, " (%s)", p.Shape)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (d RepertoireDoc) context() string {
	var b strings.Builder
	facts := d.Facts
	if len(facts) > factsExcerpt {
		facts = facts[:factsExcerpt]
	}
	if len(facts) > 0 {
		b.WriteString("\nCanon facts from earlier productions (riff on them, never contradict them):\n")
		for _, f := range facts {
			fmt.Fprintf(&b, "  - %s\n", f)
		}
	}
	summaries := d.Summaries
	if len(summaries) > summariesExcerpt {
		summaries = summaries[:summariesExcerpt]
	}
	if len(summaries) > 0 {
		b.WriteString("\nEarlier productions:\n")
		for _, s := range summaries {
			fmt.Fprintf(&b, "  - %q (%d acts, %d beats)\n", s.Title, s.Acts, s.Beats)
		}
	}
	return b.String()
}

func (d SetsDoc) context() string {
	excerpt := d
	if len(excerpt) > setsExcerpt {
		excerpt = excerpt[:setsExcerpt]
	}
	if len(excerpt) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nSet recipes already used (vary them):\n")
	for _, r := range excerpt {
		fmt.Fprintf(&b, "  - %s (%d cells, %d props, used %d×)\n", r.Backdrop, len(r.Cells), len(r.Props), r.Count)
	}
	return b.String()
}

func (d DirectorDoc) context() string {
	excerpt := d
	if len(excerpt) > lessonsExcerpt {
		excerpt = excerpt[:lessonsExcerpt]
	}
	if len(excerpt) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nDirecting lessons from earlier productions:\n")
	for _, l := range excerpt {
		fmt.Fprintf(&b, "  - %s\n", l.Text)
	}
	return b.String()
}

func (d BulletinDoc) context() string {
	excerpt := d
	if len(excerpt) > bulletinExcerpt {
		excerpt = excerpt[:bulletinExcerpt]
	}
	if len(excerpt) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nCompany bulletin:\n")
	for _, n := range excerpt {
		fmt.Fprintf(&b, "  - [%s] %s: %s\n", n.Kind, n.Author, n.Body)
	}
	return b.String()
}

// The audience excerpt reaches the director and the dramaturg (decision Q2):
// the most recent notes, so the next generation adapts to what the audience
// said — never the whole history.
func (d AudienceDoc) context() string {
	excerpt := d
	if len(excerpt) > audienceExcerpt {
		excerpt = excerpt[:audienceExcerpt]
	}
	if len(excerpt) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nAudience feedback from recent shows:\n")
	for _, n := range excerpt {
		fmt.Fprintf(&b, "  - [+%d] ", n.Rating)
		if n.Comment != "" {
			fmt.Fprintf(&b, "%q ", n.Comment)
		}
		fmt.Fprintf(&b, "(%s", n.StoryID)
		if n.Date != "" {
			fmt.Fprintf(&b, ", %s", n.Date)
		}
		b.WriteString(")\n")
	}
	return b.String()
}
