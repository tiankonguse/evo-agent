package tui

import (
	"io"

	tea "charm.land/bubbletea/v2"

	"evo-agent/internal/ui"
)

// Run starts the Bubble Tea TUI, feeding user queries into queryCh.
// It creates a Sink, registers it as the global ui.EventSink so the agent
// goroutine can write events without knowing about the TUI internals,
// then blocks until the user quits (ctrl+c / "exit").
func Run(info SidebarInfo, queryCh chan<- string) error {
	sink := NewSink(512)
	ui.SetSink(sink)

	m := NewModel(info, queryCh, sink.Chan())
	p := tea.NewProgram(&m)
	_, err := p.Run()
	return err
}

// PlainRun runs the plain-text REPL (no TUI) writing to w.
// Used when stdout is not a terminal or --plain is passed.
// The default TerminalSink is already active; nothing to configure.
func PlainRun(_ io.Writer) {
	// handled by agent.Run directly
}
