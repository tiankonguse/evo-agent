package tools

import (
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
)

// CompactInput defines the parameters for the compact tool.
type CompactInput struct {
	Focus string `json:"focus,omitempty" jsonschema_description:"What to preserve in the summary."`
}

func init() {
	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name: "compact",
			Description: anthropic.String(
				"Summarize earlier conversation so work can continue in a smaller context.",
			),
			InputSchema: GenerateSchema[CompactInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in CompactInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}

			// Return placeholder - actual compaction happens in loop.go
			return fmt.Sprintf("Compacting conversation (focus: %s)...", in.Focus), nil
		},
	})
}
