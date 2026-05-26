package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"

	"evo-agent/internal/tools"
	"evo-agent/internal/ui"
)

const subagentMaxTurns = 30

// RunSubagent spawns a child agent with the given system prompt and messages.
// The child receives all tools except "task" to prevent recursive spawning.
// Only the final text block is returned to the parent; the child context is discarded.
func (a *Agent) RunSubagent(systemPrompt string, messages []anthropic.MessageParam) string {
	subMessages := make([]anthropic.MessageParam, len(messages))
	copy(subMessages, messages)

	childTools := tools.ToolsExcept("task")
	subSystem := a.cfg.SystemMsg + "\n" + systemPrompt

	var lastText string

	for turn := 0; turn < subagentMaxTurns; turn++ {
		resp, err := a.client.Messages.New(context.Background(), anthropic.MessageNewParams{
			Model:     anthropic.Model(a.cfg.ModelID),
			System:    []anthropic.TextBlockParam{{Text: subSystem}},
			Messages:  subMessages,
			Tools:     childTools,
			MaxTokens: 8000,
		})
		if err != nil {
			return fmt.Sprintf("Subagent error: %v", err)
		}
		subMessages = append(subMessages, resp.ToParam())

		ui.PrintSystem(fmt.Sprintf("[subagent turn %d | %s]", turn+1, resp.StopReason))
		ui.PrintTokens(string(resp.Model), resp.Usage.InputTokens, resp.Usage.OutputTokens, string(resp.StopReason))

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
		subMessages = append(subMessages, anthropic.NewUserMessage(toolResults...))
	}

	if lastText == "" {
		return "(no summary)"
	}
	return lastText
}
