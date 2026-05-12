package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"evo-agent/internal/ui"
	"github.com/anthropics/anthropic-sdk-go"
)

const (
	CONTEXT_LIMIT        = 50000 // Auto-compact threshold
	KEEP_RECENT_RESULTS  = 3     // Keep this many recent tool results
	maxConversationBytes = 80000 // Max conversation bytes passed to summarization LLM
)

// EstimateContextSize returns the approximate context size in characters.
func EstimateContextSize(messages []anthropic.MessageParam) int {
	data, _ := json.Marshal(messages)
	return len(string(data))
}

// MicroCompact compresses older tool results, keeping only the most recent
// keepCount results intact. Earlier results are replaced with a short
// placeholder to reduce context size without losing recent tool outputs.
//
// Crucially, all tool results in the *last* user message that contains tool
// results are always preserved, regardless of keepCount. This prevents the
// current turn's batch of results (which may exceed keepCount) from being
// compacted away before the model has a chance to see them.
func MicroCompact(messages []anthropic.MessageParam, keepCount int) []anthropic.MessageParam {
	const placeholder = "[Earlier tool result compacted. Re-run the tool if you need full detail.]"

	// Find the index of the last user message that contains at least one
	// tool_result block. Results in that message must never be compacted.
	lastToolMsgIdx := -1
	for i := range messages {
		if messages[i].Role != anthropic.MessageParamRoleUser {
			continue
		}
		for _, blk := range messages[i].Content {
			if blk.OfToolResult != nil {
				lastToolMsgIdx = i
				break
			}
		}
	}

	// Collect pointers to tool_result blocks from messages *before* the last
	// tool-result message. Skip blocks that are already compacted (placeholder)
	// so they don't consume keepCount quota.
	var older []*anthropic.ToolResultBlockParam
	for i := range messages {
		if i >= lastToolMsgIdx {
			break
		}
		if messages[i].Role != anthropic.MessageParamRoleUser {
			continue
		}
		for j := range messages[i].Content {
			blk := messages[i].Content[j].OfToolResult
			if blk == nil {
				continue
			}
			// Skip already-compacted blocks.
			if len(blk.Content) == 1 {
				if txt := blk.Content[0].OfText; txt != nil && txt.Text == placeholder {
					continue
				}
			}
			older = append(older, blk)
		}
	}

	if len(older) <= keepCount {
		return messages
	}

	// Replace content of all but the most recent keepCount older results.
	for _, blk := range older[:len(older)-keepCount] {
		if len(blk.Content) == 1 {
			if txt := blk.Content[0].OfText; txt != nil && len(txt.Text) > 120 {
				blk.Content = []anthropic.ToolResultBlockParamContentUnion{
					{OfText: &anthropic.TextBlockParam{Text: placeholder}},
				}
			}
		}
	}

	return messages
}

// SummarizeHistory calls the LLM to generate a conversation summary.
func SummarizeHistory(client *anthropic.Client, model string, messages []anthropic.MessageParam) (string, error) {
	// Truncate conversation if too large
	data, _ := json.Marshal(messages)
	conversation := string(data)
	if len(conversation) > maxConversationBytes {
		conversation = conversation[:maxConversationBytes]
	}

	prompt := `Summarize this coding-agent conversation so work can continue.
Preserve:
1. The current goal
2. Important findings and decisions
3. Files read or changed
4. Remaining work
5. User constraints and preferences
Be compact but concrete.

` + conversation

	fmt.Printf("%sDEBUG: Generating summary...%s\n", ui.ColorMagenta, ui.ColorReset)
	start := time.Now()

	resp, err := client.Messages.New(context.Background(), anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(prompt))},
		MaxTokens: 2000,
	})

	elapsed := time.Since(start).Seconds()
	fmt.Printf("%sDEBUG: Summary generated in %.2fs%s\n", ui.ColorMagenta, elapsed, ui.ColorReset)

	if err != nil {
		return "", err
	}

	if len(resp.Content) == 0 {
		return "", fmt.Errorf("empty response from summarization")
	}

	// Extract text from response
	for _, block := range resp.Content {
		if block.Type == "text" && block.Text != "" {
			return block.Text, nil
		}
	}

	return "", fmt.Errorf("no text block in summarization response")
}

// CompactHistory triggers full context compaction with LLM summarization.
// Returns new message list containing only the summary.
func CompactHistory(
	client *anthropic.Client,
	model string,
	messages []anthropic.MessageParam,
	state *CompactState,
	focus string,
) ([]anthropic.MessageParam, error) {
	// Save transcript before compacting
	if err := WriteTranscript(messages); err != nil {
		fmt.Printf("%sWARNING: Failed to write transcript: %v%s\n", ui.ColorMagenta, err, ui.ColorReset)
	}

	// Generate summary
	summary, err := SummarizeHistory(client, model, messages)
	if err != nil {
		fmt.Printf("%sERROR: Summarization failed: %v%s\n", ui.ColorReset, err, ui.ColorReset)
		return messages, err
	}

	// Add focus if provided
	if focus != "" {
		summary += fmt.Sprintf("\n\nFocus to preserve next: %s", focus)
	}

	// Add recent files for easy re-opening
	if len(state.RecentFiles) > 0 {
		summary += "\n\nRecent files to reopen if needed:"
		for _, f := range state.RecentFiles {
			summary += fmt.Sprintf("\n- %s", f)
		}
	}

	// Update state
	state.HasCompacted = true
	state.LastSummary = summary
	state.CompactCount++

	fmt.Printf("%s[Compacted to %d chars, removed %d messages]%s\n",
		ui.ColorMagenta, len(summary), len(messages)-1, ui.ColorReset)

	// Return new message list with only the summary
	return []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(
			"This conversation was compacted so the agent can continue working.\n\n" + summary,
		)),
	}, nil
}

// TrackRecentFile adds a file to the recent files list (FIFO, max 5).
func TrackRecentFile(state *CompactState, path string) {
	// Remove if already exists
	for i, f := range state.RecentFiles {
		if f == path {
			state.RecentFiles = append(state.RecentFiles[:i], state.RecentFiles[i+1:]...)
			break
		}
	}

	// Add to end
	state.RecentFiles = append(state.RecentFiles, path)

	// Keep max 5
	if len(state.RecentFiles) > 5 {
		state.RecentFiles = state.RecentFiles[len(state.RecentFiles)-5:]
	}
}
