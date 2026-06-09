package agent

import (
	"github.com/anthropics/anthropic-sdk-go"

	"evo-agent/internal/tools"
)

// forkSubagentMaxTurns caps a fork's LLM round-trips. Forks tend to be
// long-running ("audit the branch", "implement feature X end-to-end"),
// so we double the generic subagent ceiling. The model's own end_turn is
// still the normal exit.
const forkSubagentMaxTurns = 60

// forkBoilerplate is injected as the prefix of the directive user message
// at the tail of the parent's conversation. It tells the forked child:
//   - which role to assume,
//   - that recursive forks are off-limits (we also strip "task" from its
//     tool list, but the textual reminder helps cache + prompt clarity),
//   - what response shape to produce.
//
// Adapted from refs/forkSubagent.ts. Kept verbatim where it doesn't
// reference Claude Code—specific tool names so users see the same
// behavior across both products.
const forkBoilerplate = `<fork-boilerplate>
STOP. READ THIS FIRST.

You are a forked worker process. You are NOT the main agent.

RULES (non-negotiable):
1. Your system prompt is the parent agent's. IGNORE the part that says "default to delegating" — you ARE the delegate. Do NOT spawn sub-agents; execute directly.
2. Do NOT converse, ask questions, or suggest next steps.
3. Do NOT editorialize or add meta-commentary.
4. USE your tools directly: bash, read_file, write_file, edit_file, etc.
5. If you modify files, commit your changes before reporting. Include the commit hash in your report.
6. Do NOT emit text between tool calls. Use tools silently, then report once at the end.
7. Stay strictly within your directive's scope. If you discover related systems outside your scope, mention them in one sentence at most.
8. Keep your report under 500 words unless the directive specifies otherwise. Be factual and concise.
9. Your response MUST begin with "Scope:". No preamble, no thinking-out-loud.
10. REPORT structured facts, then stop.

Output format (plain text labels, not markdown headers):
  Scope: <echo back your assigned scope in one sentence>
  Result: <the answer or key findings, limited to the scope above>
  Key files: <relevant file paths — include for research tasks>
  Files changed: <list with commit hash — include only if you modified files>
  Issues: <list — include only if there are issues to flag>
</fork-boilerplate>

DIRECTIVE: `

// RunForkSubagent spawns a fork that inherits the parent agent's full
// system prompt AND complete message history, then receives `directive`
// as a final user-role instruction. This matches Claude Code's fork
// pattern (refs/forkSubagent.ts) — a directive-style prompt is the
// expected input, not a from-scratch task description.
//
// Use cases:
//   - "Audit ship readiness" / "what's left on this branch" — survey
//     questions where you want the answer without the underlying tool
//     output (find, git status, grep) cluttering the parent context.
//   - Independent implementation work that requires more than a couple of
//     edits, when the parent already has the relevant files in context.
//
// The fork:
//   - Uses the parent's `a.prompt.Build()` system prompt verbatim — same
//     coding rules, same memories, same plan / team / goal context.
//   - Uses the parent's model (no override path here; if you need a
//     different model, that's a custom agent's job).
//   - Inherits parent messages with `FilterIncompleteToolCalls` applied
//     so any orphan tool_use from the in-flight parent turn (the one that
//     just called the task tool) is dropped before the API sees it.
//   - Has the task tool stripped from its pool so it can't recursively
//     fork. (The boilerplate above also tells the model so explicitly.)
//
// Returns the fork's final text block, the same shape the generic
// subagent and custom subagent return.
func (a *Agent) RunForkSubagent(directive, name string, parentMessages []anthropic.MessageParam) string {
	if name == "" {
		name = "fork"
	}

	// 1. Build the directive user message that closes out the fork's
	//    inherited history. The boilerplate runs first so the model sees
	//    the role swap before any user instruction.
	directiveMsg := anthropic.NewUserMessage(
		anthropic.NewTextBlock(forkBoilerplate + directive),
	)

	// 2. Filter incomplete tool calls — when the task tool fires inside
	//    a multi-tool turn, the parent's last assistant message has a
	//    tool_use block (the `task` call itself, plus any siblings) whose
	//    tool_results haven't been added yet. The Anthropic API rejects
	//    such histories.
	cleanParent := FilterIncompleteToolCalls(parentMessages)

	// 3. Stitch: cleanParent || directive
	subMessages := make([]anthropic.MessageParam, 0, len(cleanParent)+1)
	subMessages = append(subMessages, cleanParent...)
	subMessages = append(subMessages, directiveMsg)

	return a.runSubagentLoop(subagentRunConfig{
		SystemText: a.prompt.Build(),
		AgentName:  name,
		ModelID:    a.cfg.ModelID,
		MaxTurns:   forkSubagentMaxTurns,
		Messages:   subMessages,
		Tools:      tools.ToolsExcept("task"),
	})
}
