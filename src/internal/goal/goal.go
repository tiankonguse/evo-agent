// Package goal manages a session-scoped completion condition that drives
// the agent loop to keep working until satisfied.
//
// A "goal" is a free-form natural-language condition the user supplies via
// /goal <condition>. After every agent turn that finishes with no tool
// calls, the loop consults an evaluator (see evaluator.go) and, if the
// condition is not met, synthesizes a continuation prompt and runs again.
//
// State is held in process-memory and mirrored to the session transcript so
// it can be restored on --resume. This file only exposes the in-memory
// Manager; the persistence side is handled by internal/session.
package goal

import (
	"sync"
	"time"
)

// DefaultMaxIter caps the number of evaluator-driven continuations per
// /goal so a misjudged condition can't burn unbounded LLM calls.
// Aligned with subagentMaxTurns (30) for consistency.
const DefaultMaxIter = 30

// State is a snapshot of an active goal.
type State struct {
	Text     string    // user's completion condition
	PlanName string    // associated persistent plan directory name (.evo-agent/tasks/todo/<name>)
	Iter     int       // evaluator-driven continuations consumed so far
	MaxIter  int       // upper bound (DefaultMaxIter unless overridden)
	SetAt    time.Time // when /goal set fired
}

// Manager is the singleton holding the current session's goal.
//
// All methods are safe for concurrent use; the agent loop and the slash
// dispatcher both touch it.
type Manager struct {
	mu  sync.RWMutex
	cur *State
}

// Global is the process-wide goal manager. The agent loop reads from it via
// Snapshot(); slash-command handlers in main.go mutate it via Set/Clear.
var Global = &Manager{}

// Set installs a new active goal, resetting the iteration counter.
// Returns the new state for callers that want to log it.
func (m *Manager) Set(text, planName string) *State {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cur = &State{
		Text:     text,
		PlanName: planName,
		Iter:     0,
		MaxIter:  DefaultMaxIter,
		SetAt:    time.Now(),
	}
	// Return a copy so callers can't mutate live state.
	cp := *m.cur
	return &cp
}

// Clear removes the active goal. Returns the previous state (or nil if none).
func (m *Manager) Clear() *State {
	m.mu.Lock()
	defer m.mu.Unlock()
	prev := m.cur
	m.cur = nil
	if prev == nil {
		return nil
	}
	cp := *prev
	return &cp
}

// Achieve clears the goal because the evaluator confirmed completion.
// Behaviorally identical to Clear today; kept distinct so future logic can
// differentiate (e.g. for stats / logging). Returns the previous state.
func (m *Manager) Achieve() *State {
	return m.Clear()
}

// Snapshot returns a copy of the current state, or nil if no goal is set.
// Safe for the agent loop to read every turn.
func (m *Manager) Snapshot() *State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cur == nil {
		return nil
	}
	cp := *m.cur
	return &cp
}

// IncIter atomically increments the iteration counter and returns the new
// value. Returns 0 if no goal is active.
func (m *Manager) IncIter() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cur == nil {
		return 0
	}
	m.cur.Iter++
	return m.cur.Iter
}

// Active reports whether a goal is currently set.
func (m *Manager) Active() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cur != nil
}

// ActiveGoalText satisfies prompt.GoalProvider so the system-prompt builder
// can include the active goal in every turn. Returns "" when no goal is set.
func (m *Manager) ActiveGoalText() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cur == nil {
		return ""
	}
	return m.cur.Text
}
