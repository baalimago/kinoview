package tools

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/baalimago/clai/pkg/text/models"
	"github.com/baalimago/kinoview/internal/agents"
	"github.com/baalimago/kinoview/internal/model"
)

type userContextGetter struct {
	mgr agents.UserContextManager
}

func NewUserContextGetter(mgr agents.UserContextManager) (*userContextGetter, error) {
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
		return ctxTime(contexts[i]).After(ctxTime(contexts[j]))
	})

	if sessionID != "" {
		filtered := make([]model.ClientContext, 0, len(contexts))
		for _, c := range contexts {
			if ctxSessionID(c) == sessionID {
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

	switch mode {
	case "most_recent":
		return renderContexts(contexts[:1], true), nil
	case "sessions":
		return renderSessions(contexts), nil
	case "viewed":
		return renderViewed(contexts), nil
	case "summary":
		return renderContexts(contexts, false), nil
	default:
		return "unknown mode; valid: most_recent, sessions, viewed, summary", nil
	}
}

func (t *userContextGetter) Specification() models.Specification {
	return models.Specification{
		Name:        "user_context_getter",
		Description: "Inspect recorded user contexts: recent context, session timestamps, and what was viewed during sessions. Use options to return only what you need.",
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
		b.WriteString(fmt.Sprintf("- session_id=%s, time=%s\n", safe(ctxSessionID(c)), ctxTime(c).Format(time.RFC3339)))
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
		id := ctxSessionID(c)
		if id == "" {
			id = "<unknown>"
		}
		ts := ctxTime(c)
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
		id := ctxSessionID(c)
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

func safe(s string) string {
	if s == "" {
		return "<unknown>"
	}
	return s
}

// The user context model may evolve; use defensive reflective discovery of fields.
// We only use stdlib; no json tags assumptions.

func ctxSessionID(c model.ClientContext) string {
	// Common field names
	if v, ok := getStringField(c, "SessionID"); ok {
		return v
	}
	if v, ok := getStringField(c, "SessionId"); ok {
		return v
	}
	if v, ok := getStringField(c, "ID"); ok {
		return v
	}
	return ""
}

func ctxViewed(c model.ClientContext) []string {
	// Try common slice fields
	if v, ok := getStringSliceField(c, "Viewed"); ok {
		return v
	}
	if v, ok := getStringSliceField(c, "ViewedIDs"); ok {
		return v
	}
	if v, ok := getStringSliceField(c, "ViewedItems"); ok {
		return v
	}
	if v, ok := getStringSliceField(c, "Items"); ok {
		return v
	}
	return nil
}

func ctxTime(c model.ClientContext) time.Time {
	if v, ok := getTimeField(c, "Time"); ok {
		return v
	}
	if v, ok := getTimeField(c, "Timestamp"); ok {
		return v
	}
	if v, ok := getTimeField(c, "CreatedAt"); ok {
		return v
	}
	if v, ok := getTimeField(c, "UpdatedAt"); ok {
		return v
	}
	// Zero if unknown
	return time.Time{}
}

// reflection helpers

func getStringField(v any, name string) (string, bool) {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if !rv.IsValid() || rv.Kind() != reflect.Struct {
		return "", false
	}
	f := rv.FieldByName(name)
	if !f.IsValid() || f.Kind() != reflect.String {
		return "", false
	}
	return f.String(), true
}

func getStringSliceField(v any, name string) ([]string, bool) {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if !rv.IsValid() || rv.Kind() != reflect.Struct {
		return nil, false
	}
	f := rv.FieldByName(name)
	if !f.IsValid() || f.Kind() != reflect.Slice {
		return nil, false
	}
	if f.Type().Elem().Kind() != reflect.String {
		return nil, false
	}
	out := make([]string, f.Len())
	for i := 0; i < f.Len(); i++ {
		out[i] = f.Index(i).String()
	}
	return out, true
}

func getTimeField(v any, name string) (time.Time, bool) {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if !rv.IsValid() || rv.Kind() != reflect.Struct {
		return time.Time{}, false
	}
	f := rv.FieldByName(name)
	if !f.IsValid() {
		return time.Time{}, false
	}
	if f.Type() == reflect.TypeOf(time.Time{}) {
		return f.Interface().(time.Time), true
	}
	return time.Time{}, false
}
