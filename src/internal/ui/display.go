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
		fmt.Printf("%sDEBUG: model=%s in=%d out=%d stop=%s%s\n",
			ColorMagenta, e.Model, e.InputTokens, e.OutputTokens, e.StopReason, ColorReset)
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
	}
}

// ── NopSink ───────────────────────────────────────────────────────────────────

// NopSink discards all events. Useful for tests.
type NopSink struct{}

func (NopSink) Emit(Event) {}

