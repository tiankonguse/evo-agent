package agent

import "github.com/anthropics/anthropic-sdk-go"

// LoopState holds the minimal mutable state of a single agent run.
type LoopState struct {
	Messages         []anthropic.MessageParam
	TurnCount        int
	TransitionReason string
}
