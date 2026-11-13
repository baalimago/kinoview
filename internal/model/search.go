package model

import (
	"encoding/json"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Weights for the fields a query token can match. A token that hits the file
// name outranks one that only hits a description, which is what makes the top
// of a ranked result list the item the user asked for.
const (
	scoreExact       = 1000
	scoreNamePrefix  = 200
	scoreName        = 100
	scoreMetaTitle   = 80
	scoreMetaSeries  = 40
	scorePath        = 20
	scoreDescription = 10
)

// metadataFieldWeights maps the classifier's metadata keys to the weight of a
// match inside them. Keys not listed fall back to scoreDescription.
var metadataFieldWeights = map[string]int{
	"name":     scoreMetaTitle,
	"title":    scoreMetaTitle,
	"alt_name": scoreMetaTitle,
	"showName": scoreMetaSeries,
	"genre":    scoreMetaSeries,
	"actors":   scoreMetaSeries,
	"director": scoreMetaSeries,
}

// MatchScore reports how well an item answers the query. Zero means the item
// does not match. Every query token must match at least one field, so a
// multi-word query narrows the result set instead of widening it.
func MatchScore(it Item, query string) int {
	tokens := queryTokens(query)
	if len(tokens) == 0 {
		return 0
	}
	stem := normalizeSearchText(strings.TrimSuffix(it.Name, fileExt(it.Name)))
	name := normalizeSearchText(it.Name)
	path := normalizeSearchText(it.Path)
	metadata := flattenMetadata(it)

	total := 0
	for _, token := range tokens {
		score := tokenScore(token, name, stem, path, metadata)
		if score == 0 {
			return 0
		}
		total += score
	}
	return total
}

// tokenScore is the best weight a single token reaches across all fields.
func tokenScore(token, name, stem, path string, metadata metadataFields) int {
	if stem == token {
		return scoreExact
	}
	best := 0
	if strings.HasPrefix(stem, token) {
		best = scoreNamePrefix
	} else if strings.Contains(name, token) {
		best = scoreName
	}
	if score := metadata.score(token); score > best {
		best = score
	}
	if strings.Contains(path, token) && best < scorePath {
		best = scorePath
	}
	return best
}

// MatchesGlobalSearch reports whether the item matches the query at all. An
// empty or blank query matches everything.
func MatchesGlobalSearch(it Item, needle string) bool {
	if strings.TrimSpace(needle) == "" {
		return true
	}
	return MatchScore(it, needle) > 0
}

// RankItems returns the matching items ordered by relevance: best score first,
// then name, then ID so equal scores keep a stable order.
func RankItems(items []Item, query string) []Item {
	scored := make([]scoredItem, 0, len(items))
	for _, it := range items {
		score := MatchScore(it, query)
		if score == 0 {
			continue
		}
		scored = append(scored, scoredItem{item: it, score: score})
	}
	sort.SliceStable(scored, func(a, b int) bool {
		if scored[a].score != scored[b].score {
			return scored[a].score > scored[b].score
		}
		left, right := strings.ToLower(scored[a].item.Name), strings.ToLower(scored[b].item.Name)
		if left != right {
			return left < right
		}
		return scored[a].item.ID < scored[b].item.ID
	})
	ranked := make([]Item, 0, len(scored))
	for _, s := range scored {
		ranked = append(ranked, s.item)
	}
	return ranked
}

// OrderItems orders the items for listing: relevance when a query is given,
// ID order otherwise. It never reorders the caller's slice.
func OrderItems(items []Item, query string) []Item {
	if len(queryTokens(query)) > 0 {
		return RankItems(items, query)
	}
	ordered := slices.Clone(items)
	sort.SliceStable(ordered, func(a, b int) bool { return ordered[a].ID < ordered[b].ID })
	return ordered
}

type scoredItem struct {
	item  Item
	score int
}

// metadataFields is the flattened metadata of one item: every leaf value with
// the metadata key it came from.
type metadataFields []metadataField

type metadataField struct {
	key   string
	value string
}

// score is the best weight any field reaches for the token.
func (m metadataFields) score(token string) int {
	best := 0
	for _, field := range m {
		if !strings.Contains(field.value, token) {
			continue
		}
		weight, ok := metadataFieldWeights[field.key]
		if !ok {
			weight = scoreDescription
		}
		if weight > best {
			best = weight
		}
	}
	return best
}

// flattenMetadata collects the item's metadata as lowercase text per key.
func flattenMetadata(it Item) metadataFields {
	if it.Metadata == nil {
		return nil
	}
	var decoded map[string]any
	if err := json.Unmarshal(*it.Metadata, &decoded); err != nil {
		return nil
	}
	fields := metadataFields{}
	collectMetadataFields("", decoded, &fields)
	return fields
}

func collectMetadataFields(key string, value any, fields *metadataFields) {
	switch v := value.(type) {
	case map[string]any:
		for k, child := range v {
			collectMetadataFields(k, child, fields)
		}
	case []any:
		for _, child := range v {
			collectMetadataFields(key, child, fields)
		}
	case string:
		if v != "" {
			*fields = append(*fields, metadataField{key: key, value: normalizeSearchText(v)})
		}
	case float64:
		*fields = append(*fields, metadataField{key: key, value: formatNumber(v)})
	}
}

// formatNumber renders a JSON number without its exponent, so a year in
// metadata matches the same digits the user types.
func formatNumber(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// fileExt is the extension of the file name, including the leading dot.
func fileExt(name string) string {
	idx := strings.LastIndexByte(name, '.')
	if idx < 0 {
		return ""
	}
	return name[idx:]
}

// queryTokens splits the query into the tokens every matching item must
// answer, in the same normalised form as the fields.
func queryTokens(query string) []string {
	return strings.Fields(normalizeSearchText(query))
}

// normalizeSearchText puts a query and every field it is compared against into
// one form: lower case, punctuation dropped. Punctuation therefore never stands
// between a query and the field it names, so "oceans eleven" finds
// "Ocean's Eleven" and "sg1" finds "Stargate SG-1".
func normalizeSearchText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
