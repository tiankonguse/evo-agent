package llm

import (
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
)

// TestWithDefaultThinking_InjectsBudgetWhenRoom verifies the always-on
// thinking injection: a plain MessageNewParams with MaxTokens=8000 and no
// Thinking config should come back with Thinking.OfEnabled populated and
// budget = defaultThinkingBudget.
func TestWithDefaultThinking_InjectsBudgetWhenRoom(t *testing.T) {
	in := anthropic.MessageNewParams{
		Model:     anthropic.Model("claude-test"),
		MaxTokens: 8000,
	}
	out := withDefaultThinking(in)

	if param.IsOmitted(out.Thinking.OfEnabled) {
		t.Fatalf("expected Thinking.OfEnabled to be set, got omitted")
	}
	if got := out.Thinking.OfEnabled.BudgetTokens; got != defaultThinkingBudget {
		t.Errorf("budget = %d, want %d", got, defaultThinkingBudget)
	}
}

// When the caller explicitly disabled thinking, withDefaultThinking must
// keep its hands off — preserving the opt-out shape so cheap evals can
// stay non-thinking on demand.
func TestWithDefaultThinking_RespectsExplicitDisable(t *testing.T) {
	in := anthropic.MessageNewParams{
		Model:     anthropic.Model("claude-test"),
		MaxTokens: 8000,
		Thinking:  anthropic.ThinkingConfigParamUnion{OfDisabled: &anthropic.ThinkingConfigDisabledParam{}},
	}
	out := withDefaultThinking(in)

	if param.IsOmitted(out.Thinking.OfDisabled) {
		t.Errorf("OfDisabled was cleared by injection")
	}
	if !param.IsOmitted(out.Thinking.OfEnabled) {
		t.Errorf("OfEnabled was set despite explicit disable")
	}
}

// When the caller already enabled thinking with a custom budget, leave
// it alone (no clobbering).
func TestWithDefaultThinking_RespectsExplicitEnable(t *testing.T) {
	in := anthropic.MessageNewParams{
		Model:     anthropic.Model("claude-test"),
		MaxTokens: 8000,
		Thinking:  anthropic.ThinkingConfigParamOfEnabled(4096),
	}
	out := withDefaultThinking(in)

	if param.IsOmitted(out.Thinking.OfEnabled) {
		t.Fatalf("OfEnabled was cleared by injection")
	}
	if got := out.Thinking.OfEnabled.BudgetTokens; got != 4096 {
		t.Errorf("caller budget overwritten: got %d, want 4096", got)
	}
}

// Tiny MaxTokens (e.g. <2048) should skip injection entirely so the
// SDK doesn't reject the request for budget>=max_tokens.
func TestWithDefaultThinking_SkipsWhenMaxTokensTooSmall(t *testing.T) {
	in := anthropic.MessageNewParams{
		Model:     anthropic.Model("claude-test"),
		MaxTokens: 256,
	}
	out := withDefaultThinking(in)

	if !param.IsOmitted(out.Thinking.OfEnabled) {
		t.Errorf("Thinking should not be injected for tiny MaxTokens=256")
	}
}

// Mid-range MaxTokens (≥2048 but <2*budget) should still inject thinking
// but with a halved / clamped budget so room is left for the answer.
func TestWithDefaultThinking_ClampsBudgetForMidMaxTokens(t *testing.T) {
	in := anthropic.MessageNewParams{
		Model:     anthropic.Model("claude-test"),
		MaxTokens: 2048, // exactly minMaxTokensForThinking
	}
	out := withDefaultThinking(in)
	if param.IsOmitted(out.Thinking.OfEnabled) {
		t.Fatalf("expected thinking enabled at the threshold MaxTokens=%d", in.MaxTokens)
	}
	got := out.Thinking.OfEnabled.BudgetTokens
	if got >= int64(in.MaxTokens) {
		t.Errorf("budget %d must be < MaxTokens %d", got, in.MaxTokens)
	}
	if got < 1024 {
		t.Errorf("budget %d must be ≥ 1024 (SDK floor)", got)
	}
}
