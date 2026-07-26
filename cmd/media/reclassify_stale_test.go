package media

import (
	"context"
	"fmt"
	"testing"

	"github.com/baalimago/kinoview/internal/model"
)

type fakeStaleStore struct {
	items       []model.Item
	maxAttempts int
	cleared     []string
	failOn      map[string]bool
}

func (f *fakeStaleStore) Snapshot() []model.Item         { return f.items }
func (f *fakeStaleStore) ClassificationMaxAttempts() int { return f.maxAttempts }

func (f *fakeStaleStore) ClearClassificationStopLoss(id string) (bool, error) {
	if f.failOn[id] {
		return false, fmt.Errorf("cannot write %v", id)
	}
	for _, i := range f.items {
		if i.ID == id && i.ClassificationAttempts >= f.maxAttempts {
			f.cleared = append(f.cleared, id)
			return true, nil
		}
	}
	return false, nil
}

func fixtureItems() []model.Item {
	return []model.Item{
		{ID: "a", Name: "blocked.mkv", ClassificationAttempts: 5, ClassificationError: "rate limited"},
		{ID: "b", Name: "way-over.mkv", ClassificationAttempts: 12},
		{ID: "c", Name: "retrying.mkv", ClassificationAttempts: 2},
		{ID: "d", Name: "fine.mkv", ClassificationAttempts: 0},
	}
}

func TestStaleItems_OnlyAtOrOverTheCeiling(t *testing.T) {
	got := staleItems(fixtureItems(), 5)
	if len(got) != 2 {
		t.Fatalf("expected 2 stale items, got %d: %+v", len(got), got)
	}
	for _, i := range got {
		if i.ClassificationAttempts < 5 {
			t.Errorf("item below the ceiling was treated as stale: %+v", i)
		}
	}
}

func TestStaleItems_NoneWhenAllHealthy(t *testing.T) {
	items := []model.Item{{ID: "a", ClassificationAttempts: 1}}
	if got := staleItems(items, 5); len(got) != 0 {
		t.Errorf("expected nothing stale, got %+v", got)
	}
}

func runStale(t *testing.T, c *reclassifyStaleCmd) error {
	t.Helper()
	// A store path that exists, so Run gets past its existence check; the
	// injected fake store means nothing is read from it.
	c.storePath = t.TempDir()
	return c.Run(context.Background())
}

func TestReclassifyStale_ResetsOnlyBlockedItems(t *testing.T) {
	fake := &fakeStaleStore{items: fixtureItems(), maxAttempts: 5}
	c := &reclassifyStaleCmd{store: fake, force: true}

	if err := runStale(t, c); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fake.cleared) != 2 {
		t.Fatalf("cleared %v, want the two blocked items", fake.cleared)
	}
	for _, id := range fake.cleared {
		if id == "c" || id == "d" {
			t.Errorf("cleared a healthy item: %v", id)
		}
	}
}

// A dry run must be exactly that.
func TestReclassifyStale_DryRunChangesNothing(t *testing.T) {
	fake := &fakeStaleStore{items: fixtureItems(), maxAttempts: 5}
	c := &reclassifyStaleCmd{store: fake, dryRun: true, force: true}

	if err := runStale(t, c); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fake.cleared) != 0 {
		t.Errorf("dry run modified items: %v", fake.cleared)
	}
}

func TestReclassifyStale_NothingBlocked(t *testing.T) {
	fake := &fakeStaleStore{
		items:       []model.Item{{ID: "a", Name: "fine.mkv", ClassificationAttempts: 1}},
		maxAttempts: 5,
	}
	c := &reclassifyStaleCmd{store: fake, force: true}

	if err := runStale(t, c); err != nil {
		t.Fatalf("Run should succeed with nothing to do: %v", err)
	}
	if len(fake.cleared) != 0 {
		t.Errorf("cleared %v with nothing blocked", fake.cleared)
	}
}

// One unwritable item must not strand the others.
func TestReclassifyStale_ContinuesPastFailures(t *testing.T) {
	fake := &fakeStaleStore{
		items:       fixtureItems(),
		maxAttempts: 5,
		failOn:      map[string]bool{"a": true},
	}
	c := &reclassifyStaleCmd{store: fake, force: true}

	err := runStale(t, c)
	if err == nil {
		t.Error("expected Run to report that an item could not be reset")
	}
	if len(fake.cleared) != 1 || fake.cleared[0] != "b" {
		t.Errorf("cleared %v, want the other blocked item to still be handled", fake.cleared)
	}
}

func TestReclassifyStale_MissingStorePath(t *testing.T) {
	c := &reclassifyStaleCmd{storePath: "/definitely/not/here", force: true}
	if err := c.Run(context.Background()); err == nil {
		t.Error("expected an error for a nonexistent store path")
	}
}
