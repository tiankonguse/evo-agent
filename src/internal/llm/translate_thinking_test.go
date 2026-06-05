package llm

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

// TestParamsToOpenAI_EmitsReasoningFields verifies that when thinking is
// enabled in the canonical params (the always-on default), paramsToOpenAI
// emits the multi-dialect reasoning fields so DeepSeek / Qwen / OpenRouter
// / o-series each see something they recognise.
func TestParamsToOpenAI_EmitsReasoningFields(t *testing.T) {
	t.Setenv("OPENAI_THINKING", "")  // ensure default-on
	t.Setenv("OPENAI_REASONING_EFFORT", "")
	in := anthropic.MessageNewParams{
		Model:     anthropic.Model("test-model"),
		MaxTokens: 8000,
		Thinking:  anthropic.ThinkingConfigParamOfEnabled(2048),
	}
	out := paramsToOpenAI(in)

	if out.ReasoningEffort == "" {
		t.Errorf("ReasoningEffort should be set, got empty")
	}
	if out.EnableThinking == nil || !*out.EnableThinking {
		t.Errorf("EnableThinking should be true, got %v", out.EnableThinking)
	}
	if out.Reasoning == nil {
		t.Fatalf("Reasoning struct should be set")
	}
	if out.Reasoning.MaxTokens != 2048 {
		t.Errorf("Reasoning.MaxTokens = %d, want 2048", out.Reasoning.MaxTokens)
	}
	if out.Thinking == nil {
		t.Fatalf("Thinking struct should be set for budget=2048")
	}
	if out.Thinking.BudgetTokens != 2048 || out.Thinking.Type != "enabled" {
		t.Errorf("Thinking = %+v, want {Type:enabled, BudgetTokens:2048}", out.Thinking)
	}
}

// TestParamsToOpenAI_OptOutEnvVar verifies OPENAI_THINKING=0 silences the
// reasoning emission entirely so strict servers don't 400 on unknown fields.
func TestParamsToOpenAI_OptOutEnvVar(t *testing.T) {
	t.Setenv("OPENAI_THINKING", "0")
	in := anthropic.MessageNewParams{
		Model:     anthropic.Model("strict-server"),
		MaxTokens: 8000,
		Thinking:  anthropic.ThinkingConfigParamOfEnabled(2048),
	}
	out := paramsToOpenAI(in)

	if out.ReasoningEffort != "" {
		t.Errorf("ReasoningEffort should be empty when OPENAI_THINKING=0, got %q", out.ReasoningEffort)
	}
	if out.EnableThinking != nil {
		t.Errorf("EnableThinking should be nil when OPENAI_THINKING=0")
	}
	if out.Reasoning != nil {
		t.Errorf("Reasoning should be nil when OPENAI_THINKING=0")
	}
	if out.Thinking != nil {
		t.Errorf("Thinking should be nil when OPENAI_THINKING=0")
	}
}

// TestOpenAIReasoningEffort_BudgetMapping pins the budget→effort table.
func TestOpenAIReasoningEffort_BudgetMapping(t *testing.T) {
	cases := []struct {
		budget int64
		want   string
	}{
		{0, "medium"},
		{1024, "low"},
		{2047, "low"},
		{2048, "medium"},
		{4095, "medium"},
		{4096, "high"},
		{16000, "high"},
	}
	for _, c := range cases {
		// Make sure no override leaks into this case.
		_ = os.Unsetenv("OPENAI_REASONING_EFFORT")
		got := openaiReasoningEffort(c.budget)
		if got != c.want {
			t.Errorf("budget=%d → %q, want %q", c.budget, got, c.want)
		}
	}
}

// TestOpenAIReasoningEffort_EnvOverride verifies the env override path.
func TestOpenAIReasoningEffort_EnvOverride(t *testing.T) {
	t.Setenv("OPENAI_REASONING_EFFORT", "high")
	if got := openaiReasoningEffort(1024); got != "high" {
		t.Errorf("override should win, got %q", got)
	}
	t.Setenv("OPENAI_REASONING_EFFORT", "garbage")
	if got := openaiReasoningEffort(8192); got != "high" {
		// invalid override falls back to budget-derived
		t.Errorf("invalid override should fall back, got %q", got)
	}
}

// TestExtractReasoningText_DeepSeek verifies DeepSeek's reasoning_content
// field comes through verbatim.
func TestExtractReasoningText_DeepSeek(t *testing.T) {
	body := "Step 1: think about the problem.\nStep 2: produce answer."
	msg := openaiChoiceMessage{ReasoningContent: &body}
	if got := extractReasoningText(msg); got != body {
		t.Errorf("got %q, want %q", got, body)
	}
}

// TestExtractReasoningText_OpenRouterString verifies the bare-string variant
// of OpenRouter's reasoning field.
func TestExtractReasoningText_OpenRouterString(t *testing.T) {
	msg := openaiChoiceMessage{Reasoning: json.RawMessage(`"chain of thought"`)}
	if got := extractReasoningText(msg); got != "chain of thought" {
		t.Errorf("got %q", got)
	}
}

// TestExtractReasoningText_OpenRouterObject verifies the object variant
// (effort + nested text-bearing field).
func TestExtractReasoningText_OpenRouterObject(t *testing.T) {
	msg := openaiChoiceMessage{
		Reasoning: json.RawMessage(`{"effort":"medium","text":"long form thoughts"}`),
	}
	if got := extractReasoningText(msg); got != "long form thoughts" {
		t.Errorf("got %q", got)
	}
}

// TestExtractReasoningText_BothFieldsContentWins verifies DeepSeek's field
// wins when both are present.
func TestExtractReasoningText_BothFieldsContentWins(t *testing.T) {
	body := "deepseek-style"
	msg := openaiChoiceMessage{
		ReasoningContent: &body,
		Reasoning:        json.RawMessage(`"openrouter-style"`),
	}
	if got := extractReasoningText(msg); got != body {
		t.Errorf("got %q, want DeepSeek's reasoning_content to win", got)
	}
}

// TestExtractReasoningText_Empty verifies empty inputs map to empty string.
func TestExtractReasoningText_Empty(t *testing.T) {
	if got := extractReasoningText(openaiChoiceMessage{}); got != "" {
		t.Errorf("empty msg → %q", got)
	}
	empty := ""
	if got := extractReasoningText(openaiChoiceMessage{ReasoningContent: &empty}); got != "" {
		t.Errorf("empty reasoning_content → %q", got)
	}
}

// TestOpenAIToMessage_SurfacesThinkingBlock verifies that DeepSeek-style
// reasoning_content shows up as an Anthropic thinking block in the
// synthesised Message — so the TUI / FilterThinking pipeline treats it
// uniformly across providers.
func TestOpenAIToMessage_SurfacesThinkingBlock(t *testing.T) {
	body := "thought process here"
	answer := "final answer"
	resp := &openaiResponse{
		ID:    "x",
		Model: "deepseek-r1",
		Choices: []openaiChoice{
			{
				FinishReason: "stop",
				Message: openaiChoiceMessage{
					Role:             "assistant",
					Content:          &answer,
					ReasoningContent: &body,
				},
			},
		},
	}
	msg, err := openAIToMessage(resp)
	if err != nil {
		t.Fatalf("openAIToMessage: %v", err)
	}
	// Walk the SDK message; thinking should come first, then text.
	if len(msg.Content) < 2 {
		t.Fatalf("expected ≥2 blocks, got %d", len(msg.Content))
	}
	if string(msg.Content[0].Type) != "thinking" {
		t.Errorf("first block type = %q, want thinking", msg.Content[0].Type)
	}
	thinkingText := strings.ToLower(msg.Content[0].Thinking)
	if thinkingText == "" || !strings.Contains(thinkingText, "thought process") {
		t.Errorf("thinking body should be preserved, got %q", msg.Content[0].Thinking)
	}
	if string(msg.Content[1].Type) != "text" || !strings.Contains(msg.Content[1].Text, "final answer") {
		t.Errorf("second block should be the answer text, got %+v", msg.Content[1])
	}
}
