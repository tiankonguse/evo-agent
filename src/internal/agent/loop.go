package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

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

// Loop drives the agent loop until the model stops requesting tool calls.
// Loop sends the current message history to the model, appends the
// response, executes any tool calls, and returns true if another turn is needed.
func (a *Agent) Loop(state *LoopState) bool {
	for {
		resp, err := a.client.Messages.New(context.Background(), anthropic.MessageNewParams{
			Model: anthropic.Model(a.cfg.ModelID),
			System: []anthropic.TextBlockParam{
				{Text: a.cfg.SystemMsg},
			},
			Messages:  state.Messages,
			Tools:     tools.Tools(),
			MaxTokens: 8000,
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
	}
}

// Run is the top-level REPL: reads queries from r, runs the agent loop for
// each one, and prints the final assistant response.
func (a *Agent) Run(r io.Reader) {
	scanner := bufio.NewScanner(r)
	var history []anthropic.MessageParam

	for {
		fmt.Printf("%s >> %s", ui.ColorCyan, ui.ColorReset)
		if !scanner.Scan() {
			break
		}
		query := strings.TrimSpace(scanner.Text())
		if query == "" || query == "q" || query == "exit" {
			break
		}

		history = append(history, anthropic.NewUserMessage(
			anthropic.NewTextBlock(query),
		))

		state := &LoopState{
			Messages:  history,
			TurnCount: 1,
		}
		a.Loop(state)
		history = state.Messages

		if len(history) > 0 {
			last := history[len(history)-1]
			if last.Role == anthropic.MessageParamRoleAssistant {
				for _, part := range last.Content {
					if part.OfText != nil && part.OfText.Text != "" {
						fmt.Println(part.OfText.Text)
					}
				}
			}
		}
		fmt.Println()
	}
}
