package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"evo-agent/internal/session"
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

	// Persistent team roster (updated via EvTeam)
	teamMembers []ui.TeammateSnapshot
	teamName    string

	// Active /goal indicator (updated via EvGoal). When goalActive is
	// false the indicator is hidden.
	goalActive   bool
	goalText     string
	goalIter     int
	goalMaxIter  int
	goalSetAtMs  int64
	goalLastKind string // last lifecycle event ("evaluating", "continuing", ...)
	goalLastNote string // brief reason / status to surface in the indicator

	// Slash command completion
	completionActive bool     // dropdown is visible
	completionItems  []string // filtered list of matching names
	completionIdx    int      // currently highlighted index (0-based)
	allSlashNames    []string // full list: skills + commands

	// Session picker (triggered by typing exactly "/resume")
	sessionPickerActive bool
	sessionPickerItems  []session.SessionListEntry
	sessionPickerIdx    int
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
		if m.sessionPickerActive {
			m.sessionPickerActive = false
			return m, nil
		}
		if m.completionActive {
			m.completionActive = false
			return m, nil
		}
		return m, nil

	case "up":
		if m.sessionPickerActive && len(m.sessionPickerItems) > 0 {
			m.sessionPickerIdx--
			if m.sessionPickerIdx < 0 {
				m.sessionPickerIdx = len(m.sessionPickerItems) - 1
			}
			return m, nil
		}
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
		if m.sessionPickerActive && len(m.sessionPickerItems) > 0 {
			m.sessionPickerIdx++
			if m.sessionPickerIdx >= len(m.sessionPickerItems) {
				m.sessionPickerIdx = 0
			}
			return m, nil
		}
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
		// Session picker active: accept selection (if any) and auto-submit.
		// If picker is open but empty, just close it — don't fall through and
		// submit the literal "/resume" command.
		if m.sessionPickerActive {
			if len(m.sessionPickerItems) == 0 {
				m.sessionPickerActive = false
				m.textarea.Reset()
				return m, nil
			}
			id := m.sessionPickerItems[m.sessionPickerIdx].ID
			m.sessionPickerActive = false
			m.completionActive = false
			m.textarea.Reset()
			m.busy = true
			m.queryStartTime = time.Now()
			query := "/resume " + id
			w := m.width
			if w == 0 {
				w = 80
			}
			printed := userStyle.Width(w - 2).Render("You: " + query)
			go func() { m.queryCh <- query }()
			return m, tea.Batch(tea.Println(printed+"\n"), tickCmd())
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
		m.sessionPickerActive = false
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
		// Assistant text is markdown by convention (see system prompt).
		// Render it through glamour so headings/code-fences/lists/emphasis
		// look right; fall back to plain styled text on render error so
		// content is never lost.
		rendered := renderMarkdown(e.Text, bw)
		if rendered == "" {
			rendered = textStyle.Width(bw).Render(e.Text)
		}
		cmds = append(cmds, tea.Println(rendered+"\n"))

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
		// Only adopt the API-reported model when it looks meaningful.
		// Some OpenAI-compatible servers (Ollama, vLLM, gateway proxies)
		// echo a placeholder like "default" or "" instead of the actual
		// model alias the user configured — without this guard, the
		// status bar would clobber the user's MODEL_ID with that
		// placeholder on the first turn.
		if isMeaningfulModelName(e.Model) {
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

	case ui.EvBgTasks:
		// Live background-task counts shown in the status bar. Always
		// rendered (including 0/0), so users discover the feature.
		m.info.BgRunning = e.BgRunning
		m.info.BgCompleted = e.BgCompleted

	case ui.EvTeam:
		// Live team roster shown in a dedicated panel + status bar
		// counters. Always rendered (including 0/0/0), so users discover
		// the feature even with no spawned teammates yet.
		m.teamName = e.TeamName
		m.teamMembers = e.TeamMembers
		var working, idle, shutdown int
		for _, mem := range e.TeamMembers {
			switch mem.Status {
			case "working":
				working++
			case "idle":
				idle++
			case "shutdown":
				shutdown++
			}
		}
		m.info.TeamWorking = working
		m.info.TeamIdle = idle
		m.info.TeamShutdown = shutdown

	case ui.EvGoal:
		// Lifecycle dispatch — keeps the indicator's display state in
		// sync with what the agent loop's goal logic decided.
		switch e.GoalKind {
		case "set":
			m.goalActive = true
			m.goalText = e.GoalText
			m.goalIter = e.GoalIter
			m.goalMaxIter = e.GoalMaxIter
			m.goalSetAtMs = e.GoalSetAt
			m.goalLastKind = "set"
			m.goalLastNote = ""
			m.info.Goal = e.GoalText
		case "evaluating":
			m.goalLastKind = "evaluating"
			m.goalLastNote = "checking…"
		case "continuing":
			m.goalIter = e.GoalIter
			m.goalLastKind = "continuing"
			m.goalLastNote = e.GoalReason
		case "achieved":
			m.goalActive = false
			m.goalLastKind = "achieved"
			m.goalLastNote = e.GoalReason
			m.info.Goal = ""
			cmds = append(cmds, tea.Println(systemStyle.Render(
				"✓ /goal achieved: "+e.GoalReason,
			)+"\n"))
		case "cleared":
			m.goalActive = false
			m.goalLastKind = "cleared"
			m.goalLastNote = ""
			m.info.Goal = ""
			cmds = append(cmds, tea.Println(systemStyle.Render("◎ /goal cleared")+"\n"))
		case "capped":
			m.goalActive = false
			m.goalLastKind = "capped"
			m.goalLastNote = "iteration cap"
			m.info.Goal = ""
			cmds = append(cmds, tea.Println(systemStyle.Render(fmt.Sprintf(
				"× /goal capped at %d iterations — auto-cleared", e.GoalMaxIter,
			))+"\n"))
		case "status":
			if e.GoalText == "" {
				cmds = append(cmds, tea.Println(systemStyle.Render("◎ /goal: no active goal")+"\n"))
			} else {
				cmds = append(cmds, tea.Println(systemStyle.Render(fmt.Sprintf(
					"◎ /goal active: %s (iter %d/%d)", e.GoalText, e.GoalIter, e.GoalMaxIter,
				))+"\n"))
			}
		}
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

	// Show team panel when any teammate is registered
	if panel := renderTeamPanel(m.teamName, m.teamMembers, w); panel != "" {
		parts = append(parts, panel)
	}

	// Show /goal indicator when active
	if line := m.renderGoalIndicator(w); line != "" {
		parts = append(parts, line)
	}

	// Show completion dropdown when active
	if panel := m.renderCompletion(w); panel != "" {
		parts = append(parts, panel)
	}

	// Show session picker when /resume is typed alone
	if panel := m.renderSessionPicker(w); panel != "" {
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
		m.sessionPickerActive = false
		return
	}

	// Special case: typing exactly "/resume" (no args) opens the session picker.
	trimmed := strings.TrimRight(val, " ")
	if trimmed == "/resume" {
		m.completionActive = false
		m.openSessionPicker()
		return
	}
	// Once the user starts typing args after /resume, dismiss the picker —
	// they're entering an explicit id.
	if strings.HasPrefix(val, "/resume ") {
		m.sessionPickerActive = false
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

// openSessionPicker loads the session list and activates the picker dropdown.
// Sessions are read from .evo-agent/sessions/ in m.info.ProjectDir, sorted
// newest first. The currently active session is filtered out so the user
// can't try to resume the in-progress conversation.
//
// The picker is activated even when the filtered list is empty so the user
// always sees feedback when they type "/resume" — render handles the empty
// state with a helpful hint instead of an actionable list.
func (m *Model) openSessionPicker() {
	all := session.ListSessions(m.info.ProjectDir)
	filtered := all[:0]
	for _, e := range all {
		if e.ID == m.info.SessionID {
			continue
		}
		filtered = append(filtered, e)
	}
	m.sessionPickerItems = filtered
	m.sessionPickerActive = true
	m.sessionPickerIdx = 0
}

// renderCompletion renders the autocomplete dropdown panel.
func (m *Model) renderCompletion(w int) string {
	if !m.completionActive || len(m.completionItems) == 0 {
		return ""
	}

	const maxShow = 8
	// Compute scroll window so the highlighted index stays visible.
	// Window of size maxShow slides as completionIdx moves out of view.
	startIdx, endIdx := scrollWindow(m.completionIdx, len(m.completionItems), maxShow)

	innerW := w - 4
	if innerW < 20 {
		innerW = 20
	}

	var lines []string
	// Top "… N more above" hint when window doesn't start at 0
	if startIdx > 0 {
		hint := fmt.Sprintf("  ↑ %d more", startIdx)
		lines = append(lines, completionItemStyle.Width(innerW).Render(hint))
	}
	for i := startIdx; i < endIdx; i++ {
		name := m.completionItems[i]
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
	// Bottom "… N more below" hint
	if endIdx < len(m.completionItems) {
		more := fmt.Sprintf("  ↓ %d more", len(m.completionItems)-endIdx)
		lines = append(lines, completionItemStyle.Width(innerW).Render(more))
	}

	inner := strings.Join(lines, "\n")
	return completionBorderStyle.Width(w - 2).Render(inner)
}

// scrollWindow returns the [start, end) index range to display so that
// `cursor` is always visible inside a window of at most `maxShow` items
// drawn from `total` items. When the cursor sits below the bottom of the
// previous window, the window slides down so the cursor lands on the
// last visible row; symmetric for upward movement. Used by both the
// completion dropdown and the session picker so navigation past the
// initial fixed slice no longer hides the highlight.
func scrollWindow(cursor, total, maxShow int) (start, end int) {
	if total <= maxShow {
		return 0, total
	}
	// Anchor the window so cursor is always inside [start, end).
	start = cursor - maxShow + 1
	if start < 0 {
		start = 0
	}
	end = start + maxShow
	if end > total {
		end = total
		start = end - maxShow
	}
	return start, end
}

// isMeaningfulModelName guards the status bar against placeholder model
// names returned by some OpenAI-compatible servers.
//
// Ollama, vLLM, and a number of LLM gateways (Cloudflare AI Gateway,
// LiteLLM, etc.) echo a generic value like "default", "gpt", or "model"
// in the response's `model` field instead of the actual alias the user
// configured. Without this guard, the TUI would clobber the user's
// MODEL_ID with that placeholder on the first turn — making the status
// bar uselessly read "model:default" for the rest of the session.
//
// We accept anything that's non-empty AND not in the known placeholder
// list. Real model names are diverse enough that an allow-list would
// be wrong; a tiny deny-list of obvious placeholders is the conservative
// choice. Add to it as new gateways crop up.
func isMeaningfulModelName(name string) bool {
	if name == "" {
		return false
	}
	switch name {
	case "default", "model", "unknown", "n/a":
		return false
	}
	return true
}

// ── Spinner ───────────────────────────────────────────────────────────────────

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
var spinnerIdx int

func spinnerFrame() string {
	f := spinnerFrames[spinnerIdx%len(spinnerFrames)]
	spinnerIdx++
	return f
}

// ── Session picker ────────────────────────────────────────────────────────────

// renderSessionPicker renders the resume session list when active.
//
// Each row shows: time | tokens | first prompt — sorted newest first by the
// list provider. Up/Down navigate; Enter accepts and submits "/resume <id>".
// When the list is empty, a single non-selectable hint line is rendered so
// the user gets feedback that the dropdown opened but has no candidates.
func (m *Model) renderSessionPicker(w int) string {
	if !m.sessionPickerActive {
		return ""
	}

	innerW := w - 4
	if innerW < 40 {
		innerW = 40
	}

	header := completionItemStyle.Width(innerW).Render(
		"  Pick a session to resume  (↑/↓ select, Enter accept, Esc cancel)",
	)
	var lines []string
	lines = append(lines, header)

	if len(m.sessionPickerItems) == 0 {
		hint := completionItemStyle.Width(innerW).Render(
			"  (no previous sessions in this project — start chatting, then come back)",
		)
		lines = append(lines, hint)
		inner := strings.Join(lines, "\n")
		return completionBorderStyle.Width(w - 2).Render(inner)
	}

	const maxShow = 8
	startIdx, endIdx := scrollWindow(m.sessionPickerIdx, len(m.sessionPickerItems), maxShow)

	if startIdx > 0 {
		hint := completionItemStyle.Width(innerW).Render(fmt.Sprintf("  ↑ %d more", startIdx))
		lines = append(lines, hint)
	}
	for i := startIdx; i < endIdx; i++ {
		e := m.sessionPickerItems[i]
		updated := e.Updated
		if updated == 0 {
			updated = session.ParseLeadingTimestampMs(e.ID)
		}
		ts := time.UnixMilli(updated).Local().Format("2006-01-02 15:04")
		prompt := e.FirstPrompt
		if prompt == "" {
			prompt = "(no prompt yet)"
		}
		// Build line: TIME  tokens=N,NNN  「prompt…」
		head := fmt.Sprintf("  %s  tokens=%s  ", ts, formatThousands(e.TotalTokens()))
		maxPromptW := innerW - len(head) - 4
		if maxPromptW < 8 {
			maxPromptW = 8
		}
		if len(prompt) > maxPromptW {
			prompt = prompt[:maxPromptW-1] + "…"
		}
		line := head + "「" + prompt + "」"
		if i == m.sessionPickerIdx {
			line = completionSelectedStyle.Width(innerW).Render(line)
		} else {
			line = completionItemStyle.Width(innerW).Render(line)
		}
		lines = append(lines, line)
	}

	if endIdx < len(m.sessionPickerItems) {
		more := fmt.Sprintf("  ↓ %d more", len(m.sessionPickerItems)-endIdx)
		lines = append(lines, completionItemStyle.Width(innerW).Render(more))
	}

	inner := strings.Join(lines, "\n")
	return completionBorderStyle.Width(w - 2).Render(inner)
}

// parseLeadingUnix is kept as a thin alias of session.ParseLeadingTimestampMs
// for any code that already imports it; new code should call the session
// package directly.
//
// Removed — see session.ParseLeadingTimestampMs.

// formatThousands formats an int64 with comma separators.
func formatThousands(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	rem := len(s) % 3
	if rem > 0 {
		b.WriteString(s[:rem])
		if len(s) > rem {
			b.WriteByte(',')
		}
	}
	for i := rem; i < len(s); i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < len(s) {
			b.WriteByte(',')
		}
	}
	return b.String()
}
