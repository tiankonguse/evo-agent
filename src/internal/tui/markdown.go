package tui

import (
	"os"
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
// IMPORTANT — do NOT use glamour.WithAutoStyle() here. AutoStyle calls
// termenv.HasDarkBackground(), which writes an OSC 11 query directly to
// stdout and reads the terminal's reply directly from stdin. Both ends
// bypass bubbletea's I/O loop entirely, so the terminal's reply (e.g.
// `2828/2c2c/3434` from atom-one-dark backgrounds) gets consumed by the
// next stdin reader — bubbletea's input parser, which can't recognise it
// as a stray OSC reply and routes the bytes into the textarea. Users see
// gibberish like `2828/2c2c/3434` appear in the input box exactly when
// the first markdown event renders.
//
// We pick a fixed style at init time. Default: "dark" (covers the vast
// majority of dev terminals). Override with EVO_GLAMOUR_STYLE — accepted
// values are anything the standard glamour styles export, currently:
// "dark", "light", "notty", "ascii", "pink", "tokyo-night", "dracula".
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

// glamourStyle returns the style name to feed to glamour.WithStandardStyle.
// Reads EVO_GLAMOUR_STYLE once per call (cheap; happens lazily inside
// rendererFor only on cache miss), falling back to "dark".
func glamourStyle() string {
	if s := strings.TrimSpace(os.Getenv("EVO_GLAMOUR_STYLE")); s != "" {
		return s
	}
	return "dark"
}

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
		glamour.WithStandardStyle(glamourStyle()), // explicit style — never WithAutoStyle()
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
