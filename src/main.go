package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"evo-agent/internal/agent"
	"evo-agent/internal/config"
	"evo-agent/internal/ui"
)

func main() {
	config.LoadEnv()
	cfg := config.Load()

	if cfg.ModelID == "" {
		fmt.Fprintln(os.Stderr, "Error: MODEL_ID not set in environment")
		os.Exit(1)
	}

	opts := []option.RequestOption{}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
		os.Unsetenv("ANTHROPIC_AUTH_TOKEN")
	}
	if cfg.APIKey != "" {
		opts = append(opts, option.WithAPIKey(cfg.APIKey))
	} else {
		opts = append(opts, option.WithAPIKey("dummy"))
	}

	client := anthropic.NewClient(opts...)
	a := agent.New(client, cfg)

	scanner := bufio.NewScanner(os.Stdin)
	var history []anthropic.MessageParam

	for {
		fmt.Printf("%ss01 >> %s", ui.ColorCyan, ui.ColorReset)
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

		state := &agent.LoopState{
			Messages:  history,
			TurnCount: 1,
		}
		a.Loop(state)
		history = state.Messages

		// Print final text response if last message is from assistant
		if len(history) > 0 {
			last := history[len(history)-1]
			if last.Role.Value == anthropic.MessageParamRoleAssistant {
				for _, part := range last.Content.Value {
					if tb, ok := part.(anthropic.TextBlockParam); ok {
						if tb.Text.Value != "" {
							fmt.Println(tb.Text.Value)
						}
					}
				}
			}
		}
		fmt.Println()
	}
}
