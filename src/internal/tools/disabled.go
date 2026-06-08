// Package tools — disabled.go
//
// Per-project disable list for tools. Ship-side rationale: the model's
// system prompt carries the JSON schema of every registered tool on every
// LLM round-trip; turning a few unused tools off can save a meaningful
// chunk of input tokens without touching code or restarting MCP servers.
//
// State lives at <projectDir>/.evo-agent/disabled_tools.json — a sorted
// JSON array of tool names ("bash", "mcp__github__create_issue", …). Each
// SetDisabled call rewrites the file synchronously so the on-disk state is
// always consistent with the in-memory map; on next startup main.go calls
// LoadDisabled to rehydrate.
//
// The actual filtering happens inside tool.go's Tools() — every code path
// that asks for the tool catalogue (including ToolsExcept used by subagents
// and teammates) goes through Tools(), so a disabled tool is invisible to
// every LLM call without per-caller plumbing.

package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// disabledToolsFilename is the JSON store under .evo-agent/.
const disabledToolsFilename = "disabled_tools.json"

var (
	disabledMu   sync.RWMutex
	disabled     = map[string]bool{}
	disabledFile string // empty until LoadDisabled is called
)

// LoadDisabled reads .evo-agent/disabled_tools.json under projectDir into
// the in-memory disable set. Idempotent: a missing file is treated as
// "nothing disabled". Bad JSON also resets to empty rather than aborting,
// because the model loop must still start even if the user hand-edited
// the file into garbage.
func LoadDisabled(projectDir string) {
	disabledMu.Lock()
	defer disabledMu.Unlock()
	if projectDir == "" {
		return
	}
	disabledFile = filepath.Join(projectDir, ".evo-agent", disabledToolsFilename)
	data, err := os.ReadFile(disabledFile)
	if err != nil {
		return
	}
	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		return
	}
	disabled = map[string]bool{}
	for _, n := range list {
		disabled[n] = true
	}
}

// IsDisabled reports whether the named tool is in the disable set.
func IsDisabled(name string) bool {
	disabledMu.RLock()
	defer disabledMu.RUnlock()
	return disabled[name]
}

// SetDisabled flips a tool's disabled flag and persists the new state.
// `off=true` disables, `off=false` re-enables. Persistence errors are
// returned to the caller so the UI can surface them; the in-memory map
// is updated unconditionally so the next Tools() call already reflects
// the new state.
func SetDisabled(name string, off bool) error {
	disabledMu.Lock()
	if off {
		disabled[name] = true
	} else {
		delete(disabled, name)
	}
	list := make([]string, 0, len(disabled))
	for n := range disabled {
		list = append(list, n)
	}
	sort.Strings(list)
	file := disabledFile
	disabledMu.Unlock()

	if file == "" {
		return nil // no project dir → nothing to persist (tests)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	return os.WriteFile(file, data, 0o644)
}

// ResetDisabled clears the disable set (re-enables every tool) and
// persists. Equivalent to deleting disabled_tools.json from disk.
func ResetDisabled() error {
	disabledMu.Lock()
	disabled = map[string]bool{}
	file := disabledFile
	disabledMu.Unlock()
	if file == "" {
		return nil
	}
	// Write an empty array rather than removing the file so subsequent
	// SetDisabled calls don't have to recreate the parent dir.
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	return os.WriteFile(file, []byte("[]\n"), 0o644)
}

// DisabledList returns the sorted set of currently disabled tool names.
// Allocates a fresh slice — safe to mutate in callers.
func DisabledList() []string {
	disabledMu.RLock()
	defer disabledMu.RUnlock()
	out := make([]string, 0, len(disabled))
	for n := range disabled {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// ToolEntry summarizes one tool for the /tools picker / list. Source is
// either "builtin" or "mcp:<server>" so the user can spot MCP-supplied
// tools (which usually dwarf the built-ins in number).
type ToolEntry struct {
	Name     string
	Source   string
	Disabled bool
}

// AllToolEntries returns every tool currently visible to the agent
// (built-in + MCP), sorted by name, annotated with disabled state. Used
// by the TUI picker and the plain `/tools list` printer.
func AllToolEntries() []ToolEntry {
	var out []ToolEntry
	disabledMu.RLock()
	for name := range registry {
		out = append(out, ToolEntry{
			Name:     name,
			Source:   "builtin",
			Disabled: disabled[name],
		})
	}
	for _, t := range MCPTools() {
		if t.OfTool == nil {
			continue
		}
		name := t.OfTool.Name
		out = append(out, ToolEntry{
			Name:     name,
			Source:   mcpServerOf(name),
			Disabled: disabled[name],
		})
	}
	disabledMu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// mcpServerOf parses "mcp__<server>__<tool>" → "mcp:<server>". Returns
// "mcp:?" if the prefix shape is unexpected (defensive — never crashes).
func mcpServerOf(toolName string) string {
	rest := strings.TrimPrefix(toolName, "mcp__")
	if i := strings.Index(rest, "__"); i > 0 {
		return "mcp:" + rest[:i]
	}
	return "mcp:?"
}
