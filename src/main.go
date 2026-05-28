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

	a := agent.New(&client, cfg, builder)

	tools.PrintToolList()
	if *plain {
		// Plain-text REPL (original behaviour) — TerminalSink is the default.
		a.Run(os.Stdin)
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

	// Channel for user queries (TUI → agent goroutine)
	queryCh := make(chan string, 4)

	// Agent goroutine: processes one query at a time
	go func() {
		var history []anthropic.MessageParam
		compactState := &agent.CompactState{}
		for query := range queryCh {
			// ── Client-side commands (not sent to LLM) ──
			if query == "/dump-prompts" {
				on := a.ToggleDumpPrompts()
				if on {
					ui.PrintSystem("[dump-prompts: ON]")
				} else {
					ui.PrintSystem("[dump-prompts: OFF]")
				}
				ui.PrintDone()
				continue
			}

			// ── Slash command interception ──
			if result := skills.Dispatch(query); result.Found {
				if result.Content != "" {
					// Two-block message: prompt + skill content
					history = append(history, anthropic.NewUserMessage(
						anthropic.NewTextBlock(result.Prompt),
						anthropic.NewTextBlock(result.Content),
					))
				} else {
					// Error case (unknown command): single block
					history = append(history, anthropic.NewUserMessage(
						anthropic.NewTextBlock(result.Prompt),
					))
				}
				doneCh := make(chan struct{})
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
