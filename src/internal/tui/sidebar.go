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
	Provider     string
	InputTokens  int64
	OutputTokens int64
	ContextLimit int64

	Skills     []string
	Commands   []string // command names (from .evo-agent/command/)
	Tools      []string
	MCPServers []string // MCP server names only
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
