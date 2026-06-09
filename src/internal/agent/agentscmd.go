// Package agent — agentscmd.go
//
// Client-side `/agents` command for the plain REPL and the TUI's text
// path. Lists custom subagents loaded from .evo-agent/agents/<name>.md
// and supports inspecting individual definitions and reloading from disk.
//
// Forms accepted:
//
//	/agents               → list every custom agent (name, description, model, max_turns)
//	/agents list          → same as /agents
//	/agents show <name>   → print one agent's frontmatter + full system prompt body
//	/agents reload        → re-read .evo-agent/agents/ from disk (after editing files)
//
// Pure client-side inspection / state-mutation: never drives an LLM turn.

package agent

import (
	"fmt"
	"strings"

	"evo-agent/internal/agents"
	"evo-agent/internal/config"
	"evo-agent/internal/ui"
)

// AgentsCmdAction enumerates the parsed user intent.
type AgentsCmdAction int

const (
	AgentsCmdNotMatched AgentsCmdAction = iota
	AgentsCmdList                       // "/agents" or "/agents list"
	AgentsCmdShow                       // "/agents show <name>"
	AgentsCmdReload                     // "/agents reload"
)

// ParseAgentsCmd inspects a user query and returns (action, arg). `arg` is
// the agent name for Show; empty otherwise. Returns NotMatched for
// anything that isn't an /agents invocation so the caller can fall through.
func ParseAgentsCmd(query string) (action AgentsCmdAction, arg string) {
	q := strings.TrimSpace(query)
	if q == "/agents" {
		return AgentsCmdList, ""
	}
	const prefix = "/agents "
	if !strings.HasPrefix(q, prefix) {
		return AgentsCmdNotMatched, ""
	}
	rest := strings.TrimSpace(q[len(prefix):])
	switch {
	case rest == "" || rest == "list":
		return AgentsCmdList, ""
	case rest == "reload":
		return AgentsCmdReload, ""
	case strings.HasPrefix(rest, "show "):
		name := strings.TrimSpace(strings.TrimPrefix(rest, "show "))
		if name == "" {
			return AgentsCmdNotMatched, ""
		}
		return AgentsCmdShow, name
	}
	return AgentsCmdNotMatched, ""
}

// handleAgentsCmd executes a parsed /agents command.
func (r *Repl) handleAgentsCmd(action AgentsCmdAction, arg string) {
	switch action {
	case AgentsCmdList:
		ui.PrintSystem(formatAgentsList(agents.List()))
	case AgentsCmdShow:
		def, ok := agents.Get(arg)
		if !ok {
			ui.PrintError(fmt.Sprintf("/agents show: unknown agent %q. Available: %s", arg, joinNames(agents.Names())))
			return
		}
		ui.PrintSystem(formatAgentDetail(def))
	case AgentsCmdReload:
		// Re-read the directory; useful after editing a file in another window.
		// Re-uses the same path resolution as startup so the behaviour is
		// identical: missing dir → 0 agents, no error.
		cfg := config.Load()
		agents.Init(cfg.ProjectDir)
		ui.PrintSystem(fmt.Sprintf("✓ reloaded %d agent(s)", len(agents.Names())))
	}
}

// formatAgentsList renders the agent roster as a multi-line string suitable
// for ui.PrintSystem. Format:
//
//	Custom agents (3 loaded):
//	  - code-reviewer    [model=claude-3-5-haiku, max_turns=20]
//	      Review code for bugs and style
//	  - explore          [model=inherit, max_turns=30]
//	      Read-only codebase explorer
//	  ...
//	Usage: /agents show <name> | /agents reload
func formatAgentsList(defs []agents.AgentDefinition) string {
	if len(defs) == 0 {
		return "Custom agents: (none loaded)\n\n" +
			"Drop a Markdown file at .evo-agent/agents/<name>.md to define one.\n" +
			"See docs/CUSTOM_AGENTS.md for the frontmatter format."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Custom agents (%d loaded):\n", len(defs))
	for _, def := range defs {
		model := def.Model
		if model == "" {
			model = "inherit"
		}
		maxTurns := def.MaxTurns
		if maxTurns == 0 {
			maxTurns = 30
		}
		fmt.Fprintf(&b, "  - %s    [model=%s, max_turns=%d]\n", def.Name, model, maxTurns)
		fmt.Fprintf(&b, "      %s\n", def.Description)
	}
	b.WriteString("\nUsage: /agents show <name> | /agents reload")
	b.WriteString("\nInvoke from the model via: task({subagent_type: \"<name>\", prompt: \"...\"})")
	return b.String()
}

// formatAgentDetail renders one agent's full definition: frontmatter
// summary plus the system prompt body (truncated to keep terminal output
// reasonable). Used by `/agents show <name>`.
func formatAgentDetail(def agents.AgentDefinition) string {
	model := def.Model
	if model == "" {
		model = "inherit (parent's model)"
	}
	maxTurns := fmt.Sprintf("%d", def.MaxTurns)
	if def.MaxTurns == 0 {
		maxTurns = "30 (default)"
	}
	body := def.SystemPrompt
	const maxBody = 2000
	truncated := false
	if len(body) > maxBody {
		body = body[:maxBody]
		truncated = true
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Agent: %s\n", def.Name)
	fmt.Fprintf(&b, "Description: %s\n", def.Description)
	fmt.Fprintf(&b, "Model: %s\n", model)
	fmt.Fprintf(&b, "Max turns: %s\n", maxTurns)
	fmt.Fprintf(&b, "Path: %s\n", def.Path)
	b.WriteString("\n── System prompt ──\n")
	b.WriteString(body)
	if truncated {
		fmt.Fprintf(&b, "\n\n[... truncated, full content at %s]", def.Path)
	}
	return b.String()
}

// joinNames is a small helper to produce a sensible "Available: a, b, c"
// suffix in error messages. Returns "(none)" for an empty input so the
// user sees an actionable message instead of a dangling colon.
func joinNames(names []string) string {
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}
