package tui

import (
	"fmt"
	"time"
)

// renderGoalIndicator returns a single line summarizing the active goal,
// or "" when no /goal is set. Mirrors Claude Code's `◎ /goal active`
// surface so the user always knows whether autonomous continuation is
// engaged.
func (m *Model) renderGoalIndicator(width int) string {
	if !m.goalActive {
		return ""
	}

	elapsed := ""
	if m.goalSetAtMs > 0 {
		d := time.Since(time.UnixMilli(m.goalSetAtMs))
		elapsed = " · " + formatDuration(d)
	}

	// Build base line: "◎ /goal active · iter 3/30 · 2.4s · checking…"
	base := fmt.Sprintf("◎ /goal active · iter %d/%d%s",
		m.goalIter, m.goalMaxIter, elapsed,
	)
	if m.goalLastKind != "" && m.goalLastKind != "set" {
		base += " · " + m.goalLastKind
	}
	if m.goalLastNote != "" {
		note := m.goalLastNote
		// Trim notes so the indicator stays one terminal line.
		maxNote := width - len(base) - 6
		if maxNote < 8 {
			maxNote = 8
		}
		if len(note) > maxNote {
			note = note[:maxNote-1] + "…"
		}
		base += ": " + note
	}

	return goalIndicatorStyle.Width(width - 2).Render(base)
}
