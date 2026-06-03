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

func PrintTokens(model string, inputTok, outputTok int64, stopReason string, blockSummary string) {
	globalSink.Emit(Event{
		Kind:         EvTokens,
		Model:        model,
		InputTokens:  inputTok,
		OutputTokens: outputTok,
		StopReason:   stopReason,
		BlockSummary: blockSummary,
	})
}

// PrintDone signals that the agent finished processing a turn.
func PrintDone() {
	globalSink.Emit(Event{Kind: EvDone})
}

// EmitTodo broadcasts an updated memory plan to the active sink.
func EmitTodo(items []TodoItem, topic string) {
	globalSink.Emit(Event{Kind: EvTodo, TodoItems: items, TodoTopic: topic})
}

// EmitPlan broadcasts updated session plan snapshots to the active sink.
func EmitPlan(plans []PlanSnapshot) {
	globalSink.Emit(Event{Kind: EvPlan, PlanItems: plans})
}

// EmitGoal broadcasts a goal lifecycle event. kind matches the strings
// listed on Event.GoalKind.
func EmitGoal(ev Event) {
	ev.Kind = EvGoal
	globalSink.Emit(ev)
}

// PrintGoal is a convenience wrapper that builds an EvGoal Event and emits
// it. The TerminalSink prints a one-line summary; the TUI renders a richer
// indicator.
func PrintGoal(kind, text, reason, planName string, iter, maxIter int, setAtMs int64) {
	globalSink.Emit(Event{
		Kind:         EvGoal,
		GoalKind:     kind,
		GoalText:     text,
		GoalReason:   reason,
		GoalPlanName: planName,
		GoalIter:     iter,
		GoalMaxIter:  maxIter,
		GoalSetAt:    setAtMs,
	})
}

// EmitBgTasks broadcasts the latest background-task counts. The TUI status
// bar consumes this; plain mode's TerminalSink ignores it (the model echoes
// task lifecycle via tool results, so a separate stdout line would be noise).
func EmitBgTasks(running, completed int) {
	globalSink.Emit(Event{
		Kind:        EvBgTasks,
		BgRunning:   running,
		BgCompleted: completed,
	})
}
