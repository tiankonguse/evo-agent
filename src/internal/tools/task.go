package tools

import (
	"encoding/json"

	"github.com/anthropics/anthropic-sdk-go"
)

// subagentRunner is the original (anonymous) callback for spawning subagents.
// Set via RegisterSubagentRunner. Kept for backward compatibility with
// callers that don't carry an agent name.
var subagentRunner func(systemPrompt string, messages []anthropic.MessageParam) string

// namedSubagentRunner is the preferred callback that carries the agent name
// so the session layer can label subagent transcripts.
var namedSubagentRunner func(systemPrompt, agentName string, messages []anthropic.MessageParam) string

// RegisterSubagentRunner registers the legacy unnamed callback.
// Called once by agent.New() at startup.
func RegisterSubagentRunner(fn func(systemPrompt string, messages []anthropic.MessageParam) string) {
	subagentRunner = fn
}

// RegisterNamedSubagentRunner registers the preferred callback that takes an
// agent name. Falls back to the unnamed runner when not set.
func RegisterNamedSubagentRunner(fn func(systemPrompt, agentName string, messages []anthropic.MessageParam) string) {
	namedSubagentRunner = fn
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
			// Task tool: system prompt is default subagent instruction,
			// messages are just the user's task prompt.
			sysPrompt := "You are a subagent. Complete the given task using the available tools, then summarize your findings concisely."
			messages := []anthropic.MessageParam{
				anthropic.NewUserMessage(anthropic.NewTextBlock(in.Prompt)),
			}

			// Prefer the named runner so the session layer can persist the
			// subagent transcript under a meaningful name.
			agentName := taskAgentName(in.Description)
			if namedSubagentRunner != nil {
				return namedSubagentRunner(sysPrompt, agentName, messages), nil
			}
			if subagentRunner != nil {
				return subagentRunner(sysPrompt, messages), nil
			}
			return "Error: subagent runner not initialized", nil
		},
	})
}

// taskAgentName derives a short agent label from the description. The session
// layer will further slugify it for filesystem use; here we just trim length.
func taskAgentName(desc string) string {
	if desc == "" {
		return "task"
	}
	if len(desc) > 32 {
		desc = desc[:32]
	}
	return desc
}
