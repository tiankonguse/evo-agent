package tools

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseCronValid(t *testing.T) {
	cases := []string{
		"* * * * *",
		"*/5 * * * *",
		"0 9 * * *",
		"30 14 28 2 *",
		"0 9 * * 1-5",
		"0,30 9 * * *",
	}
	for _, c := range cases {
		if parseCron(c) == nil {
			t.Errorf("parseCron(%q) returned nil; expected valid", c)
		}
	}
}

func TestParseCronInvalid(t *testing.T) {
	cases := []string{
		"",
		"only-four * * *",
		"60 * * * *",     // minute out of range
		"* 24 * * *",     // hour out of range
		"* * 32 * *",     // dom out of range
		"* * * 13 *",     // month out of range
		"* * * * 8",      // dow out of range
		"abc * * * *",    // garbage
		"5-3 * * * *",    // reversed range
	}
	for _, c := range cases {
		if parseCron(c) != nil {
			t.Errorf("parseCron(%q) unexpectedly succeeded", c)
		}
	}
}

func TestMatchCron(t *testing.T) {
	// Daily at 9am
	f := parseCron("0 9 * * *")
	if f == nil {
		t.Fatal("parse failed")
	}
	at := time.Date(2026, 6, 3, 9, 0, 0, 0, time.Local)
	if !matchCron(f, at) {
		t.Error("0 9 * * * should match 09:00")
	}
	if matchCron(f, at.Add(time.Hour)) {
		t.Error("0 9 * * * should not match 10:00")
	}

	// Every 5 minutes
	f = parseCron("*/5 * * * *")
	at = time.Date(2026, 6, 3, 12, 25, 0, 0, time.Local)
	if !matchCron(f, at) {
		t.Error("*/5 should match :25")
	}
	if matchCron(f, at.Add(time.Minute)) {
		t.Error("*/5 should not match :26")
	}
}

func TestNextRun(t *testing.T) {
	f := parseCron("0 9 * * *")
	from := time.Date(2026, 6, 3, 8, 30, 0, 0, time.Local)
	next, ok := nextRun(f, from)
	if !ok {
		t.Fatal("nextRun returned false")
	}
	if next.Hour() != 9 || next.Minute() != 0 {
		t.Errorf("nextRun = %v; want 09:00", next)
	}
}

func TestSchedulerCreateListDelete(t *testing.T) {
	dir := t.TempDir()
	s := &CronScheduler{tasks: map[string]*CronTask{}, stopCh: make(chan struct{})}
	if err := s.Init(dir, "test-session"); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()

	id, err := s.Create("0 9 * * *", "say hello", true, true)
	if err != nil {
		t.Fatal(err)
	}
	tasks := s.List()
	if len(tasks) != 1 || tasks[0].ID != id {
		t.Fatalf("expected one task with id %q; got %+v", id, tasks)
	}

	// Verify tasks.json was written.
	tasksFile := filepath.Join(dir, cronTasksDirName, cronTasksFileName)
	if _, err := os.Stat(tasksFile); err != nil {
		t.Errorf("tasks.json not written: %v", err)
	}

	if !s.Delete(id) {
		t.Error("Delete returned false for known id")
	}
	if len(s.List()) != 0 {
		t.Error("expected empty list after delete")
	}
}

func TestSchedulerInvalidCron(t *testing.T) {
	s := &CronScheduler{tasks: map[string]*CronTask{}, stopCh: make(chan struct{})}
	if _, err := s.Create("not-a-cron", "x", true, false); err == nil {
		t.Error("Create accepted invalid cron")
	}
}

func TestSchedulerTickFires(t *testing.T) {
	s := &CronScheduler{tasks: map[string]*CronTask{}, stopCh: make(chan struct{})}
	// "* * * * *" matches every minute.
	id, err := s.Create("* * * * *", "fire-me", true, false)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 3, 12, 34, 0, 0, time.Local)
	s.tickAt(now)
	notifs := s.DrainNotifications()
	if len(notifs) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifs))
	}
	if notifs[0].ID != id || notifs[0].Prompt != "fire-me" {
		t.Errorf("unexpected notification: %+v", notifs[0])
	}

	// Same minute → no re-fire.
	s.tickAt(now.Add(10 * time.Second))
	if got := s.DrainNotifications(); len(got) != 0 {
		t.Errorf("expected no re-fire within same minute, got %d", len(got))
	}
}

func TestSchedulerOneShotDeletesAfterFire(t *testing.T) {
	s := &CronScheduler{tasks: map[string]*CronTask{}, stopCh: make(chan struct{})}
	if _, err := s.Create("* * * * *", "p", false /* recurring */, false); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 3, 5, 5, 0, 0, time.Local)
	s.tickAt(now)
	if got := s.DrainNotifications(); len(got) != 1 {
		t.Fatalf("expected 1 fire; got %d", len(got))
	}
	if len(s.List()) != 0 {
		t.Error("one-shot task should be auto-deleted after fire")
	}
}

// One-shot tasks whose firing window has passed (e.g. agent was offline
// at the scheduled minute) must NOT zombie-fire on the next yearly cron
// match. The FireBy deadline plus cronOneShotGrace catches this.
func TestSchedulerOneShotMissedWindowExpires(t *testing.T) {
	s := &CronScheduler{tasks: map[string]*CronTask{}, stopCh: make(chan struct{})}
	id, err := s.Create("* * * * *", "missed-me", false, false)
	if err != nil {
		t.Fatal(err)
	}
	tasks := s.List()
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task after create; got %d", len(tasks))
	}
	if tasks[0].FireBy == 0 {
		t.Errorf("FireBy not populated on one-shot create")
	}

	// Advance simulated wall clock far past FireBy + grace.
	future := time.UnixMilli(tasks[0].FireBy).
		Add(cronOneShotGrace + 5*time.Minute)
	s.tickAt(future)

	if got := s.DrainNotifications(); len(got) != 0 {
		t.Errorf("missed one-shot fired late: %+v", got)
	}
	if remaining := s.List(); len(remaining) != 0 {
		t.Errorf("expected task %s expired; still present: %+v", id, remaining)
	}
}

// Recurring tasks must NOT have FireBy set — they're bounded by the
// 7-day expiry, not by a per-task deadline.
func TestSchedulerRecurringHasNoFireBy(t *testing.T) {
	s := &CronScheduler{tasks: map[string]*CronTask{}, stopCh: make(chan struct{})}
	if _, err := s.Create("*/5 * * * *", "loop-me", true, false); err != nil {
		t.Fatal(err)
	}
	tasks := s.List()
	if tasks[0].FireBy != 0 {
		t.Errorf("recurring task got FireBy=%d; want 0", tasks[0].FireBy)
	}
}

// Init with empty sessionDir must still start the ticker so session-only
// tasks fire even when persistence is disabled.
func TestSchedulerInitNoSessionDirStillFires(t *testing.T) {
	s := &CronScheduler{tasks: map[string]*CronTask{}, stopCh: make(chan struct{})}
	if err := s.Init("", "no-session-id"); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()

	if _, err := s.Create("* * * * *", "fire-anyway", false, false); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 3, 12, 34, 0, 0, time.Local)
	s.tickAt(now)
	if got := s.DrainNotifications(); len(got) != 1 {
		t.Errorf("session-only task did not fire under empty-dir Init: got %d notifs", len(got))
	}
}

func TestFormatNotifications(t *testing.T) {
	out := FormatCronNotifications([]cronNotification{
		{ID: "abc12345", Prompt: "do the thing", Cron: "0 9 * * *"},
	})
	if out == "" {
		t.Fatal("FormatCronNotifications returned empty")
	}
	if !contains_str(out, "abc12345") || !contains_str(out, "do the thing") {
		t.Errorf("missing fields: %s", out)
	}
}

func contains_str(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
