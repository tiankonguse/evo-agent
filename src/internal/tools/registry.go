package tools

import "github.com/anthropics/anthropic-sdk-go"

// Tools is the list of tool schemas exposed to the model.
var Tools = []anthropic.ToolUnionUnionParam{
	anthropic.ToolParam{
		Name:        anthropic.F("bash"),
		Description: anthropic.F("Run a shell command in the current workspace."),
		InputSchema: anthropic.F[interface{}](map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type": "string",
				},
			},
			"required": []string{"command"},
		}),
	},
}
