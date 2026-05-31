package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"evo-agent/internal/agent"
	"evo-agent/internal/config"
	"evo-agent/internal/prompt"
	"evo-agent/internal/session"
	"evo-agent/internal/skills"
	"evo-agent/internal/tools"
	"evo-agent/internal/tui"
	"evo-agent/internal/ui"
)

const (
	agentName    = "evo-agent"
	agentVersion = "0.13.0"
	contextLimit = 200000 // Claude's context window (approx)
)

func BuildOptions(cfg *config.Config) []option.RequestOption {
	opts := []option.RequestOption{}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
		os.Unsetenv("ANTHROPIC_AUTH_TOKEN")
	}
	if cfg.APIKey != "" {
		opts = append(opts, option.WithAPIKey(cfg.APIKey))
	} else {
		opts = append(opts, option.WithAPIKey("dummy"))
	}
	return opts
}

func main() {
	plain := flag.Bool("plain", false, "disable TUI, use plain text output")
	resumeID := flag.String("resume", "", "resume an existing session by id (see ~/.evo-agent/sessions/)")
	flag.Parse()

	config.LoadEnv()
	cfg := config.Load()

	if cfg.ModelID == "" {
		fmt.Fprintln(os.Stderr, "Error: MODEL_ID not set in environment")
		os.Exit(1)
	}

	opts := BuildOptions(cfg)
	client := anthropic.NewClient(opts...)

	fmt.Printf("[%s] v%s | model=%s\n", agentName, agentVersion, cfg.ModelID)

	tools.InitMCP()
	defer tools.ShutdownMCP()

	// Load persistent memories
	tools.GlobalMemory.Init(cfg.ProjectDir)

	// Initialize session plan/task system
	tools.InitPlan(cfg.ProjectDir)

	// Print active session plan status at startup
	if summary := tools.GlobalPlan.StartupSummary(); summary != "" {
		fmt.Println(summary)
	}

	// Load skills and commands
	skills.Init()

	// Build system prompt via the prompt builder
	builder := prompt.NewBuilder(cfg, tools.GlobalMemory, skills.Provider{})

	// Load Agent.md into builder (if present in project root)
	if agentMd, err := os.ReadFile(filepath.Join(cfg.ProjectDir, "Agent.md")); err == nil {
		builder.SetAgentMd(string(agentMd))
	}

	// Set memory guidance
	builder.SetMemoryGuidance(tools.MemoryGuidance)

	// Set session plan guidance and provider
	builder.SetPlanGuidance(tools.PlanGuidance)
	builder.SetPlanProvider(tools.GlobalPlan)

	a := agent.New(&client, cfg, builder)

	// ── Session persistence: create or restore ──────────────────────────
	sess, err := session.NewSession(cfg.ProjectDir, agentVersion)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: session persistence disabled: %v\n", err)
	} else {
		a.AttachSession(sess)
	}

	var initialHistory []anthropic.MessageParam
	if *resumeID != "" {
		res, err := session.LoadForResume(cfg.ProjectDir, *resumeID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: --resume %s: %v\n", *resumeID, err)
			os.Exit(1)
		}
		initialHistory = res.Messages
		if sess != nil {
			sess.Recorder.AppendResumeMarker(res.SourceID, res.RestoredCount)
		}
		fmt.Printf("[resume] restored %d messages from session %s", res.RestoredCount, res.SourceID)
		if res.HasCompactedAt {
			fmt.Print(" (with compact summary)")
		}
		fmt.Println()
	}

	// Print session id immediately so the user knows their resume target.
	if sess != nil {
		fmt.Printf("[session] id=%s | dir=%s\n", sess.ID, sess.Dir)
		// Always print the resume hint on graceful exit. Defer covers TUI's
		// normal Quit path and plain mode's loop break.
		defer func() {
			fmt.Printf("\nResume this session with: evo-agent --resume %s\n", sess.ID)
		}()
	}

	tools.PrintToolList()
	if *plain {
		// Plain-text REPL (original behaviour) — TerminalSink is the default.
		runPlain(a, sess, initialHistory)
		return
	}

	// ── TUI mode ─────────────────────────────────────────────────────────────
	// ui.SetSink is called inside tui.Run after the Sink is created.

	// Determine provider from base URL or default
	provider := "Anthropic"
	if cfg.BaseURL != "" {
		provider = cfg.BaseURL
	}

	// Collect tool names
	toolNames := builtinToolNames()
	mcpServers := mcpServerNames()
	skillNames := skillList()
	commandNames := skills.CommandNames()

	info := tui.SidebarInfo{
		AgentName:    agentName,
		Version:      agentVersion,
		ProjectDir:   cfg.ProjectDir,
		Model:        cfg.ModelID,
		Provider:     provider,
		ContextLimit: contextLimit,
		Skills:       skillNames,
		Commands:     commandNames,
		Tools:        toolNames,
		MCPServers:   mcpServers,
	}
	if sess != nil {
		info.SessionID = sess.ID
	}

	// Channel for user queries (TUI → agent goroutine)
	queryCh := make(chan string, 4)

	// Agent goroutine: processes one query at a time
	go func() {
		history := initialHistory
		compactState := &agent.CompactState{}
		for query := range queryCh {
			// ── Client-side commands (not sent to LLM) ──
			if query == "/dump-prompts" {
				a.DumpNow(history)
				ui.PrintSystem("[dump-prompts: dumped current state]")
				ui.PrintDone()
				continue
			}

			// ── /resume <id> client-side restore ──
			if rid, ok := parseResumeArg(query); ok {
				if rid == "" {
					ui.PrintSystem("Usage: /resume <session-id>. In the TUI, type /resume and pick from the dropdown.")
					ui.PrintDone()
					continue
				}
				res, err := session.LoadForResume(cfg.ProjectDir, rid)
				if err != nil {
					ui.PrintError(fmt.Sprintf("/resume failed: %v", err))
					ui.PrintDone()
					continue
				}
				history = res.Messages
				if sess != nil {
					sess.Recorder.AppendResumeMarker(res.SourceID, res.RestoredCount)
				}
				ui.PrintSystem(fmt.Sprintf("✓ Restored %d messages from session %s", res.RestoredCount, res.SourceID))
				ui.PrintDone()
				continue
			}

			// ── Slash command interception ──
			if result := skills.Dispatch(query); result.Found {
				promptID := session.NewPromptID()
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
				history = append(history, newMsg)
				if sess != nil {
					sess.Recorder.AppendUser(promptID, newMsg)
				}
				doneCh := make(chan struct{})
				// Use RunQueryDirect — history already updated, recorder is
				// still picked up via a.session inside RunQueryDirect.
				a.RunQueryDirect(&history, &compactState, doneCh)
				<-doneCh
				ui.PrintDone()
				continue
			}
			doneCh := make(chan struct{})
			a.RunQuery(query, &history, &compactState, doneCh)
			<-doneCh
			ui.PrintDone()
		}
	}()

	// Print padding lines so TUI View doesn't overwrite startup info above
	fmt.Print("\n\n\n\n\n")

	if err := tui.Run(info, queryCh); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}
}

// runPlain is the plain-mode REPL with session persistence wired in.
// It mirrors agent.Agent.Run but seeds initial history and intercepts
// /resume <id>.
func runPlain(a *agent.Agent, sess *session.Session, initialHistory []anthropic.MessageParam) {
	// agent.Run already handles session persistence via a.session, but it
	// constructs its own empty history. Seed history via Run when no resume
	// is needed; otherwise drive a small loop here.
	if len(initialHistory) == 0 {
		a.Run(os.Stdin)
		return
	}
	// Echo the resumed messages so the user sees them.
	for _, m := range initialHistory {
		role := "user"
		if m.Role == anthropic.MessageParamRoleAssistant {
			role = "assistant"
		}
		for _, blk := range m.Content {
			if blk.OfText != nil && blk.OfText.Text != "" {
				fmt.Printf("[%s] %s\n", role, blk.OfText.Text)
			}
		}
	}
	fmt.Println("[resume complete — continue the conversation below]")
	a.Run(os.Stdin)
}

// parseResumeArg returns the argument of a "/resume" command if present.
// Returns (id, true) for "/resume <id>" or "/resume" alone (id=""), and
// (_, false) for anything else.
func parseResumeArg(query string) (string, bool) {
	if query == "/resume" {
		return "", true
	}
	const prefix = "/resume "
	if len(query) > len(prefix) && query[:len(prefix)] == prefix {
		return trimSpace(query[len(prefix):]), true
	}
	return "", false
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n') {
		s = s[:len(s)-1]
	}
	return s
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func builtinToolNames() []string {
	all := tools.RegistryNames()
	sort.Strings(all)
	return all
}

func mcpServerNames() []string {
	all := tools.MCPServerNames()
	sort.Strings(all)
	return all
}

func skillList() []string {
	names := skills.Names()
	sort.Strings(names)
	return names
}
