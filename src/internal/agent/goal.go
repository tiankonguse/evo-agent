package agent

import (
	"context"

	"github.com/anthropics/anthropic-sdk-go"

	"evo-agent/internal/goal"
	"evo-agent/internal/tools"
	"evo-agent/internal/ui"
)

// maybeContinueForGoal is invoked by Loop() at the point where the model
// has produced a response with no tool calls — i.e. the natural end of a
// turn. When a /goal is active, we ask an evaluator LLM whether the
// condition is met. If not, we synthesize a continuation prompt and
// instruct the loop to keep going. If yes (or if no goal is active, or the
// iteration cap is hit), we return false so the loop exits normally.
//
// Return value:
//   - true  → caller should `continue` and run another LLM turn
//   - false → caller should return (turn complete)
//
// All side effects (UI events, recorder writes, message append, iter
// counter) happen here so loop.go stays compact.
func (a *Agent) maybeContinueForGoal(state *LoopState) bool {
	snap := goal.Global.Snapshot()
	if snap == nil {
		return false
	}

	// Hard cap: never let a misjudged condition burn unbounded turns.
	if snap.Iter >= snap.MaxIter {
		ui.PrintGoal("capped", snap.Text, "", snap.PlanName, snap.Iter, snap.MaxIter, snap.SetAt.UnixMilli())
		goal.Global.Clear()
		if state.Recorder != nil {
			state.Recorder.AppendGoalCleared(state.PromptID)
		}
		return false
	}

	// Run the evaluator. RunEvaluator folds transport / parse errors into a
	// "not met" verdict so we never falsely declare success.
	ui.PrintGoal("evaluating", snap.Text, "", snap.PlanName, snap.Iter, snap.MaxIter, snap.SetAt.UnixMilli())
	verdict := goal.RunEvaluator(context.Background(), a.provider, a.cfg.ModelID, snap.Text, state.Messages)

	if verdict.Met {
		goal.Global.Achieve()
		if state.Recorder != nil {
			state.Recorder.AppendGoalAchieved(state.PromptID, verdict.Reason)
		}
		ui.PrintGoal("achieved", snap.Text, verdict.Reason, snap.PlanName, snap.Iter, snap.MaxIter, snap.SetAt.UnixMilli())
		return false
	}

	// Goal not met — synthesize a reminder and continue. We embed the
	// persistent-plan summary so the model remembers the cross-session
	// context (the /goal command auto-creates one).
	planSummary := tools.GlobalPlan.StartupSummary()
	cont := goal.ContinuationPrompt(snap.Text, verdict.Reason, planSummary)

	msg := anthropic.NewUserMessage(anthropic.NewTextBlock(cont))
	state.Messages = append(state.Messages, msg)
	if state.Recorder != nil {
		state.Recorder.AppendUser(state.PromptID, msg)
	}
	state.TurnCount++
	state.TransitionReason = "goal_not_met"

	newIter := goal.Global.IncIter()
	ui.PrintGoal("continuing", snap.Text, verdict.Reason, snap.PlanName, newIter, snap.MaxIter, snap.SetAt.UnixMilli())
	return true
}
