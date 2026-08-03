package theatre

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/baalimago/kinoview/internal/model"
)

// The permanent cast, as the director prompt names them: the four characters
// the player can draw, pinned by id so identity never drifts. Phase 9
// migrates the composer's own constants and these merge.
const (
	permanentIna    = "ina"    // the cat
	permanentFreija = "freija" // the dog
	permanentMouse  = "mouse1" // the mouse
	permanentPip    = "pip"    // the bird (phase 8 — perches above the rest)
)

// The wardrobe variants per species (phase 6): the coats the player can
// draw. The lists mirror the composer's palettes; phase 9 consolidates the
// copies when the composer migrates into the theatre. Coat names must pass
// the id pattern (^[a-z0-9_]{1,24}$) so they survive pin_identity and story
// validation — hence "bluetit", never "blue tit".
var (
	catVariants   = []string{"ginger", "grey", "cream", "tuxedo", "char", "siamese"}
	dogVariants   = []string{"tan", "cocoa", "cloud", "slate"}
	mouseVariants = []string{"field", "white"}
	birdVariants  = []string{"chaffinch", "bluetit", "robin", "sparrow"}
)

// permanentDefaults are the canonical looks of the permanent cast (decision
// D7, phase 6): species, canonical coat and wardrobe variants. Identity never
// drifts — a permanent id always carries its canonical coat, whatever the
// file or the draft says.
var permanentDefaults = map[string]CharacterEntry{
	permanentIna:    {ID: permanentIna, Species: "cat", Coat: "ginger", Variants: catVariants},
	permanentFreija: {ID: permanentFreija, Species: "dog", Coat: "tan", Variants: dogVariants},
	permanentMouse:  {ID: permanentMouse, Species: "mouse", Coat: "field", Variants: mouseVariants},
	permanentPip:    {ID: permanentPip, Species: "bird", Coat: "chaffinch", Variants: birdVariants},
}

// Registry is the costumer's book (decision D7): the durable character
// registry behind pin_identity. A registered id is always stamped with its
// canonical coat and species, whatever the draft says — no LLM output can
// drift identity. An unregistered id is left as-is: the LLM's own coat may
// stand for a guest, and a guest becomes a character only by explicit
// director approval at submit (Canonize). Phase 6 makes the book durable:
// it round-trips through registry.json and survives restarts.
type Registry struct {
	mu      sync.Mutex
	entries map[string]CharacterEntry
}

// newRegistry builds the registry with the permanent cast's canonical
// defaults.
func newRegistry() *Registry {
	entries := make(map[string]CharacterEntry, len(permanentDefaults))
	maps.Copy(entries, permanentDefaults)
	return &Registry{entries: entries}
}

// PinAndApply stamps the canonical look onto every registered cast member:
// coat and character come from the book, never from the draft. It reports
// how many looks were applied; an unregistered id is a guest and is left
// alone (the error table).
func (r *Registry) PinAndApply(cast []model.Cast) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	applied := 0
	for i := range cast {
		entry, ok := r.entries[cast[i].ID]
		if !ok {
			continue
		}
		cast[i].Coat = entry.Coat
		cast[i].Character = entry.Species
		applied++
	}
	return applied
}

// Lookup returns the book's entry for an id as the look the player renders.
func (r *Registry) Lookup(id string) (model.Cast, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	if !ok {
		return model.Cast{}, false
	}
	return model.Cast{ID: e.ID, Character: e.Species, Coat: e.Coat, Lane: 0, X: 0.5, Scale: 1}, true
}

// Known reports whether id is a registered character.
func (r *Registry) Known(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.entries[id]
	return ok
}

// IDs lists the registered character ids, sorted for stable rendering.
func (r *Registry) IDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.entries))
	for id := range r.entries {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Size reports how many characters the registry knows.
func (r *Registry) Size() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}

// Variants lists the coats a character may wear.
func (r *Registry) Variants(id string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	if !ok {
		return nil
	}
	return e.Variants
}

// Canonize approves newly named characters for the book — the only place
// identities are born (decision D7): each entry must name a valid species
// and a coat the player can draw, and the id must appear in the draft's
// cast — a canonized character is a character the audience actually saw.
// The permanent cast is already in the book and is never re-approved.
// Refusals come back as messages for the director.
func (r *Registry) Canonize(draft []CharacterEntry, castIDs []string) (added int, refused []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	onStage := make(map[string]bool, len(castIDs))
	for _, id := range castIDs {
		onStage[id] = true
	}
	for _, e := range draft {
		e.ID = strings.ToLower(strings.TrimSpace(e.ID))
		e.Species = strings.ToLower(strings.TrimSpace(e.Species))
		e.Coat = strings.ToLower(strings.TrimSpace(e.Coat))
		_, already := r.entries[e.ID]
		switch {
		case e.ID == "" || !artifactIDRe.MatchString(e.ID):
			refused = append(refused, fmt.Sprintf("%q: id does not match the id pattern", e.ID))
		case !onStage[e.ID]:
			refused = append(refused, fmt.Sprintf("%q: not in the draft cast", e.ID))
		case permanentDefaults[e.ID].ID != "":
			refused = append(refused, fmt.Sprintf("%q: already a permanent cast member", e.ID))
		case already:
			refused = append(refused, fmt.Sprintf("%q: already in the registry", e.ID))
		case !model.ValidCharacters[e.Species]:
			refused = append(refused, fmt.Sprintf("%q: unknown species %q", e.ID, e.Species))
		case !slices.Contains(speciesVariants(e.Species), e.Coat):
			refused = append(refused, fmt.Sprintf("%q: coat %q is not a %s variant", e.ID, e.Coat, e.Species))
		case len(r.entries) >= registryMax:
			refused = append(refused, fmt.Sprintf("%q: registry full (%d)", e.ID, registryMax))
		default:
			e.Variants = speciesVariants(e.Species)
			r.entries[e.ID] = e
			added++
		}
	}
	return added, refused
}

// Doc returns the book's durable form — the registry.json document, sorted
// by id for stable writes.
func (r *Registry) Doc() RegistryDoc {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(RegistryDoc, 0, len(r.entries))
	for id, e := range r.entries {
		e.ID = id
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// LoadDoc merges a loaded registry document into the book at startup. The
// permanent cast keeps its canonical defaults — a file never moves ina off
// ginger — and characters canonized in an earlier generation survive the
// restart. A corrupt file never reaches here: the loader degrades it to the
// empty document.
func (r *Registry) LoadDoc(d RegistryDoc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range trimRegistry(d) {
		if seed, ok := permanentDefaults[e.ID]; ok {
			r.entries[e.ID] = seed
			continue
		}
		r.entries[e.ID] = e
	}
}

// speciesVariants returns the coats a species can wear — the palettes the
// player can draw.
func speciesVariants(species string) []string {
	switch species {
	case "cat":
		return catVariants
	case "dog":
		return dogVariants
	case "mouse":
		return mouseVariants
	case "bird":
		return birdVariants
	}
	return nil
}

// filterVariants keeps the coats a species can draw, in order, deduped and
// capped at the registry's variant bound — the palette check Canonize runs at
// approval, applied to anything loaded from disk (review 3, R3-04).
func filterVariants(variants, palette []string) []string {
	out := make([]string, 0, variantCap)
	seen := map[string]bool{}
	for _, v := range variants {
		if len(out) >= variantCap {
			break
		}
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" || !slices.Contains(palette, v) || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
