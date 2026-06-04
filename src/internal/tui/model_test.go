package tui

import "testing"

// TestScrollWindow exercises the windowed-scroll math used by the completion
// dropdown and session picker. The bug it guards against: before the fix,
// `items[:maxShow]` was used unconditionally, so navigating the cursor past
// index `maxShow-1` with ↓ left the highlight invisible.
func TestScrollWindow(t *testing.T) {
	cases := []struct {
		name                       string
		cursor, total, maxShow     int
		wantStart, wantEnd         int
	}{
		// Total fits in window — always show everything from 0.
		{"all fits", 3, 5, 8, 0, 5},
		{"all fits cursor 0", 0, 5, 8, 0, 5},
		// Total > maxShow, cursor inside initial slice — window stays at top.
		{"top-anchored cursor 0", 0, 20, 8, 0, 8},
		{"top-anchored cursor 7", 7, 20, 8, 0, 8},
		// Cursor moves below initial slice — window slides down.
		{"slides for cursor 8", 8, 20, 8, 1, 9},
		{"slides for cursor 10", 10, 20, 8, 3, 11},
		// Cursor at last item — window pinned to the end.
		{"end-anchored", 19, 20, 8, 12, 20},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotStart, gotEnd := scrollWindow(c.cursor, c.total, c.maxShow)
			if gotStart != c.wantStart || gotEnd != c.wantEnd {
				t.Errorf("scrollWindow(%d, %d, %d) = (%d, %d); want (%d, %d)",
					c.cursor, c.total, c.maxShow,
					gotStart, gotEnd, c.wantStart, c.wantEnd)
			}
			// Invariant: cursor must be inside [start, end).
			if gotStart > c.cursor || c.cursor >= gotEnd {
				t.Errorf("cursor %d outside window [%d, %d)", c.cursor, gotStart, gotEnd)
			}
		})
	}
}

// TestIsMeaningfulModelName guards the status bar against placeholder model
// names returned by some OpenAI-compatible servers. Before the guard, a
// gateway echoing "default" in its response.model field would clobber the
// user's configured MODEL_ID in the TUI status bar.
func TestIsMeaningfulModelName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		// Real model names — must pass through.
		{"claude-sonnet-4-5", true},
		{"gpt-4o", true},
		{"gemma4:e2b", true},
		{"deepseek-r1:32b", true},
		{"meta-llama/Meta-Llama-3.1-70B-Instruct", true},

		// Placeholder names — must be rejected.
		{"", false},
		{"default", false},
		{"model", false},
		{"unknown", false},
		{"n/a", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isMeaningfulModelName(c.name); got != c.want {
				t.Errorf("isMeaningfulModelName(%q) = %v; want %v", c.name, got, c.want)
			}
		})
	}
}
