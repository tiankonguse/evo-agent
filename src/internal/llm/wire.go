package llm

import "encoding/json"

// openaiRequest is the subset of the OpenAI Chat Completions request
// body that the OpenAI adapter ever needs to send. Fields the adapter
// does not use are deliberately omitted so a malformed translation
// fails loudly rather than silently passing through.
//
// Reference: https://platform.openai.com/docs/api-reference/chat/create
type openaiRequest struct {
	Model       string          `json:"model"`
	Messages    []openaiMessage `json:"messages"`
	MaxTokens   int64           `json:"max_tokens,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	TopP        *float64        `json:"top_p,omitempty"`
	Stop        []string        `json:"stop,omitempty"`
	Tools       []openaiTool    `json:"tools,omitempty"`
	ToolChoice  any             `json:"tool_choice,omitempty"`
}

// openaiMessage is one entry in the OpenAI request `messages` array.
// Role takes one of "system" | "user" | "assistant" | "tool". Content
// is a plain string for everything except (a) `tool_calls`-only
// assistant turns where it is empty, and (b) tool messages where it
// holds the tool result body.
type openaiMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content"`
	Name       string           `json:"name,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openaiToolCall `json:"tool_calls,omitempty"`
}

// openaiToolCall mirrors a single entry in `assistant.tool_calls`. Note
// `arguments` is a JSON-string at the wire level — the model emits the
// tool input as text; OpenAI does not unmarshal it for us.
type openaiToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"` // always "function"
	Function openaiToolCallFunc `json:"function"`
}

type openaiToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// openaiTool is one entry in the request `tools` array. The
// `parameters` schema is a passthrough of the tool's JSON-Schema
// `input_schema` from the Anthropic tool definition.
type openaiTool struct {
	Type     string         `json:"type"` // always "function"
	Function openaiToolDecl `json:"function"`
}

type openaiToolDecl struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// openaiResponse is the subset of `/v1/chat/completions` response we
// read. We never parse log probs, system fingerprint, or service tier.
type openaiResponse struct {
	ID      string         `json:"id"`
	Model   string         `json:"model"`
	Choices []openaiChoice `json:"choices"`
	Usage   openaiUsage    `json:"usage"`
}

type openaiChoice struct {
	Index        int                 `json:"index"`
	FinishReason string              `json:"finish_reason"`
	Message      openaiChoiceMessage `json:"message"`
}

type openaiChoiceMessage struct {
	Role      string           `json:"role"` // always "assistant"
	Content   *string          `json:"content"`
	ToolCalls []openaiToolCall `json:"tool_calls"`
	// Refusal is set when finish_reason == "content_filter" on some
	// model variants. We surface it as the text body so the model has
	// something to look at rather than silently dropping it.
	Refusal *string `json:"refusal"`
}

type openaiUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

// rawArguments unmarshals the OpenAI `arguments` JSON-string into a
// generic value suitable for placement in an Anthropic tool_use block's
// `input` field. The two-step unmarshal mirrors the OpenAI wire format
// exactly: arguments is a JSON-encoded string whose body is itself
// JSON. Empty / null arguments map to {} so the agent's tool dispatcher
// always receives an object.
func rawArguments(args string) any {
	if args == "" || args == "null" {
		return map[string]any{}
	}
	var v any
	if err := json.Unmarshal([]byte(args), &v); err != nil {
		// If the model produced malformed JSON we still want to surface
		// something downstream rather than crash; return an object that
		// preserves the raw text for debugging.
		return map[string]any{"_raw": args}
	}
	return v
}
