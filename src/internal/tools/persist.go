package tools

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	persistThreshold = 30000
	previewChars     = 2000
	toolResultsDir   = ".evo-agent/tool-results"
)

// persistLargeOutput saves output exceeding the threshold to disk and returns
// a short preview placeholder. Falls back to in-memory truncation on error.
func persistLargeOutput(toolID, output string) string {
	if len(output) <= persistThreshold {
		return output
	}

	// Attempt to save to disk.
	if err := os.MkdirAll(toolResultsDir, 0o755); err == nil {
		storedPath := filepath.Join(toolResultsDir, toolID+".txt")
		if _, err := os.Stat(storedPath); os.IsNotExist(err) {
			_ = os.WriteFile(storedPath, []byte(output), 0o644)
		}
		preview := output
		if len(preview) > previewChars {
			preview = preview[:previewChars]
		}
		return fmt.Sprintf(
			"<persisted-output>\nFull output saved to: %s\nPreview:\n%s\n</persisted-output>",
			storedPath, preview,
		)
	}

	// Fallback: truncate in memory.
	if len(output) > previewChars {
		return output[:previewChars] + "\n... (truncated, output too large)"
	}
	return output
}
