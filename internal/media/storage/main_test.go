package storage

import (
	"os"
	"testing"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
)

func TestMain(m *testing.M) {
	ancli.Silent = true
	os.Exit(m.Run())
}
