package agent

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"evo-agent/internal/agents"
	"evo-agent/internal/tools"
)

// RunCustomSubagent runs a user-defined custom agent (loaded from
// .evo-agent/agents/<name>.md) with its OWN system prompt — i.e. the parent
// agent's prompt is NOT inherited. This matches Claude Code's behavior for
// custom agents and lets users define narrow specialists (code-reviewer,
// test-runner, planner-only) without inheriting the parent's full task /
// memory / plan / team guidance.
//
// The system prompt the child sees is:
//
//	<def.SystemPrompt>
//
//	# Environment
//	- Working directory: ...
//	- Is git repository: ...
//	- ...
//
// Notably absent: Agent.md, skills catalog, memories, plan status, team
// status, goal, the main agent's coding rules. If the user wants any of
// those they can put them directly in the agent's markdown body.
//
// Model and max-turn overrides come from the agent's frontmatter:
//   - model: <id>     — Anthropic model id; "" or "inherit" → parent's model
//   - max_turns: <N>  — caps LLM round-trips; 0 → 30 (default)
//
// Tool restrictions are deliberately NOT supported in this initial pass.
// The child receives the full tool set minus "task" (recursion guard),
// matching the generic subagent. Per-agent tool whitelists/blacklists can
// be added later by reading frontmatter and threading them into
// subagentRunConfig.Tools.
func (a *Agent) RunCustomSubagent(def agents.AgentDefinition, userPrompt string) string {
	// Build the system prompt: agent body first, then the environment
	// envelope. Order matters — putting the agent's instructions FIRST
	// means models that key on the start of the system prompt (cache hits,
	// behavioral anchoring) treat the custom prompt as primary, with the
	// environment as supplemental context.
	systemText := strings.TrimSpace(def.SystemPrompt) + "\n\n" + a.buildCustomAgentEnvelope()

	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt)),
	}

	return a.runSubagentLoop(subagentRunConfig{
		SystemText: systemText,
		AgentName:  def.Name,
		ModelID:    def.Model, // empty → runSubagentLoop falls back to a.cfg.ModelID
		MaxTurns:   def.MaxTurns,
		Messages:   messages,
		Tools:      tools.ToolsExcept("task"),
	})
}

// buildCustomAgentEnvelope returns the minimal environment block appended
// to a custom agent's system prompt. Mirrors the layout of
// prompt.Builder.buildEnvironment but lives here to avoid the agent
// package depending on prompt internals.
//
// Anything NOT in this envelope is invisible to the custom agent — that's
// the whole point of "custom" vs "generic" subagent.
func (a *Agent) buildCustomAgentEnvelope() string {
	items := []string{
		fmt.Sprintf("Working directory: %s", a.cfg.ProjectDir),
		fmt.Sprintf("Is git repository: %s", detectGit(a.cfg.ProjectDir)),
		fmt.Sprintf("Platform: %s/%s", runtime.GOOS, runtime.GOARCH),
		fmt.Sprintf("Shell: %s", detectShell()),
		fmt.Sprintf("Current date: %s", time.Now().Format("2006-01-02")),
		fmt.Sprintf("You are powered by the model %s", a.cfg.ModelID),
	}
	hostname, _ := os.Hostname()
	if hostname != "" {
		items = append(items, fmt.Sprintf("Host: %s", hostname))
	}
	var b strings.Builder
	b.WriteString("# Environment\n")
	for _, item := range items {
		b.WriteString(" - ")
		b.WriteString(item)
		b.WriteString("\n")
	}
	return b.String()
}

func detectGit(projectDir string) string {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = projectDir
	out, err := cmd.Output()
	if err != nil {
		return "No"
	}
	if strings.TrimSpace(string(out)) == "true" {
		return "Yes"
	}
	return "No"
}

func detectShell() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return "unknown"
	}
	parts := strings.Split(shell, "/")
	return parts[len(parts)-1]
}
