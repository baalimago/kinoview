package theatre

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/baalimago/kinoview/internal/model"
)

// The registry defaults match the table (phase 6 + phase 8): the permanent
// cast carries its canonical coat, species and wardrobe variants.
func TestRegistry_CanonicalDefaultsMatchTable(t *testing.T) {
	t.Parallel()
	reg := newRegistry()
	want := []struct{ id, species, coat string }{
		{"ina", "cat", "ginger"},
		{"freija", "dog", "tan"},
		{"mouse1", "mouse", "field"},
		{"pip", "bird", "chaffinch"},
	}
	for _, tt := range want {
		look, ok := reg.Lookup(tt.id)
		if !ok || look.Character != tt.species || look.Coat != tt.coat {
			t.Errorf("%s = %+v, want %s/%s", tt.id, look, tt.species, tt.coat)
		}
	}
	for id, variants := range map[string][]string{
		"ina":    catVariants,
		"freija": dogVariants,
		"mouse1": mouseVariants,
		"pip":    birdVariants,
	} {
		if got := reg.Variants(id); !equalStrings(got, variants) {
			t.Errorf("%s variants = %v, want %v", id, got, variants)
		}
	}
}

// pin_identity output is stable across 400 seeds and across drafts that omit
// or misstate coats: every registered id comes out with its canonical coat
// and species, whatever the draft said (acceptance criterion).
func TestRegistry_PinStableAcrossSeedsAndMisstatedCoats(t *testing.T) {
	t.Parallel()
	reg := newRegistry()
	want := map[string]struct{ species, coat string }{
		"ina":    {"cat", "ginger"},
		"freija": {"dog", "tan"},
		"mouse1": {"mouse", "field"},
		"pip":    {"bird", "chaffinch"},
	}
	rnd := rand.New(rand.NewSource(1))
	for seed := range 400 {
		cast := make([]model.Cast, 0, 4)
		for _, id := range reg.IDs() {
			coat := ""
			if rnd.Intn(2) == 0 {
				// Misstate the coat: the pin must override it.
				coat = "misstated-" + string(rune('a'+rnd.Intn(4)))
			}
			cast = append(cast, model.Cast{ID: id, Character: "wrong", Coat: coat, Lane: 0, X: 0.5})
		}
		if applied := reg.PinAndApply(cast); applied != 4 {
			t.Fatalf("seed %d: applied %d, want 4", seed, applied)
		}
		for _, c := range cast {
			w := want[c.ID]
			if c.Character != w.species || c.Coat != w.coat {
				t.Fatalf("seed %d: %s = %s/%s, want %s/%s", seed, c.ID, c.Character, c.Coat, w.species, w.coat)
			}
		}
	}
}

// pin_identity on an unregistered id leaves it as-is: the LLM's own coat may
// stand for a guest, and nothing crashes (error table). A guest never enters
// the registry without approval.
func TestRegistry_UnregisteredIdLeftAsIs(t *testing.T) {
	t.Parallel()
	reg := newRegistry()
	cast := []model.Cast{{ID: "guest1", Character: "cat", Coat: "silver", Lane: 0, X: 0.5}}
	if applied := reg.PinAndApply(cast); applied != 0 {
		t.Errorf("applied = %d, want 0 for a guest", applied)
	}
	if cast[0].Coat != "silver" || cast[0].Character != "cat" {
		t.Errorf("guest look was stamped: %+v", cast[0])
	}
	if reg.Known("guest1") {
		t.Error("a guest entered the registry without approval")
	}
}

// New characters enter the registry only by explicit director approval at
// submit: valid entries are added with their species' variants, invalid ones
// are refused with a reason, and the permanent cast is never re-approved.
func TestRegistry_CanonizeApprovesOnlyValid(t *testing.T) {
	t.Parallel()
	reg := newRegistry()
	draft := []CharacterEntry{
		{ID: "mouse2", Species: "mouse", Coat: "white"},
		{ID: "rook", Species: "bird", Coat: "robin"},         // the first guest bird
		{ID: "bad id", Species: "mouse", Coat: "white"},      // id fails the pattern
		{ID: "dragon", Species: "dragon", Coat: "red"},       // unknown species
		{ID: "ghost", Species: "cat", Coat: "ginger"},        // not in the draft cast
		{ID: "ina", Species: "cat", Coat: "grey"},            // permanent cast
		{ID: "mouse2", Species: "mouse", Coat: "white"},      // duplicate
		{ID: "bluecat", Species: "cat", Coat: "ultramarine"}, // coat not a variant
	}
	added, refused := reg.Canonize(draft, []string{"mouse2", "rook", "ina", "bluecat"})
	if added != 2 {
		t.Errorf("added = %d, want 2", added)
	}
	if len(refused) != 6 {
		t.Fatalf("refused = %v, want 6 refusals", refused)
	}
	if !reg.Known("mouse2") || !reg.Known("rook") || reg.Known("dragon") || reg.Known("ghost") || reg.Known("bluecat") {
		t.Errorf("registry = %v, want mouse2 and rook added only", reg.IDs())
	}
	if got := reg.Variants("mouse2"); !equalStrings(got, mouseVariants) {
		t.Errorf("mouse2 variants = %v, want the species' variants", got)
	}
	if got := reg.Variants("rook"); !equalStrings(got, birdVariants) {
		t.Errorf("rook variants = %v, want the bird's variants", got)
	}

	// The registry round-trips through its doc: a fresh book over the doc
	// keeps canonized characters and the permanent cast's canonical coats.
	reloaded := newRegistry()
	reloaded.LoadDoc(reg.Doc())
	if look, ok := reloaded.Lookup("mouse2"); !ok || look.Coat != "white" {
		t.Errorf("reloaded mouse2 = %+v, want white", look)
	}
	if look, ok := reloaded.Lookup("rook"); !ok || look.Coat != "robin" {
		t.Errorf("reloaded rook = %+v, want robin", look)
	}
	if look, ok := reloaded.Lookup("ina"); !ok || look.Coat != "ginger" {
		t.Errorf("reloaded ina = %+v, want the canonical ginger", look)
	}
	if look, ok := reloaded.Lookup("pip"); !ok || look.Coat != "chaffinch" {
		t.Errorf("reloaded pip = %+v, want the canonical chaffinch", look)
	}
}

// A registry doc with hostile content is repaired on load: the permanent
// cast keeps its canonical defaults, unknown species are dropped, and a
// canonized character survives.
func TestRegistry_LoadDocRepairsHostileFile(t *testing.T) {
	t.Parallel()
	reg := newRegistry()
	doc := RegistryDoc{
		{ID: "ina", Species: "dragon", Coat: "black"},    // hostile override of a permanent id
		{ID: "mouse2", Species: "mouse", Coat: "white"},  // legit canonization
		{ID: "bogus id", Species: "cat", Coat: "ginger"}, // bad id dropped
	}
	reg.LoadDoc(doc)
	if look, _ := reg.Lookup("ina"); look.Coat != "ginger" || look.Character != "cat" {
		t.Errorf("ina = %+v, want the canonical defaults — a file never moves ina off ginger", look)
	}
	if !reg.Known("mouse2") || reg.Known("bogus id") {
		t.Errorf("registry = %v, want mouse2 only", reg.IDs())
	}
}

// R3-04: a hand-edited variant list is filtered through the species palette
// at load — load and canonize agree on what a coat may be, so an
// out-of-palette coat can never surface in a working context or a wardrobe
// answer.
func TestRegistry_LoadDocDropsOutOfPaletteVariants(t *testing.T) {
	t.Parallel()
	reg := newRegistry()
	reg.LoadDoc(RegistryDoc{
		{ID: "guest1", Species: "cat", Coat: "ginger", Variants: []string{"ginger", "pink", "GREY", "pink", "ultramarine"}},
	})
	if got := reg.Variants("guest1"); !equalStrings(got, []string{"ginger", "grey"}) {
		t.Errorf("guest1 variants = %v, want the palette coats only (ginger, grey)", got)
	}
	// The coat itself is palette-checked too: an out-of-palette pin degrades
	// to unpinned rather than surviving the load gate.
	reg.LoadDoc(RegistryDoc{
		{ID: "guest2", Species: "bird", Coat: "macaw", Variants: []string{"macaw"}},
	})
	if look, ok := reg.Lookup("guest2"); !ok || look.Coat != "" {
		t.Errorf("guest2 = %+v, want the out-of-palette coat dropped", look)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The registry doc is sorted by id, so writes are stable and diffable.
func TestRegistry_DocSorted(t *testing.T) {
	t.Parallel()
	reg := newRegistry()
	reg.Canonize([]CharacterEntry{{ID: "mouse2", Species: "mouse", Coat: "white"}}, []string{"mouse2"})
	doc := reg.Doc()
	for i := 1; i < len(doc); i++ {
		if strings.Compare(doc[i-1].ID, doc[i].ID) > 0 {
			t.Errorf("doc out of order: %v", doc)
		}
	}
}
