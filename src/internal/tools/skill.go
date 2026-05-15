package tools

import (
	"encoding/json"

	"github.com/anthropics/anthropic-sdk-go"

	"evo-agent/internal/skills"
)

// loadSkillInput is the parameter struct for the load_skill tool.
type loadSkillInput struct {
	Name string `json:"name" jsonschema_description:"Name of the skill to load"`
}

func init() {
	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name: "load_skill",
			Description: anthropic.String(
				"Load the full body of a named skill into the current context. " +
					"Call this before acting on a task that needs specialized instructions.",
			),
			InputSchema: GenerateSchema[loadSkillInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in loadSkillInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}
			return skills.Load(in.Name), nil
		},
	})
}
