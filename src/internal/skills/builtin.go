package skills

import (
	"embed"
	"fmt"
	"path/filepath"
	"strings"
)

//go:embed builtin_commands/*.md
var builtinCommandsFS embed.FS

// LoadBuiltinCommands registers embedded built-in commands into commandDocuments.
// User commands from .evo-agent/command/ take priority — if a command with the
// same name already exists, the built-in version is skipped.
func LoadBuiltinCommands() {
	entries, err := builtinCommandsFS.ReadDir("builtin_commands")
	if err != nil {
		return
	}

	count := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		data, readErr := builtinCommandsFS.ReadFile("builtin_commands/" + entry.Name())
		if readErr != nil {
			fmt.Printf("[Builtin] Cannot read %s: %v\n", entry.Name(), readErr)
			continue
		}

		meta, body := parseFrontmatter(string(data))
		name := meta["name"]
		if name == "" {
			name = strings.TrimSuffix(entry.Name(), ".md")
		}

		// User commands take priority over built-in commands
		if _, exists := commandDocuments[name]; exists {
			continue
		}

		description := meta["description"]
		if description == "" {
			description = "No description"
		}
		argHint := meta["argument-hint"]
		arguments := parseArguments(meta["arguments"])
		userInvocable := meta["user-invocable"] != "false" // default true

		absPath := filepath.Join("<builtin>", entry.Name())

		commandDocuments[name] = skillDocument{
			Manifest: SkillManifest{
				Name:          name,
				Description:   description,
				ArgumentHint:  argHint,
				Arguments:     arguments,
				IsCommand:     true,
				UserInvocable: userInvocable,
			},
			Body: strings.TrimSpace(body),
			Path: absPath,
		}
		count++
	}
	if count > 0 {
		fmt.Printf("[Builtin] Loaded %d built-in command(s)\n", count)
	}
}
