// Package agent — repl.go
//
// Repl is the unified outer REPL that drives the agent loop for both TUI
// and plain-text modes. It absorbs the duplicated dispatch logic that used
// to live both in `agent.Run(io.Reader)` and the `main.go` TUI goroutine:
// reading the next user query, peeling off client-side commands
// (`/dump-prompts`, `/resume`, `/goal`), routing slash commands through
// `skills.Dispatch`, and finally calling `Agent.RunQuery` to drive a turn.
//
// Repl is parameterized by a Frontend that owns the input source. The two
// production frontends are:
//
//   - TerminalFrontend (repl_terminal.go) — bufio.Scanner over stdin,
//     used by `--plain` mode. Prints the "  >> " prompt and recognises
//     EOF / "q" / "exit" as shutdown.
//   - ChannelFrontend (also defined here) — wraps a `<-chan string`,
//     used by the TUI. The Bubble Tea program feeds queries on the
//     channel and the closed channel signals shutdown.
//
// All output (assistant text, tool calls, tool results, /goal lifecycle,
// system messages) flows through the existing ui.EventSink so frontends
// don't need any rendering logic.
package agent

import (
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"evo-agent/internal/goal"
	"evo-agent/internal/session"
	"evo-agent/internal/skills"
	"evo-agent/internal/ui"
)

// Frontend abstracts the input-reading side of the REPL so the same
// dispatch logic can be driven from a terminal scanner or a TUI channel.
//
// Implementations are expected to be single-consumer (Repl.Run is the
// only caller of NextQuery/OnTurnDone in normal use).
type Frontend interface {
	// NextQuery blocks until the user supplies the next input, or returns
	// (_, false) when the input source is exhausted (stdin EOF, channel
	// closed). The empty-string / "q" / "exit" exits are detected by
	// Repl.Run, not the frontend.
	NextQuery() (query string, ok bool)

	// OnTurnDone is called after each REPL iteration completes, regardless
	// of whether the iteration ran an LLM turn (it also fires after pure
	// status mutations like /goal, /dump-prompts). The TUI uses this to
	// emit ui.PrintDone() so the textarea unblocks; the terminal frontend
	// uses it as a no-op (TerminalSink already streams output live).
	OnTurnDone()
}

// Repl owns the turn-by-turn state of one REPL session: the message
// history, the compaction state, and the frontend it reads from. Construct
// once at startup; call Run() to block until the input source is closed.
type Repl struct {
	agent    *Agent
	frontend Frontend

	history []anthropic.MessageParam
	compact *CompactState
}

// NewRepl wires a Repl up. `initialHistory` is typically empty for a
// fresh session and pre-populated when `--resume <id>` was passed at
// startup. The compaction state always starts fresh — compaction memory
// is intentionally not carried across the resume boundary.
func NewRepl(a *Agent, fe Frontend, initialHistory []anthropic.MessageParam) *Repl {
	return &Repl{
		agent:    a,
		frontend: fe,
		history:  initialHistory,
		compact:  &CompactState{},
	}
}

// Run is the main loop. It blocks until the frontend signals exhaustion
// (NextQuery returns ok=false) or the user types one of the sentinel exit
// strings. Each iteration handles exactly one user input.
func (r *Repl) Run() {
	for {
		query, ok := r.frontend.NextQuery()
		if !ok {
			return
		}
		query = strings.TrimSpace(query)
		if query == "" || query == "q" || query == "exit" {
			return
		}
		r.handleTurn(query)
		r.frontend.OnTurnDone()
	}
}

// handleTurn dispatches one query through the same pipeline both modes
// used to duplicate inline. Order matters: client-side commands that just
// mutate state (/dump-prompts, /resume, /goal status & clear) are checked
// before any LLM-driving path so they never accidentally hit the model.
func (r *Repl) handleTurn(query string) {
	// /dump-prompts — pure debug helper, no LLM call.
	if query == "/dump-prompts" {
		r.agent.DumpNow(r.history)
		ui.PrintSystem("[dump-prompts: dumped current state]")
		return
	}

	// /resume <id> — client-side session restore. /resume with no arg in
	// plain mode falls through to a usage hint; the TUI variant has its
	// own picker that submits "/resume <id>" once the user selects.
	if rid, ok := parseResumeArg(query); ok {
		r.handleResume(rid)
		return
	}

	// /goal <text>|clear|<no-arg> — session-scoped completion condition.
	if act, text := ParseGoalCmd(query); act != GoalCmdNotMatched {
		startup, kickoff := r.agent.HandleGoalCmd(act, text, &r.history)
		if !startup {
			return
		}
		r.runOneTurn(kickoff)
		return
	}

	// /<skill-name> — slash dispatch into the skills/commands registry.
	if result := skills.Dispatch(query); result.Found {
		var newMsg anthropic.MessageParam
		if result.Content != "" {
			newMsg = anthropic.NewUserMessage(
				anthropic.NewTextBlock(result.Prompt),
				anthropic.NewTextBlock(result.Content),
			)
		} else {
			newMsg = anthropic.NewUserMessage(
				anthropic.NewTextBlock(result.Prompt),
			)
		}
		r.runOneTurn(newMsg)
		return
	}

	// Plain user text — wrap as a single-block user message and run.
	r.runOneTurn(anthropic.NewUserMessage(anthropic.NewTextBlock(query)))
}

// runOneTurn appends the message, records it under a fresh promptID, then
// drives the loop. The promptID is shared between the user record and the
// loop's assistant/tool_result records so a single turn is grouped under
// one parent in the session transcript.
func (r *Repl) runOneTurn(msg anthropic.MessageParam) {
	promptID := session.NewPromptID()
	r.history = append(r.history, msg)
	if sess := r.agent.Session(); sess != nil {
		sess.Recorder.AppendUser(promptID, msg)
	}
	doneCh := make(chan struct{})
	r.agent.RunQuery(promptID, &r.history, &r.compact, doneCh)
	<-doneCh
}

// handleResume implements the `/resume <id>` and `/resume` (no-arg)
// client-side commands. Replaces the current history with the loaded
// session's, writes a resume_marker into the active session transcript,
// and rehydrates any active /goal that was set in the source session.
func (r *Repl) handleResume(rid string) {
	if rid == "" {
		ui.PrintSystem("Usage: /resume <session-id>. In the TUI, type /resume and pick from the dropdown.")
		return
	}
	sess := r.agent.Session()
	cfg := r.agent.cfg
	if cfg == nil {
		ui.PrintError("/resume failed: agent has no config")
		return
	}
	res, err := session.LoadForResume(cfg.ProjectDir, rid)
	if err != nil {
		ui.PrintError("/resume failed: " + err.Error())
		return
	}
	r.history = res.Messages
	if sess != nil {
		sess.Recorder.AppendResumeMarker(res.SourceID, res.RestoredCount)
	}
	if res.Goal != nil {
		// Reactivate the goal in the singleton; iter counter resets per
		// the /goal docs. main.go also does this on the --resume CLI
		// flag path; here we cover the in-session "/resume <id>" path.
		st := goal.Global.Set(res.Goal.Text, res.Goal.PlanName)
		ui.PrintGoal("set", st.Text, "", st.PlanName, st.Iter, st.MaxIter, st.SetAt.UnixMilli())
	}
	ui.PrintSystem(formatResumeOk(res.RestoredCount, res.SourceID))
}

// ChannelFrontend is the TUI-facing Frontend: it reads queries from a
// channel that the Bubble Tea program writes user input into, and emits
// ui.EvDone after every turn so the TUI can unblock its textarea.
//
// A closed channel signals shutdown (NextQuery returns ok=false).
type ChannelFrontend struct {
	ch <-chan string
}

// NewChannelFrontend wraps a query channel.
func NewChannelFrontend(ch <-chan string) *ChannelFrontend {
	return &ChannelFrontend{ch: ch}
}

func (f *ChannelFrontend) NextQuery() (string, bool) {
	q, ok := <-f.ch
	return q, ok
}

func (f *ChannelFrontend) OnTurnDone() { ui.PrintDone() }

// parseResumeArg returns the argument of a "/resume" command if present.
// Returns (id, true) for "/resume <id>" or "/resume" alone (id=""), and
// (_, false) for anything else. Moved from main.go so both frontends can
// use the same parser.
func parseResumeArg(query string) (string, bool) {
	q := strings.TrimSpace(query)
	if q == "/resume" {
		return "", true
	}
	const prefix = "/resume "
	if strings.HasPrefix(q, prefix) {
		return strings.TrimSpace(q[len(prefix):]), true
	}
	return "", false
}

// formatResumeOk builds the success message for /resume — kept tiny and
// pure so it can be unit-tested without the rest of the loop.
func formatResumeOk(restored int, sourceID string) string {
	return "✓ Restored " + itoa(restored) + " messages from session " + sourceID
}

// itoa is a local stringifier so we don't pull in strconv just for one
// log line. Handles non-negative ints (the only case here).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
