package tui

import (
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
)

// markdown.go — render assistant text as markdown for the TUI.
//
// Assistant output is markdown by convention (the system prompt explicitly
// promises GitHub-flavored markdown rendering). Without a renderer, headings,
// fenced code blocks, lists, and inline emphasis all show as raw markup. This
// file wraps charmbracelet/glamour to convert markdown → ANSI styled text
// sized for the live bottom area's inner width.
//
// Width-keyed cache: glamour.NewTermRenderer is comparatively expensive
// (theme + chroma + word-wrap setup), and the TUI calls it on every assistant
// text block. Width changes only on terminal resize, so a tiny per-width
// cache keeps allocations bounded while still respecting reflow on resize.

var (
	mdMu       sync.RWMutex
	mdRenders  = map[int]*glamour.TermRenderer{}
	mdMinWidth = 20
)

// rendererFor returns a memoized glamour renderer sized for `width` columns.
// Width below `mdMinWidth` is clamped because glamour's wrapping math degrades
// on near-zero widths and the TUI sometimes renders during the first frame
// before the layout is finalized.
func rendererFor(width int) (*glamour.TermRenderer, error) {
	if width < mdMinWidth {
		width = mdMinWidth
	}
	mdMu.RLock()
	r, ok := mdRenders[width]
	mdMu.RUnlock()
	if ok {
		return r, nil
	}

	mdMu.Lock()
	defer mdMu.Unlock()
	if r, ok := mdRenders[width]; ok {
		return r, nil
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(), // picks dark/light/no-color per terminal
		glamour.WithWordWrap(width),
		glamour.WithEmoji(),
	)
	if err != nil {
		return nil, err
	}
	mdRenders[width] = r
	return r, nil
}

// renderMarkdown returns the markdown source rendered as ANSI text. On any
// renderer error it falls back to the raw text — assistant content must
// never be lost to a presentation failure.
//
// glamour appends a trailing newline (and often a leading one); both are
// trimmed so caller-side `+"\n"` produces consistent spacing alongside other
// event renders.
func renderMarkdown(text string, width int) string {
	if text == "" {
		return ""
	}
	r, err := rendererFor(width)
	if err != nil {
		return text
	}
	out, err := r.Render(text)
	if err != nil {
		return text
	}
	return strings.Trim(out, "\n")
}

// resetMarkdownCache discards memoized renderers. Called on terminal resize
// so a width that won't be queried again doesn't leak — currently the TUI
// has no resize-evict signal, so this is reserved for tests.
func resetMarkdownCache() {
	mdMu.Lock()
	mdRenders = map[int]*glamour.TermRenderer{}
	mdMu.Unlock()
}
