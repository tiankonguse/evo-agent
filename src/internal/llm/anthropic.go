package llm

import (
	"context"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

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
func (p *anthropicProvider) SendMessage(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error) {
	return p.client.Messages.New(ctx, params)
}
