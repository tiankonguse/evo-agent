package session

import "github.com/anthropics/anthropic-sdk-go"

// Record types written to messages.jsonl as one line each.
const (
	TypeSessionStart    = "session_start"
	TypeUser            = "user"
	TypeAssistant       = "assistant"
	TypeCompactBoundary = "compact_boundary"
	TypeResumeMarker    = "resume_marker"
	TypeSubagentStart   = "subagent_start"
	TypeSubagentEnd     = "subagent_end"

	// Goal lifecycle markers (used by the /goal command and the loop's
	// goal-driven continuation logic). They survive compact_boundary so a
	// goal set before a compaction is still restorable on --resume.
	TypeGoalSet      = "goal_set"
	TypeGoalCleared  = "goal_cleared"
	TypeGoalAchieved = "goal_achieved"
)

// Record is a single append-only event in a session transcript.
//
// Every record carries the same envelope (type, timestamp, agent version,
// session id, cwd, prompt id, git branch); type-specific fields are populated
// only for the matching record type.
//
// `Message` directly stores anthropic.MessageParam — the same shape used by
// agent loop history — so resume can deserialize back to a usable message slice
// without translation.
type Record struct {
	Type         string `json:"type"`
	Timestamp    string `json:"timestamp"` // ISO-8601 local + offset, e.g. "2026-05-31T19:58:42.705+08:00"
	AgentVersion string `json:"agent_version"`
	SessionID    string `json:"session_id"`
	Cwd          string `json:"cwd"`
	PromptID     string `json:"prompt_id,omitempty"`
	GitBranch    string `json:"git_branch,omitempty"`

	// type=user|assistant
	Message *anthropic.MessageParam `json:"message,omitempty"`

	// type=assistant — token usage from the LLM response.
	InputTokens  int64 `json:"input_tokens,omitempty"`
	OutputTokens int64 `json:"output_tokens,omitempty"`

	// type=compact_boundary
	Summary      string `json:"summary,omitempty"`
	CompactCount int    `json:"compact_count,omitempty"`

	// type=resume_marker
	FromSessionID string `json:"from_session_id,omitempty"`
	RestoredCount int    `json:"restored_count,omitempty"`

	// type=subagent_start | subagent_end
	AgentName    string `json:"agent_name,omitempty"`
	SubagentPath string `json:"subagent_path,omitempty"` // relative to session dir
	Result       string `json:"result,omitempty"`        // subagent_end final text

	// type=goal_set | goal_cleared | goal_achieved
	GoalText     string `json:"goal_text,omitempty"`      // full condition (goal_set)
	GoalReason   string `json:"goal_reason,omitempty"`    // evaluator reason (goal_achieved)
	GoalPlanName string `json:"goal_plan_name,omitempty"` // associated .evo-agent/tasks/todo/<name>
}
