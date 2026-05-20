package tui

import (
	"evo-agent/internal/ui"
)

// Sink implements ui.EventSink for the Bubble Tea TUI.
// It owns an internal buffered channel that the TUI model reads from.
type Sink struct {
	ch chan ui.Event
}

// NewSink creates a Sink with the given channel buffer size.
func NewSink(buf int) *Sink {
	return &Sink{ch: make(chan ui.Event, buf)}
}

// Emit satisfies ui.EventSink. Non-blocking: drops events on full buffer.
func (s *Sink) Emit(e ui.Event) {
	select {
	case s.ch <- e:
	default:
	}
}

// Chan returns the read-only event channel consumed by the TUI model.
func (s *Sink) Chan() <-chan ui.Event {
	return s.ch
}
