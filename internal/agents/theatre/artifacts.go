package theatre

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/baalimago/kinoview/internal/model"
)

// The role artifacts — brief.json, draft-report.json, scene-report.json —
// are the compact deliverables the four production roles produce. Each is
// validated at the wrapper boundary (the writer tools) before it enters the
// board or the working file, with the same strictness as model.Story.Validate:
// unknown values are dropped, ids are pattern-checked and lengths are capped.
// A deliverable that is not a JSON artifact passes through untouched — the
// free-text path is the legacy quick form, and the deterministic floor always
// answers with an artifact that validates.

// BriefArtifact is the dramaturg's deliverable: the mood, the shape, the
// 1-3 member cast lineup, what to avoid repeating and the theme to riff on.
type BriefArtifact struct {
	Mood     string   `json:"mood"`
	Shape    string   `json:"shape"`
	Lineup   []string `json:"lineup"`
	NoRepeat []string `json:"noRepeat"`
	Theme    string   `json:"theme"`
}

// DraftReport is the playwright's compact report, delivered beside the full
// draft: the author's own act structure (which supersedes the derived count,
// decision D-P1-6) and the canon facts the playwright kept (soft continuity,
// D6).
type DraftReport struct {
	Title      string   `json:"title"`
	Acts       []Act    `json:"acts"`
	Cast       []string `json:"cast"`
	Props      []string `json:"props"`
	BeatsCount int      `json:"beatsCount"`
	Canon      []string `json:"canon"`
}

// Act is one act of the draft, as the playwright sees it.
type Act struct {
	Name    string `json:"name"`
	Beats   int    `json:"beats"`
	OneLine string `json:"oneLine"`
}

// SceneReport is the scenographer's deliverable: the backdrop, the cells and
// the prop placements that dress the draft's staging, plus the reasoning.
type SceneReport struct {
	Backdrop string          `json:"backdrop"`
	Cells    []CellPlacement `json:"cells"`
	Props    []PropPlacement `json:"props"`
	Reason   string          `json:"reason"`
}

// CellPlacement is one cell the scenographer wants dressed.
type CellPlacement struct {
	Row   string `json:"row"`
	Col   int    `json:"col"`
	Piece string `json:"piece"`
}

// PropPlacement moves one of the draft's props to a mark and a lane.
type PropPlacement struct {
	ID   string  `json:"id"`
	X    float64 `json:"x"`
	Lane int     `json:"lane"`
}

// Artifact caps. These mirror the model's own limits where the artifact names
// model concepts (cast, props, beats, canon) and add the report's own text
// bounds, so an LLM-authored artifact can never grow a prompt without bound.
const (
	MaxMoodLen     = 40
	MaxShapeLen    = 60
	MaxLineup      = 3
	MaxNoRepeat    = 5
	MaxNoRepeatLen = 60

	MaxReportActs = 6
	MaxActNameLen = 40
	MaxActOneLine = 120

	MaxCellPlacements = 10
	MaxPropPlacements = 4
	MaxReasonLen      = 240
)

// artifactIDRe mirrors model.Story's id pattern (^[a-z0-9_]{1,24}$). The
// model keeps the pattern private; phase 9 consolidates the two copies when
// the composer migrates into the theatre.
var artifactIDRe = regexp.MustCompile(`^[a-z0-9_]{1,24}$`)

// parseArtifact extracts the first balanced JSON object out of text and
// unmarshals it into v. It reports whether text was a JSON artifact at all —
// a free-text deliverable is not an artifact and passes through as-is.
func parseArtifact(text string, v any) bool {
	raw := extractJSON(text)
	if raw == "" {
		return false
	}
	return json.Unmarshal([]byte(raw), v) == nil
}

// normalizeBrief repairs a brief in place: ids are pattern-checked, deduped
// and kept only when known (known is nil when no registry is wired), lengths
// are capped and counts bounded. It never fails — a partially odd brief is
// better than no brief.
func normalizeBrief(ba *BriefArtifact, known func(string) bool) {
	ba.Mood = truncateRunes(strings.TrimSpace(ba.Mood), MaxMoodLen)
	ba.Shape = truncateRunes(strings.TrimSpace(ba.Shape), MaxShapeLen)
	ba.Theme = truncateRunes(strings.TrimSpace(ba.Theme), model.MaxTitleLen)
	lineup := make([]string, 0, MaxLineup)
	seen := map[string]bool{}
	for _, id := range ba.Lineup {
		if len(lineup) >= MaxLineup {
			break
		}
		id = strings.ToLower(strings.TrimSpace(id))
		if !artifactIDRe.MatchString(id) || seen[id] {
			continue
		}
		if known != nil && !known(id) {
			continue // an unregistered character must never enter the brief
		}
		seen[id] = true
		lineup = append(lineup, id)
	}
	ba.Lineup = lineup
	nr := make([]string, 0, MaxNoRepeat)
	for _, p := range ba.NoRepeat {
		if len(nr) >= MaxNoRepeat {
			break
		}
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		nr = append(nr, truncateRunes(p, MaxNoRepeatLen))
	}
	ba.NoRepeat = nr
}

// normalizeDraftReport repairs a draft report in place: text is capped, ids
// are pattern-checked, counters are clamped into the model's limits and canon
// facts are bounded like the working file's. It never fails.
func normalizeDraftReport(rep *DraftReport) {
	rep.Title = truncateRunes(strings.TrimSpace(rep.Title), model.MaxTitleLen)
	acts := make([]Act, 0, MaxReportActs)
	for _, a := range rep.Acts {
		if len(acts) >= MaxReportActs {
			break
		}
		a.Name = truncateRunes(strings.TrimSpace(a.Name), MaxActNameLen)
		a.OneLine = truncateRunes(strings.TrimSpace(a.OneLine), MaxActOneLine)
		a.Beats = clampArtifactInt(a.Beats, 0, model.MaxBeats)
		acts = append(acts, a)
	}
	rep.Acts = acts
	rep.Cast = filterIDs(rep.Cast, model.MaxCast)
	rep.Props = filterIDs(rep.Props, model.MaxProps)
	rep.BeatsCount = clampArtifactInt(rep.BeatsCount, 0, model.MaxBeats)
	canon := make([]string, 0, CanonMaxFacts)
	for _, f := range rep.Canon {
		if len(canon) >= CanonMaxFacts {
			break
		}
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		canon = append(canon, truncateRunes(f, CanonMaxFact))
	}
	rep.Canon = canon
}

// isEmpty reports whether the report carries no information at all — nothing
// the wrapper would want stored beside the draft.
func (rep DraftReport) isEmpty() bool {
	return rep.Title == "" && len(rep.Acts) == 0 && len(rep.Cast) == 0 &&
		len(rep.Props) == 0 && rep.BeatsCount == 0 && len(rep.Canon) == 0
}

// normalizeSceneReport repairs a scene report in place: the backdrop and the
// reason are bounded, cells are checked against the model's rows and pieces
// and clamped into the grid, prop placements are pattern-checked and clamped.
// The cross-check against the draft's own props happens at the wrapper (the
// scenographer's writeScene), where the draft is in hand. It never fails.
func normalizeSceneReport(sr *SceneReport) {
	sr.Backdrop = strings.ToLower(strings.TrimSpace(sr.Backdrop))
	sr.Reason = truncateRunes(strings.TrimSpace(sr.Reason), MaxReasonLen)
	cells := make([]CellPlacement, 0, MaxCellPlacements)
	for _, c := range sr.Cells {
		if len(cells) >= MaxCellPlacements {
			break
		}
		c.Row = strings.ToLower(strings.TrimSpace(c.Row))
		c.Piece = strings.ToLower(strings.TrimSpace(c.Piece))
		if !model.ValidRows[c.Row] {
			continue
		}
		if c.Piece != "" && !model.ValidPieces[c.Piece] {
			continue
		}
		c.Col = clampArtifactInt(c.Col, 0, model.CellCols-1)
		cells = append(cells, c)
	}
	sr.Cells = cells
	props := make([]PropPlacement, 0, MaxPropPlacements)
	for _, p := range sr.Props {
		if len(props) >= MaxPropPlacements {
			break
		}
		p.ID = strings.ToLower(strings.TrimSpace(p.ID))
		if !artifactIDRe.MatchString(p.ID) {
			continue
		}
		p.X = clampArtifactFloat(p.X, 0.05, 0.95)
		p.Lane = clampArtifactInt(p.Lane, 0, model.MaxLanes-1)
		props = append(props, p)
	}
	sr.Props = props
}

// filterIDs pattern-checks and dedupes an id list, capped at max entries —
// the report's cast and prop lists get the same treatment model.Story gives
// its own.
func filterIDs(ids []string, max int) []string {
	out := make([]string, 0, max)
	seen := map[string]bool{}
	for _, id := range ids {
		if len(out) >= max {
			break
		}
		id = strings.ToLower(strings.TrimSpace(id))
		if !artifactIDRe.MatchString(id) || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// parseDraftReport parses and normalises a draft report artifact. It reports
// whether the text was a JSON draft report with something in it; the
// normalized report is what the wrapper stores.
func parseDraftReport(text string) (DraftReport, bool) {
	var rep DraftReport
	if !parseArtifact(text, &rep) {
		return DraftReport{}, false
	}
	normalizeDraftReport(&rep)
	if rep.isEmpty() {
		return DraftReport{}, false
	}
	return rep, true
}

// parseSceneReport parses and normalises a scene report artifact.
func parseSceneReport(text string) (SceneReport, bool) {
	var sr SceneReport
	if !parseArtifact(text, &sr) {
		return SceneReport{}, false
	}
	normalizeSceneReport(&sr)
	return sr, true
}

// clampArtifactInt and clampArtifactFloat are the model's clamp helpers,
// mirrored here because they are unexported there. Phase 9 consolidates the
// copies when the composer migrates into the theatre.
func clampArtifactInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampArtifactFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
