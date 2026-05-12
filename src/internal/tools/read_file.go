package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

type ReadFileInput struct {
	Path  string `json:"path" jsonschema_description:"The relative path of a file in the working directory."`
	Limit int    `json:"limit,omitempty" jsonschema_description:"Maximum number of lines to return (0 = no limit)."`
}

func init() {
	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name:        "read_file",
			Description: anthropic.String("Read the contents of a given relative file path. Use this when you want to see what's inside a file. Do not use this with directory names."),
			InputSchema: GenerateSchema[ReadFileInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in ReadFileInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}
			return runReadFile(in.Path, in.Limit)
		},
	})
}

const maxReadBytes = 50000 // Hard cap on bytes returned by read_file

// runReadFile reads the contents of a file.
// limit ≤ 0 means no limit.
func runReadFile(path string, limit int) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read_file: %w", err)
	}
	text := string(data)
	if limit > 0 {
		lines := strings.Split(text, "\n")
		if limit < len(lines) {
			text = strings.Join(lines[:limit], "\n") +
				fmt.Sprintf("\n... (%d more lines)", len(lines)-limit)
		}
	}
	if len(text) > maxReadBytes {
		text = text[:maxReadBytes]
	}
	return text, nil
}
