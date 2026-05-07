package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// RunBash executes a shell command and returns its combined output.
// execution is capped at 120 seconds.
func RunBash(command string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	if cwd, err := os.Getwd(); err == nil {
		cmd.Dir = cwd
	}

	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return "Error: Timeout (120s)"
	}
	if err != nil && len(out) == 0 {
		return fmt.Sprintf("Error: %v", err)
	}

	output := strings.TrimSpace(string(out))
	if output == "" {
		return "(no output)"
	}
	if len(output) > 50000 {
		return output[:50000]
	}
	return output
}
