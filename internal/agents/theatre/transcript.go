package theatre

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
)

// TranscriptEvent is one inter-agent event. The transcript is the single
// authoritative record of a generation; the stdout feed (phase 2) is derived
// from the same events, so the two can never disagree.
type TranscriptEvent struct {
	Gen  string    `json:"gen"`
	Seq  int       `json:"seq"`
	T    time.Time `json:"t"`
	Kind string    `json:"kind"`
	From string    `json:"from"`
	To   string    `json:"to,omitempty"`
	Body string    `json:"body"`

	// Level is the feed's severity label (notice/ok/warning/error). Empty
	// derives from the kind: submit succeeds, fail fails, everything else is a
	// notice. It is presentation metadata, never validated as hard as roles.
	Level string `json:"level,omitempty"`
}

// Transcript is the decoded event stream of a generation. It is append-only on
// disk and written by a single writer (the stage-manager wrapper); loads
// decode whatever lines are readable and drop the rest.
type Transcript struct {
	Events []TranscriptEvent
}

// normalize bounds a single event in place: roles and kind are lowercased,
// the level is validated, and the body and addressee are truncated. It never
// errors; unknown values are dropped by the caller.
func (e *TranscriptEvent) normalize() {
	e.From = strings.ToLower(strings.TrimSpace(e.From))
	e.To = strings.ToLower(strings.TrimSpace(e.To))
	e.Kind = strings.ToLower(strings.TrimSpace(e.Kind))
	e.Level = strings.ToLower(strings.TrimSpace(e.Level))
	if !ValidLevels[e.Level] {
		e.Level = ""
	}
	e.Body = truncateRunes(e.Body, TranscriptMaxBody)
	e.To = truncateRunes(e.To, TranscriptMaxTo)
}

// valid reports whether the event is recordable. Roles are required to be
// valid when present; a missing recipient is fine, a missing author is not.
// A deliver names the artifact it hands over (the draft, a report), which is
// not a role, so delivers are the one kind whose addressee is free-form.
func (e TranscriptEvent) valid() bool {
	if e.Gen == "" || !ValidTranscriptKinds[e.Kind] {
		return false
	}
	if e.From == "" || !ValidRoles[e.From] {
		return false
	}
	if e.To == "" || e.Kind == "deliver" {
		return true
	}
	return ValidRoles[e.To]
}

// LoadTranscript reads the transcript. A missing file is not an error; JSON-
// corrupt lines are dropped with a warning, because a partial transcript is
// more useful than none. Events with unknown kinds or roles are dropped
// silently — that is forward compatibility, not corruption.
func (c *Company) LoadTranscript() (Transcript, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return loadTranscript(c.transcriptPath())
}

// scanTranscript reads and validates every transcript line, dropping
// unreadable or semantically invalid events. It returns the readable events
// and the count of unreadable lines, so callers warn in their own voice: the
// loader warns via ancli, the dialog renderer in its output.
func scanTranscript(path string) (events []TranscriptEvent, bad int, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64*1024), TranscriptMaxBody*8+4096)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var ev TranscriptEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			bad++
			continue
		}
		ev.normalize()
		if !ev.valid() {
			continue
		}
		events = append(events, ev)
	}
	if err := sc.Err(); err != nil {
		return events, bad, fmt.Errorf("scan transcript: %w", err)
	}
	return events, bad, nil
}

func loadTranscript(path string) (Transcript, error) {
	events, bad, err := scanTranscript(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Transcript{}, nil
		}
		ancli.Errf("theatre: transcript unreadable: %v", err)
		return Transcript{Events: events}, err
	}
	if bad > 0 {
		ancli.Warnf("theatre: transcript: %d line(s) unreadable, dropped", bad)
	}
	return Transcript{Events: events}, nil
}

// AppendTranscript appends one event to the transcript. Appends are serialised
// (single writer), seq continues from the last readable event, and the file is
// trimmed to the newest TranscriptMaxLines lines whenever it would exceed the
// cap. The append and any trim happen in one atomic rewrite, so a reader never
// observes a half-appended or over-cap file. An untrusted event records
// nothing and is not an error.
func (c *Company) AppendTranscript(ev TranscriptEvent) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return appendTranscript(c.transcriptPath(), ev)
}

func appendTranscript(path string, ev TranscriptEvent) error {
	ev.normalize()
	if !ev.valid() {
		return nil
	}
	lines, err := readLines(path)
	if err != nil {
		return err
	}
	seq := len(lines) + 1
	if n := len(lines); n > 0 {
		var last TranscriptEvent
		if json.Unmarshal(lines[n-1], &last) == nil && last.Seq >= 0 {
			seq = last.Seq + 1
		}
	}
	if ev.T.IsZero() {
		ev.T = time.Now()
	}
	ev.Seq = seq
	line, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal transcript event: %w", err)
	}
	lines = append(lines, line)
	if len(lines) > TranscriptMaxLines {
		lines = lines[len(lines)-TranscriptMaxLines:]
	}
	return writeLines(path, lines)
}

// readLines reads a JSONL file, tolerating a missing file (no lines yet).
func readLines(path string) ([][]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read transcript: %w", err)
	}
	var lines [][]byte
	for l := range bytes.SplitSeq(b, []byte{'\n'}) {
		if l = bytes.TrimSpace(l); len(l) > 0 {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

// writeLines writes a JSONL file atomically, newline-terminated.
func writeLines(path string, lines [][]byte) error {
	data := bytes.Join(lines, []byte{'\n'})
	data = append(data, '\n')
	return writeFileAtomic(path, data)
}
