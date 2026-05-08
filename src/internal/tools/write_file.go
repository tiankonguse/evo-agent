package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/anthropics/anthropic-sdk-go"
)

type WriteFileInput struct {
	Path    string `json:"path" jsonschema_description:"The relative path of the file to write."`
	Content string `json:"content" jsonschema_description:"Full content to write to the file."`
}

func init() {
	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name:        "write_file",
			Description: anthropic.String("Write content to a file, creating parent directories as needed."),
			InputSchema: GenerateSchema[WriteFileInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in WriteFileInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}
			return runWriteFile(in.Path, in.Content)
		},
	})
}

// runWriteFile writes content to a file, creating parent directories as needed.
func runWriteFile(path, content string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("write_file: mkdir: %w", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write_file: %w", err)
	}
	return fmt.Sprintf("Wrote %d bytes to %s", len(content), path), nil
}
