package llm

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

// ──────────────────────────────────────────────────────────────────
//  Anthropic → OpenAI request translation (paramsToOpenAI)
// ──────────────────────────────────────────────────────────────────

func TestParamsToOpenAI_SystemPlusUser(t *testing.T) {
	in := anthropic.MessageNewParams{
		Model:     "gpt-4o",
		MaxTokens: 100,
		System: []anthropic.TextBlockParam{
			{Text: "You are X."},
			{Text: "Be brief."},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("hi")),
		},
	}
	got := paramsToOpenAI(in)

	if got.Model != "gpt-4o" {
		t.Errorf("Model = %q, want %q", got.Model, "gpt-4o")
	}
	if got.MaxTokens != 100 {
		t.Errorf("MaxTokens = %d, want 100", got.MaxTokens)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("Messages = %d, want 2", len(got.Messages))
	}
	if got.Messages[0].Role != "system" || got.Messages[0].Content != "You are X.\n\nBe brief." {
		t.Errorf("system msg = %+v", got.Messages[0])
	}
	if got.Messages[1].Role != "user" || got.Messages[1].Content != "hi" {
		t.Errorf("user msg = %+v", got.Messages[1])
	}
}

func TestParamsToOpenAI_AssistantTextAndToolUse(t *testing.T) {
	// Build an assistant turn that has both a text block and a tool_use
	// block — exactly the shape resp.ToParam() emits when the model
	// thinks aloud before calling a tool.
	assistant := anthropic.MessageParam{
		Role: anthropic.MessageParamRoleAssistant,
		Content: []anthropic.ContentBlockParamUnion{
			{OfText: &anthropic.TextBlockParam{Text: "Looking up..."}},
			{OfToolUse: &anthropic.ToolUseBlockParam{
				ID:    "call_1",
				Name:  "bash",
				Input: map[string]any{"command": "ls"},
			}},
		},
	}
	in := anthropic.MessageNewParams{
		Model:     "gpt-4o",
		MaxTokens: 100,
		Messages:  []anthropic.MessageParam{assistant},
	}
	got := paramsToOpenAI(in)

	if len(got.Messages) != 1 {
		t.Fatalf("Messages = %d, want 1", len(got.Messages))
	}
	m := got.Messages[0]
	if m.Role != "assistant" {
		t.Errorf("role = %q, want assistant", m.Role)
	}
	if m.Content != "Looking up..." {
		t.Errorf("content = %q", m.Content)
	}
	if len(m.ToolCalls) != 1 {
		t.Fatalf("tool_calls = %d, want 1", len(m.ToolCalls))
	}
	tc := m.ToolCalls[0]
	if tc.ID != "call_1" || tc.Type != "function" || tc.Function.Name != "bash" {
		t.Errorf("tool_call = %+v", tc)
	}
	// Arguments must be a JSON-string per OpenAI spec.
	var parsed map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &parsed); err != nil {
		t.Fatalf("arguments not JSON: %v / %q", err, tc.Function.Arguments)
	}
	if parsed["command"] != "ls" {
		t.Errorf("arguments parsed = %+v", parsed)
	}
}

func TestParamsToOpenAI_MultipleToolResultsExpand(t *testing.T) {
	// One MessageParam holding 3 tool_results → 3 OpenAI tool messages,
	// in encounter order.
	in := anthropic.MessageNewParams{
		Model:     "gpt-4o",
		MaxTokens: 50,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(
				anthropic.NewToolResultBlock("id-A", "result A", false),
				anthropic.NewToolResultBlock("id-B", "result B", false),
				anthropic.NewToolResultBlock("id-C", "result C", true),
			),
		},
	}
	got := paramsToOpenAI(in)
	if len(got.Messages) != 3 {
		t.Fatalf("Messages = %d, want 3", len(got.Messages))
	}
	want := []struct {
		id      string
		content string
	}{
		{"id-A", "result A"},
		{"id-B", "result B"},
		{"id-C", "[error] result C"},
	}
	for i, w := range want {
		m := got.Messages[i]
		if m.Role != "tool" || m.ToolCallID != w.id || m.Content != w.content {
			t.Errorf("[%d] = %+v, want id=%s content=%q", i, m, w.id, w.content)
		}
	}
}

func TestParamsToOpenAI_ToolResultThenReminderText(t *testing.T) {
	// This is the actual shape loop.go produces: NewUserMessage(
	// toolResults..., reminderTextBlock). The reminder must come AFTER
	// the tool messages, as a {role:"user"} message.
	in := anthropic.MessageNewParams{
		Model:     "gpt-4o",
		MaxTokens: 50,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(
				anthropic.NewToolResultBlock("id-A", "ok", false),
				anthropic.NewTextBlock("reminder: keep going"),
			),
		},
	}
	got := paramsToOpenAI(in)
	if len(got.Messages) != 2 {
		t.Fatalf("Messages = %d, want 2 (one tool, one user)", len(got.Messages))
	}
	if got.Messages[0].Role != "tool" || got.Messages[0].ToolCallID != "id-A" {
		t.Errorf("[0] = %+v", got.Messages[0])
	}
	if got.Messages[1].Role != "user" || got.Messages[1].Content != "reminder: keep going" {
		t.Errorf("[1] = %+v", got.Messages[1])
	}
}

func TestParamsToOpenAI_ThinkingBlocksDropped(t *testing.T) {
	// Anthropic-only thinking blocks must not leak into OpenAI requests.
	thinking := &anthropic.ThinkingBlockParam{Thinking: "deep thoughts"}
	asst := anthropic.MessageParam{
		Role: anthropic.MessageParamRoleAssistant,
		Content: []anthropic.ContentBlockParamUnion{
			{OfThinking: thinking},
			{OfText: &anthropic.TextBlockParam{Text: "answer"}},
		},
	}
	in := anthropic.MessageNewParams{
		Model:     "gpt-4o",
		MaxTokens: 50,
		Messages:  []anthropic.MessageParam{asst},
	}
	got := paramsToOpenAI(in)
	if len(got.Messages) != 1 || got.Messages[0].Content != "answer" {
		t.Errorf("expected single assistant msg with 'answer', got %+v", got.Messages)
	}
	body, _ := json.Marshal(got)
	if strings.Contains(string(body), "deep thoughts") {
		t.Error("thinking text leaked into OpenAI request body")
	}
}

func TestParamsToOpenAI_ToolsTranslation(t *testing.T) {
	in := anthropic.MessageNewParams{
		Model:     "gpt-4o",
		MaxTokens: 10,
		Tools: []anthropic.ToolUnionParam{
			{OfTool: &anthropic.ToolParam{
				Name:        "search",
				Description: anthropic.String("Search the web"),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{
						"q": map[string]any{"type": "string"},
					},
					Required: []string{"q"},
				},
			}},
		},
	}
	got := paramsToOpenAI(in)
	if len(got.Tools) != 1 {
		t.Fatalf("Tools = %d, want 1", len(got.Tools))
	}
	tool := got.Tools[0]
	if tool.Type != "function" {
		t.Errorf("type = %q, want function", tool.Type)
	}
	if tool.Function.Name != "search" || tool.Function.Description != "Search the web" {
		t.Errorf("function = %+v", tool.Function)
	}
	if tool.Function.Parameters["type"] != "object" {
		t.Errorf("parameters.type = %v, want 'object'", tool.Function.Parameters["type"])
	}
	props, ok := tool.Function.Parameters["properties"].(map[string]any)
	if !ok || props["q"] == nil {
		t.Errorf("parameters.properties = %v", tool.Function.Parameters["properties"])
	}
}

func TestParamsToOpenAI_DescriptionUnsetIsEmpty(t *testing.T) {
	in := anthropic.MessageNewParams{
		Model:     "gpt-4o",
		MaxTokens: 10,
		Tools: []anthropic.ToolUnionParam{
			{OfTool: &anthropic.ToolParam{Name: "no_desc"}},
		},
	}
	got := paramsToOpenAI(in)
	if got.Tools[0].Function.Description != "" {
		t.Errorf("description = %q, want empty", got.Tools[0].Function.Description)
	}
}

func TestParamsToOpenAI_TemperatureAndStop(t *testing.T) {
	in := anthropic.MessageNewParams{
		Model:         "gpt-4o",
		MaxTokens:     10,
		Temperature:   anthropic.Float(0.2),
		StopSequences: []string{"\n\n", "###"},
	}
	got := paramsToOpenAI(in)
	if got.Temperature == nil || *got.Temperature != 0.2 {
		t.Errorf("Temperature = %v", got.Temperature)
	}
	if len(got.Stop) != 2 || got.Stop[0] != "\n\n" {
		t.Errorf("Stop = %v", got.Stop)
	}
}

// ──────────────────────────────────────────────────────────────────
//  OpenAI → Anthropic response translation (openAIToMessage)
// ──────────────────────────────────────────────────────────────────

func TestOpenAIToMessage_SimpleText(t *testing.T) {
	body := strPtr("hello world")
	resp := &openaiResponse{
		ID:    "chatcmpl-1",
		Model: "gpt-4o",
		Choices: []openaiChoice{
			{
				Index:        0,
				FinishReason: "stop",
				Message:      openaiChoiceMessage{Role: "assistant", Content: body},
			},
		},
		Usage: openaiUsage{PromptTokens: 12, CompletionTokens: 7},
	}
	msg, err := openAIToMessage(resp)
	if err != nil {
		t.Fatal(err)
	}
	if msg.StopReason != anthropic.StopReasonEndTurn {
		t.Errorf("StopReason = %q", msg.StopReason)
	}
	if msg.Usage.InputTokens != 12 || msg.Usage.OutputTokens != 7 {
		t.Errorf("Usage = %+v", msg.Usage)
	}
	if len(msg.Content) != 1 {
		t.Fatalf("Content len = %d", len(msg.Content))
	}
	tb, ok := msg.Content[0].AsAny().(anthropic.TextBlock)
	if !ok {
		t.Fatalf("AsAny = %T, want TextBlock", msg.Content[0].AsAny())
	}
	if tb.Text != "hello world" {
		t.Errorf("Text = %q", tb.Text)
	}
}

func TestOpenAIToMessage_ToolCallsRoundTrip(t *testing.T) {
	// Critical regression test: synthesize a tool-call response and
	// verify (a) AsAny() returns a populated ToolUseBlock (not zero),
	// (b) ToParam() round-trips into a non-empty MessageParam. This
	// pins the JSON-marshal-then-UnmarshalJSON contract that the entire
	// agent loop depends on.
	resp := &openaiResponse{
		ID:    "chatcmpl-2",
		Model: "gpt-4o",
		Choices: []openaiChoice{
			{
				Index:        0,
				FinishReason: "tool_calls",
				Message: openaiChoiceMessage{
					Role: "assistant",
					ToolCalls: []openaiToolCall{
						{
							ID:   "call_42",
							Type: "function",
							Function: openaiToolCallFunc{
								Name:      "bash",
								Arguments: `{"command":"ls -la"}`,
							},
						},
					},
				},
			},
		},
		Usage: openaiUsage{PromptTokens: 5, CompletionTokens: 9},
	}
	msg, err := openAIToMessage(resp)
	if err != nil {
		t.Fatal(err)
	}
	if msg.StopReason != anthropic.StopReasonToolUse {
		t.Errorf("StopReason = %q", msg.StopReason)
	}
	if len(msg.Content) != 1 {
		t.Fatalf("Content = %d, want 1", len(msg.Content))
	}
	tu, ok := msg.Content[0].AsAny().(anthropic.ToolUseBlock)
	if !ok {
		t.Fatalf("AsAny = %T, want ToolUseBlock — JSON.raw not populated?", msg.Content[0].AsAny())
	}
	if tu.ID != "call_42" || tu.Name != "bash" {
		t.Errorf("ToolUse = %+v", tu)
	}
	// Input is json.RawMessage — confirm it round-trips.
	var got map[string]any
	if err := json.Unmarshal(tu.Input, &got); err != nil {
		t.Fatalf("Input not JSON: %v / %s", err, string(tu.Input))
	}
	if got["command"] != "ls -la" {
		t.Errorf("Input = %v", got)
	}
	// ToParam must produce a non-empty MessageParam — this is the call
	// the agent loop does at loop.go:155 to append to history.
	mp := msg.ToParam()
	if mp.Role != anthropic.MessageParamRoleAssistant {
		t.Errorf("ToParam Role = %q", mp.Role)
	}
	if len(mp.Content) != 1 || mp.Content[0].OfToolUse == nil {
		t.Fatalf("ToParam content = %+v", mp.Content)
	}
	if mp.Content[0].OfToolUse.Name != "bash" {
		t.Errorf("ToParam tool_use Name = %q", mp.Content[0].OfToolUse.Name)
	}
}

func TestOpenAIToMessage_TextPlusToolCalls(t *testing.T) {
	body := strPtr("Let me check that.")
	resp := &openaiResponse{
		ID:    "chatcmpl-3",
		Model: "gpt-4o",
		Choices: []openaiChoice{
			{
				FinishReason: "tool_calls",
				Message: openaiChoiceMessage{
					Role:    "assistant",
					Content: body,
					ToolCalls: []openaiToolCall{
						{
							ID:   "c1",
							Type: "function",
							Function: openaiToolCallFunc{
								Name:      "search",
								Arguments: `{"q":"weather"}`,
							},
						},
					},
				},
			},
		},
	}
	msg, err := openAIToMessage(resp)
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.Content) != 2 {
		t.Fatalf("Content = %d, want 2", len(msg.Content))
	}
	if _, ok := msg.Content[0].AsAny().(anthropic.TextBlock); !ok {
		t.Errorf("[0] = %T, want TextBlock", msg.Content[0].AsAny())
	}
	if _, ok := msg.Content[1].AsAny().(anthropic.ToolUseBlock); !ok {
		t.Errorf("[1] = %T, want ToolUseBlock", msg.Content[1].AsAny())
	}
}

func TestOpenAIToMessage_FinishReasonMapping(t *testing.T) {
	cases := map[string]anthropic.StopReason{
		"stop":           anthropic.StopReasonEndTurn,
		"tool_calls":     anthropic.StopReasonToolUse,
		"function_call":  anthropic.StopReasonToolUse,
		"length":         anthropic.StopReasonMaxTokens,
		"content_filter": anthropic.StopReasonRefusal,
		"":               anthropic.StopReasonEndTurn,
		"unknown_value":  anthropic.StopReasonEndTurn,
	}
	for in, want := range cases {
		if got := mapFinishReason(in); got != want {
			t.Errorf("mapFinishReason(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOpenAIToMessage_ArgumentsJSONStringDecoded(t *testing.T) {
	resp := &openaiResponse{
		ID:    "x",
		Model: "gpt-4o",
		Choices: []openaiChoice{
			{
				FinishReason: "tool_calls",
				Message: openaiChoiceMessage{
					ToolCalls: []openaiToolCall{
						{
							ID:   "c1",
							Type: "function",
							Function: openaiToolCallFunc{
								Name:      "f",
								Arguments: ``, // empty
							},
						},
						{
							ID:   "c2",
							Type: "function",
							Function: openaiToolCallFunc{
								Name:      "f",
								Arguments: `null`,
							},
						},
						{
							ID:   "c3",
							Type: "function",
							Function: openaiToolCallFunc{
								Name:      "f",
								Arguments: `{"x":1,"y":[2,3]}`,
							},
						},
					},
				},
			},
		},
	}
	msg, err := openAIToMessage(resp)
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.Content) != 3 {
		t.Fatalf("Content = %d", len(msg.Content))
	}
	// First two should be empty objects.
	for i := 0; i < 2; i++ {
		tu := msg.Content[i].AsAny().(anthropic.ToolUseBlock)
		var got any
		json.Unmarshal(tu.Input, &got)
		obj, ok := got.(map[string]any)
		if !ok || len(obj) != 0 {
			t.Errorf("[%d] Input = %v, want {}", i, got)
		}
	}
	tu := msg.Content[2].AsAny().(anthropic.ToolUseBlock)
	var got map[string]any
	json.Unmarshal(tu.Input, &got)
	if got["x"] != float64(1) {
		t.Errorf("Input[x] = %v", got["x"])
	}
}

func TestOpenAIToMessage_NoChoices(t *testing.T) {
	if _, err := openAIToMessage(&openaiResponse{}); err == nil {
		t.Fatal("expected error on empty choices")
	}
}

func TestOpenAIToMessage_RefusalSurfaced(t *testing.T) {
	refusal := strPtr("I can't help with that.")
	resp := &openaiResponse{
		ID: "x", Model: "gpt-4o",
		Choices: []openaiChoice{
			{
				FinishReason: "content_filter",
				Message:      openaiChoiceMessage{Role: "assistant", Refusal: refusal},
			},
		},
	}
	msg, err := openAIToMessage(resp)
	if err != nil {
		t.Fatal(err)
	}
	if msg.StopReason != anthropic.StopReasonRefusal {
		t.Errorf("StopReason = %q", msg.StopReason)
	}
	tb := msg.Content[0].AsAny().(anthropic.TextBlock)
	if tb.Text != "I can't help with that." {
		t.Errorf("Text = %q", tb.Text)
	}
}

func TestOpenAIToMessage_EmptyContentEmitsEmptyTextBlock(t *testing.T) {
	resp := &openaiResponse{
		ID: "x", Model: "gpt-4o",
		Choices: []openaiChoice{
			{FinishReason: "stop", Message: openaiChoiceMessage{Role: "assistant"}},
		},
	}
	msg, err := openAIToMessage(resp)
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.Content) != 1 {
		t.Fatalf("Content = %d, want 1 (empty placeholder)", len(msg.Content))
	}
}

// ──────────────────────────────────────────────────────────────────
//  helpers
// ──────────────────────────────────────────────────────────────────

func strPtr(s string) *string { return &s }
