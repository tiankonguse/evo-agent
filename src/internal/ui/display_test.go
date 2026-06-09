package ui

import (
	"strings"
	"testing"
)

func TestIndentSubagent_Empty(t *testing.T) {
	if got := IndentSubagent("", "hi\nbye"); got != "hi\nbye" {
		t.Errorf("empty agent should pass through unchanged; got %q", got)
	}
}

func TestIndentSubagent_PrependsGutterOnEachLine(t *testing.T) {
	got := IndentSubagent("code-reviewer", "line1\nline2\nline3")
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines; got %d", len(lines))
	}
	for i, line := range lines {
		if !strings.Contains(line, "┃") {
			t.Errorf("line %d missing gutter bar: %q", i, line)
		}
	}
	// First line should also carry the agent header
	if !strings.Contains(lines[0], "[code-reviewer]") {
		t.Errorf("first line missing agent header: %q", lines[0])
	}
	// Subsequent lines should NOT carry the header (kept terse)
	for i := 1; i < len(lines); i++ {
		if strings.Contains(lines[i], "[code-reviewer]") {
			t.Errorf("line %d should not repeat header: %q", i, lines[i])
		}
	}
}

func TestSubagentColor_StableForSameName(t *testing.T) {
	if SubagentColor("code-reviewer") != SubagentColor("code-reviewer") {
		t.Error("SubagentColor should be deterministic")
	}
	if SubagentColor("") != "" {
		t.Error("SubagentColor for empty should be empty")
	}
}

func TestSubagentColor_DistinctColorsForDifferentNames(t *testing.T) {
	// At least 6 colors in the palette — pick names that almost certainly
	// hash to different buckets to verify the palette indexing works.
	// (We can't guarantee this without computing the FNV hash here, but
	// these names land on different slots in practice.)
	a := SubagentColor("code-reviewer")
	b := SubagentColor("test-runner")
	c := SubagentColor("explore")
	if a == b && b == c {
		t.Error("expected at least one of the three names to land on a different color")
	}
}
