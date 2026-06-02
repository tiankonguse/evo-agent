package agent

import (
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"evo-agent/internal/goal"
	"evo-agent/internal/session"
	"evo-agent/internal/tools"
	"evo-agent/internal/ui"
)

// GoalCmdAction is the parsed user intent of a /goal invocation.
type GoalCmdAction int

const (
	GoalCmdNotMatched GoalCmdAction = iota
	GoalCmdStatus                   // "/goal" alone
	GoalCmdClear                    // "/goal clear" (or stop|off|reset|cancel|none)
	GoalCmdSet                      // "/goal <text>"
)

// goalClearAliases lists the words that, when supplied as the only
// argument to /goal, cancel the active goal. Mirrors Claude Code docs.
var goalClearAliases = map[string]bool{
	"clear":  true,
	"stop":   true,
	"off":    true,
	"reset":  true,
	"cancel": true,
	"none":   true,
}

// ParseGoalCmd inspects a user query and reports whether it is a /goal
// invocation, plus the parsed action and any goal text.
//
// Usage:
//
//	/goal                  → (GoalCmdStatus, "")
//	/goal clear            → (GoalCmdClear,  "")
//	/goal <free-form text> → (GoalCmdSet,    "<text>")
//
// Returns GoalCmdNotMatched for non-/goal input so the caller can fall
// through to the normal slash-dispatch pipeline.
func ParseGoalCmd(query string) (action GoalCmdAction, text string) {
	q := strings.TrimSpace(query)
	if q == "/goal" {
		return GoalCmdStatus, ""
	}
	const prefix = "/goal "
	if !strings.HasPrefix(q, prefix) {
		return GoalCmdNotMatched, ""
	}
	rest := strings.TrimSpace(q[len(prefix):])
	if rest == "" {
		return GoalCmdStatus, ""
	}
	// Single-word clear aliases.
	if !strings.ContainsRune(rest, ' ') && goalClearAliases[strings.ToLower(rest)] {
		return GoalCmdClear, ""
	}
	return GoalCmdSet, rest
}

// HandleGoalCmd processes a /goal invocation. The caller (main.go for TUI
// mode, agent.Run() for plain mode) invokes this from the agent goroutine
// after slash interception but before passing the query to the LLM
// pipeline.
//
// For status / clear actions, this only mutates state and emits UI events
// — it does NOT drive an LLM turn (returns startup=false).
//
// For "set", it persists the goal, auto-creates a corresponding persistent
// plan under .evo-agent/tasks/todo/<name>/, and synthesizes an initial user message
// (the <goal-start> kickoff) that the caller is expected to feed into
// RunQuery to drive the first turn. Returns startup=true and the
// kickoff message in that case.
func (a *Agent) HandleGoalCmd(
	action GoalCmdAction,
	text string,
	history *[]anthropic.MessageParam,
) (startup bool, kickoff anthropic.MessageParam) {
	var sess *session.Session = a.session
	var rec *session.Recorder
	if sess != nil {
		rec = sess.Recorder
	}

	switch action {
	case GoalCmdStatus:
		snap := goal.Global.Snapshot()
		if snap == nil {
			ui.PrintGoal("status", "", "", "", 0, 0, 0)
		} else {
			ui.PrintGoal("status", snap.Text, "", snap.PlanName, snap.Iter, snap.MaxIter, snap.SetAt.UnixMilli())
		}
		return false, anthropic.MessageParam{}

	case GoalCmdClear:
		prev := goal.Global.Clear()
		if rec != nil {
			rec.AppendGoalCleared(session.NewPromptID())
		}
		if prev == nil {
			ui.PrintSystem("[/goal] no active goal to clear")
		} else {
			ui.PrintGoal("cleared", prev.Text, "", prev.PlanName, prev.Iter, prev.MaxIter, prev.SetAt.UnixMilli())
		}
		return false, anthropic.MessageParam{}

	case GoalCmdSet:
		// Auto-create a persistent plan so the work survives restarts.
		// Best-effort: a duplicate-name collision (rare — names are
		// date-prefixed + slugified) does NOT abort goal activation.
		planName := tools.DerivePlanName(text)
		if _, err := tools.GlobalPlan.CreateForGoal(planName, text, "Driven by /goal command"); err != nil {
			ui.PrintSystem(fmt.Sprintf("[/goal] plan create skipped: %v", err))
		}

		st := goal.Global.Set(text, planName)
		if rec != nil {
			rec.AppendGoalSet(session.NewPromptID(), text, planName)
		}
		ui.PrintGoal("set", st.Text, "", st.PlanName, st.Iter, st.MaxIter, st.SetAt.UnixMilli())

		// Build the kickoff user message that drives the first LLM turn.
		// The caller appends this to history and calls RunQuery.
		kickoffText := fmt.Sprintf(
			"<goal-start>\nA new /goal has been set.\n\nGoal: %s\n\n"+
				"A persistent plan has been created at .evo-agent/tasks/todo/%s/. "+
				"Use plan_task_create / plan_task_update to break this goal into tasks. "+
				"Begin work now — call tools as needed. After every turn that ends with no tool calls, "+
				"an evaluator will check whether the goal is met.\n</goal-start>",
			text, planName,
		)
		kickoff = anthropic.NewUserMessage(anthropic.NewTextBlock(kickoffText))
		return true, kickoff
	}

	// GoalCmdNotMatched should never reach here.
	return false, anthropic.MessageParam{}
}
