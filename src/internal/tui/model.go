package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"evo-agent/internal/skills"
	"evo-agent/internal/ui"
)

// ── External messages (sent from agent goroutine) ────────────────────────────

// AgentEventMsg wraps a ui.Event for Bubble Tea.
type AgentEventMsg ui.Event

// AgentDoneMsg signals the agent goroutine finished processing a turn.
type AgentDoneMsg struct{}

// tickMsg drives spinner animation.
type tickMsg time.Time

// ── Model ────────────────────────────────────────────────────────────────────

// Model is the root Bubble Tea model.
// Conversation content is printed permanently via tea.Println so the terminal
// scroll buffer works natively. View() only renders the live input area.
type Model struct {
	width int

	// In-flight tool calls (pending result); completed ones are printed and removed.
	pendingTools []Block

	// Input (multi-line textarea)
	textarea       textarea.Model
	busy           bool
	queryCh        chan<- string
	queryStartTime time.Time

	// Event channel — injected from Sink.Chan()
	eventCh <-chan ui.Event

	// Status bar info
	info SidebarInfo

	// Memory plan items (updated via EvTodo)
	todoItems []ui.TodoItem
	todoTopic string

	// Persistent plan items (updated via EvPlan)
	planItems []ui.PlanSnapshot

	// Slash command completion
	completionActive bool     // dropdown is visible
	completionItems  []string // filtered list of matching names
	completionIdx    int      // currently highlighted index (0-based)
	allSlashNames    []string // full list: skills + commands
}

// NewModel creates the initial TUI model.
func NewModel(info SidebarInfo, queryCh chan<- string, eventCh <-chan ui.Event) Model {
	ta := textarea.New()
	ta.Placeholder = "Ask something… (Enter to send, Ctrl+Enter for newline)"
	ta.Prompt = " >> "
	styles := ta.Styles()
	styles.Focused.Prompt = inputPromptStyle
	ta.SetStyles(styles)
	ta.CharLimit = 10000
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline.SetKeys("ctrl+enter", "alt+enter")

	// Merge skills + commands into a sorted list for autocomplete
	allNames := make([]string, 0, len(info.Skills)+len(info.Commands))
	allNames = append(allNames, info.Skills...)
	allNames = append(allNames, info.Commands...)
	sort.Strings(allNames)

	return Model{
		textarea:      ta,
		info:          info,
		queryCh:       queryCh,
		eventCh:       eventCh,
		allSlashNames: allNames,
	}
}

// ── Init ─────────────────────────────────────────────────────────────────────

func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		m.textarea.Focus(),
		textarea.Blink,
		m.listenForEvents(),
	)
}

// ── Update ───────────────────────────────────────────────────────────────────

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		inputW := m.width - 4
		if inputW < 10 {
			inputW = 10
		}
		m.textarea.SetWidth(inputW)
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case AgentEventMsg:
		return m.handleAgentEvent(ui.Event(msg))

	case AgentDoneMsg:
		m.busy = false
		return m, nil

	case tickMsg:
		if m.busy {
			return m, tickCmd()
		}
		return m, nil
	}

	// Delegate to textarea when not busy
	if !m.busy {
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "ctrl+d":
		return m, func() tea.Msg { return tea.Quit() }

	case "ctrl+enter", "alt+enter":
		if !m.busy {
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(msg)
			m.updateCompletion()
			return m, cmd
		}
		return m, nil

	case "escape":
		if m.completionActive {
			m.completionActive = false
			return m, nil
		}
		return m, nil

	case "up":
		if m.completionActive && len(m.completionItems) > 0 {
			m.completionIdx--
			if m.completionIdx < 0 {
				m.completionIdx = len(m.completionItems) - 1
			}
			return m, nil
		}
		if !m.busy {
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(msg)
			return m, cmd
		}
		return m, nil

	case "down":
		if m.completionActive && len(m.completionItems) > 0 {
			m.completionIdx++
			if m.completionIdx >= len(m.completionItems) {
				m.completionIdx = 0
			}
			return m, nil
		}
		if !m.busy {
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(msg)
			return m, cmd
		}
		return m, nil

	case "tab":
		if m.completionActive && len(m.completionItems) > 0 {
			selected := m.completionItems[m.completionIdx]
			m.textarea.SetValue("/" + selected + " ")
			m.completionActive = false
			return m, nil
		}
		if !m.busy {
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(msg)
			m.updateCompletion()
			return m, cmd
		}
		return m, nil

	case "enter":
		if m.busy {
			return m, nil
		}
		// If completion active, accept selection
		if m.completionActive && len(m.completionItems) > 0 {
			selected := m.completionItems[m.completionIdx]
			m.textarea.SetValue("/" + selected + " ")
			m.completionActive = false
			return m, nil
		}
		// Otherwise submit the query
		query := strings.TrimSpace(m.textarea.Value())
		if query == "" {
			return m, nil
		}
		if query == "q" || query == "exit" {
			return m, func() tea.Msg { return tea.Quit() }
		}
		m.textarea.Reset()
		m.busy = true
		m.completionActive = false
		m.queryStartTime = time.Now()
		// Print user message permanently into scroll buffer
		w := m.width
		if w == 0 {
			w = 80
		}
		printed := userStyle.Width(w - 2).Render("You: " + query)
		go func() { m.queryCh <- query }()
		return m, tea.Batch(tea.Println(printed+"\n"), tickCmd())
	}

	// Forward to textarea
	if !m.busy {
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		m.updateCompletion()
		return m, cmd
	}
	return m, nil
}

// handleAgentEvent prints completed content permanently and tracks pending tools.
func (m *Model) handleAgentEvent(e ui.Event) (tea.Model, tea.Cmd) {
	cmds := []tea.Cmd{m.listenForEvents()}
	w := m.width
	if w == 0 {
		w = 80
	}
	bw := w - 2

	switch e.Kind {
	case ui.EvThinking:
		b := newThinkingBlock(e.Text)
		b.Duration = time.Since(m.queryStartTime)
		cmds = append(cmds, tea.Println(renderThinking(b, bw)+"\n"))

	case ui.EvText:
		cmds = append(cmds, tea.Println(textStyle.Width(bw).Render(e.Text)+"\n"))

	case ui.EvToolCall:
		// Store pending; will be printed when result arrives
		m.pendingTools = append(m.pendingTools, newToolBlock(e.ToolID, e.ToolName, e.ToolInput))

	case ui.EvToolResult:
		for i, b := range m.pendingTools {
			if b.Kind == KindToolCall && b.ID == e.ResultID {
				m.pendingTools[i].HasResult = true
				m.pendingTools[i].Result = e.ResultOutput
				m.pendingTools[i].Duration = time.Since(m.pendingTools[i].StartTime)
				if e.ResultError {
					m.pendingTools[i].ToolStatus = StatusFailed
				} else {
					m.pendingTools[i].ToolStatus = StatusSuccess
				}
				// Print completed tool call permanently
				cmds = append(cmds, tea.Println(renderToolCall(m.pendingTools[i], bw)+"\n"))
				// Remove from pending
				m.pendingTools = append(m.pendingTools[:i], m.pendingTools[i+1:]...)
				break
			}
		}

	case ui.EvDone:
		m.busy = false
		elapsed := time.Since(m.queryStartTime)
		cmds = append(cmds, tea.Println(elapsedStyle.Render("🕐 "+formatDuration(elapsed))+"\n"))
		// Re-focus textarea so completion and input work after agent finishes
		cmds = append(cmds, m.textarea.Focus())

	case ui.EvSystem:
		if e.Text != "" {
			cmds = append(cmds, tea.Println(systemStyle.Render(e.Text)+"\n"))
		}

	case ui.EvTokens:
		m.info.InputTokens = e.InputTokens
		m.info.OutputTokens = e.OutputTokens
		if e.Model != "" {
			m.info.Model = e.Model
		}
		if e.BlockSummary != "" {
			tokenInfo := fmt.Sprintf("model=%s in=%d out=%d stop=%s blocks=[%s]",
				e.Model, e.InputTokens, e.OutputTokens, e.StopReason, e.BlockSummary)
			cmds = append(cmds, tea.Println(systemStyle.Render(tokenInfo)))
		}

	case ui.EvTodo:
		// Store updated plan; View() will re-render it live
		m.todoItems = e.TodoItems
		m.todoTopic = e.TodoTopic

	case ui.EvPlan:
		// Store updated session plan; View() will re-render it live
		m.planItems = e.PlanItems
	}

	return m, tea.Batch(cmds...)
}

// ── View ─────────────────────────────────────────────────────────────────────
// View only renders the live/interactive bottom area.
// All conversation content is already in the terminal scroll buffer via tea.Println.

func (m *Model) View() tea.View {
	w := m.width
	if w == 0 {
		w = 80
	}

	var parts []string

	// Show any pending (in-flight) tool calls
	for _, b := range m.pendingTools {
		parts = append(parts, renderToolCall(b, w-2))
	}

	// Show memory plan when items exist
	if panel := renderTodoPanel(m.todoItems, m.todoTopic, w); panel != "" {
		parts = append(parts, panel)
	}

	// Show session plan when active
	if panel := renderPlanPanel(m.planItems, w); panel != "" {
		parts = append(parts, panel)
	}

	// Show completion dropdown when active
	if panel := m.renderCompletion(w); panel != "" {
		parts = append(parts, panel)
	}

	if m.busy {
		parts = append(parts, inputBusyStyle.Render(spinnerFrame()+" Thinking…"))
	}

	parts = append(parts, m.renderInputArea(w))
	parts = append(parts, renderStatusBar(m.info, w))
	parts = append(parts, helpBarStyle.Width(w).Render(
		"Enter send • Ctrl+Enter/Alt+Enter newline • ctrl+c quit",
	))

	return tea.NewView(strings.Join(parts, "\n"))
}

func (m *Model) renderInputArea(w int) string {
	if m.busy {
		return inputBusyStyle.Width(w).Render("  Processing…")
	}
	inputW := w - 4
	if inputW < 10 {
		inputW = 10
	}
	m.textarea.SetWidth(inputW)
	return m.textarea.View()
}

// ── Commands ─────────────────────────────────────────────────────────────────

// listenForEvents returns a Cmd that blocks until the next event arrives on
// the injected event channel.
func (m *Model) listenForEvents() tea.Cmd {
	return func() tea.Msg {
		e := <-m.eventCh
		return AgentEventMsg(e)
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// ── Completion ───────────────────────────────────────────────────────────

// updateCompletion checks the textarea value and shows/hides the completion dropdown.
func (m *Model) updateCompletion() {
	val := m.textarea.Value()

	// Must start with "/" and not be busy
	if m.busy || len(val) == 0 || val[0] != '/' {
		m.completionActive = false
		return
	}

	// Extract the prefix after "/" (up to first space)
	rest := val[1:]
	if idx := strings.IndexByte(rest, ' '); idx >= 0 {
		// User is already typing args — dismiss completion
		m.completionActive = false
		return
	}

	// Filter allSlashNames by prefix
	prefix := strings.ToLower(rest)
	var filtered []string
	for _, name := range m.allSlashNames {
		if strings.HasPrefix(strings.ToLower(name), prefix) {
			filtered = append(filtered, name)
		}
	}

	if len(filtered) == 0 {
		m.completionActive = false
		return
	}

	m.completionActive = true
	m.completionItems = filtered
	// Reset index if out of bounds
	if m.completionIdx >= len(filtered) {
		m.completionIdx = 0
	}
}

// renderCompletion renders the autocomplete dropdown panel.
func (m *Model) renderCompletion(w int) string {
	if !m.completionActive || len(m.completionItems) == 0 {
		return ""
	}

	maxShow := 8
	items := m.completionItems
	if len(items) > maxShow {
		items = items[:maxShow]
	}

	innerW := w - 4
	if innerW < 20 {
		innerW = 20
	}

	var lines []string
	for i, name := range items {
		manifest := skills.GetManifest(name)
		hint := ""
		if manifest.ArgumentHint != "" {
			hint = " " + manifest.ArgumentHint
		}
		desc := manifest.Description
		maxDesc := innerW - len(name) - len(hint) - 6
		if maxDesc > 0 && len(desc) > maxDesc {
			desc = desc[:maxDesc-1] + "…"
		} else if maxDesc <= 0 {
			desc = ""
		}

		line := fmt.Sprintf("  /%s%s  %s", name, hint, desc)
		if i == m.completionIdx {
			line = completionSelectedStyle.Width(innerW).Render(line)
		} else {
			line = completionItemStyle.Width(innerW).Render(line)
		}
		lines = append(lines, line)
	}

	if len(m.completionItems) > maxShow {
		more := fmt.Sprintf("  … %d more", len(m.completionItems)-maxShow)
		lines = append(lines, completionItemStyle.Render(more))
	}

	inner := strings.Join(lines, "\n")
	return completionBorderStyle.Width(w - 2).Render(inner)
}

// ── Spinner ───────────────────────────────────────────────────────────────────

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
var spinnerIdx int

func spinnerFrame() string {
	f := spinnerFrames[spinnerIdx%len(spinnerFrames)]
	spinnerIdx++
	return f
}
