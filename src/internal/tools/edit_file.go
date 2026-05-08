package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

type EditFileInput struct {
	Path   string `json:"path" jsonschema_description:"The path to the file to edit. File will be created if it does not exist and old_str is empty."`
	OldStr string `json:"old_str" jsonschema_description:"Text to search for — must match exactly and appear only once. Empty string to create a new file."`
	NewStr string `json:"new_str" jsonschema_description:"Text to replace old_str with."`
}

func init() {
	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name: "edit_file",
			Description: anthropic.String(`Make edits to a text file.

Replaces 'old_str' with 'new_str' in the given file. 'old_str' and 'new_str' MUST be different from each other.

If the file specified with path doesn't exist, it will be created.
`),
			InputSchema: GenerateSchema[EditFileInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in EditFileInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}
			return runEditFile(in.Path, in.OldStr, in.NewStr)
		},
	})
}

// runEditFile replaces the first occurrence of oldStr with newStr in the file.
// If the file does not exist and oldStr is empty, the file is created with newStr as content.
func runEditFile(path, oldStr, newStr string) (string, error) {
	if path == "" || oldStr == newStr {
		return "", fmt.Errorf("edit_file: invalid input parameters")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) && oldStr == "" {
			return runWriteFile(path, newStr)
		}
		return "", fmt.Errorf("edit_file: %w", err)
	}

	content := string(data)
	if !strings.Contains(content, oldStr) {
		return "", fmt.Errorf("edit_file: old_str not found in %s", path)
	}

	newContent := strings.Replace(content, oldStr, newStr, 1)
	if err := os.WriteFile(path, []byte(newContent), 0o644); err != nil {
		return "", fmt.Errorf("edit_file: %w", err)
	}
	return fmt.Sprintf("Edited %s", path), nil
}
