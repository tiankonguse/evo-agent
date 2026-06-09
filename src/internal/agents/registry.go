// Package agents loads and serves custom subagent definitions from
// .evo-agent/agents/<name>.md.
//
// Each agent file is a Markdown document with YAML-style frontmatter:
//
//	---
//	name: code-reviewer
//	description: Review code for security, style, and correctness issues
//	model: inherit            # optional, default = parent's model
//	max_turns: 30             # optional, default = 30
//	---
//	You are a senior code reviewer ...
//
// The body of the file (after the closing `---`) becomes the agent's full
// system prompt. Unlike the generic subagent (internal/agent/subagent.go),
// custom agents do NOT inherit the parent's system prompt — they get a
// minimal environment envelope plus their own prompt body. This matches
// Claude Code's behavior for custom agents.
//
// The package is a leaf — no dependencies on internal/tools or
// internal/agent — so any caller (including tools/task.go) can import it
// without risking import cycles.
package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// AgentDefinition is the parsed in-memory representation of one
// .evo-agent/agents/<name>.md file.
//
// All fields are intentionally plain values (no pointers, no nested types)
// so the struct can be passed across package boundaries without coupling
// callers to this package's internals.
type AgentDefinition struct {
	Name         string // unique identifier; falls back to filename minus .md
	Description  string // shown in the parent agent's prompt to advertise this agent
	SystemPrompt string // the body of the markdown file (trimmed)
	Model        string // optional override; "" or "inherit" = use parent's model
	MaxTurns     int    // optional override; 0 = use the runner's default
	Path         string // absolute path to the source file (for diagnostics)
}

var (
	// frontmatterRe matches YAML frontmatter at the top of a markdown file.
	// Mirrors the pattern used in internal/skills/registry.go.
	frontmatterRe = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n(.*)`)

	registry = map[string]AgentDefinition{}
)

// Init scans <projectDir>/.evo-agent/agents/*.md and populates the
// in-memory registry. Missing directory is silently treated as
// "no custom agents" (consistent with skills.Init / commands).
//
// Init is called once at startup from main.go. Subsequent calls reset
// the registry — useful for tests but not exposed as public API.
func Init(projectDir string) {
	registry = map[string]AgentDefinition{}

	dir := filepath.Join(projectDir, ".evo-agent", "agents")
	if _, err := os.Stat(dir); err != nil {
		// Missing directory or unreadable — skip silently.
		fmt.Printf("[Agents] Loaded 0 agent(s)\n")
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[Agents] ReadDir error: %v\n", err)
		return
	}

	count := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			fmt.Fprintf(os.Stderr, "[Agents] Cannot read %s: %v\n", path, readErr)
			continue
		}
		meta, body := parseFrontmatter(string(data))
		name := meta["name"]
		if name == "" {
			name = strings.TrimSuffix(entry.Name(), ".md")
		}
		desc := meta["description"]
		if desc == "" {
			desc = "No description"
		}
		model := strings.TrimSpace(meta["model"])
		if strings.EqualFold(model, "inherit") {
			model = ""
		}
		maxTurns := 0
		if v := strings.TrimSpace(meta["max_turns"]); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				maxTurns = n
			}
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			absPath = path
		}
		registry[name] = AgentDefinition{
			Name:         name,
			Description:  desc,
			SystemPrompt: strings.TrimSpace(body),
			Model:        model,
			MaxTurns:     maxTurns,
			Path:         absPath,
		}
		count++
	}
	fmt.Printf("[Agents] Loaded %d agent(s)\n", count)
}

// Get returns the named agent definition. ok=false when no agent by that
// name has been loaded.
func Get(name string) (AgentDefinition, bool) {
	def, ok := registry[name]
	return def, ok
}

// List returns all loaded agent definitions sorted by name.
// Used for "available agents" listings (Catalog, error messages, /agents UI).
func List() []AgentDefinition {
	defs := make([]AgentDefinition, 0, len(registry))
	for _, def := range registry {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	return defs
}

// Names returns the loaded agent names sorted ascending. Convenient for
// constructing error messages ("available: a, b, c").
func Names() []string {
	defs := List()
	out := make([]string, 0, len(defs))
	for _, d := range defs {
		out = append(out, d.Name)
	}
	return out
}

// Catalog returns a formatted listing of available agents suitable for
// inclusion in the main agent's system prompt. Returns an empty string
// when no custom agents are loaded so the caller can omit the section.
//
// Example output:
//
//	Available custom agents (invoke via task tool with subagent_type):
//	- code-reviewer: Review code for security, style, and correctness issues
//	- test-runner: Run tests and report failures
func Catalog() string {
	defs := List()
	if len(defs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Available custom agents (invoke via the task tool with subagent_type):\n")
	for _, def := range defs {
		fmt.Fprintf(&b, "- %s: %s\n", def.Name, def.Description)
	}
	b.WriteString("\nWhen the model determines a task matches one of these agents, call task with subagent_type set to the agent's name. The subagent runs with its own system prompt and (optionally) a different model.")
	return b.String()
}

// parseFrontmatter mirrors the implementation in internal/skills/registry.go.
// Kept local rather than shared to avoid creating a new utility package
// for a 15-line helper used in two leaf packages.
func parseFrontmatter(text string) (meta map[string]string, body string) {
	meta = map[string]string{}
	matches := frontmatterRe.FindStringSubmatch(text)
	if matches == nil {
		return meta, text
	}
	for _, line := range strings.Split(matches[1], "\n") {
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		meta[key] = val
	}
	return meta, matches[2]
}
