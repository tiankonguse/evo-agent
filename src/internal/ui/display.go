package ui

import "fmt"

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

func (TerminalSink) Emit(e Event) {
	switch e.Kind {
	case EvThinking:
		fmt.Printf("%sTHINKING: %s%s\n", ColorGreen, e.Text, ColorReset)
	case EvText:
		fmt.Printf("%s%s%s\n", ColorCyan, e.Text, ColorReset)
	case EvToolCall:
		fmt.Printf("%sDEBUG: Tool called: %s%s\n", ColorBlue, e.ToolName, ColorReset)
	case EvToolResult:
		// Print a short preview of the tool output in plain mode.
		const previewLen = 200
		preview := e.ResultOutput
		if len(preview) > previewLen {
			preview = preview[:previewLen]
		}
		if preview != "" {
			fmt.Println(preview)
		}
	case EvSystem:
		fmt.Printf("%s%s%s\n", ColorMagenta, e.Text, ColorReset)
	case EvTokens:
		fmt.Printf("%sDEBUG: model=%s in=%d out=%d stop=%s blocks=[%s]%s\n",
			ColorMagenta, e.Model, e.InputTokens, e.OutputTokens, e.StopReason, e.BlockSummary, ColorReset)
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

