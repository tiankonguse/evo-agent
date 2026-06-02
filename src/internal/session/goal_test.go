package session

import (
	"testing"
)

// TestGoalSetRestoredOnResume verifies that LoadForResume returns
// LoadResult.Goal populated with the latest TypeGoalSet payload when no
// subsequent goal_cleared / goal_achieved record cancels it.
func TestGoalSetRestoredOnResume(t *testing.T) {
	dir := t.TempDir()
	sess, err := NewSession(dir, "v1")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	pid := NewPromptID()
	sess.Recorder.AppendGoalSet(pid, "ship the feature", "2026-06-01-ship")

	res, err := LoadForResume(dir, sess.ID)
	if err != nil {
		t.Fatalf("LoadForResume: %v", err)
	}
	if res.Goal == nil {
		t.Fatalf("Goal was nil after goal_set; want restored")
	}
	if res.Goal.Text != "ship the feature" {
		t.Fatalf("Goal.Text = %q", res.Goal.Text)
	}
	if res.Goal.PlanName != "2026-06-01-ship" {
		t.Fatalf("Goal.PlanName = %q", res.Goal.PlanName)
	}
}

// TestGoalClearedSuppressesRestore — once a goal is cleared, resume must
// not restore the old goal even if a goal_set record exists earlier in the
// transcript.
func TestGoalClearedSuppressesRestore(t *testing.T) {
	dir := t.TempDir()
	sess, err := NewSession(dir, "v1")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	pid := NewPromptID()
	sess.Recorder.AppendGoalSet(pid, "old goal", "old-plan")
	sess.Recorder.AppendGoalCleared(pid)

	res, err := LoadForResume(dir, sess.ID)
	if err != nil {
		t.Fatalf("LoadForResume: %v", err)
	}
	if res.Goal != nil {
		t.Fatalf("Goal should be nil after goal_cleared; got %+v", res.Goal)
	}
}

// TestGoalAchievedSuppressesRestore — same as TestGoalClearedSuppressesRestore
// but via the goal_achieved exit path.
func TestGoalAchievedSuppressesRestore(t *testing.T) {
	dir := t.TempDir()
	sess, err := NewSession(dir, "v1")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	pid := NewPromptID()
	sess.Recorder.AppendGoalSet(pid, "fixed", "p")
	sess.Recorder.AppendGoalAchieved(pid, "all tests green")

	res, err := LoadForResume(dir, sess.ID)
	if err != nil {
		t.Fatalf("LoadForResume: %v", err)
	}
	if res.Goal != nil {
		t.Fatalf("Goal should be nil after goal_achieved; got %+v", res.Goal)
	}
}

// TestLatestGoalSetWins — when multiple goal_set records exist back-to-back
// (e.g. user changed their mind), the most recent one is restored.
func TestLatestGoalSetWins(t *testing.T) {
	dir := t.TempDir()
	sess, err := NewSession(dir, "v1")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	pid := NewPromptID()
	sess.Recorder.AppendGoalSet(pid, "first", "p1")
	sess.Recorder.AppendGoalSet(pid, "second", "p2")

	res, err := LoadForResume(dir, sess.ID)
	if err != nil {
		t.Fatalf("LoadForResume: %v", err)
	}
	if res.Goal == nil || res.Goal.Text != "second" || res.Goal.PlanName != "p2" {
		t.Fatalf("expected second goal to win, got %+v", res.Goal)
	}
}
