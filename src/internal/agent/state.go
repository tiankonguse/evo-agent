package agent

import (
	"github.com/anthropics/anthropic-sdk-go"

	"evo-agent/internal/session"
)

// LoopState holds the minimal mutable state of a single agent run.
type LoopState struct {
	Messages         []anthropic.MessageParam
	TurnCount        int
	TransitionReason string
	CompactState     *CompactState

	// Session persistence (optional). When Recorder is non-nil, the loop
	// appends a record to the session transcript every time a message is
	// added to Messages. PromptID groups records that belong to the same
	// user turn; it should be set fresh by the caller before invoking Loop.
	Recorder *session.Recorder
	PromptID string

	// LastUsage holds the input/output tokens of the most recent LLM
	// response. The agent loop fills this immediately before appending the
	// assistant record so per-message token totals can be persisted.
	LastUsage TokenUsage
}

// TokenUsage carries per-response token counts.
type TokenUsage struct {
	Input  int64
	Output int64
}

// CompactState tracks context compression across turns.
type CompactState struct {
	HasCompacted bool     // Whether any compaction has occurred
	LastSummary  string   // The last generated summary
	RecentFiles  []string // Recently accessed file paths (FIFO, max 5)
	CompactCount int      // Count of compression operations
}
