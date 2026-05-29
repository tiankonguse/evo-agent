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

// Event is a union struct carrying data for any EventKind.
type Event struct {
	Kind EventKind

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
}
