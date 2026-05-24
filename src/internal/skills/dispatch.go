package skills

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// SlashResult holds the outcome of processing a slash command.
type SlashResult struct {
	Found   bool   // true if /name matched a skill or command
	Prompt  string // instruction telling the model what was invoked
	Content string // rendered skill/command body
	Name    string // skill/command name (for UI display)
}

// Dispatch checks if input starts with "/" followed by a letter and processes it
// as a slash command. Returns SlashResult with Found=false if not a slash command.
func Dispatch(input string) SlashResult {
	input = strings.TrimSpace(input)

	// Must start with "/" followed by a letter (avoid matching file paths like /usr/bin)
	if len(input) < 2 || input[0] != '/' || !unicode.IsLetter(rune(input[1])) {
		return SlashResult{Found: false}
	}

	// Split into name and raw arguments at first space
	rest := input[1:] // remove leading "/"
	name := rest
	rawArgs := ""
	if idx := strings.IndexByte(rest, ' '); idx >= 0 {
		name = rest[:idx]
		rawArgs = strings.TrimSpace(rest[idx+1:])
	}

	// If "name" contains a slash, it's a file path, not a command
	if strings.ContainsRune(name, '/') {
		return SlashResult{Found: false}
	}

	// Look up: commands take priority over skills
	doc, ok := LookupForSlash(name)
	if !ok {
		return SlashResult{
			Found:   true,
			Prompt:  slashNotFoundError(name),
			Content: "",
			Name:    name,
		}
	}

	// Check if the skill is user-invocable
	if !doc.Manifest.UserInvocable {
		return SlashResult{
			Found:   true,
			Prompt:  fmt.Sprintf("Error: /%s is not user-invocable. This skill can only be invoked by the model via load_skill.", name),
			Content: "",
			Name:    name,
		}
	}

	// Parse arguments and render body
	args := ParseArgs(rawArgs)
	rendered := RenderBody(doc.Body, doc.Manifest.Arguments, args, rawArgs)

	// Build prompt: tell the model what was invoked
	source := "skill"
	if doc.Manifest.IsCommand {
		source = "command"
	}
	var prompt string
	if rawArgs != "" {
		prompt = fmt.Sprintf("User invoked /%s (%s) with arguments: %s. Follow the instructions below.", name, source, rawArgs)
	} else {
		prompt = fmt.Sprintf("User invoked /%s (%s). Follow the instructions below.", name, source)
	}

	// Wrap body in XML tags
	content := fmt.Sprintf("<skill name=%q source=\"slash\" type=%q>\n%s\n</skill>", doc.Manifest.Name, source, rendered)

	return SlashResult{
		Found:   true,
		Prompt:  prompt,
		Content: content,
		Name:    doc.Manifest.Name,
	}
}

// SlashNames returns all skill/command names available for slash invocation (user-invocable only).
// When a command and skill share the same name, both are included (deduplicated).
func SlashNames() []string {
	seen := map[string]bool{}
	names := make([]string, 0)
	for name, doc := range commandDocuments {
		if doc.Manifest.UserInvocable {
			names = append(names, name)
			seen[name] = true
		}
	}
	for name, doc := range skillDocuments {
		if doc.Manifest.UserInvocable && !seen[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// slashNotFoundError builds a helpful error message for unknown slash commands.
func slashNotFoundError(name string) string {
	available := SlashNames()
	if len(available) == 0 {
		return fmt.Sprintf("Error: Unknown command /%s. No skills or commands are available.", name)
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("Error: Unknown command /%s.", name))
	lines = append(lines, "Available commands:")
	for _, n := range available {
		manifest := GetManifest(n)
		hint := ""
		if manifest.ArgumentHint != "" {
			hint = " " + manifest.ArgumentHint
		}
		lines = append(lines, fmt.Sprintf("  /%s%s - %s", n, hint, manifest.Description))
	}
	return strings.Join(lines, "\n")
}
