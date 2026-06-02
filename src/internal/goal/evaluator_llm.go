package goal

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"

	"evo-agent/internal/llm"
)

// evaluatorMaxTokens caps the size of the evaluator response. The verdict is
// just one short JSON line, so 256 is plenty.
const evaluatorMaxTokens = 256

// RunEvaluator invokes the LLM as a yes/no judge for the active goal.
//
// Inputs:
//   - ctx: cancellation
//   - provider: the same llm.Provider the agent loop uses (caller passes
//     it in to avoid creating per-call providers). May be either the
//     Anthropic or OpenAI adapter — the MessageNewParams shape is
//     canonical across both.
//   - modelID: which model to ask (caller decides; today main loop reuses
//     cfg.ModelID)
//   - goalText: the user-supplied condition
//   - msgs: the agent's recent conversation, used to build the transcript
//
// Returns the parsed Verdict. Network / parse failures are folded into a
// "Met:false, Reason:<diagnostic>" verdict so the caller never sees an
// error and can treat the response uniformly. Falling back to "not met" is
// the safe choice — we'd rather keep working than falsely declare success.
func RunEvaluator(
	ctx context.Context,
	provider llm.Provider,
	modelID, goalText string,
	msgs []anthropic.MessageParam,
) Verdict {
	system, user := BuildEvalRequest(goalText, msgs, EvalRecentTurns)

	resp, err := provider.SendMessage(ctx, anthropic.MessageNewParams{
		Model: anthropic.Model(modelID),
		System: []anthropic.TextBlockParam{
			{Text: system},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(user)),
		},
		MaxTokens: evaluatorMaxTokens,
	})
	if err != nil {
		return Verdict{
			Met:    false,
			Reason: fmt.Sprintf("evaluator transport error: %v", err),
		}
	}

	// Concatenate all text blocks from the response. Evaluator should not
	// emit tool calls; if it does we'll just ignore them.
	var raw string
	for _, blk := range resp.Content {
		if blk.Type == "text" {
			raw += blk.Text
		}
	}
	if raw == "" {
		return Verdict{Met: false, Reason: "evaluator produced no text"}
	}
	return ParseVerdict(raw)
}
