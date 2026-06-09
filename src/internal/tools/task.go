package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"evo-agent/internal/agents"
)

// subagentRunner is the original (anonymous) callback for spawning subagents.
// Set via RegisterSubagentRunner. Kept for backward compatibility with
// callers that don't carry an agent name.
var subagentRunner func(systemPrompt string, messages []anthropic.MessageParam) string

// namedSubagentRunner is the preferred callback that carries the agent name
// so the session layer can label subagent transcripts.
var namedSubagentRunner func(systemPrompt, agentName string, messages []anthropic.MessageParam) string

// customSubagentRunner is the callback that runs a user-defined custom agent
// loaded from .evo-agent/agents/<name>.md. The runner is responsible for
// constructing the message list and system prompt envelope from the agent
// definition; this tool only forwards the user prompt.
var customSubagentRunner func(def agents.AgentDefinition, userPrompt string) string

// forkSubagentRunner is the callback that spawns a fork: a child agent that
// inherits the parent's full conversation history and system prompt, then
// receives a directive to act on. Caller (the task tool handler) supplies
// the parent's current message slice so the runner doesn't need a side
// channel to fetch state.
var forkSubagentRunner func(directive, name string, parentMessages []anthropic.MessageParam) string

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

// RegisterCustomSubagentRunner registers the callback that executes a custom
// agent definition. Called once by agent.New() at startup. When unset, calls
// to task() with subagent_type fall back to an error string.
func RegisterCustomSubagentRunner(fn func(def agents.AgentDefinition, userPrompt string) string) {
	customSubagentRunner = fn
}

// RegisterForkSubagentRunner registers the callback for fork-style subagent
// invocations. Called once by agent.New() at startup.
func RegisterForkSubagentRunner(fn func(directive, name string, parentMessages []anthropic.MessageParam) string) {
	forkSubagentRunner = fn
}

type TaskInput struct {
	Prompt       string `json:"prompt"                  jsonschema_description:"Full task description for the subagent."`
	Description  string `json:"description"             jsonschema_description:"One-line summary of the task shown in the UI."`
	SubagentType string `json:"subagent_type,omitempty" jsonschema_description:"Optional name of a custom agent defined in .evo-agent/agents/. When omitted, a generic subagent runs that inherits the parent's system prompt. When set, the named agent's own system prompt and (optionally) model take over."`
	Fork         bool   `json:"fork,omitempty"          jsonschema_description:"When true, run as a fork: the child inherits the parent's full conversation history and system prompt, then acts on the prompt as a directive. Mutually exclusive with subagent_type. Use for survey questions or implementation work where the intermediate tool output is not worth keeping in your context."`
}

func init() {
	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name: "task",
			Description: anthropic.String(
				"Spawn a subagent with fresh context to complete a subtask. " +
					"The subagent shares the filesystem but not conversation history. " +
					"Only a text summary is returned; the subagent context is discarded. " +
					"Use this to delegate exploration or complex subtasks while keeping the parent context clean. " +
					"Set subagent_type to invoke a specialized custom agent defined in .evo-agent/agents/."),
			InputSchema: GenerateSchema[TaskInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in TaskInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}

			// ── Fork path ──────────────────────────────────────────────
			// fork=true means the child inherits the parent's full
			// conversation context. Mutually exclusive with subagent_type
			// because a custom agent has its own (different) system prompt
			// and history would conflict.
			if in.Fork {
				if strings.TrimSpace(in.SubagentType) != "" {
					return "", fmt.Errorf("fork and subagent_type are mutually exclusive")
				}
				if forkSubagentRunner == nil {
					return "Error: fork subagent runner not initialized", nil
				}
				name := taskAgentName(in.Description)
				parentMsgs := getConversationMessages()
				return forkSubagentRunner(in.Prompt, name, parentMsgs), nil
			}

			// ── Custom agent path ───────────────────────────────────────
			// When subagent_type is set, look up the named agent and route
			// through the custom runner (which uses the agent's own system
			// prompt and optional model override).
			if t := strings.TrimSpace(in.SubagentType); t != "" {
				def, ok := agents.Get(t)
				if !ok {
					return "", fmt.Errorf("unknown subagent_type %q. Available: %s", t, availableAgentNames())
				}
				if customSubagentRunner == nil {
					return "Error: custom subagent runner not initialized", nil
				}
				return customSubagentRunner(def, in.Prompt), nil
			}

			// ── Generic subagent path (current behavior) ───────────────
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

// availableAgentNames returns a comma-joined list of loaded agent names,
// or "(none)" when no custom agents are defined. Used in unknown-type errors.
func availableAgentNames() string {
	names := agents.Names()
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}
