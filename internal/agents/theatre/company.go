// Package theatre is the company that produces the intro splash story: a
// director superagent orchestrating mini-agent subagents over a shared
// production board. This file holds the persistent substrate — the board, the
// working file, the ledger and the transcript — that the agents run on.
package theatre

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
)

// CompanyDir is the subdirectory of the cache dir that holds every piece of
// the theatre's on-disk paperwork: the board, the working file, the ledger and
// the transcript. The theatre runs the company; the company's files live in
// the company directory. The intro story cache file stays at the cache root,
// so a pre-migration cache still loads.
const CompanyDir = "intro/company"

// Company file names, relative to CompanyDir.
const (
	boardFileName      = "board.json"
	workingFileName    = "working.json"
	ledgerFileName     = "ledger.json"
	transcriptFileName = "transcript.jsonl"

	// Phase 6: the company's durable memory — one document per file, all
	// atomic-written, all validated on load, all trimmed to their caps.
	premisesFileName   = "premises.json"
	repertoireFileName = "repertoire.json"
	setsFileName       = "sets.json"
	registryFileName   = "registry.json"
	directorFileName   = "director.json"
	bulletinFileName   = "bulletin.json"
	audienceFileName   = "audience.json"
)

// Company is the persistent paperwork of a theatre production. Every file
// lives under <cacheDir>/intro/company/ and is written atomically; a reader
// can never observe a torn document, and a corrupt document degrades to an
// empty one rather than crashing the production.
//
// One mutex serialises every read and write. Writes are already atomic on disk
// (temp file + rename); the mutex additionally keeps concurrent in-process
// writers from interleaving, mirroring the theatre's saveStory pattern.
type Company struct {
	dir string
	mu  sync.Mutex
}

// Open returns a Company rooted at the given cache dir. Nothing is written
// until the first Save; the directory tree is created on demand.
func Open(cacheDir string) *Company {
	return &Company{dir: filepath.Join(cacheDir, CompanyDir)}
}

func (c *Company) boardPath() string      { return filepath.Join(c.dir, boardFileName) }
func (c *Company) workingPath() string    { return filepath.Join(c.dir, workingFileName) }
func (c *Company) ledgerPath() string     { return filepath.Join(c.dir, ledgerFileName) }
func (c *Company) transcriptPath() string { return filepath.Join(c.dir, transcriptFileName) }

// readJSON reads and unmarshals path into v. A missing file is not an error:
// it means nothing has been produced yet, which every caller treats as the
// empty document.
func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return json.Unmarshal(b, v)
}

// logLoadFailure reports a document that could not be loaded. Loads have a
// built-in fallback (the empty document), so the error is diagnostic: it is
// logged here and returned for tests and for callers that want to distinguish
// "nothing yet" from "corrupt". Saves, by contrast, return their errors
// unlogged — there is no fallback, and the caller must act on them.
func logLoadFailure(what string, err error) {
	ancli.Errf("theatre: %s unreadable: %v", what, err)
}
