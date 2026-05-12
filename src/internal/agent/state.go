package agent

import "github.com/anthropics/anthropic-sdk-go"

// LoopState holds the minimal mutable state of a single agent run.
type LoopState struct {
	Messages         []anthropic.MessageParam
	TurnCount        int
	TransitionReason string
	CompactState     *CompactState
}

// CompactState tracks context compression across turns.
type CompactState struct {
	HasCompacted bool     // Whether any compaction has occurred
	LastSummary  string   // The last generated summary
	RecentFiles  []string // Recently accessed file paths (FIFO, max 5)
	CompactCount int      // Count of compression operations
}
