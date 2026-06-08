// Package agent — toolscmd.go
//
// Client-side `/tools` command for the plain REPL and the TUI's text
// path. The TUI also has an interactive picker (typing exactly "/tools"
// in the textarea opens a dropdown that the user navigates with
// up/down/space/enter), but the picker is implemented inside the tui
// package and never reaches Repl. This file handles the text forms that
// both modes accept:
//
//   /tools                      → list every tool (with [✓]/[✗] markers)
//   /tools list                 → same as /tools
//   /tools disable <name>       → turn one off
//   /tools off <name>           → alias of disable
//   /tools enable <name>        → turn one on
//   /tools on <name>            → alias of enable
//   /tools reset                → re-enable everything
//
// Pure status / state-mutation: never drives an LLM turn, so persisting
// to .evo-agent/disabled_tools.json is the only side effect on disk.

package agent

import (
	"fmt"
	"sort"
	"strings"

	"evo-agent/internal/tools"
	"evo-agent/internal/ui"
)

// ToolsCmdAction enumerates the parsed user intent.
type ToolsCmdAction int

const (
	ToolsCmdNotMatched ToolsCmdAction = iota
	ToolsCmdList                      // "/tools" or "/tools list"
	ToolsCmdDisable                   // "/tools disable <name>"
	ToolsCmdEnable                    // "/tools enable <name>"
	ToolsCmdReset                     // "/tools reset"
)

// ParseToolsCmd inspects a user query and returns (action, arg). `arg` is
// the tool name for Disable / Enable; empty otherwise. Returns NotMatched
// for anything that isn't a /tools invocation so the caller can fall
// through to the next handler.
func ParseToolsCmd(query string) (action ToolsCmdAction, arg string) {
	q := strings.TrimSpace(query)
	if q == "/tools" {
		return ToolsCmdList, ""
	}
	const prefix = "/tools "
	if !strings.HasPrefix(q, prefix) {
		return ToolsCmdNotMatched, ""
	}
	rest := strings.TrimSpace(q[len(prefix):])
	switch {
	case rest == "" || rest == "list":
		return ToolsCmdList, ""
	case rest == "reset":
		return ToolsCmdReset, ""
	case strings.HasPrefix(rest, "disable ") || strings.HasPrefix(rest, "off "):
		name := strings.TrimSpace(strings.SplitN(rest, " ", 2)[1])
		if name == "" {
			return ToolsCmdNotMatched, ""
		}
		return ToolsCmdDisable, name
	case strings.HasPrefix(rest, "enable ") || strings.HasPrefix(rest, "on "):
		name := strings.TrimSpace(strings.SplitN(rest, " ", 2)[1])
		if name == "" {
			return ToolsCmdNotMatched, ""
		}
		return ToolsCmdEnable, name
	}
	return ToolsCmdNotMatched, ""
}

// handleToolsCmd executes a parsed /tools command.
func (r *Repl) handleToolsCmd(action ToolsCmdAction, arg string) {
	switch action {
	case ToolsCmdList:
		ui.PrintSystem(formatToolsList(tools.AllToolEntries()))
	case ToolsCmdDisable:
		if !toolExists(arg) {
			ui.PrintError(fmt.Sprintf("/tools: unknown tool %q", arg))
			return
		}
		if err := tools.SetDisabled(arg, true); err != nil {
			ui.PrintError(fmt.Sprintf("/tools disable: %v", err))
			return
		}
		ui.PrintSystem(fmt.Sprintf("✗ disabled %s", arg))
	case ToolsCmdEnable:
		if !toolExists(arg) {
			ui.PrintError(fmt.Sprintf("/tools: unknown tool %q", arg))
			return
		}
		if err := tools.SetDisabled(arg, false); err != nil {
			ui.PrintError(fmt.Sprintf("/tools enable: %v", err))
			return
		}
		ui.PrintSystem(fmt.Sprintf("✓ enabled %s", arg))
	case ToolsCmdReset:
		if err := tools.ResetDisabled(); err != nil {
			ui.PrintError(fmt.Sprintf("/tools reset: %v", err))
			return
		}
		ui.PrintSystem("✓ all tools enabled")
	}
}

// formatToolsList renders the tool roster as a multi-line string suitable
// for ui.PrintSystem. Format:
//
//	Tools (N total, M disabled):
//	  [✓] bash                        builtin
//	  [✗] mcp__github__create_issue   mcp:github
//	  ...
//
// Sorted by name (entries are already sorted by AllToolEntries).
func formatToolsList(entries []tools.ToolEntry) string {
	if len(entries) == 0 {
		return "Tools: (none registered)"
	}
	var disabled int
	var maxName int
	for _, e := range entries {
		if e.Disabled {
			disabled++
		}
		if n := len(e.Name); n > maxName {
			maxName = n
		}
	}
	if maxName > 40 {
		maxName = 40
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Tools (%d total, %d disabled):\n", len(entries), disabled)
	for _, e := range entries {
		marker := "[✓]"
		if e.Disabled {
			marker = "[✗]"
		}
		name := e.Name
		if len(name) > maxName {
			name = name[:maxName-1] + "…"
		}
		fmt.Fprintf(&b, "  %s %-*s  %s\n", marker, maxName, name, e.Source)
	}
	b.WriteString("\nUsage: /tools disable <name> | /tools enable <name> | /tools reset")
	return b.String()
}

// toolExists checks whether a name corresponds to either a registered
// built-in or a known MCP tool. Used to give a useful error before we
// persist a name that nothing maps to.
func toolExists(name string) bool {
	for _, e := range tools.AllToolEntries() {
		if e.Name == name {
			return true
		}
	}
	return false
}

// sortedToolNames is exported because the TUI's autocomplete / help
// surfaces want a quick alphabetical roster without needing to call
// tools.AllToolEntries() (which returns richer ToolEntry values).
func sortedToolNames() []string {
	entries := tools.AllToolEntries()
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name)
	}
	sort.Strings(out)
	return out
}
