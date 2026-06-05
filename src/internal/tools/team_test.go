package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

// stubRunner is the test-only TeammateRunner. By default it returns a
// single text block + end_turn so the goroutine settles to idle on the
// first wake. Behaviour can be overridden per-test via the field.
type stubRunner struct {
	mu     []anthropic.MessageParam // captured messages from the most recent call
	respFn func(call int, messages []anthropic.MessageParam) (*anthropic.Message, error)
	calls  int
}

func (s *stubRunner) Run(ctx context.Context, _ string, msgs []anthropic.MessageParam, _ []anthropic.ToolUnionParam) (*anthropic.Message, error) {
	s.calls++
	s.mu = msgs
	if s.respFn != nil {
		return s.respFn(s.calls, msgs)
	}
	// Default: one text block, no tool calls → triggers idle.
	return synthMsg("done"), nil
}

// synthMsg builds a real *anthropic.Message via UnmarshalJSON so the SDK
// JSON-cache fields are populated (the team goroutine calls .ToParam()
// on the response, which depends on those internals).
func synthMsg(text string) *anthropic.Message {
	body := map[string]any{
		"id":    "msg_test",
		"type":  "message",
		"role":  "assistant",
		"model": "test",
		"content": []map[string]any{
			{"type": "text", "text": text},
		},
		"stop_reason": "end_turn",
		"usage":       map[string]any{"input_tokens": 1, "output_tokens": 1},
	}
	raw, _ := json.Marshal(body)
	var m anthropic.Message
	_ = m.UnmarshalJSON(raw)
	return &m
}

// withTeam wires up a fresh TeamManager rooted at a per-test tmpdir under
// the project's ./.test-tmp/ directory (per project critical rules: no
// fs ops outside the project root). Registers the stubRunner.
func withTeam(t *testing.T) (*TeamManager, *stubRunner, string) {
	t.Helper()
	// Find project root by walking up from CWD looking for go.mod. The
	// test binary's CWD is the package dir (internal/tools), so go.mod
	// is two levels up (src/).
	root := projectRoot(t)
	base := filepath.Join(root, ".test-tmp", "team-"+timestampSuffix())
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", base, err)
	}
	t.Cleanup(func() {
		// Best-effort cleanup; user can decide to keep them.
		_ = os.RemoveAll(base)
	})

	mgr := &TeamManager{}
	if err := mgr.Init(base); err != nil {
		t.Fatalf("Init: %v", err)
	}
	prev := teammateRunner
	stub := &stubRunner{}
	teammateRunner = stub.Run
	t.Cleanup(func() {
		mgr.Stop()
		teammateRunner = prev
	})
	return mgr, stub, base
}

func projectRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	d := cwd
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			// go.mod is in src/; project root is one above.
			return filepath.Dir(d)
		}
		d = filepath.Dir(d)
	}
	t.Fatalf("could not locate project root from %s", cwd)
	return ""
}

func timestampSuffix() string {
	return time.Now().Format("20060102-150405.000")
}

// waitFor polls a predicate up to timeout. Mostly used to wait for the
// goroutine to drain its inbox and settle to idle.
func waitFor(t *testing.T, timeout time.Duration, what string, pred func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pred() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("waitFor(%q) timed out after %s", what, timeout)
}

// ── Test cases ──────────────────────────────────────────────────────────────

func TestSpawnIdleStateMachine(t *testing.T) {
	mgr, _, base := withTeam(t)

	if _, err := mgr.Spawn("alice", "coder", "say hi"); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// Goroutine should drain inbox, run runner once, mark idle.
	waitFor(t, 2*time.Second, "alice idle", func() bool {
		_, snaps := mgr.Snapshot()
		for _, s := range snaps {
			if s.Name == "alice" && s.Status == "idle" {
				return true
			}
		}
		return false
	})

	// History file should have at least 2 lines: the inbox user msg and
	// the assistant reply.
	histPath := filepath.Join(base, ".evo-agent/team", "history", "alice.jsonl")
	data, err := os.ReadFile(histPath)
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("history is empty")
	}
}

func TestInboxRoundTrip(t *testing.T) {
	mgr, _, _ := withTeam(t)

	// Use a recipient name that won't trigger a goroutine — register a
	// member-shaped record manually so SendMessage's recipient check
	// passes, then read its inbox without ever spawning.
	mgr.cfg.Members = append(mgr.cfg.Members, &teammateRecord{
		Name:   "bob",
		Role:   "tester",
		Status: "idle",
	})

	if _, err := mgr.SendMessage("lead", "bob", "hello bob", "message", nil); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if _, err := mgr.SendMessage("lead", "bob", "second", "broadcast", nil); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	got, err := mgr.ReadInbox("bob")
	if err != nil {
		t.Fatalf("ReadInbox: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 messages, got %d", len(got))
	}
	if got[0].Content != "hello bob" || got[0].Type != "message" {
		t.Errorf("unexpected first message: %+v", got[0])
	}
	if got[1].Type != "broadcast" {
		t.Errorf("want second type=broadcast, got %q", got[1].Type)
	}

	// Second drain returns nothing.
	more, err := mgr.ReadInbox("bob")
	if err != nil {
		t.Fatalf("ReadInbox 2: %v", err)
	}
	if len(more) != 0 {
		t.Fatalf("want 0 messages on second drain, got %d", len(more))
	}
}

func TestSendMessageInvalidRecipient(t *testing.T) {
	mgr, _, _ := withTeam(t)

	if _, err := mgr.SendMessage("lead", "ghost", "hi", "message", nil); err == nil {
		t.Fatalf("expected error for unknown teammate")
	}
}

func TestSendMessageInvalidType(t *testing.T) {
	mgr, _, _ := withTeam(t)
	mgr.cfg.Members = append(mgr.cfg.Members, &teammateRecord{
		Name: "x", Role: "r", Status: "idle",
	})
	if _, err := mgr.SendMessage("lead", "x", "hi", "bogus", nil); err == nil {
		t.Fatalf("expected error for invalid msg_type")
	}
}

func TestShutdownPersistsRecord(t *testing.T) {
	mgr, _, _ := withTeam(t)

	if _, err := mgr.Spawn("carol", "qa", "kickoff"); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	waitFor(t, 2*time.Second, "carol idle", func() bool {
		_, snaps := mgr.Snapshot()
		for _, s := range snaps {
			if s.Name == "carol" && s.Status == "idle" {
				return true
			}
		}
		return false
	})

	if _, err := mgr.Shutdown("carol"); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	_, snaps := mgr.Snapshot()
	found := false
	for _, s := range snaps {
		if s.Name == "carol" {
			found = true
			if s.Status != "shutdown" {
				t.Errorf("carol status = %q, want shutdown", s.Status)
			}
		}
	}
	if !found {
		t.Errorf("carol record disappeared after Shutdown")
	}
}

func TestSpawnCapEnforced(t *testing.T) {
	mgr, _, _ := withTeam(t)

	for i := 0; i < teamMaxMembers; i++ {
		name := "t" + string(rune('a'+i))
		if _, err := mgr.Spawn(name, "role", "p"); err != nil {
			t.Fatalf("Spawn %s: %v", name, err)
		}
	}
	if _, err := mgr.Spawn("overflow", "role", "p"); err == nil {
		t.Fatalf("expected cap error on %d-th spawn", teamMaxMembers+1)
	}
}

func TestNotificationQueueDrains(t *testing.T) {
	mgr, _, _ := withTeam(t)
	if _, err := mgr.Spawn("dave", "coder", "kickoff"); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	waitFor(t, 2*time.Second, "dave idle", func() bool {
		_, snaps := mgr.Snapshot()
		for _, s := range snaps {
			if s.Name == "dave" && s.Status == "idle" {
				return true
			}
		}
		return false
	})

	notifs := mgr.DrainNotifications()
	if len(notifs) == 0 {
		t.Fatalf("expected at least 1 notification (idle), got 0")
	}
	// Second drain returns nothing.
	if more := mgr.DrainNotifications(); len(more) != 0 {
		t.Fatalf("expected empty drain, got %d", len(more))
	}
}

func TestValidateTeammateName(t *testing.T) {
	cases := []struct {
		name string
		ok   bool
	}{
		{"alice", true},
		{"alice-1", true},
		{"alice_2", true},
		{"", false},
		{"lead", false},
		{"has space", false},
		{"with/slash", false},
	}
	for _, c := range cases {
		err := validateTeammateName(c.name)
		if (err == nil) != c.ok {
			t.Errorf("validateTeammateName(%q) ok=%v err=%v", c.name, c.ok, err)
		}
	}
}

func TestFormatTeamInbox(t *testing.T) {
	if got := FormatTeamInbox(nil); got != "" {
		t.Errorf("empty input: want \"\", got %q", got)
	}
	body := FormatTeamInbox([]InboxMessage{
		{Type: "message", From: "alice", Content: "hi"},
	})
	if body == "" {
		t.Fatalf("expected non-empty body")
	}
	if !startsWith(body, "<team-inbox>") {
		t.Errorf("expected wrapping tag, got %q", body)
	}
}

func TestFormatTeamNotifications(t *testing.T) {
	if got := FormatTeamNotifications(nil); got != "" {
		t.Errorf("empty input: want \"\", got %q", got)
	}
	body := FormatTeamNotifications([]TeamNotification{
		{Name: "alice", Role: "coder", Status: "idle", Reason: "task done"},
	})
	if body == "" || !startsWith(body, "<team-notifications>") {
		t.Errorf("unexpected: %q", body)
	}
}

func startsWith(s, prefix string) bool { return len(s) >= len(prefix) && s[:len(prefix)] == prefix }

func TestSpawnRequiresRunner(t *testing.T) {
	prev := teammateRunner
	teammateRunner = nil
	defer func() { teammateRunner = prev }()

	root := projectRoot(t)
	base := filepath.Join(root, ".test-tmp", "team-norunner-"+timestampSuffix())
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	defer os.RemoveAll(base)

	mgr := &TeamManager{}
	if err := mgr.Init(base); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := mgr.Spawn("eve", "coder", "p"); err == nil {
		t.Fatalf("expected error when runner is unregistered")
	}
}
