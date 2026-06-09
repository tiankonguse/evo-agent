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

	// Bright ANSI variants (90-97) — used for the subagent gutter so the
	// indent bar stays vivid against dark terminal themes. The 30-37 range
	// resolves to muted/grayish tones in many default dark themes (macOS
	// Terminal Pro, iTerm Solarized Dark) which made the gutter blend into
	// the background.
	ColorBoldBrightYellow  = "\033[1;93m"
	ColorBoldBrightGreen   = "\033[1;92m"
	ColorBoldBrightMagenta = "\033[1;95m"
	ColorBoldBrightRed     = "\033[1;91m"
	ColorBoldBrightCyan    = "\033[1;96m"
	ColorBoldBrightWhite   = "\033[1;97m"
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

// EmitTeam broadcasts the latest team roster + per-member status. Used by
// tools/team.go on every spawn / idle / shutdown / wake transition. The
// TUI redraws the team panel and status counts; plain mode prints a short
// summary line per non-trivial change (handled in TerminalSink).
func EmitTeam(name string, members []TeammateSnapshot) {
	globalSink.Emit(Event{
		Kind:        EvTeam,
		TeamName:    name,
		TeamMembers: members,
	})
}

// ── Subagent-scoped variants ─────────────────────────────────────────────────
//
// These helpers mirror the unscoped Print* variants above but tag every
// emitted Event with AgentName so sinks can render the line with a
// distinguishing color and an indent gutter. The main agent loop uses the
// unscoped helpers (no AgentName); subagent / fork runners use these.

// PrintTextAs is PrintText scoped to a subagent identity.
func PrintTextAs(agentName, text string) {
	globalSink.Emit(Event{Kind: EvText, AgentName: agentName, Text: text})
}

// PrintToolCallAs is PrintToolCall scoped to a subagent identity.
func PrintToolCallAs(agentName, id, name, input string) {
	globalSink.Emit(Event{
		Kind:      EvToolCall,
		AgentName: agentName,
		ToolID:    id,
		ToolName:  name,
		ToolInput: input,
	})
}

// PrintToolResultAs is PrintToolResult scoped to a subagent identity.
func PrintToolResultAs(agentName, id, output string, isError bool) {
	globalSink.Emit(Event{
		Kind:         EvToolResult,
		AgentName:    agentName,
		ResultID:     id,
		ResultOutput: output,
		ResultError:  isError,
	})
}

// PrintCommandAs is reserved for future per-tool argument display in the TUI;
// currently a no-op for parity with the unscoped PrintCommand.
func PrintCommandAs(agentName, cmd string) {
	// no-op — TUI ignores EvToolCall detail today; kept symmetric with PrintCommand
	_ = agentName
	_ = cmd
}

// PrintErrorAs is PrintError scoped to a subagent identity.
func PrintErrorAs(agentName, msg string) {
	globalSink.Emit(Event{Kind: EvSystem, AgentName: agentName, Text: msg})
}

// PrintSystemAs is PrintSystem scoped to a subagent identity.
func PrintSystemAs(agentName, msg string) {
	globalSink.Emit(Event{Kind: EvSystem, AgentName: agentName, Text: msg})
}

// PrintTokensAs is PrintTokens scoped to a subagent identity.
func PrintTokensAs(agentName, model string, inputTok, outputTok int64, stopReason, blockSummary string) {
	globalSink.Emit(Event{
		Kind:         EvTokens,
		AgentName:    agentName,
		Model:        model,
		InputTokens:  inputTok,
		OutputTokens: outputTok,
		StopReason:   stopReason,
		BlockSummary: blockSummary,
	})
}
