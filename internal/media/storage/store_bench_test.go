package storage

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/model"
)

func writePersistedItem(t testing.TB, storeDir string, it model.Item) {
	t.Helper()
	p := filepath.Join(storeDir, it.ID) // matches store(): path.Join(s.storePath, i.ID)
	b, err := json.Marshal(it)
	if err != nil {
		t.Fatalf("marshal item: %v", err)
	}
	// Encoder in store() adds newline; not required, but keep it similar.
	b = append(b, '\n')
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatalf("write persisted item: %v", err)
	}
}

func makeDataset(t testing.TB, n int) (storeDir string) {
	t.Helper()
	root := t.TempDir()
	storeDir = filepath.Join(root, "store")
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		t.Fatalf("mkdir store: %v", err)
	}

	mediaDir := filepath.Join(root, "media")
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		t.Fatalf("mkdir media: %v", err)
	}

	for i := range n {
		underlying := filepath.Join(mediaDir, "f_"+strconv.Itoa(i))
		if err := os.WriteFile(underlying, []byte("x"), 0o644); err != nil {
			t.Fatalf("write underlying: %v", err)
		}

		// Minimal fields: Setup/loadPersistedItems uses ID and Path for sure.
		it := model.Item{
			ID:   "id_" + strconv.Itoa(i),
			Path: underlying,
			// Name/MIMEType/etc can be empty; decoder will fill zero-values.
		}
		writePersistedItem(t, storeDir, it)
	}
	return storeDir
}

// go test ./internal/media/storage -run 'BenchmarkStoreSetup_NoClassifier' -bench 'StoreSetup' -benchmem
func BenchmarkStoreSetup_NoClassifier(b *testing.B) {
	ctx := context.Background()

	ancli.Silent = true
	for _, n := range []int{10, 100, 1000, 5000, 50000} {
		b.Run("n="+strconv.Itoa(n), func(b *testing.B) {
			storeDir := makeDataset(b, n)

			b.ResetTimer()
			s := NewStore(
				WithStorePath(storeDir),
				WithClassifier(nil), // isolate filesystem + json decode
			)
			_, err := s.Setup(ctx)
			if err != nil {
				b.Fatal(err)
			}
		})
	}
}
