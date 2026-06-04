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

// RunSubagent spawns a child agent with the given system prompt and messages.
// The child receives all tools except "task" to prevent recursive spawning.
// Only the final text block is returned to the parent; the child message
// history is discarded from the parent loop's perspective.
//
// agentName is used to slugify the subagent transcript filename. If empty,
// "task" is used as the default.
//
// Persistence: when the parent loop has an active recorder (set via
// LoopState.Recorder), this method:
//   1. Creates a sidechain transcript at sessions/<sid>/subagent/<file>.jsonl
//   2. Writes a subagent_start record to the parent transcript pointing at it
//   3. Records every sub-message into the sidechain
//   4. Writes a subagent_end record (with the final text) to the parent
func (a *Agent) RunSubagent(systemPrompt string, messages []anthropic.MessageParam, agentName string) string {
	if agentName == "" {
		agentName = "task"
	}

	subMessages := make([]anthropic.MessageParam, len(messages))
	copy(subMessages, messages)

	childTools := tools.ToolsExcept("task")
	subSystem := a.prompt.Build() + "\n" + systemPrompt

	// ── Set up the subagent sidechain recorder if persistence is active ──
	var subRec *session.SubagentRecorder
	parentRec := a.currentRecorder
	parentPID := a.currentPromptID
	if parentRec != nil && a.session != nil {
		var err error
		subRec, err = session.NewSubagentRecorder(a.session, agentName)
		if err != nil {
			ui.PrintError(fmt.Sprintf("[subagent] sidechain create failed: %v", err))
		} else {
			parentRec.AppendSubagentStart(parentPID, agentName, subRec.Filename)
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

	for turn := 0; turn < subagentMaxTurns; turn++ {
		resp, err := a.provider.SendMessage(context.Background(), anthropic.MessageNewParams{
			Model:     anthropic.Model(a.cfg.ModelID),
			System:    []anthropic.TextBlockParam{{Text: subSystem}},
			Messages:  FilterThinking(subMessages),
			Tools:     childTools,
			MaxTokens: 8000,
		})
		if err != nil {
			result := fmt.Sprintf("Subagent error: %v", err)
			if subRec != nil {
				parentRec.AppendSubagentEnd(parentPID, agentName, subRec.Filename, result)
			}
			return result
		}
		assistantMsg := resp.ToParam()
		subMessages = append(subMessages, assistantMsg)
		if subRec != nil {
			subRec.AppendAssistant(parentPID, assistantMsg, resp.Usage.InputTokens, resp.Usage.OutputTokens)
		}

		ui.PrintSystem(fmt.Sprintf("[subagent turn %d | %s]", turn+1, resp.StopReason))

		// Count content block types for subagent
		blockCounts := map[string]int{}
		for _, block := range resp.Content {
			blockCounts[string(block.Type)]++
		}
		var blockParts []string
		for t, c := range blockCounts {
			blockParts = append(blockParts, fmt.Sprintf("%s:%d", t, c))
		}
		ui.PrintTokens(string(resp.Model), resp.Usage.InputTokens, resp.Usage.OutputTokens, string(resp.StopReason), strings.Join(blockParts, " "))

		var toolResults []anthropic.ContentBlockParamUnion
		lastText = ""

		for _, block := range resp.Content {
			switch v := block.AsAny().(type) {
			case anthropic.TextBlock:
				lastText = v.Text
				ui.PrintText("[subagent] " + v.Text)

			case anthropic.ToolUseBlock:
				inputRaw := v.JSON.Input.Raw()
				ui.PrintToolCall(v.ID, "[sub] "+v.Name, inputRaw)
				ui.PrintCommand(fmt.Sprintf("[sub] %s(%s)", v.Name, inputRaw))

				inputBytes, _ := json.Marshal(v.Input)
				output, dispErr := tools.Dispatch(v.Name, inputBytes)
				isError := dispErr != nil
				if isError {
					output = dispErr.Error()
					ui.PrintError(fmt.Sprintf("[subagent] Error: %v", dispErr))
				} else {
					output = tools.PersistLargeOutput(v.ID, output)
				}

				ui.PrintToolResult(v.ID, output, isError)
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
		parentRec.AppendSubagentEnd(parentPID, agentName, subRec.Filename, lastText)
	}
	return lastText
}
