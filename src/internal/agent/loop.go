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
	"evo-agent/internal/prompt"
	"evo-agent/internal/skills"
	"evo-agent/internal/tools"
	"evo-agent/internal/ui"
)

// Agent orchestrates multi-turn conversations with the model.
type Agent struct {
	client      *anthropic.Client
	cfg         *config.Config
	prompt      *prompt.Builder
	DumpPrompts bool   // toggle via /dump-prompts
	dumpFile    string // session dump file path (set on first dump)
}

// New creates an Agent with the given LLM client, configuration, and prompt builder.
// It also wires up tools.SubagentRunner so the task tool can spawn child agents.
func New(client *anthropic.Client, cfg *config.Config, pb *prompt.Builder) *Agent {
	a := &Agent{client: client, cfg: cfg, prompt: pb}
	tools.RegisterSubagentRunner(func(systemPrompt string, messages []anthropic.MessageParam) string {
		return a.RunSubagent(systemPrompt, messages)
	})
	return a
}

// autoCompact applies MicroCompact, then triggers a full LLM summarization if
// the estimated context size still exceeds CONTEXT_LIMIT.
func (a *Agent) autoCompact(state *LoopState) {
	state.Messages = MicroCompact(state.Messages, KEEP_RECENT_RESULTS)

	contextSize := EstimateContextSize(state.Messages)
	if contextSize <= CONTEXT_LIMIT {
		return
	}

	ui.PrintSystem(fmt.Sprintf("[auto compact triggered: %d chars]", contextSize))

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
		ui.PrintError(fmt.Sprintf("ERROR: Compaction failed: %v", err))
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

		ui.PrintSystem("[manual compact requested]")
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
func (a *Agent) Loop(state *LoopState) bool {
	// Initialize CompactState if needed
	if state.CompactState == nil {
		state.CompactState = &CompactState{}
	}

	for {
		// Apply micro-compaction + auto compact before LLM call
		a.autoCompact(state)

		systemPrompt := a.prompt.Build()

		// Dump API call if enabled
		if a.DumpPrompts {
			a.DumpAPICall(systemPrompt, state.Messages)
		}

		resp, err := a.client.Messages.New(context.Background(), anthropic.MessageNewParams{
			Model: anthropic.Model(a.cfg.ModelID),
			System: []anthropic.TextBlockParam{
				{Text: systemPrompt},
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

		// Count content block types
		blockCounts := map[string]int{}
		for _, block := range resp.Content {
			blockCounts[string(block.Type)]++
		}
		var blockParts []string
		for t, c := range blockCounts {
			blockParts = append(blockParts, fmt.Sprintf("%s:%d", t, c))
		}
		blockSummary := strings.Join(blockParts, " ")

		ui.PrintTokens(
			string(resp.Model),
			resp.Usage.InputTokens,
			resp.Usage.OutputTokens,
			string(resp.StopReason),
			blockSummary,
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

		// Pass current conversation to tools so the remember tool can serialize it
		tools.SetConversationMessages(state.Messages)

		toolResults := tools.Execute(resp.Content, state.CompactState)
		if len(toolResults) == 0 {
			state.TransitionReason = ""
			return false
		}

		// ── Todo reminder injection ───────────────────────────────────────────
		// Track whether the model used the todo tool this turn and, if not,
		// inject a reminder after todoReminderInterval rounds without a plan update.
		usedTodo := false
		for _, block := range resp.Content {
			if block.Type == "tool_use" && block.Name == "todo" {
				usedTodo = true
				break
			}
		}
		tools.GlobalTodo.NoteRound(usedTodo)
		if reminder := tools.GlobalTodo.Reminder(); reminder != "" {
			toolResults = append(toolResults, anthropic.NewTextBlock(reminder))
		}

		state.Messages = append(state.Messages, anthropic.NewUserMessage(toolResults...))
		state.TurnCount++
		state.TransitionReason = "tool_result"

		// Apply manual compaction if the model called the compact tool
		a.manualCompact(state, resp.Content)
	}
}

// Run is the top-level REPL for plain-text mode.
func (a *Agent) Run(r io.Reader) {
	scanner := bufio.NewScanner(r)
	var history []anthropic.MessageParam
	compactState := &CompactState{}

	for {
		fmt.Printf("%s >> %s", ui.ColorCyan, ui.ColorReset)
		if !scanner.Scan() {
			break
		}
		query := strings.TrimSpace(scanner.Text())
		if query == "" || query == "q" || query == "exit" {
			break
		}

		// ── Client-side commands (not sent to LLM) ──
		if query == "/dump-prompts" {
			on := a.ToggleDumpPrompts()
			if on {
				fmt.Println("[dump-prompts: ON]")
			} else {
				fmt.Println("[dump-prompts: OFF]")
			}
			continue
		}

		// ── Slash command interception ──
		if result := skills.Dispatch(query); result.Found {
			if result.Content != "" {
				// Two-block message: prompt + skill content
				history = append(history, anthropic.NewUserMessage(
					anthropic.NewTextBlock(result.Prompt),
					anthropic.NewTextBlock(result.Content),
				))
			} else {
				// Error case (unknown command): single block
				history = append(history, anthropic.NewUserMessage(
					anthropic.NewTextBlock(result.Prompt),
				))
			}
		} else {
			history = append(history, anthropic.NewUserMessage(
				anthropic.NewTextBlock(query),
			))
		}

		state := &LoopState{
			Messages:     history,
			TurnCount:    1,
			CompactState: compactState,
		}
		a.Loop(state)
		history = state.Messages
		compactState = state.CompactState

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

// RunQuery executes a single query and signals done via doneCh.
// Used by the TUI mode.
func (a *Agent) RunQuery(query string, history *[]anthropic.MessageParam, compactState **CompactState, doneCh chan<- struct{}) {
	*history = append(*history, anthropic.NewUserMessage(
		anthropic.NewTextBlock(query),
	))

	state := &LoopState{
		Messages:     *history,
		TurnCount:    1,
		CompactState: *compactState,
	}
	a.Loop(state)
	*history = state.Messages
	*compactState = state.CompactState

	close(doneCh)
}

// RunQueryDirect executes the agent loop without appending a new message.
// The caller has already appended the user message(s) to history.
// Used by slash command handling where multi-block messages are constructed.
func (a *Agent) RunQueryDirect(history *[]anthropic.MessageParam, compactState **CompactState, doneCh chan<- struct{}) {
	state := &LoopState{
		Messages:     *history,
		TurnCount:    1,
		CompactState: *compactState,
	}
	a.Loop(state)
	*history = state.Messages
	*compactState = state.CompactState

	close(doneCh)
}
