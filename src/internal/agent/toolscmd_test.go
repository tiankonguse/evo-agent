package agent

import "testing"

// TestParseToolsCmd pins the user-visible grammar of /tools so we don't
// silently break aliases or the no-op-list invariant. The picker path
// (typing "/tools" alone in the TUI) is handled inside the tui package
// and never reaches this parser, so the no-arg form here is the *plain*
// REPL list invocation.
func TestParseToolsCmd(t *testing.T) {
	cases := []struct {
		in      string
		want    ToolsCmdAction
		wantArg string
	}{
		// Empty / list invocations
		{"/tools", ToolsCmdList, ""},
		{"/tools list", ToolsCmdList, ""},
		{"  /tools  ", ToolsCmdList, ""},
		{"/tools ", ToolsCmdList, ""},

		// Reset
		{"/tools reset", ToolsCmdReset, ""},

		// Disable + alias
		{"/tools disable bash", ToolsCmdDisable, "bash"},
		{"/tools off bash", ToolsCmdDisable, "bash"},
		{"/tools disable mcp__github__create_issue", ToolsCmdDisable, "mcp__github__create_issue"},

		// Enable + alias
		{"/tools enable bash", ToolsCmdEnable, "bash"},
		{"/tools on bash", ToolsCmdEnable, "bash"},

		// Bad forms
		{"/tools disable", ToolsCmdNotMatched, ""},
		{"/tools enable", ToolsCmdNotMatched, ""},
		{"/tools weird arg", ToolsCmdNotMatched, ""},
		{"/toolset", ToolsCmdNotMatched, ""},

		// Unrelated
		{"hello", ToolsCmdNotMatched, ""},
		{"/resume foo", ToolsCmdNotMatched, ""},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			act, arg := ParseToolsCmd(c.in)
			if act != c.want || arg != c.wantArg {
				t.Errorf("ParseToolsCmd(%q) = (%d, %q); want (%d, %q)",
					c.in, act, arg, c.want, c.wantArg)
			}
		})
	}
}
