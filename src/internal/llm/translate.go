package llm

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
)

// paramsToOpenAI translates an anthropic.MessageNewParams into the
// equivalent OpenAI Chat Completions request body.
//
// What is dropped on purpose (scope-decision: OpenAI Chat Completions
// only, no streaming, no thinking surface):
//   - RedactedThinking blocks (Anthropic-only)
//   - CacheControl breakpoints (Anthropic-only)
//   - All non-text tool-result content variants (image, search_result,
//     document, tool_reference) — we only forward the joined text body
//   - Image / Document / SearchResult input blocks (out of scope for
//     this first cut)
//   - All non-OfTool ToolUnionParam variants (server tools, code
//     execution, web search) — they have no OpenAI equivalent
//   - TopK (no OpenAI counterpart on chat completions)
//
// Translated:
//   - Thinking config (params.Thinking.OfEnabled) → reasoning_effort +
//     enable_thinking + reasoning + thinking fields (multi-dialect; see
//     wire.go). Skipped entirely when OPENAI_THINKING=0.
//
// The function is intentionally pure so it can be unit-tested without a
// network round-trip. See translate_test.go for the table-driven suite
// that pins each rule above.
func paramsToOpenAI(params anthropic.MessageNewParams) openaiRequest {
	out := openaiRequest{
		Model:     string(params.Model),
		MaxTokens: params.MaxTokens,
	}
	if params.Temperature.Valid() {
		v := params.Temperature.Or(0)
		out.Temperature = &v
	}
	if params.TopP.Valid() {
		v := params.TopP.Or(0)
		out.TopP = &v
	}
	if len(params.StopSequences) > 0 {
		out.Stop = append([]string(nil), params.StopSequences...)
	}

	// Reasoning / thinking fields (multi-dialect). Read the canonical
	// Thinking config from the params; emit OpenAI / Qwen / OpenRouter
	// variants together. OPENAI_THINKING=0 short-circuits this for
	// strict servers that 400 on unknown fields.
	if shouldEmitOpenAIThinking() {
		applyThinkingToOpenAI(&out, params)
	}

	// Leading system message: concatenate every TextBlockParam so the
	// model sees one cohesive system prompt. Empty system stays empty.
	if sys := joinSystemText(params.System); sys != "" {
		out.Messages = append(out.Messages, openaiMessage{
			Role:    "system",
			Content: sys,
		})
	}

	for _, m := range params.Messages {
		out.Messages = append(out.Messages, messageParamToOpenAI(m)...)
	}

	for _, t := range params.Tools {
		if t.OfTool == nil {
			// Drop server tools / code execution / web search — they have
			// no OpenAI Chat Completions equivalent. Native function
			// tools registered via tools.Register are the only variant
			// evo-agent uses today, so this is the expected path.
			continue
		}
		out.Tools = append(out.Tools, toolParamToOpenAI(*t.OfTool))
	}

	return out
}

// shouldEmitOpenAIThinking returns false only when OPENAI_THINKING is
// explicitly disabled. Default is on so the always-on thinking promise
// holds for OpenAI-compatible providers without per-deployment config.
func shouldEmitOpenAIThinking() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("OPENAI_THINKING"))) {
	case "0", "false", "no", "off", "disabled":
		return false
	}
	return true
}

// openaiReasoningEffort lets the user override the effort label sent to
// OpenAI-compatible providers. Default "medium" is a safe middle ground
// for budget≈2048. Explicit override wins over the budget-derived map.
func openaiReasoningEffort(budget int64) string {
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("OPENAI_REASONING_EFFORT"))); v != "" {
		switch v {
		case "minimal", "low", "medium", "high":
			return v
		}
	}
	switch {
	case budget <= 0:
		return "medium"
	case budget < 2048:
		return "low"
	case budget < 4096:
		return "medium"
	default:
		return "high"
	}
}

// applyThinkingToOpenAI mutates `out` in place to add the multi-dialect
// reasoning fields. Reads from params.Thinking — when the caller hasn't
// enabled thinking we still emit a sensible "medium" effort so OpenAI's
// reasoning models (o1/o3/…) think by default. Anthropic's withDefault
// Thinking sets params.Thinking when MaxTokens is large enough; for the
// OpenAI path we mirror that intent and always emit reasoning fields.
func applyThinkingToOpenAI(out *openaiRequest, params anthropic.MessageNewParams) {
	var budget int64
	if !param.IsOmitted(params.Thinking.OfEnabled) {
		budget = params.Thinking.OfEnabled.BudgetTokens
	}
	effort := openaiReasoningEffort(budget)
	out.ReasoningEffort = effort
	yes := true
	out.EnableThinking = &yes
	out.Reasoning = &openaiReasoning{Effort: effort, MaxTokens: budget}
	if budget > 0 {
		out.Thinking = &openaiThinking{Type: "enabled", BudgetTokens: budget}
	}
}

// joinSystemText concatenates the Text fields of a System slice with
// "\n\n" separators. Empty entries are skipped so we don't emit
// stray blank lines.
func joinSystemText(blocks []anthropic.TextBlockParam) string {
	var parts []string
	for _, b := range blocks {
		if b.Text == "" {
			continue
		}
		parts = append(parts, b.Text)
	}
	return strings.Join(parts, "\n\n")
}

// messageParamToOpenAI converts a single anthropic MessageParam (which
// holds an array of content blocks) into one or more OpenAI messages.
// User-role expansion is the interesting case: each tool_result block
// becomes its own {role:"tool"} message, with consecutive text blocks
// coalesced into one trailing {role:"user"} message.
func messageParamToOpenAI(m anthropic.MessageParam) []openaiMessage {
	switch m.Role {
	case anthropic.MessageParamRoleAssistant:
		return []openaiMessage{assistantBlocksToOpenAI(m.Content)}
	default: // user (and any unknown role we'll treat as user)
		return userBlocksToOpenAI(m.Content)
	}
}

// assistantBlocksToOpenAI emits exactly one assistant message. Multiple
// text blocks are concatenated with "\n"; tool_use blocks become
// tool_calls; thinking and redacted_thinking blocks are dropped.
func assistantBlocksToOpenAI(blocks []anthropic.ContentBlockParamUnion) openaiMessage {
	msg := openaiMessage{Role: "assistant"}
	var textParts []string
	for _, b := range blocks {
		switch {
		case b.OfText != nil:
			if b.OfText.Text != "" {
				textParts = append(textParts, b.OfText.Text)
			}
		case b.OfToolUse != nil:
			argsBytes, err := json.Marshal(b.OfToolUse.Input)
			if err != nil || string(argsBytes) == "null" {
				argsBytes = []byte("{}")
			}
			msg.ToolCalls = append(msg.ToolCalls, openaiToolCall{
				ID:   b.OfToolUse.ID,
				Type: "function",
				Function: openaiToolCallFunc{
					Name:      b.OfToolUse.Name,
					Arguments: string(argsBytes),
				},
			})
		default:
			// Drop OfThinking, OfRedactedThinking, OfServerToolUse, etc.
			// silently — they have no OpenAI representation.
		}
	}
	msg.Content = strings.Join(textParts, "\n")
	return msg
}

// userBlocksToOpenAI walks user-side blocks in encounter order. Each
// tool_result becomes its own {role:"tool"} message. Adjacent text
// blocks are coalesced and emitted as a single {role:"user"} message
// after any preceding tool messages. This matches the only flow
// evo-agent's loop generates today: tools.Execute() emits a batch of
// tool results, then loop.go appends the optional reminder text block
// (loop.go:215, 221).
func userBlocksToOpenAI(blocks []anthropic.ContentBlockParamUnion) []openaiMessage {
	var out []openaiMessage
	var pendingText []string

	flushText := func() {
		if len(pendingText) == 0 {
			return
		}
		out = append(out, openaiMessage{
			Role:    "user",
			Content: strings.Join(pendingText, "\n"),
		})
		pendingText = nil
	}

	for _, b := range blocks {
		switch {
		case b.OfToolResult != nil:
			// Each tool_result must precede any user text in the same
			// message — the assistant turn that produced these
			// tool_calls is the *previous* MessageParam, so the tool
			// messages bind to it. Flush any *prior* text to preserve
			// chronological order; in practice evo-agent never
			// interleaves text before tool_result inside a single user
			// MessageParam, so this branch is rarely hit.
			flushText()
			out = append(out, openaiMessage{
				Role:       "tool",
				ToolCallID: b.OfToolResult.ToolUseID,
				Content:    stringifyToolResult(*b.OfToolResult),
			})
		case b.OfText != nil:
			if b.OfText.Text != "" {
				pendingText = append(pendingText, b.OfText.Text)
			}
		default:
			// Drop OfImage, OfDocument, OfSearchResult, etc. silently.
		}
	}
	flushText()
	return out
}

// stringifyToolResult flattens a ToolResultBlockParam into a single
// string. Text content variants are joined with "\n"; non-text content
// (image, document, search_result, tool_reference) is dropped because
// OpenAI's `tool` role only accepts a string body. If IsError is set,
// the result is prefixed with "[error] " so the model can still tell
// the call failed (OpenAI tool messages have no separate error flag).
func stringifyToolResult(r anthropic.ToolResultBlockParam) string {
	var parts []string
	for _, c := range r.Content {
		if c.OfText != nil && c.OfText.Text != "" {
			parts = append(parts, c.OfText.Text)
		}
	}
	body := strings.Join(parts, "\n")
	if r.IsError.Or(false) {
		if body == "" {
			return "[error]"
		}
		return "[error] " + body
	}
	return body
}

// toolParamToOpenAI converts a single anthropic.ToolParam into the
// OpenAI tool declaration shape. The InputSchema is round-tripped
// through JSON so OpenAI receives a plain map[string]any (its
// `function.parameters` field is unstructured JSON-Schema).
func toolParamToOpenAI(t anthropic.ToolParam) openaiTool {
	out := openaiTool{
		Type: "function",
		Function: openaiToolDecl{
			Name:        t.Name,
			Description: t.Description.Or(""),
		},
	}
	// The schema is best-effort: an unmarshalable schema becomes empty
	// rather than failing the whole request. evo-agent's GenerateSchema
	// always produces a valid object schema so this guard is defensive.
	if raw, err := json.Marshal(t.InputSchema); err == nil {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err == nil {
			out.Function.Parameters = m
		}
	}
	return out
}

// openAIToMessage synthesizes an *anthropic.Message from an OpenAI
// response.
//
// IMPORTANT — the JSON-marshal-then-UnmarshalJSON dance is deliberate.
// The Anthropic SDK's ContentBlockUnion.AsAny() and Message.ToParam()
// rely on the unexported `JSON.raw` field that only
// apijson.UnmarshalRoot populates. Field-filling a Message struct
// directly leaves AsAny() returning zero values and ToParam() emitting
// empty content, which silently corrupts the agent's history within a
// single round-trip. By marshalling a synthetic anthropic-shaped map
// and feeding it back through (*Message).UnmarshalJSON, we let the SDK
// rebuild every internal cache exactly as if the message came off the
// wire from the real Anthropic API.
func openAIToMessage(resp *openaiResponse) (*anthropic.Message, error) {
	if resp == nil || len(resp.Choices) == 0 {
		return nil, fmt.Errorf("openai response has no choices")
	}
	choice := resp.Choices[0]

	content := []map[string]any{}

	// Reasoning / thinking content (DeepSeek's reasoning_content +
	// OpenRouter's reasoning) is surfaced as an Anthropic thinking block
	// so the TUI's EvThinking renderer + agent.FilterThinking strip both
	// work uniformly across providers. The signature is empty (we have
	// no real Anthropic signature for OpenAI-derived thinking, and
	// FilterThinking strips the block before re-feeding to the LLM so
	// the missing signature never causes a wire-level rejection).
	if think := extractReasoningText(choice.Message); think != "" {
		content = append(content, map[string]any{
			"type":      "thinking",
			"thinking":  think,
			"signature": "",
		})
	}

	if choice.Message.Content != nil && *choice.Message.Content != "" {
		content = append(content, map[string]any{
			"type": "text",
			"text": *choice.Message.Content,
		})
	} else if choice.Message.Refusal != nil && *choice.Message.Refusal != "" {
		// Surface refusals as text so downstream block walking still
		// sees something. The stop_reason carries the categorical
		// signal separately.
		content = append(content, map[string]any{
			"type": "text",
			"text": *choice.Message.Refusal,
		})
	}
	for _, tc := range choice.Message.ToolCalls {
		content = append(content, map[string]any{
			"type":  "tool_use",
			"id":    tc.ID,
			"name":  tc.Function.Name,
			"input": rawArguments(tc.Function.Arguments),
		})
	}
	if len(content) == 0 {
		// Anthropic's wire shape requires at least one content block;
		// emit an empty text block so consumers (block walking,
		// ToParam) keep working.
		content = append(content, map[string]any{"type": "text", "text": ""})
	}

	synth := map[string]any{
		"id":            resp.ID,
		"type":          "message",
		"role":          "assistant",
		"model":         resp.Model,
		"content":       content,
		"stop_reason":   string(mapFinishReason(choice.FinishReason)),
		"stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":  resp.Usage.PromptTokens,
			"output_tokens": resp.Usage.CompletionTokens,
		},
	}

	raw, err := json.Marshal(synth)
	if err != nil {
		return nil, fmt.Errorf("synthesize anthropic message: marshal: %w", err)
	}
	var msg anthropic.Message
	if err := msg.UnmarshalJSON(raw); err != nil {
		return nil, fmt.Errorf("synthesize anthropic message: unmarshal: %w", err)
	}
	return &msg, nil
}

// mapFinishReason translates an OpenAI `finish_reason` to the closest
// Anthropic stop_reason. Unknown values fall back to "end_turn"
// because the agent loop's continuation decision is driven by block
// presence (any tool_use → keep looping), not stop_reason itself.
func mapFinishReason(r string) anthropic.StopReason {
	switch r {
	case "stop":
		return anthropic.StopReasonEndTurn
	case "tool_calls", "function_call":
		return anthropic.StopReasonToolUse
	case "length":
		return anthropic.StopReasonMaxTokens
	case "content_filter":
		return anthropic.StopReasonRefusal
	default:
		return anthropic.StopReasonEndTurn
	}
}

// extractReasoningText pulls chain-of-thought text out of an OpenAI
// response message. Two dialects are accepted:
//
//   - DeepSeek's `reasoning_content` (string) — preferred when present
//   - OpenRouter's `reasoning` (string OR object {"text":"…"} OR
//     {"effort":"…","content":"…"}) — best-effort decode
//
// Returns "" when neither field carries usable text. The result is
// embedded as an Anthropic thinking block so the TUI / FilterThinking
// pipeline treats it identically to native Anthropic thinking.
func extractReasoningText(msg openaiChoiceMessage) string {
	if msg.ReasoningContent != nil && *msg.ReasoningContent != "" {
		return *msg.ReasoningContent
	}
	if len(msg.Reasoning) == 0 {
		return ""
	}
	// Try string first — OpenRouter sometimes returns a plain string.
	var s string
	if err := json.Unmarshal(msg.Reasoning, &s); err == nil && s != "" {
		return s
	}
	// Fall back to object with a text-bearing field.
	var obj map[string]any
	if err := json.Unmarshal(msg.Reasoning, &obj); err != nil {
		return ""
	}
	for _, k := range []string{"text", "content", "reasoning", "thinking"} {
		if v, ok := obj[k]; ok {
			if vs, ok := v.(string); ok && vs != "" {
				return vs
			}
		}
	}
	return ""
}
