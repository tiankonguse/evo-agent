package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"evo-agent/internal/ui"
)

const DumpDir = ".evo-agent/dump-prompts"

// dumpEntry represents one API call dumped to JSONL.
type dumpEntry struct {
	Timestamp string                    `json:"timestamp"`
	Model     string                    `json:"model"`
	System    string                    `json:"system"`
	Messages  []anthropic.MessageParam  `json:"messages"`
}

// DumpAPICall writes one API request (system + messages) to the session JSONL file.
func (a *Agent) DumpAPICall(system string, messages []anthropic.MessageParam) {
	// Lazy-init: create dump file on first call
	if a.dumpFile == "" {
		if err := os.MkdirAll(DumpDir, 0o755); err != nil {
			ui.PrintError(fmt.Sprintf("[dump-prompts] mkdir failed: %v", err))
			return
		}
		a.dumpFile = filepath.Join(DumpDir, fmt.Sprintf("dump_%d.jsonl", time.Now().Unix()))
	}

	entry := dumpEntry{
		Timestamp: time.Now().Format(time.RFC3339),
		Model:     a.cfg.ModelID,
		System:    system,
		Messages:  messages,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		ui.PrintError(fmt.Sprintf("[dump-prompts] marshal failed: %v", err))
		return
	}

	f, err := os.OpenFile(a.dumpFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		ui.PrintError(fmt.Sprintf("[dump-prompts] open failed: %v", err))
		return
	}
	defer f.Close()

	f.Write(data)
	f.WriteString("\n")
}

// ToggleDumpPrompts toggles the dump-prompts mode and returns the new state.
func (a *Agent) ToggleDumpPrompts() bool {
	a.DumpPrompts = !a.DumpPrompts
	return a.DumpPrompts
}
