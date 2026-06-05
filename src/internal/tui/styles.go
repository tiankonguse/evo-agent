package tui

import "charm.land/lipgloss/v2"

// Layout constants
const (
	defaultResultRows = 10 // lines shown before truncating a tool-result block
)

var (
	// ── Thinking block ────────────────────────────────────────────────────────
	thinkingHeaderStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#2d1b69")).
				Foreground(lipgloss.Color("#a78bfa")).
				Bold(true).
				PaddingLeft(1).
				PaddingRight(1)

	thinkingBodyStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#1e1244")).
				Foreground(lipgloss.Color("#c8b8ff")).
				PaddingLeft(1).
				PaddingRight(1)

	// ── Tool result block ─────────────────────────────────────────────────────
	resultBodyStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#0d1117")).
			Foreground(lipgloss.Color("#a5d6ff")).
			PaddingLeft(1).
			PaddingRight(1)

	// ── Tool call line ────────────────────────────────────────────────────────
	toolSuccessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#3fb950")) // ✓
	toolErrorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#f85149")) // ✗
	toolPendingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#d29922")) // ●
	toolNameStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#58a6ff")).Bold(true)
	toolArgsStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#8b949e"))
	toolMoreStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#484f58")).Italic(true)

	// ── User message block ────────────────────────────────────────────────────
	userStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#79c0ff")).
			Bold(true).
			PaddingLeft(1)

	// ── Text block ────────────────────────────────────────────────────────────
	textStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#e6edf3")).
			PaddingLeft(1)

	// ── System / debug ────────────────────────────────────────────────────────
	systemStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#484f58")).
			Italic(true).
			PaddingLeft(1)

	// ── Turn elapsed time bar ─────────────────────────────────────────────────
	elapsedStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#1c2128")).
			Foreground(lipgloss.Color("#d2a679")).
			Bold(true).
			PaddingLeft(1).
			PaddingRight(1)

	// ── Input bar ─────────────────────────────────────────────────────────────
	inputPromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#3fb950")).Bold(true)
	inputBusyStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#d29922")).Bold(true)

	// ── Help / status bar ─────────────────────────────────────────────────────
	helpBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#161b22")).
			Foreground(lipgloss.Color("#8b949e")).
			PaddingLeft(1)

	// ── Status bar (bottom) ───────────────────────────────────────────────────
	statusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#161b22")).
			Foreground(lipgloss.Color("#8b949e")).
			PaddingLeft(1)

	statusLabelStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#161b22")).
				Foreground(lipgloss.Color("#6e7681"))

	statusValueStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#161b22")).
				Foreground(lipgloss.Color("#58a6ff"))

	// ── Todo panel ────────────────────────────────────────────────────────────
	todoBorderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#30363d")).
			PaddingLeft(1).
			PaddingRight(1)

	todoHeaderStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#58a6ff")).
			Bold(true)

	todoPendingStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#8b949e"))
	todoInProgressStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#d29922")).Bold(true)
	todoCompletedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#3fb950"))
	todoActiveFormStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#6e7681")).Italic(true)

	// ── Plan panel ───────────────────────────────────────────────────────────
	planBorderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#1f6feb")).
			PaddingLeft(1).
			PaddingRight(1)

	planHeaderStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#1f6feb")).
			Bold(true)

	// ── Team panel ───────────────────────────────────────────────────────────
	// Magenta/purple to differentiate from todo (subtle gray) and plan (blue).
	teamBorderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#bc8cff")).
			PaddingLeft(1).
			PaddingRight(1)

	teamHeaderStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#bc8cff")).
			Bold(true)

	teamWorkingStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#d29922")).Bold(true)
	teamIdleStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#3fb950"))
	teamShutdownStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#6e7681")).Italic(true)
	teamMetaStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#6e7681"))

	// ── Goal indicator ───────────────────────────────────────────────────────
	// One-line surface in the live bottom area showing /goal active state.
	// Yellow/amber matches Claude Code's `◎ /goal active` colour family.
	goalIndicatorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#d29922")).
				Bold(true)

	// ── Completion dropdown ──────────────────────────────────────────────────
	completionBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#58a6ff")).
				PaddingLeft(0).
				PaddingRight(0)

	completionSelectedStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#1f6feb")).
				Foreground(lipgloss.Color("#ffffff")).
				Bold(true)

	completionItemStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#c9d1d9"))
)
