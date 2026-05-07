package agent

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"

	"evo-agent/internal/config"
	"evo-agent/internal/tools"
	"evo-agent/internal/ui"
)

// Agent orchestrates multi-turn conversations with the model.
type Agent struct {
	client *anthropic.Client
	cfg    *config.Config
}

// New creates an Agent with the given LLM client and configuration.
func New(client *anthropic.Client, cfg *config.Config) *Agent {
	return &Agent{client: client, cfg: cfg}
}

// RunOneTurn sends the current message history to the model, appends the
// response, executes any tool calls, and returns true if another turn is needed.
func (a *Agent) RunOneTurn(state *LoopState) bool {
	resp, err := a.client.Messages.New(context.Background(), anthropic.MessageNewParams{
		Model:     anthropic.F(anthropic.Model(a.cfg.ModelID)),
		System:    anthropic.F([]anthropic.TextBlockParam{{Text: anthropic.F(a.cfg.SystemMsg)}}),
		Messages:  anthropic.F(state.Messages),
		Tools:     anthropic.F(tools.Tools),
		MaxTokens: anthropic.F(int64(8000)),
	})
	if err != nil {
		ui.PrintError(fmt.Sprintf("Error calling API: %v", err))
		return false
	}

	// Append assistant response to history
	state.Messages = append(state.Messages, resp.ToParam())

	fmt.Println()
	fmt.Printf("%sDEBUG: Model used: %s, Tokens input: %d, Tokens output: %d, stop_reason: %s%s\n",
		ui.ColorMagenta,
		resp.Model,
		resp.Usage.InputTokens,
		resp.Usage.OutputTokens,
		resp.StopReason,
		ui.ColorReset,
	)

	toolResults := tools.Execute(resp.Content)
	if len(toolResults) == 0 {
		state.TransitionReason = ""
		return false
	}

	state.Messages = append(state.Messages, anthropic.NewUserMessage(toolResults...))
	state.TurnCount++
	state.TransitionReason = "tool_result"
	return true
}

// Run drives the agent loop until the model stops requesting tool calls.
func (a *Agent) Loop(state *LoopState) {
	for a.RunOneTurn(state) {
	}
}
