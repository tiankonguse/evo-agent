// Package agent — team.go
//
// Glue between tools.GlobalTeam (which owns the teammate goroutines) and
// the LLM provider. tools/team.go holds the outer "wake → drain inbox →
// run a tool-use burst → idle" state machine; this file supplies the one
// missing piece: a single LLM round-trip that respects the same provider
// + filtering rules the lead and subagent already use.
//
// Registered once by Agent.New() so the cycle-breaking callback in
// tools/team.go (RegisterTeammateRunner) sees a live runner before any
// team_spawn call can fire.

package agent

import (
	"context"

	"github.com/anthropics/anthropic-sdk-go"

	"evo-agent/internal/tools"
)

// runTeammateTurn performs ONE LLM call for a teammate goroutine.
//
// It mirrors agent.Loop / RunSubagent in the way it builds the request:
//   - System prompt is the teammate-specific prompt that tools/team.go
//     constructs on first spawn (role + name + lead-only-tools rules).
//     It is NOT prefixed with the lead's prompt.Builder output — teammates
//     deliberately get a leaner prompt so they don't inherit the lead's
//     project-wide guidance every turn.
//   - Messages are filtered through FilterThinking so the model never sees
//     its own prior thinking blocks (consistent with Loop / Subagent).
//   - MaxTokens matches the loop's 8000.
//
// The caller (tools/team.go) owns history persistence, tool dispatch, and
// the working↔idle transitions; we just translate
// (system + messages + tools) → *anthropic.Message.
func (a *Agent) runTeammateTurn(ctx context.Context, systemPrompt string, messages []anthropic.MessageParam, childTools []anthropic.ToolUnionParam) (*anthropic.Message, error) {
	return a.provider.SendMessage(ctx, anthropic.MessageNewParams{
		Model: anthropic.Model(a.cfg.ModelID),
		System: []anthropic.TextBlockParam{
			{Text: systemPrompt},
		},
		Messages:  FilterThinking(messages),
		Tools:     childTools,
		MaxTokens: 8000,
	})
}

// init-time registration is impossible (a is per-instance), so the
// runner is registered inside (a *Agent) New(). See agent.go.
//
// This compile-time reference keeps the import on tools alive even when
// agent/team.go is the only consumer:
var _ tools.TeammateRunner = (&Agent{}).runTeammateTurn
