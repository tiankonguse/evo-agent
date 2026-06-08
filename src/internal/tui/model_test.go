package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"evo-agent/internal/ui"
)

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

// TestListenForEventsBatchesQueuedEvents proves the burst-handling fix:
// when many events are already queued in the channel before the listener
// fires, listenForEvents must drain them all into a single
// AgentEventBatchMsg instead of returning one event at a time. Without
// this, every event walks through Update→View on its own, the channel
// fills faster than the renderer drains, and Sink backpressure stalls
// the agent — which is the bug we're fixing.
func TestListenForEventsBatchesQueuedEvents(t *testing.T) {
	ch := make(chan ui.Event, 64)
	m := &Model{eventCh: ch}

	// Pre-queue several events so the drain side of the listener has
	// material to scoop up after the blocking first read.
	for i := 0; i < 5; i++ {
		ch <- ui.Event{Kind: ui.EvText, Text: "burst"}
	}

	cmd := m.listenForEvents()
	if cmd == nil {
		t.Fatal("listenForEvents returned nil Cmd")
	}

	done := make(chan tea.Msg, 1) // tea.Msg = interface{}; use any-value channel
	go func() { done <- cmd() }()

	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("listener Cmd did not return")
	}

	batch, ok := msg.(AgentEventBatchMsg)
	if !ok {
		t.Fatalf("Cmd returned %T; want AgentEventBatchMsg", msg)
	}
	if len(batch) != 5 {
		t.Errorf("batch length = %d; want 5 (drain failed to scoop queued events)", len(batch))
	}
}

// TestListenForEventsBlocksUntilEvent guards the other half of the
// contract: when the channel is empty, the listener Cmd must BLOCK,
// not spin or return an empty batch — otherwise Bubble Tea's command
// runner would chew CPU re-arming itself.
func TestListenForEventsBlocksUntilEvent(t *testing.T) {
	ch := make(chan ui.Event, 4)
	m := &Model{eventCh: ch}

	cmd := m.listenForEvents()
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()

	select {
	case msg := <-done:
		t.Fatalf("listener returned %T while channel was empty — should have blocked", msg)
	case <-time.After(30 * time.Millisecond):
		// good — listener is blocked waiting
	}

	ch <- ui.Event{Kind: ui.EvDone}
	select {
	case msg := <-done:
		batch, ok := msg.(AgentEventBatchMsg)
		if !ok {
			t.Fatalf("got %T; want AgentEventBatchMsg", msg)
		}
		if len(batch) != 1 || batch[0].Kind != ui.EvDone {
			t.Errorf("batch = %+v; want exactly one EvDone event", batch)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("listener did not unblock after event was sent")
	}
}
