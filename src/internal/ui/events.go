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
)

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
}
