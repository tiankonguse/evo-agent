package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/anthropics/anthropic-sdk-go"

	"evo-agent/internal/agent"
	"evo-agent/internal/config"
	"evo-agent/internal/goal"
	"evo-agent/internal/llm"
	"evo-agent/internal/prompt"
	"evo-agent/internal/session"
	"evo-agent/internal/skills"
	"evo-agent/internal/tools"
	"evo-agent/internal/tui"
)

const (
	agentName    = "evo-agent"
	agentVersion = "0.18.0"
	contextLimit = 200000 // Claude's context window (approx)
)

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

	provider, err := llm.New(llm.Config{
		ProviderID:       cfg.ProviderID,
		ModelID:          cfg.ModelID,
		AnthropicAPIKey:  cfg.APIKey,
		AnthropicBaseURL: cfg.BaseURL,
		OpenAIAPIKey:     cfg.OpenAIAPIKey,
		OpenAIBaseURL:    cfg.OpenAIBaseURL,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: llm.New:", err)
		os.Exit(1)
	}

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

	// Wire scheduled-tasks (cron) guidance — tells the model how to map
	// natural language ("remind me at 3pm") to cron expressions and which
	// tool to call (cron_create / cron_list / cron_delete). The tools are
	// already in the registry; this prompt section is what makes
	// natural-language CLI input route reliably to them.
	builder.SetCronGuidance(tools.CronGuidance)

	// Wire active /goal provider so the system prompt always reflects the
	// current condition.
	builder.SetGoalProvider(goal.Global)

	a := agent.New(provider, cfg, builder)

	// ── Session persistence: create or restore ──────────────────────────
	sess, err := session.NewSession(cfg.ProjectDir, agentVersion)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: session persistence disabled: %v\n", err)
	} else {
		a.AttachSession(sess)
		// Bind tools that need a session-scoped working dir (currently
		// just bgtask) to the live session. SetSessionContext fills the
		// gap left by the registry-style tool dispatch having no context
		// argument — mirrors tools.SetConversationMessages.
		tools.SetSessionContext(sess.Dir, sess.ID)
		if err := tools.GlobalBgTasks.Init(sess.Dir, sess.ID); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: bg tasks disabled: %v\n", err)
		}
	}

	// Cron scheduler: ALWAYS start, even when persistence is disabled.
	// Without a session dir, durable=true degrades to session-only
	// (saveDurable is a no-op when rootDir is empty), but the ticker
	// goroutine still fires session tasks — otherwise cron_create
	// would silently never fire.
	cronDir, cronID := "", ""
	if sess != nil {
		cronDir, cronID = sess.Dir, sess.ID
	}
	if err := tools.GlobalCron.Init(cronDir, cronID); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: scheduled tasks disabled: %v\n", err)
	}
	defer tools.GlobalCron.Stop()

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
		if res.Goal != nil {
			goal.Global.Set(res.Goal.Text, res.Goal.PlanName)
			fmt.Printf("[goal] restored: %s (plan=%s)\n", res.Goal.Text, res.Goal.PlanName)
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
		// Plain-text REPL — uses the unified Repl driver with a
		// TerminalFrontend over stdin. Existing TerminalSink (default
		// sink) renders assistant text, tool calls, and tool results
		// live as the loop runs, so no extra wiring is needed here.
		runPlain(a, initialHistory)
		return
	}

	// ── TUI mode ─────────────────────────────────────────────────────────────
	// ui.SetSink is called inside tui.Run after the Sink is created.

	// Determine provider label for the TUI sidebar from PROVIDER_ID,
	// then prefer an explicit base URL when one is set so users with a
	// gateway/proxy see the actual endpoint in the sidebar.
	providerLabel := "Anthropic"
	switch cfg.ProviderID {
	case "openai":
		providerLabel = "OpenAI"
		if cfg.OpenAIBaseURL != "" {
			providerLabel = cfg.OpenAIBaseURL
		}
	default:
		if cfg.BaseURL != "" {
			providerLabel = cfg.BaseURL
		}
	}

	// Canonical protocol id for the bottom status bar — empty / unset
	// is normalized to "anthropic" so the bar always carries an explicit
	// value and users can tell at a glance which protocol is live.
	providerID := cfg.ProviderID
	if providerID == "" {
		providerID = "anthropic"
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
		Provider:     providerLabel,
		ProviderID:   providerID,
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

	// Agent goroutine: drives the unified Repl with a ChannelFrontend
	// that reads queries from the TUI's textarea and emits ui.PrintDone
	// after each turn so the textarea unblocks.
	go func() {
		fe := agent.NewChannelFrontend(queryCh)
		repl := agent.NewRepl(a, fe, initialHistory)
		repl.Run()
	}()

	// Print padding lines so TUI View doesn't overwrite startup info above
	fmt.Print("\n\n\n\n\n")

	if err := tui.Run(info, queryCh); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}
}

// runPlain is the plain-mode REPL. Constructs a TerminalFrontend over
// stdin and drives the unified Repl. Initial history (from --resume) is
// echoed once, then the Repl owns the conversation forever.
func runPlain(a *agent.Agent, initialHistory []anthropic.MessageParam) {
	if len(initialHistory) > 0 {
		// Echo the resumed messages so the user sees what they're picking
		// up from before the prompt appears.
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
	}
	fe := agent.NewTerminalFrontend(os.Stdin)
	repl := agent.NewRepl(a, fe, initialHistory)
	repl.Run()
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
