package theatre

import (
	"math/rand"
	"testing"
)

// DressDraft is the scenographer's deterministic floor: it dresses a draft's
// set around wherever the cast stands. The draft's backdrop is kept when
// valid, and a clear piece never shares a column with a performer — the
// staging rules staging_test.go exists to protect.
func TestDressDraft_KeepsBackdropAndRespectsStaging(t *testing.T) {
	t.Parallel()
	clearPieces := map[string]bool{"bush": true, "fence": true, "sofa": true, "plant": true, "log": true}
	for seed := range int64(200) {
		r := rand.New(rand.NewSource(seed))
		s := Compose(r)
		scene := DressDraft(rand.New(rand.NewSource(seed+1000)), s)

		if scene.Backdrop != s.Scene.Backdrop {
			t.Fatalf("seed %d: backdrop = %q, want the draft's %q kept", seed, scene.Backdrop, s.Scene.Backdrop)
		}
		// A clear piece goes into a column nobody occupies (or neighbours).
		occ := occupiedCols(planFromCast(s.Cast))
		for _, c := range scene.Cells {
			if clearPieces[c.Piece] && occ[c.Col] {
				t.Fatalf("seed %d: %s at column %d stands through a performer", seed, c.Piece, c.Col)
			}
		}
	}
}

// An invalid backdrop on the draft degrades to the default — a naming slip
// must not stop the floor from dressing.
func TestDressDraft_InvalidBackdropDefaults(t *testing.T) {
	t.Parallel()
	s := Compose(rand.New(rand.NewSource(1)))
	s.Scene.Backdrop = "bogus"
	scene := DressDraft(rand.New(rand.NewSource(2)), s)
	if scene.Backdrop == "bogus" {
		t.Error("invalid backdrop survived the floor")
	}
}
