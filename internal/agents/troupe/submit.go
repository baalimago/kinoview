package troupe

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/baalimago/clai/pkg/text/models"
)

// ErrAlreadySubmitted refuses a second submit of the same play: the resolved
// served artifact is already durably on disk. The director authors a fresh
// play id instead — each play is a new UTC datetime, never a rewrite.
var ErrAlreadySubmitted = errors.New("troupe: submit refused: play already submitted")

// PlayIndexEntry is one plays/index.json metadata object. The paginated play
// API reads this list, never the play files (D-28). The id is a story_<UTC>
// datetime id, lexicographically sortable newest-first.
type PlayIndexEntry struct {
	ID         string `json:"id"`
	Status     Status `json:"status"`
	Author     string `json:"author"`
	Provenance string `json:"provenance"`
	Created    string `json:"created"`
}

// PlayIndex is the plays/ metadata index: the newest-first entry list the
// play API pages over.
type PlayIndex struct {
	Index []PlayIndexEntry `json:"index"`
}

// Submitter is the single-writer persistence boundary: the director alone
// submits the play. It reads the materialised notebook worktree, validates
// and resolves the named play through the phase-3 resolver, stamps it
// submitted and durably persists it — the resolved play under
// plays/story_<UTC>.json, then a metadata entry in plays/index.json. The
// order is fixed: write the play file first, append the index entry after,
// so a crash can never leave an index entry pointing at a missing play.
// Old plays are kept, never overwritten: each play id is a fresh UTC
// datetime and a second submit of an id is refused.
type Submitter struct {
	mu       sync.Mutex // the single writer: one submit at a time
	worktree string     // the materialised notebook worktree
}

// NewSubmitter builds the play gate over a materialised notebook worktree —
// the same directory the agents pull and edit, and the one the director
// writes the authored play into.
func NewSubmitter(worktree string) (*Submitter, error) {
	if worktree == "" {
		return nil, errors.New("troupe: submitter: worktree can't be empty")
	}
	return &Submitter{worktree: worktree}, nil
}

// Submit validates and resolves the named play from the worktree, stamps it
// submitted and durably persists it. Exact resolver errors return wherever
// they manifest — a missing model, a filename/envelope mismatch, a
// conflict-marked file, a budget overrun — and nothing is written. A second
// submit of the same play id is refused: the resolved play is already on
// disk, so the refusal rewrites nothing. A persist failure aborts with the
// atomic-write error; the paper trail never claims a success the disk did
// not record.
func (s *Submitter) Submit(playID string) (ResolvedPlay, error) {
	if !playIDRe.MatchString(playID) {
		return ResolvedPlay{}, fmt.Errorf("troupe: submit: play id %q must match story_YYYYMMDDTHHMMSSZ", playID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// A second submit is refused: a resolved play at the id's path is the
	// served artifact, and only submit_play writes that shape. The refusal
	// precedes any work — a resubmit rewrites nothing.
	playPath := filepath.Join(s.worktree, "plays", playID+".json")
	if data, err := os.ReadFile(playPath); err == nil && isResolvedPlay(data) {
		return ResolvedPlay{}, fmt.Errorf("troupe: submit %s: %w", playID, ErrAlreadySubmitted)
	}

	snap, err := snapshotFromWorktree(s.worktree)
	if err != nil {
		return ResolvedPlay{}, fmt.Errorf("troupe: submit %s: materialise worktree: %w", playID, err)
	}
	rp, err := ResolvePlay(snap, playID)
	if err != nil {
		return ResolvedPlay{}, fmt.Errorf("troupe: submit %s: %w", playID, err)
	}

	// Submission is a persistence boundary: the status flips only in the
	// durable copy, and the index entry is appended only after the resolved
	// play is durably on disk.
	rp.Play.Status = StatusSubmitted
	data, err := json.Marshal(rp)
	if err != nil {
		return ResolvedPlay{}, fmt.Errorf("troupe: submit %s: marshal resolved play: %w", playID, err)
	}
	if err := writeFileAtomic(playPath, data); err != nil {
		return ResolvedPlay{}, fmt.Errorf("troupe: submit %s: persist play: %w", playID, err)
	}

	entry := PlayIndexEntry{
		ID:         rp.Play.ID,
		Status:     StatusSubmitted,
		Author:     rp.Play.Author,
		Provenance: rp.Play.Provenance,
		Created:    time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.appendIndexEntry(entry); err != nil {
		return ResolvedPlay{}, fmt.Errorf("troupe: submit %s: index play: %w", playID, err)
	}
	return rp, nil
}

// appendIndexEntry appends one entry to plays/index.json — created on first
// use — and re-sorts the list newest-first, the plain lexicographic sort of
// the datetime ids (decision 15). The write is atomic, like the play file.
func (s *Submitter) appendIndexEntry(entry PlayIndexEntry) error {
	path := filepath.Join(s.worktree, playIndex)
	idx, err := readIndex(path)
	if err != nil {
		return err
	}
	idx.Index = append(idx.Index, entry)
	slices.SortFunc(idx.Index, func(a, b PlayIndexEntry) int {
		return strings.Compare(b.ID, a.ID)
	})
	data, err := json.Marshal(idx)
	if err != nil {
		return fmt.Errorf("troupe: marshal play index: %w", err)
	}
	if err := writeFileAtomic(path, data); err != nil {
		return fmt.Errorf("troupe: %w", err)
	}
	return nil
}

// readIndex loads plays/index.json; an absent index is an empty list — the
// first submit creates the file.
func readIndex(path string) (PlayIndex, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return PlayIndex{}, nil
	}
	if err != nil {
		return PlayIndex{}, fmt.Errorf("troupe: read play index: %w", err)
	}
	var idx PlayIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return PlayIndex{}, fmt.Errorf("troupe: %s: %w", path, err)
	}
	return idx, nil
}

// snapshotFromWorktree reads the materialised notebook worktree into a
// resolver Snapshot: relative paths ("models/cat@1.json") to file bytes.
// The resolver performs no I/O of its own (D-24); this reader is the submit
// gate's filesystem seam.
func snapshotFromWorktree(root string) (Snapshot, error) {
	snap := Snapshot{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		snap[rel] = data
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("troupe: walk worktree: %w", err)
	}
	return snap, nil
}

// writeFileAtomic writes data to path the way submit_play persists a play: a
// temp file in the same directory, fsync'd, then renamed over the target. A
// reader never observes a torn file; a crash leaves either the old contents
// or the new, never a splice. On failure the temp file is removed and the
// target is left untouched.
func writeFileAtomic(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("troupe: mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("troupe: create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("troupe: write temp: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("troupe: sync temp: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("troupe: close temp: %w", err)
	}
	if err = os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("troupe: rename onto %s: %w", path, err)
	}
	return nil
}

// submitPlayTool is the director-only submit_play tool. It is excluded from
// the general registry (decision 20) — roles can never select it — and phase
// 7 grants it to the director's fixed tool set.
type submitPlayTool struct {
	submitter *Submitter
}

// newSubmitPlayTool builds the submit_play tool bound to this submitter.
func (s *Submitter) newSubmitPlayTool() models.LLMTool {
	return &submitPlayTool{submitter: s}
}

func (t *submitPlayTool) Call(input models.Input) (string, error) {
	play, ok := input["play"].(string)
	if !ok || play == "" {
		return "", errors.New("submit_play: play must be a non-empty string (the story_<UTC> id of plays/story_<UTC>.json)")
	}
	rp, err := t.submitter.Submit(play)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("submitted %s: plays/%s.json written, plays/index.json updated", rp.Play.ID, rp.Play.ID), nil
}

func (t *submitPlayTool) Specification() models.Specification {
	return models.Specification{
		Name:        "submit_play",
		Description: "Submit the play authored at plays/story_<UTC>.json: validate every reference in the worktree resolves, atomically persist the resolved play under that id and append its metadata entry to plays/index.json. Exact errors return for the director to fix; a second submit of the same play id is refused. The director is the only writer of plays/.",
		Inputs: &models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterObject{
				"play": {
					Type:        "string",
					Description: "The play id to submit, e.g. story_20260820T161500Z (the file plays/story_20260820T161500Z.json in the worktree)",
				},
			},
			Required: []string{"play"},
		},
	}
}
