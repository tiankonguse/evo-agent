package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"evo-agent/internal/config"
	"evo-agent/internal/llm"
	"evo-agent/internal/prompt"
	"evo-agent/internal/session"
	"evo-agent/internal/tools"
	"evo-agent/internal/ui"
)

// Agent orchestrates multi-turn conversations with the model.
type Agent struct {
	provider llm.Provider
	cfg      *config.Config
	prompt   *prompt.Builder
	dumpFile string // session dump file path (set on first dump)

	// session is the active persistence target. May be nil in tests.
	session *session.Session

	// currentRecorder / currentPromptID are valid only while a query is in
	// flight. They let RunSubagent (which is invoked through a fixed-shape
	// callback in the tools package) discover its parent session without
	// changing the callback signature.
	currentRecorder *session.Recorder
	currentPromptID string
}

// New creates an Agent with the given LLM provider, configuration, and prompt builder.
// It also wires up tools.SubagentRunner so the task tool can spawn child agents.
//
// The provider abstraction (internal/llm.Provider) lets the agent talk to
// either the Anthropic Messages API or the OpenAI Chat Completions API
// while keeping anthropic.MessageNewParams / *anthropic.Message as the
// canonical internal types — translation only happens inside the
// provider implementation.
func New(provider llm.Provider, cfg *config.Config, pb *prompt.Builder) *Agent {
	a := &Agent{provider: provider, cfg: cfg, prompt: pb}
	tools.RegisterSubagentRunner(func(systemPrompt string, messages []anthropic.MessageParam) string {
		return a.RunSubagent(systemPrompt, messages, "")
	})
	tools.RegisterNamedSubagentRunner(func(systemPrompt, agentName string, messages []anthropic.MessageParam) string {
		return a.RunSubagent(systemPrompt, messages, agentName)
	})
	return a
}

// AttachSession binds a persistent session to the agent. Subsequent
// RunQuery calls will record their messages, compact boundaries, and
// subagent activity to the session transcript.
func (a *Agent) AttachSession(s *session.Session) {
	a.session = s
}

// Session returns the currently attached session, or nil if persistence is
// disabled.
func (a *Agent) Session() *session.Session {
	return a.session
}

// autoCompact applies MicroCompact, then triggers a full LLM summarization if
// the estimated context size still exceeds CONTEXT_LIMIT.
func (a *Agent) autoCompact(state *LoopState) {
	state.Messages = MicroCompact(state.Messages, KEEP_RECENT_RESULTS)

	contextSize := EstimateContextSize(state.Messages)
	if contextSize <= CONTEXT_LIMIT {
		return
	}

	ui.PrintSystem(fmt.Sprintf("[auto compact triggered: %d chars]", contextSize))

	newMessages, err := CompactHistory(
		a.provider,
		a.cfg.ModelID,
		state.Messages,
		state.CompactState,
		"",
		state.Recorder,
		state.PromptID,
	)
	if err == nil {
		state.Messages = newMessages
	} else {
		ui.PrintError(fmt.Sprintf("ERROR: Compaction failed: %v", err))
	}
}

// manualCompact inspects the response content for a "compact" tool call and,
// if found, runs a full LLM summarization with the optional focus hint.
func (a *Agent) manualCompact(state *LoopState, content []anthropic.ContentBlockUnion) {
	for _, block := range content {
		if block.Type != "tool_use" || block.Name != "compact" {
			continue
		}
		var input map[string]interface{}
		json.Unmarshal(block.Input, &input)
		focus, _ := input["focus"].(string)

		ui.PrintSystem("[manual compact requested]")
		newMessages, err := CompactHistory(
			a.provider,
			a.cfg.ModelID,
			state.Messages,
			state.CompactState,
			focus,
			state.Recorder,
			state.PromptID,
		)
		if err == nil {
			state.Messages = newMessages
		}
		return // only one compact call per turn
	}
}

// Loop drives the agent loop until the model stops requesting tool calls.
func (a *Agent) Loop(state *LoopState) bool {
	// Initialize CompactState if needed
	if state.CompactState == nil {
		state.CompactState = &CompactState{}
	}

	// Track the recorder/prompt for the duration of this query so that
	// RunSubagent (called via the tools-package callback) can find them.
	a.currentRecorder = state.Recorder
	a.currentPromptID = state.PromptID
	defer func() {
		a.currentRecorder = nil
		a.currentPromptID = ""
	}()

	for {
		// Apply micro-compaction + auto compact before LLM call
		a.autoCompact(state)

		systemPrompt := a.prompt.Build()

		resp, err := a.provider.SendMessage(context.Background(), anthropic.MessageNewParams{
			Model: anthropic.Model(a.cfg.ModelID),
			System: []anthropic.TextBlockParam{
				{Text: systemPrompt},
			},
			Messages:  state.Messages,
			Tools:     tools.Tools(),
			MaxTokens: 8000,
		})
		if err != nil {
			ui.PrintError(fmt.Sprintf("Error calling API: %v", err))
			return false
		}

		// Append assistant response to history
		assistantMsg := resp.ToParam()
		state.Messages = append(state.Messages, assistantMsg)
		state.LastUsage = TokenUsage{
			Input:  resp.Usage.InputTokens,
			Output: resp.Usage.OutputTokens,
		}
		if state.Recorder != nil {
			state.Recorder.AppendAssistant(state.PromptID, assistantMsg, resp.Usage.InputTokens, resp.Usage.OutputTokens)
		}

		// Count content block types
		blockCounts := map[string]int{}
		for _, block := range resp.Content {
			blockCounts[string(block.Type)]++
		}
		var blockParts []string
		for t, c := range blockCounts {
			blockParts = append(blockParts, fmt.Sprintf("%s:%d", t, c))
		}
		blockSummary := strings.Join(blockParts, " ")

		ui.PrintTokens(
			string(resp.Model),
			resp.Usage.InputTokens,
			resp.Usage.OutputTokens,
			string(resp.StopReason),
			blockSummary,
		)

		// Track file reads before executing tools
		for _, block := range resp.Content {
			if block.Type == "tool_use" && block.Name == "read_file" {
				var input map[string]interface{}
				if err := json.Unmarshal(block.Input, &input); err == nil {
					if path, ok := input["path"].(string); ok {
						TrackRecentFile(state.CompactState, path)
					}
				}
			}
		}

		// Pass current conversation to tools so the remember tool can serialize it
		tools.SetConversationMessages(state.Messages)

		toolResults := tools.Execute(resp.Content, state.CompactState)
		if len(toolResults) == 0 {
			// No tool calls — the model thinks it's done. If a /goal is
			// active, ask the evaluator whether the condition is met; on
			// "not met" the evaluator-injected continuation message lands
			// in state.Messages and we keep looping.
			if a.maybeContinueForGoal(state) {
				continue
			}
			state.TransitionReason = ""
			return false
		}

		// ── Todo reminder injection ───────────────────────────────────────────
		tools.GlobalTodo.NoteRound(tools.CheckTodoUsed(resp.Content))
		if reminder := tools.GlobalTodo.Reminder(); reminder != "" {
			toolResults = append(toolResults, anthropic.NewTextBlock(reminder))
		}

		// ── Plan reminder injection ────────────────────────────────────────
		tools.GlobalPlan.NoteRound(tools.CheckPlanUsed(resp.Content))
		if reminder := tools.GlobalPlan.Reminder(); reminder != "" {
			toolResults = append(toolResults, anthropic.NewTextBlock(reminder))
		}

		state.Messages = append(state.Messages, anthropic.NewUserMessage(toolResults...))
		if state.Recorder != nil {
			state.Recorder.AppendUser(state.PromptID, state.Messages[len(state.Messages)-1])
		}
		state.TurnCount++
		state.TransitionReason = "tool_result"

		// Apply manual compaction if the model called the compact tool
		a.manualCompact(state, resp.Content)
	}
}

// Run is the top-level REPL for plain-text mode.
//
// Deprecated: replaced by the unified Repl driver in repl.go. Kept-as-stub
// removed; callers should construct an `agent.NewTerminalFrontend(stdin)`
// and `agent.NewRepl(a, fe, initialHistory).Run()` instead.
//
// (No body — function fully removed; this comment block intentionally
// stays as a breadcrumb so anyone searching for `func (a *Agent) Run`
// finds the migration hint.)

// RunQuery drives one user turn through the agent loop and signals done
// via doneCh. The caller is responsible for everything that happens BEFORE
// the LLM is asked:
//
//  1. construct the `anthropic.MessageParam` (single text block, slash
//     two-block message, /goal kickoff, etc.)
//  2. append it to `*history`
//  3. record it via `a.Session().Recorder.AppendUser(promptID, msg)` if
//     persistence is desired (recorder may be nil)
//
// The `promptID` argument MUST match the one the caller used in step 3 —
// it is threaded through the loop so the assistant + tool_result records
// emitted during this turn share a parent identifier with the user
// message. Callers that don't have a recorder can pass any unique string
// (or `session.NewPromptID()`).
//
// This function only owns what comes after: building `LoopState`, running
// the loop, and writing back the resulting history / compact state.
//
// Used by the unified Repl driver (repl.go); not normally called directly
// from outside the agent package.
func (a *Agent) RunQuery(promptID string, history *[]anthropic.MessageParam, compactState **CompactState, doneCh chan<- struct{}) {
	var recorder *session.Recorder
	if a.session != nil {
		recorder = a.session.Recorder
	}

	state := &LoopState{
		Messages:     *history,
		TurnCount:    1,
		CompactState: *compactState,
		Recorder:     recorder,
		PromptID:     promptID,
	}
	a.Loop(state)
	*history = state.Messages
	*compactState = state.CompactState

	close(doneCh)
}
