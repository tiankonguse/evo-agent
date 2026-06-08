package tui

import (
	"sync/atomic"

	"evo-agent/internal/ui"
)

// Sink implements ui.EventSink for the Bubble Tea TUI.
//
// Emit is BLOCKING by design. The previous non-blocking emit (with a 512-event
// buffer) silently dropped events whenever the agent goroutine — or any of
// the new event sources added in v0.17–v0.19 (background tasks, cron,
// teammates) — out-paced the TUI's per-event Update→View cycle. The most
// damaging dropped event was `EvDone`: when it was dropped the TUI's
// `m.busy` flag stayed `true` forever and the user saw "stuck" output even
// though the agent loop kept running. Other dropped events (EvText,
// EvToolResult) produced silently incomplete transcripts.
//
// Backpressure is the right semantics here: the agent's hot path is the LLM
// API round-trip, which dwarfs a TUI Update tick by orders of magnitude. A
// few microseconds of channel backpressure when the buffer briefly fills is
// invisible to the user; a permanently stuck textarea is not.
//
// We still keep a generous channel buffer (so normal bursts don't block at
// all) and an atomic dropped counter as a safety valve when the receiver is
// closed mid-flight (TUI quit). That counter is exported via Dropped() for
// observability — `cmd.go` writes a single line to stderr at shutdown if it
// is non-zero, which is enough to confirm whether a future hang was caused
// by drops without polluting stdout during the live session.
type Sink struct {
	ch      chan ui.Event
	done    chan struct{} // closed by Close(); makes Emit non-blocking after shutdown
	dropped uint64        // atomic; incremented when Emit had to drop because the sink was closed
}

// NewSink creates a Sink with the given channel buffer size.
//
// The buffer is sized for "comfortably absorb a turn's worth of events
// without blocking" — recent feature additions push roughly 10–30 events per
// LLM round (PrintTokens, PrintText, N×PrintToolCall, N×PrintToolResult,
// optional thinking blocks, plus EvBgTasks/EvTeam/EvGoal lifecycle pings),
// and several teammates can be co-emitting through this same sink.
func NewSink(buf int) *Sink {
	return &Sink{
		ch:   make(chan ui.Event, buf),
		done: make(chan struct{}),
	}
}

// Emit satisfies ui.EventSink. It tries a non-blocking send first (the
// common case when the TUI is keeping up); only on full buffer does it fall
// back to a blocking send so the producer is held until the TUI catches up.
//
// If the sink has been closed (TUI exited) we drop the event and bump the
// dropped counter — without this guard the agent goroutine would deadlock
// on shutdown.
func (s *Sink) Emit(e ui.Event) {
	// Fast path: room in the buffer.
	select {
	case s.ch <- e:
		return
	default:
	}

	// Slow path: backpressure. Block until either the TUI consumes one
	// event or the sink is closed.
	select {
	case s.ch <- e:
	case <-s.done:
		atomic.AddUint64(&s.dropped, 1)
	}
}

// Chan returns the read-only event channel consumed by the TUI model.
func (s *Sink) Chan() <-chan ui.Event {
	return s.ch
}

// Close marks the sink as shut down. After Close, Emit returns immediately
// (incrementing the dropped counter) instead of blocking forever. Idempotent.
func (s *Sink) Close() {
	select {
	case <-s.done:
		// already closed
	default:
		close(s.done)
	}
}

// Dropped returns the count of events that were discarded because Emit was
// called after Close. Used at process shutdown to surface a single warning
// line if the count is non-zero.
func (s *Sink) Dropped() uint64 {
	return atomic.LoadUint64(&s.dropped)
}
