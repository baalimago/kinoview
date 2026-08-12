package theatre

import (
	"fmt"
	"strings"
)

// AssembleContext builds the working-context standard every agent call runs
// inside (decision D5): the generation id and theme, the board excerpt (the
// last BoardExcerptMax entries, oldest first), the working-file summary, the
// role prompt and the task — in that order. Board growth beyond the excerpt
// cap never grows the prompt.
func AssembleContext(gen, theme string, board Board, working Summary, rolePrompt, task string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Generation: %s\n", gen)
	if theme != "" {
		fmt.Fprintf(&b, "Theme: %s\n", theme)
	}
	b.WriteString("\nBoard (most recent work):\n")
	excerpt := board.Excerpt(BoardExcerptMax)
	if len(excerpt) == 0 {
		b.WriteString("(empty — nothing posted yet)\n")
	}
	for _, e := range excerpt {
		to := e.To
		if to == "" {
			to = "company"
		}
		fmt.Fprintf(&b, "[%d] %s (%s) → %s: %s\n", e.Seq, e.Author, e.Kind, to, e.Body)
	}
	b.WriteString("\nWorking file:\n")
	if working.Title == "" && len(working.Cast) == 0 && working.Beats == 0 && working.Backdrop == "" {
		// A fresh generation starts with no draft (the working file is reset
		// at openProduction): the previous generation's story must not prime
		// the new one, and an empty summary must not render as noise.
		b.WriteString("(no draft yet — this generation starts fresh)\n")
	} else {
		fmt.Fprintf(&b, "Title: %s\n", working.Title)
		if len(working.Cast) == 0 {
			b.WriteString("Cast: (none yet)\n")
		} else {
			fmt.Fprintf(&b, "Cast: %s\n", strings.Join(working.Cast, ", "))
		}
		fmt.Fprintf(&b, "Beats: %d\nActs: %d\n", working.Beats, working.Acts)
		fmt.Fprintf(&b, "Backdrop: %s\nStatus: %s\n", working.Backdrop, working.Status)
		if len(working.Canon) > 0 {
			// The canon facts are the soft-continuity seam (D6): the playwright
			// is told them and riffs on them; phase 6 seeds them from the
			// repertoire doc.
			fmt.Fprintf(&b, "Canon: %s\n", strings.Join(working.Canon, "; "))
		}
	}
	b.WriteString("\nYour role:\n")
	b.WriteString(rolePrompt)
	b.WriteString("\n\nTask:\n")
	b.WriteString(task)
	b.WriteString("\n")
	return b.String()
}
