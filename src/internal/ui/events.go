package ui

// EventKind identifies the type of a TUI event.
type EventKind uint8

const (
	EvThinking  EventKind = iota // Model thinking block
	EvText                       // Assistant text block
	EvToolCall                   // Tool invoked
	EvToolResult                 // Tool result received
	EvSystem                     // Debug / status message
	EvTokens                     // Token usage update
	EvDone                       // Agent finished a turn
	EvTodo                       // Session plan updated
	EvPlan                       // Persistent plan updated
	EvGoal                       // Active goal status changed (set/cleared/achieved/evaluating/continuing/capped)
	EvBgTasks                    // Background-task counts changed (running/completed)
	EvTeam                       // Persistent teammate roster / status changed
)

// TodoItem is one entry in the memory plan.
type TodoItem struct {
	ID         int
	Content    string
	Status     string // "pending" | "in_progress" | "completed"
	ActiveForm string // present-continuous label shown while in_progress
}

// PlanTaskItem is one task in a session plan, for TUI rendering.
type PlanTaskItem struct {
	ID        int
	Subject   string
	Status    string // "pending" | "in_progress" | "completed" | "deleted"
	BlockedBy []int
}

// PlanSnapshot is the TUI-visible summary of an active session plan.
type PlanSnapshot struct {
	Name  string
	Tasks []PlanTaskItem
}

// TeammateSnapshot is the TUI-visible summary of one persistent teammate.
// Mirrors the on-disk teammate record stripped to fields the UI needs.
type TeammateSnapshot struct {
	Name         string
	Role         string
	Status       string // "working" | "idle" | "shutdown"
	LastActiveMs int64  // Unix ms; 0 = never active
}

// Event is a union struct carrying data for any EventKind.
type Event struct {
	Kind EventKind

	// AgentName, when non-empty, identifies the subagent that emitted this
	// event. Sinks use it to render the line with a distinguishing color
	// and an indent gutter so the user can see at a glance which output
	// came from a delegated agent vs the main loop. Empty = main agent.
	AgentName string

	// EvThinking, EvText, EvSystem
	Text string

	// EvToolCall
	ToolID    string
	ToolName  string
	ToolInput string // raw JSON string

	// EvToolResult
	ResultID     string
	ResultOutput string
	ResultError  bool

	// EvTokens
	InputTokens  int64
	OutputTokens int64
	Model        string
	StopReason   string
	BlockSummary string // e.g. "tool_use:2 text:1 thinking:1"

	// EvTodo
	TodoItems []TodoItem
	TodoTopic string

	// EvPlan
	PlanItems []PlanSnapshot

	// EvGoal — populated by the /goal slash handler and the agent loop's
	// goal-driven continuation logic.
	GoalKind     string // "set"|"cleared"|"achieved"|"evaluating"|"continuing"|"capped"|"status"
	GoalText     string // active goal condition (set/status/continuing)
	GoalReason   string // evaluator reason (continuing/achieved)
	GoalPlanName string // associated .evo-agent/tasks/todo/<name>
	GoalIter     int    // continuation count consumed (0-based)
	GoalMaxIter  int    // cap (e.g. 30)
	GoalSetAt    int64  // Unix ms when the goal was set; used for elapsed display

	// EvBgTasks — populated by tools/bgtask.go whenever a task starts /
	// finishes / is cancelled, so the TUI status bar can show live counts.
	BgRunning   int
	BgCompleted int

	// EvTeam — populated by tools/team.go whenever a teammate spawns, goes
	// idle, returns to work, or is shut down. The TUI consumes the
	// snapshot to redraw the team panel and status bar.
	TeamName    string
	TeamMembers []TeammateSnapshot
}
