package llm

import (
	"context"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
)

// defaultThinkingBudget is the budget_tokens we ask for when the caller
// didn't supply an explicit Thinking config. 2048 leaves the model
// meaningful headroom for chain-of-thought while staying small enough to
// fit alongside an 8000-token answer in the lead loop and the 4096-token
// goal evaluator. The SDK enforces budget_tokens ≥ 1024 and
// budget_tokens < max_tokens — see SendMessage's per-call clamp.
const defaultThinkingBudget int64 = 2048

// minMaxTokensForThinking is the smallest MaxTokens that can fit the
// 1024-token SDK floor for thinking AND leave room for a useful answer.
// Below this we silently skip injecting thinking — the request still
// succeeds, the response just won't include thinking blocks.
const minMaxTokensForThinking int64 = 2048

// anthropicProvider is a thin pass-through to the official Anthropic
// Go SDK. Behaviour is intentionally byte-identical to the pre-refactor
// main.BuildOptions + anthropic.NewClient + client.Messages.New flow.
type anthropicProvider struct {
	client *anthropic.Client
}

// newAnthropicProvider builds an SDK client from the supplied Config.
// Empty AnthropicAPIKey falls back to the literal "dummy" so requests
// against custom base URLs (proxies, gateways) that don't enforce auth
// still work — this preserves the previous main.BuildOptions behaviour.
func newAnthropicProvider(cfg Config) (*anthropicProvider, error) {
	opts := []option.RequestOption{}
	if cfg.AnthropicBaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.AnthropicBaseURL))
		// When a custom base URL is supplied we drop any inherited
		// ANTHROPIC_AUTH_TOKEN so the SDK falls back to our explicit
		// API key option below. Mirrors main.BuildOptions verbatim.
		os.Unsetenv("ANTHROPIC_AUTH_TOKEN")
	}
	if cfg.AnthropicAPIKey != "" {
		opts = append(opts, option.WithAPIKey(cfg.AnthropicAPIKey))
	} else {
		opts = append(opts, option.WithAPIKey("dummy"))
	}
	c := anthropic.NewClient(opts...)
	return &anthropicProvider{client: &c}, nil
}

// SendMessage forwards the call straight to the SDK so resp.ToParam(),
// block.AsAny() switches and Usage/StopReason fields all behave exactly
// as they did before the Provider abstraction was introduced.
//
// Always-on extended thinking: when the caller didn't provide a Thinking
// config (zero union), we inject a budget that fits within MaxTokens.
// Callers that explicitly want thinking off must set
// `params.Thinking = anthropic.ThinkingConfigParamUnion{OfDisabled: …}`.
// The OpenAI provider ignores params.Thinking entirely (translation
// drops it), so this stays Anthropic-only without per-call switches.
func (p *anthropicProvider) SendMessage(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error) {
	params = withDefaultThinking(params)
	return p.client.Messages.New(ctx, params)
}

// withDefaultThinking injects a default thinking budget when the caller
// didn't supply one and the request has enough max_tokens headroom.
// Pure / side-effect-free so it's easy to unit-test (see anthropic_test.go).
func withDefaultThinking(params anthropic.MessageNewParams) anthropic.MessageNewParams {
	// Caller already opted in / out — respect them.
	if !param.IsOmitted(params.Thinking.OfEnabled) ||
		!param.IsOmitted(params.Thinking.OfDisabled) ||
		!param.IsOmitted(params.Thinking.OfAdaptive) {
		return params
	}
	// Skip when there's no room. Tiny calls (e.g. an early test stub
	// with MaxTokens 64) just stay non-thinking.
	if params.MaxTokens < minMaxTokensForThinking {
		return params
	}
	budget := defaultThinkingBudget
	// API requires budget_tokens < max_tokens. Reserve at least half the
	// budget for the answer so we don't starve the response.
	if params.MaxTokens-budget < budget {
		// Halve max_tokens, rounded down, never below the 1024 floor.
		budget = params.MaxTokens / 2
		if budget < 1024 {
			budget = 1024
		}
	}
	// Final guard: budget must be strictly less than max_tokens.
	if budget >= params.MaxTokens {
		return params
	}
	params.Thinking = anthropic.ThinkingConfigParamOfEnabled(budget)
	return params
}
