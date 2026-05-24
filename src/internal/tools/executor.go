package tools

import (
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"

	"evo-agent/internal/ui"
)

const previewPrintLen = 200 // Max chars of a tool result printed in plain mode

// Execute iterates over a response's content blocks, emits UI events for each,
// runs any tool calls via the registry, and returns the accumulated tool results.
func Execute(content []anthropic.ContentBlockUnion, compactState interface{}) []anthropic.ContentBlockParamUnion {
	var results []anthropic.ContentBlockParamUnion

	for _, block := range content {
		switch v := block.AsAny().(type) {
		case anthropic.ThinkingBlock:
			ui.PrintThinking(v.Thinking)

		case anthropic.TextBlock:
			ui.PrintText(v.Text)

		case anthropic.ToolUseBlock:
			inputRaw := v.JSON.Input.Raw()
			ui.PrintToolCall(v.ID, v.Name, inputRaw)
			ui.PrintCommand(fmt.Sprintf("%s(%s)", v.Name, inputRaw))

			inputBytes, _ := json.Marshal(v.Input)
			output, err := Dispatch(v.Name, inputBytes)
			isError := false
			if err != nil {
				output = err.Error()
				isError = true
				ui.PrintError(fmt.Sprintf("Error: %v", err))
			} else {
				output = PersistLargeOutput(v.ID, output)
			}

			ui.PrintToolResult(v.ID, output, isError)
			results = append(results, anthropic.NewToolResultBlock(v.ID, output, isError))

		default:
			ui.PrintError(fmt.Sprintf("DEBUG: Unknown block type: %T", v))
		}
	}

	return results
}
