package agent

import "github.com/anthropics/anthropic-sdk-go"

// filter.go — pre-LLM message filtering.
//
// Today's only consumer is `FilterThinking`, which drops thinking +
// redacted_thinking content blocks from the message history before each
// `provider.SendMessage` call. The model's thinking trace is informative
// for the UI (rendered live as `EvThinking`) and for the saved
// transcript, but re-feeding it to the model on every subsequent turn:
//   - inflates context size for no semantic gain (the assistant's text
//     and tool_use blocks already encode the conclusions),
//   - and on non-Anthropic providers the OpenAI translation already
//     drops thinking — so leaving it in the Anthropic path is the only
//     drift between providers.
//
// The OpenAI provider strips thinking blocks inside translate.go
// (`messageParamToOpenAI`); FilterThinking is the symmetric pre-filter
// for the Anthropic provider, applied one level higher (in the agent
// loop) so both transports see the same trimmed history.

// FilterThinking returns a copy of `messages` with all `OfThinking` and
// `OfRedactedThinking` content blocks removed. Messages whose entire
// content was thinking-only are dropped to avoid sending an empty
// assistant turn (which the API rejects).
//
// The returned slice is a fresh allocation; the input is not mutated, so
// callers can keep the unfiltered history around for `state.Messages` /
// session persistence.
func FilterThinking(messages []anthropic.MessageParam) []anthropic.MessageParam {
	out := make([]anthropic.MessageParam, 0, len(messages))
	for _, m := range messages {
		filtered := filterContent(m.Content)
		if len(filtered) == 0 {
			// All content was thinking — drop the whole message rather
			// than send an empty content array (Anthropic 400s on it).
			continue
		}
		out = append(out, anthropic.MessageParam{
			Role:    m.Role,
			Content: filtered,
		})
	}
	return out
}

// filterContent drops thinking + redacted_thinking blocks from a single
// content slice. Other block types (text, tool_use, tool_result, image,
// document, server_tool_use, ...) pass through unchanged.
func filterContent(content []anthropic.ContentBlockParamUnion) []anthropic.ContentBlockParamUnion {
	out := make([]anthropic.ContentBlockParamUnion, 0, len(content))
	for _, b := range content {
		if b.OfThinking != nil || b.OfRedactedThinking != nil {
			continue
		}
		out = append(out, b)
	}
	return out
}

// FilterIncompleteToolCalls drops assistant messages that contain a
// `tool_use` block whose `tool_use_id` is never matched by a `tool_result`
// later in the slice. Anthropic's API rejects message histories with such
// orphan tool calls, so this pre-filter lets us hand off mid-turn message
// state (e.g. when forking) without the API screaming.
//
// Mirrors `filterIncompleteToolCalls` in refs/runAgent.ts. Other message
// types (user, attachment) pass through unchanged. The input is not
// mutated; the returned slice is a fresh allocation.
func FilterIncompleteToolCalls(messages []anthropic.MessageParam) []anthropic.MessageParam {
	// Pass 1: collect every tool_use_id that has a matching tool_result.
	resolved := map[string]bool{}
	for _, m := range messages {
		if m.Role != anthropic.MessageParamRoleUser {
			continue
		}
		for _, b := range m.Content {
			if b.OfToolResult != nil && b.OfToolResult.ToolUseID != "" {
				resolved[b.OfToolResult.ToolUseID] = true
			}
		}
	}

	// Pass 2: drop assistant messages whose tool_use blocks aren't all
	// resolved. We drop the whole message rather than removing just the
	// orphan tool_use because that would also strip the surrounding text
	// that explains what the assistant was about to do — and partial
	// assistant content is harder for the model to make sense of than
	// the message simply not being there.
	out := make([]anthropic.MessageParam, 0, len(messages))
	for _, m := range messages {
		if m.Role == anthropic.MessageParamRoleAssistant {
			hasOrphan := false
			for _, b := range m.Content {
				if b.OfToolUse != nil && b.OfToolUse.ID != "" && !resolved[b.OfToolUse.ID] {
					hasOrphan = true
					break
				}
			}
			if hasOrphan {
				continue
			}
		}
		out = append(out, m)
	}
	return out
}
