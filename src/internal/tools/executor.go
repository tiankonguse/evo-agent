package tools

import (
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"

	"evo-agent/internal/ui"
)

// Execute iterates over a response's content blocks, prints output for each,
// runs any tool calls, and returns the accumulated tool results.
func Execute(content []anthropic.ContentBlock) []anthropic.ContentBlockParamUnion {
	var results []anthropic.ContentBlockParamUnion

	for _, block := range content {
		switch v := block.AsUnion().(type) {
		case anthropic.ThinkingBlock:
			ui.PrintThinking(v.Thinking)

		case anthropic.TextBlock:
			ui.PrintText(v.Text)

		case anthropic.ToolUseBlock:
			ui.PrintToolCall(v.Name)

			var inputMap map[string]interface{}
			if err := json.Unmarshal(v.Input, &inputMap); err != nil {
				ui.PrintError(fmt.Sprintf("Error: failed to parse tool input: %v", err))
				continue
			}
			command, _ := inputMap["command"].(string)

			ui.PrintCommand(command)
			output := RunBash(command)

			// Print a short preview of the output
			preview := output
			if len(preview) > 200 {
				preview = preview[:200]
			}
			fmt.Println(preview)

			results = append(results, anthropic.NewToolResultBlock(v.ID, output, false))

		default:
			ui.PrintError(fmt.Sprintf("DEBUG: Unknown block type: %T", v))
		}
	}

	return results
}
