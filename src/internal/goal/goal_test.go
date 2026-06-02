package goal

import (
	"sync"
	"testing"
)

func TestManager_LifecycleSetSnapshotIncIterClear(t *testing.T) {
	m := &Manager{}

	if m.Active() {
		t.Fatalf("fresh manager should not be active")
	}
	if m.Snapshot() != nil {
		t.Fatalf("fresh manager Snapshot should be nil")
	}
	if got := m.IncIter(); got != 0 {
		t.Fatalf("IncIter on inactive should return 0, got %d", got)
	}

	st := m.Set("ship the feature", "2026-06-01-ship")
	if st == nil || st.Text != "ship the feature" || st.PlanName != "2026-06-01-ship" {
		t.Fatalf("Set returned unexpected state: %+v", st)
	}
	if st.Iter != 0 {
		t.Fatalf("new state Iter must be 0, got %d", st.Iter)
	}
	if st.MaxIter != DefaultMaxIter {
		t.Fatalf("MaxIter = %d, want %d", st.MaxIter, DefaultMaxIter)
	}

	if !m.Active() {
		t.Fatalf("Active should be true after Set")
	}

	if got := m.IncIter(); got != 1 {
		t.Fatalf("IncIter #1 = %d, want 1", got)
	}
	if got := m.IncIter(); got != 2 {
		t.Fatalf("IncIter #2 = %d, want 2", got)
	}
	if snap := m.Snapshot(); snap == nil || snap.Iter != 2 {
		t.Fatalf("Snapshot.Iter = %v, want 2", snap)
	}

	if got := m.ActiveGoalText(); got != "ship the feature" {
		t.Fatalf("ActiveGoalText = %q", got)
	}

	prev := m.Clear()
	if prev == nil || prev.Iter != 2 {
		t.Fatalf("Clear should return prev with Iter=2, got %+v", prev)
	}
	if m.Active() {
		t.Fatalf("Active should be false after Clear")
	}
	if got := m.ActiveGoalText(); got != "" {
		t.Fatalf("ActiveGoalText after clear = %q", got)
	}
}

func TestManager_SetResetsIter(t *testing.T) {
	m := &Manager{}
	m.Set("first", "")
	m.IncIter()
	m.IncIter()
	m.IncIter()

	st := m.Set("second", "")
	if st.Iter != 0 {
		t.Fatalf("Set must reset Iter to 0, got %d", st.Iter)
	}
}

func TestManager_AchieveSameAsClear(t *testing.T) {
	m := &Manager{}
	m.Set("done?", "")
	m.IncIter()
	prev := m.Achieve()
	if prev == nil || prev.Iter != 1 {
		t.Fatalf("Achieve should return prev state with Iter=1, got %+v", prev)
	}
	if m.Active() {
		t.Fatalf("Active should be false after Achieve")
	}
}

func TestManager_SnapshotIsCopy(t *testing.T) {
	m := &Manager{}
	m.Set("watch me", "")
	snap := m.Snapshot()
	if snap == nil {
		t.Fatalf("snapshot was nil")
	}
	snap.Iter = 9999 // mutate copy
	if got := m.Snapshot(); got.Iter != 0 {
		t.Fatalf("internal state was mutated by external snapshot edit; Iter=%d", got.Iter)
	}
}

func TestManager_ConcurrentIncIter(t *testing.T) {
	m := &Manager{}
	m.Set("race", "")

	const goroutines = 16
	const perG = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				m.IncIter()
			}
		}()
	}
	wg.Wait()

	snap := m.Snapshot()
	if snap == nil {
		t.Fatalf("snapshot was nil after concurrent increments")
	}
	if want := goroutines * perG; snap.Iter != want {
		t.Fatalf("Iter = %d, want %d (lost increments)", snap.Iter, want)
	}
}
