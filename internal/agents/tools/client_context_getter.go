package tools

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/agents"
	"github.com/baalimago/kinoview/internal/model"
)

type userContextGetter struct {
	mgr agents.ClientContextManager
}

func NewUserContextGetter(mgr agents.ClientContextManager) (*userContextGetter, error) {
	if mgr == nil {
		return nil, errors.New("user context manager can't be nil")
	}
	return &userContextGetter{mgr: mgr}, nil
}

func (t *userContextGetter) Call(input models.Input) (string, error) {
	mode, _ := input["mode"].(string)
	if mode == "" {
		mode = "summary"
	}

	limit := 5
	if v, ok := input["limit"].(float64); ok {
		if int(v) > 0 {
			limit = int(v)
		}
	}

	offset := 0
	if v, ok := input["offset"].(float64); ok {
		if int(v) >= 0 {
			offset = int(v)
		}
	}

	sessionID, _ := input["session_id"].(string)

	contexts := append([]model.ClientContext(nil), t.mgr.AllClientContexts()...)
	if len(contexts) == 0 {
		return "no user contexts have been recorded", nil
	}

	// Try to order by some timestamp if present; otherwise keep stable order.
	sort.SliceStable(contexts, func(i, j int) bool {
		return contexts[i].StartTime.After(contexts[j].StartTime)
	})

	if sessionID != "" {
		filtered := make([]model.ClientContext, 0, len(contexts))
		for _, c := range contexts {
			if c.SessionID == sessionID {
				filtered = append(filtered, c)
			}
		}
		contexts = filtered
		if len(contexts) == 0 {
			return fmt.Sprintf("no user context found for session_id=%q", sessionID), nil
		}
	}

	// Paging
	if offset >= len(contexts) {
		return "no results (offset past available contexts)", nil
	}
	contexts = contexts[offset:]
	if limit > len(contexts) {
		limit = len(contexts)
	}
	contexts = contexts[:limit]

	ret := ""
	switch mode {
	case "most_recent":
		ret = renderContexts(contexts[:1], true)
	case "sessions":
		ret = renderSessions(contexts)
	case "viewed":
		ret = renderViewed(contexts)
	case "summary":
		ret = renderContexts(contexts, false)
	default:
		return "unknown mode; valid: most_recent, sessions, viewed, summary", nil
	}
	return ret, nil
}

const desc = `Inspect recorded user contexts: recent context, session timestamps, and what was viewed during sessions. Use options to return only what you need.
Mode explanation:
	* sessions: Show session timestamps and event counts
	* most_recent: Show the most recent context entry
	* viewed: Show what was viewed per session
	* summary: Show recent context entries (default)`

func (t *userContextGetter) Specification() models.Specification {
	return models.Specification{
		Name:        "user_context_getter",
		Description: desc,
		Inputs: &models.InputSchema{
			Type:     "object",
			Required: make([]string, 0),
			Properties: map[string]models.ParameterObject{
				"mode": {
					Type:        "string",
					Description: "What to return: 'most_recent', 'sessions', 'viewed', or 'summary' (default).",
				},
				"limit": {
					Type:        "integer",
					Description: "Max number of contexts to return (default 5).",
				},
				"offset": {
					Type:        "integer",
					Description: "Offset into the ordered contexts (default 0).",
				},
				"session_id": {
					Type:        "string",
					Description: "Filter results to a specific session id (if present in stored context).",
				},
			},
		},
	}
}

func renderContexts(contexts []model.ClientContext, detailed bool) string {
	var b strings.Builder
	for i, c := range contexts {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(fmt.Sprintf("- session_id=%s, time=%s, am viewed: %v\n",
			c.SessionID,
			c.StartTime.Format(time.RFC3339),
			len(c.ViewingHistory)))
		if detailed {
			b.WriteString(fmt.Sprintf("  viewed: %s\n", strings.Join(ctxViewed(c), ", ")))
		}
	}
	return strings.TrimSpace(b.String())
}

func renderSessions(contexts []model.ClientContext) string {
	// Group by session id
	type sess struct {
		id    string
		start time.Time
		end   time.Time
		count int
	}
	m := map[string]*sess{}
	for _, c := range contexts {
		id := c.SessionID
		if id == "" {
			id = "<unknown>"
		}
		ts := c.StartTime
		s := m[id]
		if s == nil {
			s = &sess{id: id, start: ts, end: ts, count: 0}
			m[id] = s
		}
		if ts.Before(s.start) {
			s.start = ts
		}
		if ts.After(s.end) {
			s.end = ts
		}
		s.count++
	}

	sessions := make([]sess, 0, len(m))
	for _, v := range m {
		sessions = append(sessions, *v)
	}
	sort.SliceStable(sessions, func(i, j int) bool { return sessions[i].end.After(sessions[j].end) })

	var b strings.Builder
	b.WriteString("sessions:\n")
	for _, s := range sessions {
		b.WriteString(fmt.Sprintf("- id=%s, start=%s, end=%s, events=%d\n",
			s.id,
			s.start.Format(time.RFC3339),
			s.end.Format(time.RFC3339),
			s.count,
		))
	}
	return strings.TrimSpace(b.String())
}

func renderViewed(contexts []model.ClientContext) string {
	// Group by session id -> set of viewed ids/names
	m := map[string]map[string]struct{}{}
	for _, c := range contexts {
		id := c.SessionID
		if id == "" {
			id = "<unknown>"
		}
		set := m[id]
		if set == nil {
			set = map[string]struct{}{}
			m[id] = set
		}
		for _, v := range ctxViewed(c) {
			if v == "" {
				continue
			}
			set[v] = struct{}{}
		}
	}

	ids := make([]string, 0, len(m))
	for k := range m {
		ids = append(ids, k)
	}
	sort.Strings(ids)

	var b strings.Builder
	b.WriteString("viewed per session:\n")
	for _, sid := range ids {
		items := make([]string, 0, len(m[sid]))
		for it := range m[sid] {
			items = append(items, it)
		}
		sort.Strings(items)
		b.WriteString(fmt.Sprintf("- session=%s: %s\n", sid, strings.Join(items, ", ")))
	}
	return strings.TrimSpace(b.String())
}

func ctxViewed(c model.ClientContext) []string {
	ret := make([]string, 0)
	for _, vh := range c.ViewingHistory {
		ret = append(ret, vh.Name)
	}
	return ret
}
