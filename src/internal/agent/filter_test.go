package agent

import (
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

// asstWithThinking builds an assistant MessageParam containing a thinking
// block followed by a text block, mirroring how the Anthropic SDK shapes a
// real extended-thinking response after `resp.ToParam()`.
func asstWithThinking(thoughts, text string) anthropic.MessageParam {
	return anthropic.MessageParam{
		Role: anthropic.MessageParamRoleAssistant,
		Content: []anthropic.ContentBlockParamUnion{
			{OfThinking: &anthropic.ThinkingBlockParam{Thinking: thoughts}},
			{OfText: &anthropic.TextBlockParam{Text: text}},
		},
	}
}

func TestFilterThinking_DropsThinkingBlocks(t *testing.T) {
	in := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("hi")),
		asstWithThinking("deep thoughts", "Hello! How can I help?"),
	}
	out := FilterThinking(in)

	if len(out) != 2 {
		t.Fatalf("expected 2 messages out; got %d", len(out))
	}
	if len(out[1].Content) != 1 {
		t.Fatalf("assistant content: expected 1 block (text only); got %d", len(out[1].Content))
	}
	if out[1].Content[0].OfText == nil || out[1].Content[0].OfText.Text != "Hello! How can I help?" {
		t.Errorf("text block lost or modified: %+v", out[1].Content[0])
	}
	for _, b := range out[1].Content {
		if b.OfThinking != nil {
			t.Error("thinking block leaked through filter")
		}
	}
}

func TestFilterThinking_DropsRedactedThinking(t *testing.T) {
	in := []anthropic.MessageParam{
		{
			Role: anthropic.MessageParamRoleAssistant,
			Content: []anthropic.ContentBlockParamUnion{
				{OfRedactedThinking: &anthropic.RedactedThinkingBlockParam{Data: "redacted-bytes"}},
				{OfText: &anthropic.TextBlockParam{Text: "answer"}},
			},
		},
	}
	out := FilterThinking(in)
	if len(out[0].Content) != 1 || out[0].Content[0].OfText == nil {
		t.Errorf("redacted thinking not dropped: %+v", out[0].Content)
	}
}

func TestFilterThinking_DropsThinkingOnlyMessage(t *testing.T) {
	// A message containing only thinking blocks would render as an empty
	// content array, which Anthropic's API rejects with 400.
	// FilterThinking should drop the whole message.
	in := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("hi")),
		{
			Role: anthropic.MessageParamRoleAssistant,
			Content: []anthropic.ContentBlockParamUnion{
				{OfThinking: &anthropic.ThinkingBlockParam{Thinking: "..."}},
			},
		},
		anthropic.NewUserMessage(anthropic.NewTextBlock("still there?")),
	}
	out := FilterThinking(in)
	if len(out) != 2 {
		t.Errorf("expected 2 messages (thinking-only assistant dropped); got %d", len(out))
	}
}

func TestFilterThinking_PreservesToolBlocks(t *testing.T) {
	// Tool use and tool results must pass through untouched — they
	// carry actionable history the model needs.
	asst := anthropic.MessageParam{
		Role: anthropic.MessageParamRoleAssistant,
		Content: []anthropic.ContentBlockParamUnion{
			{OfThinking: &anthropic.ThinkingBlockParam{Thinking: "let me check"}},
			{OfText: &anthropic.TextBlockParam{Text: "Calling tool"}},
			{OfToolUse: &anthropic.ToolUseBlockParam{
				ID:    "tu_1",
				Name:  "read_file",
				Input: map[string]any{"path": "x"},
			}},
		},
	}
	out := FilterThinking([]anthropic.MessageParam{asst})
	if len(out[0].Content) != 2 {
		t.Fatalf("expected 2 surviving blocks (text + tool_use); got %d", len(out[0].Content))
	}
	if out[0].Content[0].OfText == nil {
		t.Error("text block dropped")
	}
	if out[0].Content[1].OfToolUse == nil {
		t.Error("tool_use block dropped")
	}
}

func TestFilterThinking_DoesNotMutateInput(t *testing.T) {
	in := []anthropic.MessageParam{asstWithThinking("t", "txt")}
	_ = FilterThinking(in)
	// Original input must still carry its thinking block (transcript /
	// state.Messages relies on this).
	if len(in[0].Content) != 2 {
		t.Errorf("FilterThinking mutated input: content len=%d, want 2", len(in[0].Content))
	}
	if in[0].Content[0].OfThinking == nil {
		t.Error("FilterThinking removed thinking block from input")
	}
}

// ── FilterIncompleteToolCalls ──────────────────────────────────────────────

func TestFilterIncompleteToolCalls_DropsOrphanToolUse(t *testing.T) {
	// Mid-turn snapshot: the assistant just called the `task` tool but
	// the loop hasn't yet appended the user message with tool_results.
	// FilterIncompleteToolCalls should drop the assistant message so the
	// fork API call doesn't error.
	asst := anthropic.MessageParam{
		Role: anthropic.MessageParamRoleAssistant,
		Content: []anthropic.ContentBlockParamUnion{
			{OfText: &anthropic.TextBlockParam{Text: "I'll fork."}},
			{OfToolUse: &anthropic.ToolUseBlockParam{
				ID:    "tu_orphan",
				Name:  "task",
				Input: map[string]any{"prompt": "..."},
			}},
		},
	}
	in := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("survey the branch")),
		asst,
	}
	out := FilterIncompleteToolCalls(in)
	if len(out) != 1 {
		t.Fatalf("expected 1 message (orphan assistant dropped); got %d", len(out))
	}
	if out[0].Role != anthropic.MessageParamRoleUser {
		t.Errorf("survivor should be the user message, got role=%q", out[0].Role)
	}
}

func TestFilterIncompleteToolCalls_KeepsResolvedToolCalls(t *testing.T) {
	asst := anthropic.MessageParam{
		Role: anthropic.MessageParamRoleAssistant,
		Content: []anthropic.ContentBlockParamUnion{
			{OfToolUse: &anthropic.ToolUseBlockParam{
				ID:    "tu_a",
				Name:  "read_file",
				Input: map[string]any{"path": "x"},
			}},
		},
	}
	userResult := anthropic.MessageParam{
		Role: anthropic.MessageParamRoleUser,
		Content: []anthropic.ContentBlockParamUnion{
			{OfToolResult: &anthropic.ToolResultBlockParam{
				ToolUseID: "tu_a",
				Content:   []anthropic.ToolResultBlockParamContentUnion{{OfText: &anthropic.TextBlockParam{Text: "ok"}}},
			}},
		},
	}
	in := []anthropic.MessageParam{asst, userResult}
	out := FilterIncompleteToolCalls(in)
	if len(out) != 2 {
		t.Fatalf("expected both messages preserved; got %d", len(out))
	}
}

func TestFilterIncompleteToolCalls_HandlesPartiallyResolvedAssistant(t *testing.T) {
	// Assistant called two tools in the same turn; only one was answered.
	// The whole assistant message should be dropped because Anthropic's
	// API rejects ANY orphan tool_use, not just lonely ones.
	asst := anthropic.MessageParam{
		Role: anthropic.MessageParamRoleAssistant,
		Content: []anthropic.ContentBlockParamUnion{
			{OfToolUse: &anthropic.ToolUseBlockParam{ID: "tu_a", Name: "read_file"}},
			{OfToolUse: &anthropic.ToolUseBlockParam{ID: "tu_b", Name: "task"}},
		},
	}
	userResult := anthropic.MessageParam{
		Role: anthropic.MessageParamRoleUser,
		Content: []anthropic.ContentBlockParamUnion{
			{OfToolResult: &anthropic.ToolResultBlockParam{
				ToolUseID: "tu_a",
				Content:   []anthropic.ToolResultBlockParamContentUnion{{OfText: &anthropic.TextBlockParam{Text: "ok"}}},
			}},
		},
	}
	in := []anthropic.MessageParam{asst, userResult}
	out := FilterIncompleteToolCalls(in)
	for _, m := range out {
		if m.Role == anthropic.MessageParamRoleAssistant {
			t.Error("partially-resolved assistant message should be dropped")
		}
	}
}

func TestFilterIncompleteToolCalls_LeavesUserMessagesAlone(t *testing.T) {
	in := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("hello")),
		anthropic.NewUserMessage(anthropic.NewTextBlock("again")),
	}
	out := FilterIncompleteToolCalls(in)
	if len(out) != 2 {
		t.Errorf("user-only history was modified: got %d messages, want 2", len(out))
	}
}
