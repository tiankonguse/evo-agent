package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"evo-agent/internal/tools"
	"evo-agent/internal/ui"
)

// teamcmd.go — client-side `/team` command. Mirrors bgtaskcmd.go: pure
// status / state-mutation, never drives an LLM turn. Talks to
// tools.GlobalTeam and emits ui.PrintSystem lines.
//
// Forms:
//
//   /team                       → list every teammate (alias of `list`)
//   /team list                  → list every teammate
//   /team shutdown <name>       → ask GlobalTeam to terminate one teammate
//   /team inbox <name>          → drain and pretty-print one teammate's inbox
//                                  (debug helper; rarely useful in normal flow)

// TeamCmdAction enumerates the parsed user intent.
type TeamCmdAction int

const (
	TeamCmdNotMatched TeamCmdAction = iota
	TeamCmdList                     // "/team" or "/team list"
	TeamCmdShutdown                 // "/team shutdown <name>"
	TeamCmdInbox                    // "/team inbox <name>"
)

// ParseTeamCmd inspects a user query and returns (action, arg). `arg` is
// the teammate name for Shutdown / Inbox; empty for List. Returns
// NotMatched for anything that isn't a /team invocation so the caller can
// fall through.
func ParseTeamCmd(query string) (action TeamCmdAction, arg string) {
	q := strings.TrimSpace(query)
	if q == "/team" {
		return TeamCmdList, ""
	}
	const prefix = "/team "
	if !strings.HasPrefix(q, prefix) {
		return TeamCmdNotMatched, ""
	}
	rest := strings.TrimSpace(q[len(prefix):])
	if rest == "" || rest == "list" {
		return TeamCmdList, ""
	}
	if strings.HasPrefix(rest, "shutdown ") {
		name := strings.TrimSpace(strings.TrimPrefix(rest, "shutdown"))
		if name == "" {
			return TeamCmdNotMatched, ""
		}
		return TeamCmdShutdown, name
	}
	if strings.HasPrefix(rest, "inbox ") {
		name := strings.TrimSpace(strings.TrimPrefix(rest, "inbox"))
		if name == "" {
			return TeamCmdNotMatched, ""
		}
		return TeamCmdInbox, name
	}
	return TeamCmdNotMatched, ""
}

// handleTeamCmd executes a parsed /team command and prints the result.
// It never blocks on the LLM and never mutates message history.
func (r *Repl) handleTeamCmd(action TeamCmdAction, arg string) {
	switch action {
	case TeamCmdList:
		ui.PrintSystem(tools.GlobalTeam.List())
	case TeamCmdShutdown:
		out, err := tools.GlobalTeam.Shutdown(arg)
		if err != nil {
			ui.PrintError(fmt.Sprintf("/team shutdown: %v", err))
			return
		}
		ui.PrintSystem(out)
	case TeamCmdInbox:
		msgs, err := tools.GlobalTeam.ReadInbox(arg)
		if err != nil {
			ui.PrintError(fmt.Sprintf("/team inbox: %v", err))
			return
		}
		if len(msgs) == 0 {
			ui.PrintSystem(fmt.Sprintf("Inbox for %s: (empty)", arg))
			return
		}
		body, _ := json.MarshalIndent(msgs, "", "  ")
		ui.PrintSystem(fmt.Sprintf("Inbox for %s:\n%s", arg, string(body)))
	}
}
