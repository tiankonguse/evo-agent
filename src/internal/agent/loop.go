package agent

import (
	"bufio"
	"context"
	"encoding/json"
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

// autoCompact applies MicroCompact, then triggers a full LLM summarization if
// the estimated context size still exceeds CONTEXT_LIMIT.
func (a *Agent) autoCompact(state *LoopState) {
	state.Messages = MicroCompact(state.Messages, KEEP_RECENT_RESULTS)

	contextSize := EstimateContextSize(state.Messages)
	if contextSize <= CONTEXT_LIMIT {
		return
	}

	fmt.Printf("%s[auto compact triggered: %d chars]%s\n",
		ui.ColorMagenta, contextSize, ui.ColorReset)

	newMessages, err := CompactHistory(
		a.client,
		a.cfg.ModelID,
		state.Messages,
		state.CompactState,
		"",
	)
	if err == nil {
		state.Messages = newMessages
	} else {
		fmt.Printf("%sERROR: Compaction failed: %v%s\n",
			ui.ColorReset, err, ui.ColorReset)
	}
}

// manualCompact inspects the response content for a "compact" tool call and,
// if found, runs a full LLM summarization with the optional focus hint.
func (a *Agent) manualCompact(state *LoopState, content []anthropic.ContentBlockUnion) {
	for _, block := range content {
		if block.Type != "tool_use" || block.Name != "compact" {
			continue
		}
		var input map[string]interface{}
		json.Unmarshal(block.Input, &input)
		focus, _ := input["focus"].(string)

		fmt.Printf("%s[manual compact requested]%s\n", ui.ColorMagenta, ui.ColorReset)
		newMessages, err := CompactHistory(
			a.client,
			a.cfg.ModelID,
			state.Messages,
			state.CompactState,
			focus,
		)
		if err == nil {
			state.Messages = newMessages
		}
		return // only one compact call per turn
	}
}

// Loop drives the agent loop until the model stops requesting tool calls.
// Loop sends the current message history to the model, appends the
// response, executes any tool calls, and returns true if another turn is needed.
func (a *Agent) Loop(state *LoopState) bool {
	// Initialize CompactState if needed
	if state.CompactState == nil {
		state.CompactState = &CompactState{}
	}

	for {
		// Apply micro-compaction + auto compact before LLM call
		a.autoCompact(state)

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

		// Track file reads before executing tools
		for _, block := range resp.Content {
			if block.Type == "tool_use" && block.Name == "read_file" {
				var input map[string]interface{}
				if err := json.Unmarshal(block.Input, &input); err == nil {
					if path, ok := input["path"].(string); ok {
						TrackRecentFile(state.CompactState, path)
					}
				}
			}
		}

		toolResults := tools.Execute(resp.Content, state.CompactState)
		if len(toolResults) == 0 {
			state.TransitionReason = ""
			return false
		}

		state.Messages = append(state.Messages, anthropic.NewUserMessage(toolResults...))
		state.TurnCount++
		state.TransitionReason = "tool_result"

		// Apply manual compaction if the model called the compact tool
		a.manualCompact(state, resp.Content)
	}
}

// Run is the top-level REPL: reads queries from r, runs the agent loop for
// each one, and prints the final assistant response.
func (a *Agent) Run(r io.Reader) {
	scanner := bufio.NewScanner(r)
	var history []anthropic.MessageParam
	compactState := &CompactState{} // Initialize once for session

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
			Messages:     history,
			TurnCount:    1,
			CompactState: compactState, // Persist across queries
		}
		a.Loop(state)
		history = state.Messages
		compactState = state.CompactState // Update reference

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
