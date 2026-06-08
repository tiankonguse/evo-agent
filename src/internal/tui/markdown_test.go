package tui

import (
	"regexp"
	"strings"
	"testing"
)

// ansiEscape strips SGR escape sequences so substring assertions don't
// fight the syntax-highlighter. Now that we use a fixed "dark" style
// (instead of glamour's WithAutoStyle, which fell back to no-color in
// non-TTY tests), every code-fence token gets wrapped in its own SGR
// pair — `fmt.Println` becomes `\x1b[…mfmt\x1b[0m\x1b[…m.\x1b[0m…`. The
// content is still there, just colored.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func stripANSI(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}

// renderMarkdown should leave plain text recognisable (no markdown syntax
// to convert) and return non-empty output. We don't assert exact ANSI
// because glamour's auto-style depends on the terminal env, but we can
// pin invariants: input substrings survive, output is non-empty.
func TestRenderMarkdown_PlainText(t *testing.T) {
	resetMarkdownCache()
	out := renderMarkdown("hello world", 80)
	if out == "" {
		t.Fatal("renderMarkdown returned empty for non-empty input")
	}
	if !strings.Contains(stripANSI(out), "hello world") {
		t.Errorf("rendered output lost the input text: %q", out)
	}
}

func TestRenderMarkdown_Heading(t *testing.T) {
	resetMarkdownCache()
	src := "# Heading\n\nSome body text."
	out := renderMarkdown(src, 80)
	if out == "" {
		t.Fatal("empty output")
	}
	// Heading body must survive rendering. We don't assert that the "#"
	// marker is gone because some style configs keep it for accessibility.
	// The invariant we DO want: the renderer ran (output is reformatted,
	// not byte-equal to input) and the content is recoverable after we
	// strip ANSI styling.
	plain := stripANSI(out)
	if !strings.Contains(plain, "Heading") || !strings.Contains(plain, "Some body text.") {
		t.Errorf("rendered output lost content: %q", out)
	}
	if out == src {
		t.Errorf("output identical to input — renderer didn't run: %q", out)
	}
}

func TestRenderMarkdown_CodeFence(t *testing.T) {
	resetMarkdownCache()
	src := "Look at this:\n\n```go\nfmt.Println(\"hi\")\n```\n"
	out := renderMarkdown(src, 80)
	plain := stripANSI(out)
	if !strings.Contains(plain, "fmt.Println") {
		t.Errorf("code body lost: %q", out)
	}
	if out == src {
		t.Errorf("output identical to input — renderer didn't run: %q", out)
	}
}

func TestRenderMarkdown_EmptyInput(t *testing.T) {
	if got := renderMarkdown("", 80); got != "" {
		t.Errorf("expected empty output for empty input; got %q", got)
	}
}

func TestRenderMarkdown_NarrowWidthClampedToMin(t *testing.T) {
	resetMarkdownCache()
	// Width below mdMinWidth is clamped — should not panic.
	out := renderMarkdown("**bold**", 5)
	if out == "" {
		t.Errorf("empty output for narrow width; expected fallback or rendered text")
	}
}

func TestRenderMarkdown_RendererCachedPerWidth(t *testing.T) {
	resetMarkdownCache()
	r1, err := rendererFor(80)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := rendererFor(80)
	if err != nil {
		t.Fatal(err)
	}
	if r1 != r2 {
		t.Error("rendererFor(80) returned a different instance on second call; cache miss")
	}
	r3, err := rendererFor(120)
	if err != nil {
		t.Fatal(err)
	}
	if r3 == r1 {
		t.Error("rendererFor(120) returned the same instance as rendererFor(80); cache key wrong")
	}
}
