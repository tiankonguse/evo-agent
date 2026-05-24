package main

import (
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"evo-agent/internal/agent"
	"evo-agent/internal/config"
	"evo-agent/internal/skills"
	"evo-agent/internal/tools"
	"evo-agent/internal/tui"
	"evo-agent/internal/ui"
)

const (
	agentName    = "evo-agent"
	agentVersion = "0.9.0"
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

	tools.InitMCP()
	defer tools.ShutdownMCP()

	// Load skills and inject catalog into system prompt
	skills.Init()
	if catalog := skills.Catalog(); catalog != "" {
		cfg.SystemMsg += "\nSkills available:\n" + catalog +
			"\nUse load_skill when a task needs specialized instructions before you act."
	}

	a := agent.New(&client, cfg)

	if *plain {
		// Plain-text REPL (original behaviour) — TerminalSink is the default.
		tools.PrintToolList()
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

	info := tui.SidebarInfo{
		AgentName:    agentName,
		Version:      agentVersion,
		ProjectDir:   cfg.ProjectDir,
		Model:        cfg.ModelID,
		Provider:     provider,
		ContextLimit: contextLimit,
		Skills:       skillNames,
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
			doneCh := make(chan struct{})
			a.RunQuery(query, &history, &compactState, doneCh)
			<-doneCh
			ui.PrintDone()
		}
	}()

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
