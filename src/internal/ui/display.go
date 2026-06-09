package ui

import (
	"fmt"
	"hash/fnv"
	"strings"
)

// EventSink is the interface any display frontend must implement.
// The agent and tools write events through this interface; the
// concrete implementation (TUI, plain-text, test stub) decides how
// to render them.
type EventSink interface {
	Emit(Event)
}

// SetSink registers the active display sink.
// Call this once at startup before the agent goroutine begins.
func SetSink(s EventSink) {
	globalSink = s
}

// globalSink is the package-level sink used by all Print* helpers.
// Default is TerminalSink (plain ANSI output).
var globalSink EventSink = TerminalSink{}

// ── TerminalSink ─────────────────────────────────────────────────────────────

// TerminalSink renders events as plain ANSI text to stdout.
// Used when running in --plain mode or when no TUI is active.
type TerminalSink struct{}

// subagentPalette is the rotating set of ANSI colors used for subagent
// gutters. Each agentName is hashed to one of these colors so the same
// agent always gets the same gutter color in a session, while different
// agents are visually distinguishable.
//
// All entries are bold + bright-fg variants (90-97 range) so the gutter
// pops against dark terminal themes — the previous regular-weight 30-37
// range read as gray on default macOS / iTerm dark themes.
var subagentPalette = []string{
	ColorBoldBrightYellow,
	ColorBoldBrightGreen,
	ColorBoldBrightMagenta,
	ColorBoldBrightRed,
	ColorBoldBrightCyan,
	ColorBoldBrightWhite,
}

// SubagentColor returns the ANSI color code assigned to agentName by FNV
// hash modulo the subagent palette. Stable across the process lifetime.
// Exported so the TUI sink (different package) can pick the matching color.
func SubagentColor(agentName string) string {
	if agentName == "" {
		return ""
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(agentName))
	return subagentPalette[int(h.Sum32())%len(subagentPalette)]
}

// IndentSubagent prefixes every line of body with a colored gutter bar so
// the user can visually attribute the line to a delegated subagent.
// Exported for the TUI sink to apply identical gutter styling on its side.
//
// The first line gets a header label "[<agentName>] " right after the
// gutter so the user always sees which agent emitted this block, even
// if the surrounding scrollback has scrolled the parent header off-screen.
func IndentSubagent(agentName, body string) string {
	if agentName == "" {
		return body
	}
	color := SubagentColor(agentName)
	gutter := color + "┃ " + ColorReset
	header := color + "[" + agentName + "]" + ColorReset + " "

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

func (TerminalSink) Emit(e Event) {
	switch e.Kind {
	case EvThinking:
		fmt.Println(IndentSubagent(e.AgentName, ColorGreen+"THINKING: "+e.Text+ColorReset))
	case EvText:
		fmt.Println(IndentSubagent(e.AgentName, ColorCyan+e.Text+ColorReset))
	case EvToolCall:
		fmt.Println(IndentSubagent(e.AgentName, ColorBlue+"DEBUG: Tool called: "+e.ToolName+ColorReset))
	case EvToolResult:
		// Print a short preview of the tool output in plain mode.
		const previewLen = 200
		preview := e.ResultOutput
		if len(preview) > previewLen {
			preview = preview[:previewLen]
		}
		if preview != "" {
			fmt.Println(IndentSubagent(e.AgentName, preview))
		}
	case EvSystem:
		fmt.Println(IndentSubagent(e.AgentName, ColorMagenta+e.Text+ColorReset))
	case EvTokens:
		line := fmt.Sprintf("%sDEBUG: model=%s in=%d out=%d stop=%s blocks=[%s]%s",
			ColorMagenta, e.Model, e.InputTokens, e.OutputTokens, e.StopReason, e.BlockSummary, ColorReset)
		fmt.Println(IndentSubagent(e.AgentName, line))
	case EvDone:
		// nothing to print in plain mode
	case EvTodo:
		markers := map[string]string{
			"pending":     "[ ]",
			"in_progress": "[>]",
			"completed":   "[x]",
		}
		fmt.Printf("%s── TODO ──%s\n", ColorMagenta, ColorReset)
		for _, item := range e.TodoItems {
			marker := markers[item.Status]
			line := item.Content
			if item.Status == "in_progress" && item.ActiveForm != "" {
				line += " (" + item.ActiveForm + ")"
			}
			fmt.Printf("%s  %s %s%s\n", ColorMagenta, marker, line, ColorReset)
		}
	case EvTeam:
		// Plain mode: print one compact roster line so the user sees who's
		// up. The TUI renders a multi-line panel separately.
		if len(e.TeamMembers) == 0 {
			fmt.Printf("%s── TEAM (%s) ── (no members)%s\n", ColorMagenta, e.TeamName, ColorReset)
			return
		}
		fmt.Printf("%s── TEAM (%s) ──%s\n", ColorMagenta, e.TeamName, ColorReset)
		for _, m := range e.TeamMembers {
			marker := map[string]string{
				"working":  "▶",
				"idle":     "◯",
				"shutdown": "✖",
			}[m.Status]
			if marker == "" {
				marker = "·"
			}
			fmt.Printf("%s  %s %s (%s) %s%s\n", ColorMagenta, marker, m.Name, m.Role, m.Status, ColorReset)
		}
	case EvGoal:
		// Plain mode: one line per lifecycle event so the user can see what
		// the loop's goal logic decided. The TUI renders a richer indicator
		// in the live bottom area instead.
		switch e.GoalKind {
		case "set":
			fmt.Printf("%s◎ /goal set: %s%s\n", ColorYellow, e.GoalText, ColorReset)
			if e.GoalPlanName != "" {
				fmt.Printf("%s  associated plan: .evo-agent/tasks/todo/%s/%s\n", ColorYellow, e.GoalPlanName, ColorReset)
			}
		case "evaluating":
			fmt.Printf("%s◎ /goal evaluating (iter %d/%d)…%s\n", ColorYellow, e.GoalIter, e.GoalMaxIter, ColorReset)
		case "continuing":
			fmt.Printf("%s◎ /goal not yet met — continuing (iter %d/%d). reason: %s%s\n",
				ColorYellow, e.GoalIter, e.GoalMaxIter, e.GoalReason, ColorReset)
		case "achieved":
			fmt.Printf("%s✓ /goal achieved: %s%s\n", ColorGreen, e.GoalReason, ColorReset)
		case "cleared":
			fmt.Printf("%s◎ /goal cleared%s\n", ColorYellow, ColorReset)
		case "capped":
			fmt.Printf("%s× /goal capped at %d iterations — auto-cleared%s\n", ColorRed, e.GoalMaxIter, ColorReset)
		case "status":
			if e.GoalText == "" {
				fmt.Printf("%s◎ /goal: no active goal%s\n", ColorYellow, ColorReset)
			} else {
				fmt.Printf("%s◎ /goal active: %s (iter %d/%d)%s\n",
					ColorYellow, e.GoalText, e.GoalIter, e.GoalMaxIter, ColorReset)
				if e.GoalPlanName != "" {
					fmt.Printf("%s  plan: .evo-agent/tasks/todo/%s/%s\n", ColorYellow, e.GoalPlanName, ColorReset)
				}
			}
		}
	}
}

// ── NopSink ───────────────────────────────────────────────────────────────────

// NopSink discards all events. Useful for tests.
type NopSink struct{}

func (NopSink) Emit(Event) {}

