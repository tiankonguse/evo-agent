package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

const TRANSCRIPT_DIR = ".evo_agent/transcripts"

// WriteTranscript saves message history to a timestamped JSONL file.
func WriteTranscript(messages []anthropic.MessageParam) error {
	// Create directory if needed
	if err := os.MkdirAll(TRANSCRIPT_DIR, 0o755); err != nil {
		return fmt.Errorf("mkdir failed: %w", err)
	}

	// Generate timestamped filename
	filename := fmt.Sprintf("transcript_%d.jsonl", time.Now().Unix())
	path := filepath.Join(TRANSCRIPT_DIR, filename)

	// Open file for writing
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create failed: %w", err)
	}
	defer f.Close()

	// Write one message per line (JSONL format)
	for _, msg := range messages {
		data, err := json.Marshal(msg)
		if err != nil {
			continue // Skip messages that can't serialize
		}

		if _, err := f.Write(data); err != nil {
			return fmt.Errorf("write failed: %w", err)
		}

		if _, err := f.WriteString("\n"); err != nil {
			return fmt.Errorf("newline failed: %w", err)
		}
	}

	fmt.Printf("[transcript saved: %s]\n", path)
	return nil
}

// LoadTranscript loads a JSONL transcript file (optional - for debugging).
func LoadTranscript(path string) ([]anthropic.MessageParam, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var messages []anthropic.MessageParam
	for _, line := range strings.Split(string(data), "\n") {
		if len(line) == 0 {
			continue
		}

		var msg anthropic.MessageParam
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue // Skip malformed lines
		}

		messages = append(messages, msg)
	}

	return messages, nil
}
