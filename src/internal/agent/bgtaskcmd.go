package agent

import (
	"fmt"
	"strings"

	"evo-agent/internal/tools"
	"evo-agent/internal/ui"
)

// bgtaskcmd.go — client-side `/bgtask` command. Like `/goal status`, this
// is a pure status / state-mutation command: it never drives an LLM turn,
// it just talks to tools.GlobalBgTasks and emits ui.PrintSystem lines.
//
// Forms:
//
//   /bgtask                  → list every task (running + archived)
//   /bgtask <id>             → show one task's full record (json)
//   /bgtask cancel <id>      → kill the task and archive as cancelled

// BgTaskCmdAction enumerates the parsed user intent.
type BgTaskCmdAction int

const (
	BgTaskCmdNotMatched BgTaskCmdAction = iota
	BgTaskCmdList                       // "/bgtask"
	BgTaskCmdShow                       // "/bgtask <id>"
	BgTaskCmdCancel                     // "/bgtask cancel <id>"
)

// ParseBgTaskCmd inspects a user query and returns (action, arg). `arg` is
// the task id for Show / Cancel; empty for List. Returns NotMatched for
// anything that isn't a /bgtask invocation so the caller can fall through.
func ParseBgTaskCmd(query string) (action BgTaskCmdAction, arg string) {
	q := strings.TrimSpace(query)
	if q == "/bgtask" {
		return BgTaskCmdList, ""
	}
	const prefix = "/bgtask "
	if !strings.HasPrefix(q, prefix) {
		return BgTaskCmdNotMatched, ""
	}
	rest := strings.TrimSpace(q[len(prefix):])
	if rest == "" {
		return BgTaskCmdList, ""
	}
	// "cancel <id>" form.
	if strings.HasPrefix(rest, "cancel ") {
		id := strings.TrimSpace(strings.TrimPrefix(rest, "cancel"))
		if id == "" {
			return BgTaskCmdNotMatched, ""
		}
		return BgTaskCmdCancel, id
	}
	// Single token = task id to show.
	if strings.ContainsRune(rest, ' ') {
		return BgTaskCmdNotMatched, ""
	}
	return BgTaskCmdShow, rest
}

// handleBgTaskCmd executes a parsed /bgtask command and prints the result.
// It never blocks on the LLM and never mutates message history.
func (r *Repl) handleBgTaskCmd(action BgTaskCmdAction, arg string) {
	switch action {
	case BgTaskCmdList:
		ui.PrintSystem(tools.GlobalBgTasks.Check(""))
	case BgTaskCmdShow:
		ui.PrintSystem(tools.GlobalBgTasks.Check(arg))
	case BgTaskCmdCancel:
		out, err := tools.GlobalBgTasks.Cancel(arg)
		if err != nil {
			ui.PrintError(fmt.Sprintf("/bgtask cancel: %v", err))
			return
		}
		ui.PrintSystem(out)
	}
}
