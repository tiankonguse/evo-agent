package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestMgr builds an isolated BgTaskManager so tests don't share global
// state with other tests using GlobalBgTasks.
func newTestMgr() *BgTaskManager {
	return &BgTaskManager{
		tasks:   map[string]*BgTaskRecord{},
		procs:   map[string]*exec.Cmd{},
		cancels: map[string]context.CancelFunc{},
	}
}

// TestBgRunCompletes checks that a fast command runs to completion, the
// directory is archived from todo/ to done/, the record is "completed",
// and Counts/DrainNotifications agree.
func TestBgRunCompletes(t *testing.T) {
	dir := t.TempDir()
	mgr := newTestMgr()
	if err := mgr.Init(dir, "test-sess"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	mgr.DrainNotifications()

	out, err := mgr.Run("echo hello-bgtask")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "started") {
		t.Fatalf("Run output unexpected: %q", out)
	}

	// Wait up to 3s for the task to finish.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if r, _ := mgr.Counts(); r == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	r, c := mgr.Counts()
	if r != 0 || c != 1 {
		t.Fatalf("Counts after completion: got (%d,%d), want (0,1)", r, c)
	}

	notifs := mgr.DrainNotifications()
	if len(notifs) != 1 {
		t.Fatalf("DrainNotifications: got %d, want 1", len(notifs))
	}
	if notifs[0].Status != "completed" {
		t.Fatalf("status: got %q, want completed", notifs[0].Status)
	}
	if !strings.Contains(notifs[0].Preview, "hello-bgtask") {
		t.Fatalf("preview missing payload: %q", notifs[0].Preview)
	}

	doneEntries, _ := os.ReadDir(filepath.Join(dir, "runtime-tasks", "done"))
	if len(doneEntries) != 1 {
		t.Fatalf("done/ entries: got %d, want 1", len(doneEntries))
	}
	// output.log must contain the command's output
	logBytes, err := os.ReadFile(filepath.Join(dir, "runtime-tasks", "done", doneEntries[0].Name(), "output.log"))
	if err != nil {
		t.Fatalf("read output.log: %v", err)
	}
	if !strings.Contains(string(logBytes), "hello-bgtask") {
		t.Fatalf("output.log missing payload: %q", string(logBytes))
	}

	// Second drain is empty.
	if extra := mgr.DrainNotifications(); len(extra) != 0 {
		t.Fatalf("second drain returned %d", len(extra))
	}
}

// TestBgCancelMovesDir spawns a long-running task, cancels it, and verifies
// the directory moves to done/ with status=cancelled.
func TestBgCancelMovesDir(t *testing.T) {
	dir := t.TempDir()
	mgr := newTestMgr()
	if err := mgr.Init(dir, "test-sess-2"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	mgr.DrainNotifications()

	startMsg, err := mgr.Run("sleep 30")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Parse "Background task <id> started: ..." → fields[2] = id
	fields := strings.Fields(startMsg)
	if len(fields) < 3 {
		t.Fatalf("can't parse id from %q", startMsg)
	}
	id := fields[2]
	if len(id) != 8 {
		t.Fatalf("id length: got %d (%q), want 8", len(id), id)
	}

	// Let the goroutine start the subprocess.
	time.Sleep(150 * time.Millisecond)

	out, err := mgr.Cancel(id)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if !strings.Contains(out, "cancelled") {
		t.Fatalf("Cancel output: %q", out)
	}
	// Allow the killed process to exit so its goroutine clears state.
	time.Sleep(300 * time.Millisecond)

	if _, err := os.Stat(filepath.Join(dir, "runtime-tasks", "todo", id)); !os.IsNotExist(err) {
		t.Fatalf("todo/%s should be gone, got err=%v", id, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "runtime-tasks", "done", id)); err != nil {
		t.Fatalf("done/%s should exist: %v", id, err)
	}

	mgr.mu.RLock()
	rec := mgr.tasks[id]
	mgr.mu.RUnlock()
	if rec == nil {
		t.Fatalf("record missing for %s", id)
	}
	if rec.Status != "cancelled" {
		t.Fatalf("status: got %q, want cancelled", rec.Status)
	}
}

// TestFormatBgNotifications checks the wire format that gets injected into
// the LLM as a `<background-results>` user message.
func TestFormatBgNotifications(t *testing.T) {
	got := FormatBgNotifications([]bgNotification{
		{ID: "a1b2c3d4", Status: "completed", Command: "echo ok", Preview: "ok", OutputFile: "rt/done/a1b2c3d4/output.log"},
	})
	if !strings.Contains(got, "<background-results>") || !strings.Contains(got, "</background-results>") {
		t.Fatalf("missing wrapper tags: %q", got)
	}
	if !strings.Contains(got, "[bg:a1b2c3d4] completed: ok") {
		t.Fatalf("missing per-task line: %q", got)
	}
}

// TestParseBgTaskCmd asserts /bgtask flavour parsing — it lives here only
// to keep the test surface together; agent package can also test if useful.
// (left intentionally minimal — agent.ParseBgTaskCmd is in another package
// and pulling it in would create a cycle in tests.)
