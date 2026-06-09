package tui

import "time"

// BlockKind identifies what a block represents.
type BlockKind uint8

const (
	KindThinking BlockKind = iota
	KindText
	KindToolCall
	KindSystem
	KindUser
)

// ToolStatus is the execution state of a tool call block.
type ToolStatus uint8

const (
	StatusPending ToolStatus = iota
	StatusSuccess
	StatusFailed
)

// Block is one rendered unit in the conversation viewport.
type Block struct {
	ID   string
	Kind BlockKind

	// AgentName, when set, identifies the subagent that produced this
	// block. Sinks use it to render a colored gutter / indent so the user
	// can attribute the line at a glance. Empty = main agent.
	AgentName string

	// KindThinking / KindText / KindSystem
	Content string

	// KindToolCall
	ToolName   string
	ToolArgs   string // raw JSON input string
	ToolStatus ToolStatus
	Result     string
	HasResult  bool
	StartTime  time.Time
	Duration   time.Duration
}

func newThinkingBlock(text string) Block {
	return Block{Kind: KindThinking, Content: text}
}

func newTextBlock(text string) Block {
	return Block{Kind: KindText, Content: text}
}

func newToolBlock(id, name, args string) Block {
	return Block{
		ID:         id,
		Kind:       KindToolCall,
		ToolName:   name,
		ToolArgs:   args,
		ToolStatus: StatusPending,
		StartTime:  time.Now(),
	}
}

func newSystemBlock(text string) Block {
	return Block{Kind: KindSystem, Content: text}
}

func newUserBlock(text string) Block {
	return Block{Kind: KindUser, Content: text}
}
