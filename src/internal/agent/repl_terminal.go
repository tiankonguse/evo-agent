package agent

import (
	"bufio"
	"fmt"
	"io"

	"evo-agent/internal/ui"
)

// TerminalFrontend is the plain-text Frontend: reads queries from an
// `io.Reader` (typically `os.Stdin`) via a `bufio.Scanner`, prints a
// "  >> " prompt before each query, and emits nothing on turn completion
// because TerminalSink already streams assistant text, tool calls, and
// tool results live as the loop runs.
//
// EOF on the reader (or the user typing one of the sentinel exit strings
// — handled by Repl.Run) terminates the loop.
type TerminalFrontend struct {
	scanner *bufio.Scanner
}

// NewTerminalFrontend wraps an io.Reader (typically os.Stdin) for line-by-
// line input. The scanner uses default behaviour — one line per query.
func NewTerminalFrontend(r io.Reader) *TerminalFrontend {
	return &TerminalFrontend{scanner: bufio.NewScanner(r)}
}

// NextQuery prints the input prompt and returns the next non-EOF line.
// Returns ok=false on EOF / read error so Repl.Run can shut down cleanly.
func (f *TerminalFrontend) NextQuery() (string, bool) {
	fmt.Printf("%s >> %s", ui.ColorCyan, ui.ColorReset)
	if !f.scanner.Scan() {
		return "", false
	}
	return f.scanner.Text(), true
}

// OnTurnDone prints a trailing newline so the next prompt visually
// separates from the prior turn's output. The agent loop's events are
// already rendered live by TerminalSink, so nothing else is needed here.
func (f *TerminalFrontend) OnTurnDone() {
	fmt.Println()
}
