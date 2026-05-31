package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/anthropics/anthropic-sdk-go"
)

// LoadResult is what /resume needs to seed a new agent run from an old
// session: the messages to inject as initial history, the count of restored
// real messages (excluding the synthetic summary), and any compact summary
// that was present at the most recent boundary.
type LoadResult struct {
	Messages       []anthropic.MessageParam
	RestoredCount  int    // number of real user/assistant messages restored
	HasCompactedAt bool   // true if the source contained a compact_boundary
	Summary        string // the summary stored at the last compact_boundary
	SourceID       string
}

// LoadForResume reads the messages.jsonl of the given session id and rebuilds
// a runnable message slice.
//
// Recovery rules (mirroring refs/ref.md §8 and the user-approved plan):
//  1. Walk the file; remember the offset of the last compact_boundary record.
//  2. Drop every user/assistant record before that offset.
//  3. If a boundary existed, prepend a synthetic user message containing the
//     summary wrapped in <previous-conversation-summary> tags so the model
//     knows it is reading a digest, not a literal turn.
//  4. subagent_end records contribute their `Result` text as a system note
//     appended to the transcript so the parent retains the subagent's
//     conclusion (the per-message subagent body itself is not replayed).
func LoadForResume(projectDir, sessionID string) (*LoadResult, error) {
	path := filepath.Join(projectDir, SessionsDirName, sessionID, MessagesFile)
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open session %s: %w", sessionID, err)
	}
	defer f.Close()

	// Pass 1: collect every record (cheap; transcripts are small relative to
	// memory).
	var records []Record
	sc := bufio.NewScanner(f)
	// Allow long lines; tool results can be sizable.
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec Record
		if err := json.Unmarshal(line, &rec); err != nil {
			// Skip malformed lines (matches existing transcripts.go behavior).
			continue
		}
		records = append(records, rec)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan session %s: %w", sessionID, err)
	}

	// Find the index of the last compact_boundary.
	lastBoundary := -1
	var summary string
	for i, rec := range records {
		if rec.Type == TypeCompactBoundary {
			lastBoundary = i
			summary = rec.Summary
		}
	}

	out := &LoadResult{
		SourceID:       sessionID,
		HasCompactedAt: lastBoundary >= 0,
		Summary:        summary,
	}

	// Inject the summary first if there was a boundary, so the model sees it
	// as "previous conversation context" before any restored real turn.
	if out.HasCompactedAt && summary != "" {
		out.Messages = append(out.Messages, anthropic.NewUserMessage(
			anthropic.NewTextBlock(
				"<previous-conversation-summary>\n"+summary+"\n</previous-conversation-summary>",
			),
		))
	}

	// Walk records after the boundary and rebuild messages.
	startAt := lastBoundary + 1 // 0 if no boundary
	for i := startAt; i < len(records); i++ {
		rec := records[i]
		switch rec.Type {
		case TypeUser, TypeAssistant:
			if rec.Message == nil {
				continue
			}
			out.Messages = append(out.Messages, *rec.Message)
			out.RestoredCount++
		case TypeSubagentEnd:
			// Surface the subagent's final answer so the parent context still
			// "remembers" the conclusion. We attach it as a user-visible system
			// note rather than replaying the entire subagent transcript.
			if rec.Result != "" {
				out.Messages = append(out.Messages, anthropic.NewUserMessage(
					anthropic.NewTextBlock(fmt.Sprintf(
						"<subagent-result name=%q>\n%s\n</subagent-result>",
						rec.AgentName, rec.Result,
					)),
				))
			}
		}
	}

	return out, nil
}
