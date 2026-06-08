package tui

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"evo-agent/internal/ui"
)

// TestSinkBlocksWhenFull is the core regression guard for the original
// "TUI stuck not refreshing" bug. The previous Sink dropped events on a full
// buffer (non-blocking select with `default`), so when the agent emitted
// faster than the TUI consumed, EvDone could disappear and `m.busy` would
// stay true forever. The new contract is: Emit blocks until the receiver
// catches up. We prove that here by filling the buffer, then asserting the
// next Emit doesn't return until something is read.
func TestSinkBlocksWhenFull(t *testing.T) {
	const buf = 4
	s := NewSink(buf)

	// Fill the buffer to capacity.
	for i := 0; i < buf; i++ {
		s.Emit(ui.Event{Kind: ui.EvSystem, Text: "fill"})
	}

	emitDone := make(chan struct{})
	go func() {
		s.Emit(ui.Event{Kind: ui.EvDone})
		close(emitDone)
	}()

	// The producer must not finish until we drain.
	select {
	case <-emitDone:
		t.Fatal("Emit returned without backpressure when buffer was full — events would be dropped")
	case <-time.After(50 * time.Millisecond):
		// good — producer is blocked
	}

	// Drain one slot; producer should now be able to land its event.
	<-s.Chan()
	select {
	case <-emitDone:
		// good
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Emit did not unblock after the receiver drained an event")
	}
}

// TestSinkCloseUnblocksProducer guards against a deadlock at process
// shutdown: if the TUI program exits while the agent goroutine is still
// blocked in Emit, the agent must be released so it can finish cleanly.
// Close() flips a done channel; Emit's blocking select includes that case
// and bumps the dropped counter so we can surface drops at exit time.
func TestSinkCloseUnblocksProducer(t *testing.T) {
	s := NewSink(1)
	s.Emit(ui.Event{Kind: ui.EvSystem, Text: "fill"}) // buffer full

	emitDone := make(chan struct{})
	go func() {
		s.Emit(ui.Event{Kind: ui.EvDone})
		close(emitDone)
	}()

	// Confirm the producer is blocked before we close.
	select {
	case <-emitDone:
		t.Fatal("Emit returned before Close — buffer was supposed to be full")
	case <-time.After(20 * time.Millisecond):
	}

	s.Close()
	select {
	case <-emitDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Close did not unblock blocked Emit")
	}

	if got := s.Dropped(); got != 1 {
		t.Errorf("Dropped() = %d; want 1 after one event was dropped on close", got)
	}
}

// TestSinkConcurrentEmitDoesNotDrop hammers the sink from many goroutines
// while a single consumer drains. Every emitted event must arrive — the
// blocking-write contract is what guarantees this. Catches regressions
// where someone "optimizes" Emit back to non-blocking semantics.
func TestSinkConcurrentEmitDoesNotDrop(t *testing.T) {
	const producers = 8
	const perProducer = 100
	s := NewSink(16) // intentionally small so backpressure is exercised

	var got int64
	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		for range s.Chan() {
			atomic.AddInt64(&got, 1)
			if atomic.LoadInt64(&got) == producers*perProducer {
				return
			}
		}
	}()

	var wg sync.WaitGroup
	for p := 0; p < producers; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perProducer; i++ {
				s.Emit(ui.Event{Kind: ui.EvText, Text: "x"})
			}
		}()
	}
	wg.Wait()

	select {
	case <-consumerDone:
	case <-time.After(2 * time.Second):
		t.Fatalf("consumer did not drain all events; got=%d want=%d", atomic.LoadInt64(&got), producers*perProducer)
	}
	if got := atomic.LoadInt64(&got); got != producers*perProducer {
		t.Errorf("delivered=%d; want=%d (lossless contract violated)", got, producers*perProducer)
	}
}
