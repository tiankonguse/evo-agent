package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

type BashInput struct {
	Command string `json:"command" jsonschema_description:"The shell command to run."`
}

func init() {
	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name:        "bash",
			Description: anthropic.String("Run a shell command in the current workspace."),
			InputSchema: GenerateSchema[BashInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in BashInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}
			return runBash(in.Command), nil
		},
	})
}

// runBash executes a shell command and returns its combined output.
// Execution is capped at 120 seconds.
func runBash(command string) string {
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
