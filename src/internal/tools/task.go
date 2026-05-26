package tools

import (
	"encoding/json"

	"github.com/anthropics/anthropic-sdk-go"
)

// subagentRunner is the registered callback for spawning subagents.
// Set via RegisterSubagentRunner to avoid an import cycle (agent → tools).
var subagentRunner func(systemPrompt string, messages []anthropic.MessageParam) string

// RegisterSubagentRunner registers the function used to spawn subagents.
// Called once by agent.New() at startup.
func RegisterSubagentRunner(fn func(systemPrompt string, messages []anthropic.MessageParam) string) {
	subagentRunner = fn
}

type TaskInput struct {
	Prompt      string `json:"prompt"      jsonschema_description:"Full task description for the subagent."`
	Description string `json:"description" jsonschema_description:"One-line summary of the task shown in the UI."`
}

func init() {
	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name: "task",
			Description: anthropic.String(
				"Spawn a subagent with fresh context to complete a subtask. " +
					"The subagent shares the filesystem but not conversation history. " +
					"Only a text summary is returned; the subagent context is discarded. " +
					"Use this to delegate exploration or complex subtasks while keeping the parent context clean."),
			InputSchema: GenerateSchema[TaskInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in TaskInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}
			if subagentRunner == nil {
				return "Error: subagent runner not initialized", nil
			}
			// Task tool: system prompt is default subagent instruction,
			// messages are just the user's task prompt.
			sysPrompt := "You are a subagent. Complete the given task using the available tools, then summarize your findings concisely."
			messages := []anthropic.MessageParam{
				anthropic.NewUserMessage(anthropic.NewTextBlock(in.Prompt)),
			}
			return subagentRunner(sysPrompt, messages), nil
		},
	})
}
