package theatre

import (
	"fmt"
	"strings"
)

// AssembleContext builds the working-context standard every agent call runs
// inside (decision D5): the generation id and theme, the working-file summary,
// the role prompt and the task — in that order.
func AssembleContext(gen, theme string, working Summary, rolePrompt, task string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Generation: %s\n", gen)
	if theme != "" {
		fmt.Fprintf(&b, "Theme: %s\n", theme)
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
			// writes them via append_canon and they ride on the draft report,
			// so a new generation reads the previous one's canon out of the
			// working file. No durable library exists anymore — the notebook is
			// the only cross-generation memory (phases 4–5).
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
