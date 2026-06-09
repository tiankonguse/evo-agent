package tui

import (
	"hash/fnv"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// subagentPalette is the rotating set of lipgloss colors used for the
// gutter prefix on subagent-emitted blocks (text, system, tool calls).
// Each agentName is hashed to one of these so the same agent always gets
// the same color in a session, while different agents are visually
// distinguishable.
//
// Colors picked for high visibility on dark terminal themes — the earlier
// (#d29922-style GitHub palette) washed out on dark mode and read as
// gray. These are saturated, near-primary tones that pop against a dark
// background; combined with Bold() in the gutter they're hard to miss.
var subagentPalette = []color.Color{
	lipgloss.Color("#ffd000"), // bright yellow
	lipgloss.Color("#00ff95"), // bright green
	lipgloss.Color("#ff7eff"), // bright magenta
	lipgloss.Color("#ff5555"), // bright red
	lipgloss.Color("#00e5ff"), // bright cyan
	lipgloss.Color("#ff9b3f"), // bright orange
}

// subagentColor returns the lipgloss color assigned to agentName by FNV
// hash modulo the subagent palette. Stable across the process lifetime.
func subagentColor(agentName string) color.Color {
	h := fnv.New32a()
	_, _ = h.Write([]byte(agentName))
	return subagentPalette[int(h.Sum32())%len(subagentPalette)]
}

// indentSubagent prefixes every line of body with a colored gutter bar +
// a "[<agentName>] " header on the first line so the user can attribute
// the block to a delegated subagent at a glance. Returns body unchanged
// when agentName == "" (= main-agent output).
//
// The gutter character is "┃ " (heavy vertical bar + space). lipgloss
// width-padding stays correct because the ANSI escape sequences don't
// count toward visible cell width. Both gutter and header are rendered
// Bold so the dye saturates on dark terminal themes (the previous
// non-bold rendering read as gray on default macOS / iTerm dark themes).
func indentSubagent(agentName, body string) string {
	if agentName == "" {
		return body
	}
	color := subagentColor(agentName)
	gutter := lipgloss.NewStyle().Foreground(color).Bold(true).Render("┃ ")
	header := lipgloss.NewStyle().Foreground(color).Bold(true).Render("[" + agentName + "] ")

	lines := strings.Split(body, "\n")
	out := make([]string, len(lines))
	for i, line := range lines {
		if i == 0 {
			out[i] = gutter + header + line
		} else {
			out[i] = gutter + line
		}
	}
	return strings.Join(out, "\n")
}
