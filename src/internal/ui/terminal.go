package ui

// ANSI color codes (kept for plain-text / --plain mode)
const (
	ColorReset   = "\033[0m"
	ColorGreen   = "\033[32m"
	ColorCyan    = "\033[36m"
	ColorBlue    = "\033[34m"
	ColorYellow  = "\033[33m"
	ColorMagenta = "\033[35m"
	ColorRed     = "\033[31m"
)

// All Print* helpers below route through globalSink.
// The active sink is set by SetSink() at startup.

func PrintThinking(text string) {
	globalSink.Emit(Event{Kind: EvThinking, Text: text})
}

func PrintText(text string) {
	globalSink.Emit(Event{Kind: EvText, Text: text})
}

func PrintToolCall(id, name, input string) {
	globalSink.Emit(Event{Kind: EvToolCall, ToolID: id, ToolName: name, ToolInput: input})
}

func PrintToolResult(id, output string, isError bool) {
	globalSink.Emit(Event{Kind: EvToolResult, ResultID: id, ResultOutput: output, ResultError: isError})
}

func PrintCommand(cmd string) {
	// shown as tool args in TUI; in plain mode TerminalSink ignores EvToolCall detail
}

func PrintError(msg string) {
	globalSink.Emit(Event{Kind: EvSystem, Text: msg})
}

func PrintSystem(msg string) {
	globalSink.Emit(Event{Kind: EvSystem, Text: msg})
}

func PrintTokens(model string, inputTok, outputTok int64, stopReason string) {
	globalSink.Emit(Event{
		Kind:         EvTokens,
		Model:        model,
		InputTokens:  inputTok,
		OutputTokens: outputTok,
		StopReason:   stopReason,
	})
}

// PrintDone signals that the agent finished processing a turn.
func PrintDone() {
	globalSink.Emit(Event{Kind: EvDone})
}

// EmitTodo broadcasts an updated session plan to the active sink.
func EmitTodo(items []TodoItem) {
	globalSink.Emit(Event{Kind: EvTodo, TodoItems: items})
}
