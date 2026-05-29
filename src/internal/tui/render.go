package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/lipgloss/v2"

	"evo-agent/internal/ui"
)

// renderBlock renders a single block into a string.
// mainWidth is the width of the terminal.
func renderBlock(b Block, mainWidth int) string {
	w := mainWidth - 2
	if w < 10 {
		w = 10
	}

	switch b.Kind {
	case KindThinking:
		return renderThinking(b, w)

	case KindText:
		return textStyle.Width(w).Render(b.Content)

	case KindToolCall:
		return renderToolCall(b, w)

	case KindUser:
		return userStyle.Width(w).Render("You: " + b.Content)

	case KindSystem:
		return systemStyle.Width(w).Render(b.Content)
	}
	return ""
}

// renderThinking always shows full content with purple styling.
func renderThinking(b Block, w int) string {
	header := "▸ Thinking"
	if b.Duration > 0 {
		header += "  🕐 " + formatDuration(b.Duration)
	}
	var lines []string
	lines = append(lines, thinkingHeaderStyle.Width(w).Render(header))
	for _, l := range strings.Split(b.Content, "\n") {
		lines = append(lines, thinkingBodyStyle.Width(w).Render(l))
	}
	return strings.Join(lines, "\n")
}

func renderToolCall(b Block, w int) string {
	var lines []string

	// Status icon
	var icon string
	switch b.ToolStatus {
	case StatusSuccess:
		icon = toolSuccessStyle.Render("✓")
	case StatusFailed:
		icon = toolErrorStyle.Render("✗")
	default:
		icon = toolPendingStyle.Render("●")
	}

	// Tool name + args on one line
	name := toolNameStyle.Render(b.ToolName)
	args := toolArgsStyle.Render(truncate(b.ToolArgs, w-utf8.RuneCountInString(b.ToolName)-6))
	dur := ""
	if b.HasResult {
		dur = "  🕐 " + formatDuration(b.Duration)
	}
	callLine := fmt.Sprintf(" %s %s %s%s", icon, name, args, toolArgsStyle.Render(dur))
	lines = append(lines, callLine)

	// Tool result — truncated to defaultResultRows, no toggle
	if b.HasResult {
		lines = append(lines, resultBodyStyle.Width(w).Render("  Result:"))
		resultLines := strings.Split(b.Result, "\n")
		shown := resultLines
		if len(resultLines) > defaultResultRows {
			shown = resultLines[:defaultResultRows]
		}
		for _, l := range shown {
			lines = append(lines, resultBodyStyle.Width(w).Render(l))
		}
		if len(resultLines) > defaultResultRows {
			more := fmt.Sprintf("  … %d more lines", len(resultLines)-defaultResultRows)
			lines = append(lines, toolMoreStyle.Width(w).Render(more))
		}
	}

	return strings.Join(lines, "\n")
}

// renderStatusBar renders the bottom status bar as a single line.
func renderStatusBar(info SidebarInfo, width int) string {
	used := info.InputTokens + info.OutputTokens
	var pct float64
	if info.ContextLimit > 0 {
		pct = float64(used) / float64(info.ContextLimit) * 100
	}

	label := func(k, v string) string {
		return statusLabelStyle.Render(k+":") + statusValueStyle.Render(v)
	}

	sep := statusLabelStyle.Render("  │  ")

	parts := []string{
		label("tokens", fmt.Sprintf("%d/%d(%.1f%%)", used, info.ContextLimit, pct)),
		label("model", truncate(info.Model, 20)),
		label("agent", truncate(info.AgentName, 16)),
		label("skills", fmt.Sprintf("%d", len(info.Skills))),
		label("tools", fmt.Sprintf("%d", len(info.Tools))),
		label("mcp", fmt.Sprintf("%d", len(info.MCPServers))),
	}

	_ = lipgloss.Width // keep import used
	return statusBarStyle.Width(width).Render(strings.Join(parts, sep))
}

// formatDuration formats a duration concisely: ms for <1s, seconds otherwise.
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// renderTodoPanel renders the memory plan as a compact bordered panel.
// Returns "" when there are no items so callers can skip it cleanly.
func renderTodoPanel(items []ui.TodoItem, topic string, width int) string {
	if len(items) == 0 && topic == "" {
		return ""
	}

	// Inner width excluding border + padding
	innerW := width - 4
	if innerW < 10 {
		innerW = 10
	}

	var lines []string
	header := "▸ Memory Plan"
	if topic != "" {
		header += ": " + topic
	}
	lines = append(lines, todoHeaderStyle.Render(header))

	completed := 0
	for _, item := range items {
		var marker, styledLine string
		switch item.Status {
		case "completed":
			completed++
			marker = todoCompletedStyle.Render("[x]")
			styledLine = todoCompletedStyle.Render(fmt.Sprintf("#%d %s", item.ID, item.Content))
		case "in_progress":
			marker = todoInProgressStyle.Render("[>]")
			styledLine = todoInProgressStyle.Render(fmt.Sprintf("#%d %s", item.ID, item.Content))
			if item.ActiveForm != "" {
				styledLine += " " + todoActiveFormStyle.Render("("+item.ActiveForm+")")
			}
		default: // pending
			marker = todoPendingStyle.Render("[ ]")
			styledLine = todoPendingStyle.Render(fmt.Sprintf("#%d %s", item.ID, item.Content))
		}
		lines = append(lines, fmt.Sprintf(" %s %s", marker, styledLine))
	}

	// Progress footer
	progressBar := buildProgressBar(completed, len(items), innerW-20)
	footer := todoActiveFormStyle.Render(
		fmt.Sprintf("%s %d/%d completed", progressBar, completed, len(items)),
	)
	lines = append(lines, footer)

	inner := strings.Join(lines, "\n")
	return todoBorderStyle.Width(width - 2).Render(inner)
}

// buildProgressBar returns a simple ASCII progress bar of the given width.
func buildProgressBar(done, total, width int) string {
	if width < 4 || total == 0 {
		return ""
	}
	filled := done * width / total
	bar := "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
	return bar
}

// renderPlanPanel renders the session plan tasks as a compact bordered panel.
// Returns "" when there are no active plans so callers can skip it cleanly.
func renderPlanPanel(plans []ui.PlanSnapshot, width int) string {
	if len(plans) == 0 {
		return ""
	}

	innerW := width - 4
	if innerW < 10 {
		innerW = 10
	}

	var lines []string
	for _, plan := range plans {
		lines = append(lines, planHeaderStyle.Render("▸ Plan: "+plan.Name))

		completed := 0
		for _, task := range plan.Tasks {
			var marker, styledLine string
			switch task.Status {
			case "completed":
				completed++
				marker = todoCompletedStyle.Render("[x]")
				styledLine = todoCompletedStyle.Render(fmt.Sprintf("#%d %s", task.ID, task.Subject))
			case "in_progress":
				marker = todoInProgressStyle.Render("[>]")
				styledLine = todoInProgressStyle.Render(fmt.Sprintf("#%d %s", task.ID, task.Subject))
			case "deleted":
				marker = todoPendingStyle.Render("[-]")
				styledLine = todoPendingStyle.Render(fmt.Sprintf("#%d %s", task.ID, task.Subject))
			default: // pending
				marker = todoPendingStyle.Render("[ ]")
				styledLine = todoPendingStyle.Render(fmt.Sprintf("#%d %s", task.ID, task.Subject))
				if len(task.BlockedBy) > 0 {
					styledLine += todoActiveFormStyle.Render(fmt.Sprintf(" (blocked by %v)", task.BlockedBy))
				}
			}
			lines = append(lines, fmt.Sprintf(" %s %s", marker, styledLine))
		}

		// Progress footer
		total := len(plan.Tasks)
		progressBar := buildProgressBar(completed, total, innerW-20)
		footer := todoActiveFormStyle.Render(
			fmt.Sprintf("%s %d/%d completed", progressBar, completed, total),
		)
		lines = append(lines, footer)
	}

	inner := strings.Join(lines, "\n")
	return planBorderStyle.Width(width - 2).Render(inner)
}
