// Package llm is the boundary abstraction for all LLM backends.
//
// Internal callers stay typed in anthropic.MessageNewParams /
// *anthropic.Message — that is the canonical message shape across the
// rest of the codebase (agent loop, tools, session, subagent, executor,
// compact). The Provider interface only translates at the network edge:
// the Anthropic adapter is a thin pass-through to the official SDK, the
// OpenAI adapter (added in a later phase) translates anthropic shapes
// to/from OpenAI Chat Completions.
//
// Translation is runtime-only: session JSONL on disk continues to be
// stored in anthropic shape regardless of which provider is active.
package llm

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
)

// Provider sends a single message turn to an LLM backend and returns a
// fully-populated *anthropic.Message.
//
// Returning *anthropic.Message (not a custom canonical type) is
// deliberate: callers immediately call resp.ToParam() to append to
// history, walk resp.Content as []anthropic.ContentBlockUnion via
// block.AsAny(), and read resp.Usage / resp.StopReason / resp.Model.
// Keeping the surface identical means none of those call sites need to
// change beyond a one-line rename.
type Provider interface {
	SendMessage(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error)
}

// Config carries everything New needs to pick and construct a backend.
// It is built from config.Config inside main.go; this package never
// reads environment variables directly.
type Config struct {
	// ProviderID selects the backend. Empty or "anthropic" → Anthropic
	// Messages API. "openai" → OpenAI /v1/chat/completions (added in a
	// later phase). Any other value is rejected by New.
	ProviderID string

	// ModelID is passed through unchanged to the chosen backend.
	ModelID string

	// Anthropic-specific knobs. Empty values fall back to SDK defaults.
	AnthropicAPIKey  string
	AnthropicBaseURL string

	// OpenAI-specific knobs. Used only when ProviderID == "openai".
	OpenAIAPIKey  string
	OpenAIBaseURL string
}

// New constructs a Provider for the requested backend.
//
// Empty / "anthropic" → Anthropic Messages API via the official Go SDK.
// "openai"            → OpenAI Chat Completions (and any compatible
//
//	gateway exposing /v1/chat/completions).
//
// Any other value is rejected so a misconfigured PROVIDER_ID fails fast
// instead of silently falling back.
func New(cfg Config) (Provider, error) {
	switch cfg.ProviderID {
	case "", "anthropic":
		return newAnthropicProvider(cfg)
	case "openai":
		return newOpenAIProvider(cfg)
	default:
		return nil, fmt.Errorf("unknown PROVIDER_ID: %q (supported: anthropic, openai)", cfg.ProviderID)
	}
}
