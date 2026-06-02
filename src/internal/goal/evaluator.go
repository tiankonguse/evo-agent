package goal

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// EvalSystemPrompt is the static instruction sent to the evaluator LLM.
// Kept as a package var so tests can inspect it.
const EvalSystemPrompt = `You are a goal-completion evaluator. Read the agent's recent transcript and decide whether the user's stated goal is fully met. Reply with EXACTLY ONE JSON object on a single line, no prose, no markdown fences:
{"met": true|false, "reason": "<=30 words"}
If unsure, return false.`

// EvalRecentTurns controls how many trailing messages we excerpt for the
// evaluator. Six is enough to surface tool results and the final assistant
// answer without burning tokens on stale context.
const EvalRecentTurns = 6

// Verdict is the evaluator's parsed response.
type Verdict struct {
	Met    bool   `json:"met"`
	Reason string `json:"reason"`
}

// ParseVerdict tolerantly extracts a {"met": ..., "reason": ...} object from
// raw evaluator output. Handles three common LLM response shapes:
//  1. raw JSON                → {"met":true,"reason":"..."}
//  2. fenced JSON             → ```json\n{"met":true,...}\n```
//  3. prose with embedded JSON → "Sure! {"met":false,"reason":"..."}"
//
// Anything that fails to parse is treated as Met:false with a diagnostic
// reason — we MUST never falsely declare success.
func ParseVerdict(raw string) Verdict {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Verdict{Met: false, Reason: "evaluator returned empty response"}
	}

	// Strip common markdown fences.
	if strings.HasPrefix(raw, "```") {
		raw = strings.TrimPrefix(raw, "```json")
		raw = strings.TrimPrefix(raw, "```")
		raw = strings.TrimSuffix(raw, "```")
		raw = strings.TrimSpace(raw)
	}

	// Find the first balanced {...} block. We don't try to validate JSON
	// nesting properly — Anthropic models are reliable enough that the first
	// "{" through the last "}" usually parses.
	start := strings.IndexByte(raw, '{')
	end := strings.LastIndexByte(raw, '}')
	if start < 0 || end <= start {
		return Verdict{Met: false, Reason: "evaluator response unparseable"}
	}
	candidate := raw[start : end+1]

	var v Verdict
	if err := json.Unmarshal([]byte(candidate), &v); err != nil {
		return Verdict{Met: false, Reason: "evaluator JSON parse error"}
	}
	if v.Reason == "" {
		v.Reason = "no reason given"
	}
	return v
}

// BuildEvalRequest assembles the system + user prompts that go to the
// evaluator LLM. We hand-render the recent transcript as plain text so the
// evaluator doesn't need to parse anthropic.MessageParam internals.
//
// tailN messages are excerpted from the end of msgs; pass 0 to use the
// EvalRecentTurns default.
func BuildEvalRequest(goalText string, msgs []anthropic.MessageParam, tailN int) (system, user string) {
	if tailN <= 0 {
		tailN = EvalRecentTurns
	}
	start := 0
	if len(msgs) > tailN {
		start = len(msgs) - tailN
	}
	var lines []string
	for _, m := range msgs[start:] {
		role := "user"
		if m.Role == anthropic.MessageParamRoleAssistant {
			role = "assistant"
		}
		txt := joinMessageText(m)
		if txt == "" {
			continue
		}
		// Cap individual block size so a giant tool result doesn't blow the
		// evaluator's prompt budget.
		if len(txt) > 2000 {
			txt = txt[:2000] + "…(truncated)"
		}
		lines = append(lines, fmt.Sprintf("[%s]\n%s", role, txt))
	}
	transcript := strings.Join(lines, "\n\n")

	system = EvalSystemPrompt
	user = fmt.Sprintf(
		"<goal>%s</goal>\n<recent-transcript turns=%q>\n%s\n</recent-transcript>\nDecide now.",
		goalText,
		fmt.Sprintf("%d", tailN),
		transcript,
	)
	return
}

// ContinuationPrompt builds the synthetic <goal-reminder> user message that
// the loop appends when the evaluator returns met=false. It carries:
//
//   - the goal text (so the model is reminded of the target)
//   - the evaluator's reason (so the model knows what's still missing)
//   - optional persistent-plan summary (so the model has cross-session
//     context — supplied by tools.GlobalPlan.StartupSummary())
func ContinuationPrompt(goalText, reason, planSummary string) string {
	var b strings.Builder
	b.WriteString("<goal-reminder>\nThe goal is not yet met. Evaluator reason: ")
	b.WriteString(strings.TrimSpace(reason))
	b.WriteString("\n\nActive goal: ")
	b.WriteString(strings.TrimSpace(goalText))
	if s := strings.TrimSpace(planSummary); s != "" {
		b.WriteString("\n\nPersistent plan context:\n")
		b.WriteString(s)
	}
	b.WriteString("\n\nContinue working toward this goal. Use available tools (read_file, edit_file, bash, plan_task_update …) as needed. When you believe the goal is met, provide a final answer with no further tool calls.\n</goal-reminder>")
	return b.String()
}

// joinMessageText pulls all text segments out of a message for transcript
// rendering. Tool calls and tool results are rendered inline so the
// evaluator sees what the agent did.
func joinMessageText(m anthropic.MessageParam) string {
	var parts []string
	for _, blk := range m.Content {
		switch {
		case blk.OfText != nil && blk.OfText.Text != "":
			parts = append(parts, blk.OfText.Text)
		case blk.OfToolUse != nil:
			tu := blk.OfToolUse
			rawInput, _ := json.Marshal(tu.Input)
			parts = append(parts, fmt.Sprintf("(tool_use %s %s)", tu.Name, string(rawInput)))
		case blk.OfToolResult != nil:
			tr := blk.OfToolResult
			var inner []string
			for _, c := range tr.Content {
				if c.OfText != nil && c.OfText.Text != "" {
					inner = append(inner, c.OfText.Text)
				}
			}
			parts = append(parts, fmt.Sprintf("(tool_result %s)", strings.Join(inner, " ")))
		case blk.OfThinking != nil && blk.OfThinking.Thinking != "":
			// Skip thinking blocks — evaluator only needs externally
			// observable behaviour.
		}
	}
	return strings.Join(parts, "\n")
}
