package tools

import (
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"

	"evo-agent/internal/ui"
)

// Execute iterates over a response's content blocks, prints output for each,
// runs any tool calls via the registry, and returns the accumulated tool results.
func Execute(content []anthropic.ContentBlockUnion) []anthropic.ContentBlockParamUnion {
	var results []anthropic.ContentBlockParamUnion

	for _, block := range content {
		switch v := block.AsAny().(type) {
		case anthropic.ThinkingBlock:
			ui.PrintThinking(v.Thinking)

		case anthropic.TextBlock:
			ui.PrintText(v.Text)

		case anthropic.ToolUseBlock:
			ui.PrintToolCall(v.Name)
			ui.PrintCommand(fmt.Sprintf("%s(%s)", v.Name, v.JSON.Input.Raw()))

			inputBytes, _ := json.Marshal(v.Input)
			output, err := Dispatch(v.Name, inputBytes)
			isError := false
			if err != nil {
				output = err.Error()
				isError = true
				ui.PrintError(fmt.Sprintf("Error: %v", err))
			} else {
				preview := output
				if len(preview) > 200 {
					preview = preview[:200]
				}
				fmt.Println(preview)
			}

			results = append(results, anthropic.NewToolResultBlock(v.ID, output, isError))

		default:
			ui.PrintError(fmt.Sprintf("DEBUG: Unknown block type: %T", v))
		}
	}

	return results
}
