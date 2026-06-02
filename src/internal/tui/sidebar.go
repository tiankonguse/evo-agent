package tui

import (
	"strings"
)

// SidebarInfo holds metadata displayed in the status bar.
type SidebarInfo struct {
	AgentName    string
	Version      string
	ProjectDir   string
	Model        string
	Provider     string // freeform label (BaseURL or backend name); shown in sidebar
	ProviderID   string // canonical protocol id ("anthropic" | "openai"); shown in status bar
	SessionID    string // active session id (for status + /resume hint)
	InputTokens  int64
	OutputTokens int64
	ContextLimit int64

	Skills     []string
	Commands   []string // command names (from .evo-agent/command/)
	Tools      []string
	MCPServers []string // MCP server names only

	Goal string // active /goal condition (truncated by sidebar renderer); "" when no goal
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n <= 3 {
		return string(runes[:n])
	}
	return string(runes[:n-1]) + "…"
}

func shortenPath(p string, n int) string {
	if len(p) <= n {
		return p
	}
	parts := strings.Split(p, "/")
	// try to fit last 2 segments
	short := "…/" + strings.Join(parts[len(parts)-2:], "/")
	if len(short) <= n {
		return short
	}
	return truncate(p, n)
}
