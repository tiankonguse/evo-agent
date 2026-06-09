package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"evo-agent/internal/session"
	"evo-agent/internal/tools"
	"evo-agent/internal/ui"
)

const subagentMaxTurns = 30

// subagentRunConfig is the parameter bundle for runSubagentLoop. Splitting
// out the config struct lets RunSubagent and RunCustomSubagent share the
// turn-loop without growing a 7-arg helper.
type subagentRunConfig struct {
	// SystemText is the fully-assembled system prompt sent to the LLM.
	// The two callers build this differently:
	//   - RunSubagent: parent's prompt + caller-supplied addendum
	//   - RunCustomSubagent: just the agent's own prompt + minimal envelope
	SystemText string

	// AgentName labels the subagent in the session sidechain filename and
	// in UI events. Empty string is replaced with "task".
	AgentName string

	// ModelID is the Anthropic model id for the SendMessage call. Empty
	// string is replaced with the parent's a.cfg.ModelID.
	ModelID string

	// MaxTurns caps the number of LLM round-trips. Zero or negative falls
	// back to subagentMaxTurns (30).
	MaxTurns int

	// Messages is the initial conversation. The loop appends assistant and
	// tool-result messages to this slice but does not return it.
	Messages []anthropic.MessageParam

	// Tools is the tool schema list exposed to the LLM. Callers should pass
	// tools.ToolsExcept("task") to prevent recursive subagent spawning
	// (matches Claude Code's recursion guard).
	Tools []anthropic.ToolUnionParam
}

// RunSubagent spawns a child agent that inherits the parent's system prompt
// (with a caller-supplied addendum). The child receives all tools except
// "task" to prevent recursive spawning. Only the final text block is
// returned to the parent; the child message history is discarded from the
// parent loop's perspective.
//
// agentName is used to slugify the subagent transcript filename. If empty,
// "task" is used as the default.
//
// Persistence: when the parent loop has an active recorder (set via
// LoopState.Recorder), this method:
//  1. Creates a sidechain transcript at sessions/<sid>/subagent/<file>.jsonl
//  2. Writes a subagent_start record to the parent transcript pointing at it
//  3. Records every sub-message into the sidechain
//  4. Writes a subagent_end record (with the final text) to the parent
func (a *Agent) RunSubagent(systemPrompt string, messages []anthropic.MessageParam, agentName string) string {
	if agentName == "" {
		agentName = "task"
	}

	subMessages := make([]anthropic.MessageParam, len(messages))
	copy(subMessages, messages)

	return a.runSubagentLoop(subagentRunConfig{
		SystemText: a.prompt.Build() + "\n" + systemPrompt,
		AgentName:  agentName,
		ModelID:    a.cfg.ModelID,
		MaxTurns:   subagentMaxTurns,
		Messages:   subMessages,
		Tools:      tools.ToolsExcept("task"),
	})
}

// runSubagentLoop is the shared turn-loop for both RunSubagent and
// RunCustomSubagent. Returns the last text block emitted by the child.
//
// The loop performs:
//  1. Optional sidechain recorder setup if the parent loop has persistence
//     active. The parent's recorder gets subagent_start/_end markers.
//  2. Up to cfg.MaxTurns rounds of: SendMessage → emit UI events →
//     dispatch tool calls → append tool results.
//  3. Termination on either end_turn (no tool calls) or MaxTurns cap.
//
// All UI events are tagged with cfg.AgentName so the TUI / plain sink
// can distinguish parent from sub output.
func (a *Agent) runSubagentLoop(cfg subagentRunConfig) string {
	if cfg.AgentName == "" {
		cfg.AgentName = "task"
	}
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = subagentMaxTurns
	}
	if cfg.ModelID == "" {
		cfg.ModelID = a.cfg.ModelID
	}
	if cfg.Tools == nil {
		cfg.Tools = tools.ToolsExcept("task")
	}

	subMessages := cfg.Messages

	// ── Set up the subagent sidechain recorder if persistence is active ──
	var subRec *session.SubagentRecorder
	parentRec := a.currentRecorder
	parentPID := a.currentPromptID
	if parentRec != nil && a.session != nil {
		var err error
		subRec, err = session.NewSubagentRecorder(a.session, cfg.AgentName)
		if err != nil {
			ui.PrintErrorAs(cfg.AgentName, fmt.Sprintf("sidechain create failed: %v", err))
		} else {
			parentRec.AppendSubagentStart(parentPID, cfg.AgentName, subRec.Filename)
			// Persist the inherited prompt(s) at the head of the sidechain so
			// the file is self-contained for forensic review.
			for _, m := range subMessages {
				if m.Role == anthropic.MessageParamRoleUser {
					subRec.AppendUser(parentPID, m)
				} else {
					subRec.AppendAssistant(parentPID, m, 0, 0)
				}
			}
		}
	}

	var lastText string

	for turn := 0; turn < cfg.MaxTurns; turn++ {
		resp, err := a.provider.SendMessage(context.Background(), anthropic.MessageNewParams{
			Model:     anthropic.Model(cfg.ModelID),
			System:    []anthropic.TextBlockParam{{Text: cfg.SystemText}},
			Messages:  FilterThinking(subMessages),
			Tools:     cfg.Tools,
			MaxTokens: 8000,
		})
		if err != nil {
			result := fmt.Sprintf("Subagent error: %v", err)
			if subRec != nil {
				parentRec.AppendSubagentEnd(parentPID, cfg.AgentName, subRec.Filename, result)
			}
			return result
		}
		assistantMsg := resp.ToParam()
		subMessages = append(subMessages, assistantMsg)
		if subRec != nil {
			subRec.AppendAssistant(parentPID, assistantMsg, resp.Usage.InputTokens, resp.Usage.OutputTokens)
		}

		ui.PrintSystemAs(cfg.AgentName, fmt.Sprintf("turn %d | %s", turn+1, resp.StopReason))

		// Count content block types for subagent
		blockCounts := map[string]int{}
		for _, block := range resp.Content {
			blockCounts[string(block.Type)]++
		}
		var blockParts []string
		for t, c := range blockCounts {
			blockParts = append(blockParts, fmt.Sprintf("%s:%d", t, c))
		}
		ui.PrintTokensAs(cfg.AgentName, string(resp.Model), resp.Usage.InputTokens, resp.Usage.OutputTokens, string(resp.StopReason), strings.Join(blockParts, " "))

		var toolResults []anthropic.ContentBlockParamUnion
		lastText = ""

		for _, block := range resp.Content {
			switch v := block.AsAny().(type) {
			case anthropic.TextBlock:
				lastText = v.Text
				ui.PrintTextAs(cfg.AgentName, v.Text)

			case anthropic.ToolUseBlock:
				inputRaw := v.JSON.Input.Raw()
				ui.PrintToolCallAs(cfg.AgentName, v.ID, v.Name, inputRaw)
				ui.PrintCommandAs(cfg.AgentName, fmt.Sprintf("%s(%s)", v.Name, inputRaw))

				inputBytes, _ := json.Marshal(v.Input)
				output, dispErr := tools.Dispatch(v.Name, inputBytes)
				isError := dispErr != nil
				if isError {
					output = dispErr.Error()
					ui.PrintErrorAs(cfg.AgentName, fmt.Sprintf("Error: %v", dispErr))
				} else {
					output = tools.PersistLargeOutput(v.ID, output)
				}

				ui.PrintToolResultAs(cfg.AgentName, v.ID, output, isError)
				toolResults = append(toolResults, anthropic.NewToolResultBlock(v.ID, output, isError))
			}
		}

		if len(toolResults) == 0 {
			break // no more tool calls — subagent is done
		}
		userMsg := anthropic.NewUserMessage(toolResults...)
		subMessages = append(subMessages, userMsg)
		if subRec != nil {
			subRec.AppendUser(parentPID, userMsg)
		}
	}

	if lastText == "" {
		lastText = "(no summary)"
	}

	if subRec != nil {
		parentRec.AppendSubagentEnd(parentPID, cfg.AgentName, subRec.Filename, lastText)
	}
	return lastText
}
