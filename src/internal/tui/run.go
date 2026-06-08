package tui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"evo-agent/internal/ui"
)

// Run starts the Bubble Tea TUI, feeding user queries into queryCh.
// It creates a Sink, registers it as the global ui.EventSink so the agent
// goroutine can write events without knowing about the TUI internals,
// then blocks until the user quits (ctrl+c / "exit").
//
// Buffer size of 4096 is generous on purpose — see sink.go for why we now
// prefer backpressure over silent drops.
func Run(info SidebarInfo, queryCh chan<- string) error {
	sink := NewSink(512)
	ui.SetSink(sink)

	m := NewModel(info, queryCh, sink.Chan())
	p := tea.NewProgram(&m)
	_, err := p.Run()

	// Wake up any agent goroutine that's blocked on Emit so it can shut
	// down cleanly instead of deadlocking on a sink that no longer has a
	// reader.
	sink.Close()

	// Surface any events that were dropped during the shutdown race
	// (TUI exited while goroutines were still emitting). Two channels:
	//   - .evo-agent/tui-drops.log under the project dir — durable record
	//     so you can grep for hangs after the fact, and survives stderr
	//     redirection.
	//   - os.Stderr — printed AFTER tui exit so it lands above the next
	//     shell prompt; only meaningful when the user runs interactively.
	// The on-disk log is the primary signal; stderr is convenience.
	if n := sink.Dropped(); n > 0 {
		reportDroppedEvents(info.ProjectDir, n)
	}
	return err
}

// reportDroppedEvents records a non-zero dropped-events count after TUI
// shutdown. We write to .evo-agent/tui-drops.log under the project dir;
// errors are tolerated (best-effort diagnostic). Stderr fallback covers
// the case where the .evo-agent dir isn't writable.
func reportDroppedEvents(projectDir string, n uint64) {
	stamp := time.Now().Format(time.RFC3339)
	line := fmt.Sprintf("%s dropped=%d (events emitted after sink close — shutdown race)\n", stamp, n)

	if projectDir != "" {
		dir := filepath.Join(projectDir, ".evo-agent")
		// MkdirAll is idempotent and cheap; the dir is normally already
		// there because session/dump/tools all write under it.
		if err := os.MkdirAll(dir, 0o755); err == nil {
			path := filepath.Join(dir, "tui-drops.log")
			if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
				_, _ = f.WriteString(line)
				_ = f.Close()
				fmt.Fprintf(os.Stderr, "[tui] dropped %d events after shutdown (logged to %s)\n", n, path)
				return
			}
		}
	}
	// Fallback: project dir unwritable, or unknown — just stderr.
	fmt.Fprintf(os.Stderr, "[tui] dropped %d events after shutdown\n", n)
}

// PlainRun runs the plain-text REPL (no TUI) writing to w.
// Used when stdout is not a terminal or --plain is passed.
// The default TerminalSink is already active; nothing to configure.
func PlainRun(_ io.Writer) {
	// handled by agent.Run directly
}
