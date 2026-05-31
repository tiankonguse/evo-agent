package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

// TestNewSessionWritesStartRecord verifies a fresh session creates the
// directory tree and writes a session_start record so the file is non-empty
// even before the first user query.
func TestNewSessionWritesStartRecord(t *testing.T) {
	dir := t.TempDir()
	sess, err := NewSession(dir, "test-version")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	expectedPath := filepath.Join(dir, SessionsDirName, sess.ID, MessagesFile)
	if expectedPath != sess.MessagesPath {
		t.Fatalf("messages path mismatch: want %s got %s", expectedPath, sess.MessagesPath)
	}
	data, err := os.ReadFile(sess.MessagesPath)
	if err != nil {
		t.Fatalf("read messages.jsonl: %v", err)
	}
	if !strings.Contains(string(data), `"type":"session_start"`) {
		t.Fatalf("expected session_start record, got: %s", data)
	}
	if !strings.Contains(string(data), `"agent_version":"test-version"`) {
		t.Fatalf("expected agent_version persisted, got: %s", data)
	}

	// meta.json should exist with the same id.
	metaData, err := os.ReadFile(sess.MetaPath)
	if err != nil {
		t.Fatalf("read meta.json: %v", err)
	}
	if !strings.Contains(string(metaData), sess.ID) {
		t.Fatalf("meta.json missing session id: %s", metaData)
	}
}

// TestRoundTrip exercises the core write/read path: a few user/assistant
// turns are appended, then LoadForResume rebuilds the message slice.
func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sess, err := NewSession(dir, "v1")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	pid := NewPromptID()
	sess.Recorder.AppendUser(pid, anthropic.NewUserMessage(anthropic.NewTextBlock("hello there")))
	sess.Recorder.AppendAssistant(pid, anthropic.NewAssistantMessage(anthropic.NewTextBlock("hi back")), 100, 50)
	pid2 := NewPromptID()
	sess.Recorder.AppendUser(pid2, anthropic.NewUserMessage(anthropic.NewTextBlock("how are you?")))
	sess.Recorder.AppendAssistant(pid2, anthropic.NewAssistantMessage(anthropic.NewTextBlock("fine!")), 80, 30)

	res, err := LoadForResume(dir, sess.ID)
	if err != nil {
		t.Fatalf("LoadForResume: %v", err)
	}
	if res.RestoredCount != 4 {
		t.Fatalf("expected 4 restored messages, got %d", res.RestoredCount)
	}
	if res.HasCompactedAt {
		t.Fatalf("did not expect a compact boundary in this test")
	}
	if len(res.Messages) != 4 {
		t.Fatalf("expected 4 messages in result, got %d", len(res.Messages))
	}
	first := res.Messages[0]
	if first.Role != anthropic.MessageParamRoleUser {
		t.Fatalf("first message role: want user, got %s", first.Role)
	}

	// meta.json should reflect cumulative tokens + first prompt.
	entries := ListSessions(dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 session listed, got %d", len(entries))
	}
	got := entries[0]
	if got.ID != sess.ID {
		t.Fatalf("listed wrong session id: %s", got.ID)
	}
	if got.TotalTokens() != 100+50+80+30 {
		t.Fatalf("token total mismatch: want %d got %d", 100+50+80+30, got.TotalTokens())
	}
	if got.FirstPrompt != "hello there" {
		t.Fatalf("first prompt mismatch: want 'hello there', got %q", got.FirstPrompt)
	}
}

// TestCompactBoundaryClipsPriorMessages verifies the resume rule that
// messages older than the most recent compact_boundary are dropped, and that
// the boundary's summary is surfaced as a synthetic user message.
func TestCompactBoundaryClipsPriorMessages(t *testing.T) {
	dir := t.TempDir()
	sess, err := NewSession(dir, "v1")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	// Pre-boundary turns — these must not be returned by LoadForResume.
	pid1 := NewPromptID()
	sess.Recorder.AppendUser(pid1, anthropic.NewUserMessage(anthropic.NewTextBlock("old question 1")))
	sess.Recorder.AppendAssistant(pid1, anthropic.NewAssistantMessage(anthropic.NewTextBlock("old answer 1")), 10, 10)

	// Compact boundary with a known summary.
	sess.Recorder.AppendCompactBoundary(pid1, "the gist of older work", 1)

	// Post-boundary turn — this must be restored.
	pid2 := NewPromptID()
	sess.Recorder.AppendUser(pid2, anthropic.NewUserMessage(anthropic.NewTextBlock("fresh question")))
	sess.Recorder.AppendAssistant(pid2, anthropic.NewAssistantMessage(anthropic.NewTextBlock("fresh answer")), 20, 20)

	res, err := LoadForResume(dir, sess.ID)
	if err != nil {
		t.Fatalf("LoadForResume: %v", err)
	}
	if !res.HasCompactedAt {
		t.Fatalf("expected HasCompactedAt=true")
	}
	if res.Summary != "the gist of older work" {
		t.Fatalf("summary mismatch: want 'the gist of older work', got %q", res.Summary)
	}
	if res.RestoredCount != 2 {
		t.Fatalf("expected only 2 real messages restored (post-boundary), got %d", res.RestoredCount)
	}
	// Total messages = 1 synthetic summary + 2 restored real messages.
	if len(res.Messages) != 3 {
		t.Fatalf("expected 3 messages (1 summary + 2 real), got %d", len(res.Messages))
	}
	// The first message must contain the wrapped summary so the model knows
	// it is reading a digest.
	first := res.Messages[0]
	found := false
	for _, blk := range first.Content {
		if blk.OfText != nil && strings.Contains(blk.OfText.Text, "previous-conversation-summary") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("synthetic first message did not wrap the summary in <previous-conversation-summary>")
	}
}

// TestSubagentSidechain verifies a subagent transcript is created in the
// session's subagent/ directory and the parent transcript records start/end
// markers — without replaying the subagent body in the parent.
func TestSubagentSidechain(t *testing.T) {
	dir := t.TempDir()
	sess, err := NewSession(dir, "v1")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	pid := NewPromptID()
	sub, err := NewSubagentRecorder(sess, "exploration")
	if err != nil {
		t.Fatalf("NewSubagentRecorder: %v", err)
	}
	if !strings.Contains(sub.Filename, "exploration") {
		t.Fatalf("subagent filename should slugify the name: %s", sub.Filename)
	}

	sess.Recorder.AppendSubagentStart(pid, "exploration", sub.Filename)
	sub.AppendUser(pid, anthropic.NewUserMessage(anthropic.NewTextBlock("explore this")))
	sub.AppendAssistant(pid, anthropic.NewAssistantMessage(anthropic.NewTextBlock("found nothing")), 5, 5)
	sess.Recorder.AppendSubagentEnd(pid, "exploration", sub.Filename, "found nothing")

	// Parent transcript should mention start/end and the subagent path.
	parent, err := os.ReadFile(sess.MessagesPath)
	if err != nil {
		t.Fatalf("read parent: %v", err)
	}
	if !strings.Contains(string(parent), `"type":"subagent_start"`) {
		t.Fatalf("parent missing subagent_start: %s", parent)
	}
	if !strings.Contains(string(parent), `"type":"subagent_end"`) {
		t.Fatalf("parent missing subagent_end: %s", parent)
	}
	if !strings.Contains(string(parent), sub.Filename) {
		t.Fatalf("parent did not reference subagent file %s", sub.Filename)
	}

	// Subagent file must exist with its own messages.
	subData, err := os.ReadFile(sub.Path)
	if err != nil {
		t.Fatalf("read subagent file: %v", err)
	}
	if !strings.Contains(string(subData), "explore this") {
		t.Fatalf("subagent file missing user message: %s", subData)
	}

	// LoadForResume should surface the subagent's final result as a
	// <subagent-result> block in the resumed transcript.
	res, err := LoadForResume(dir, sess.ID)
	if err != nil {
		t.Fatalf("LoadForResume: %v", err)
	}
	hasResult := false
	for _, m := range res.Messages {
		for _, blk := range m.Content {
			if blk.OfText != nil && strings.Contains(blk.OfText.Text, "subagent-result") {
				hasResult = true
			}
		}
	}
	if !hasResult {
		t.Fatalf("expected <subagent-result> tag in resumed transcript")
	}
}

// TestResumeMarker verifies a session that was resumed records its
// provenance via a resume_marker line.
func TestResumeMarker(t *testing.T) {
	dir := t.TempDir()
	sess, err := NewSession(dir, "v1")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	sess.Recorder.AppendResumeMarker("OLD-SID", 5)
	data, _ := os.ReadFile(sess.MessagesPath)
	if !strings.Contains(string(data), `"from_session_id":"OLD-SID"`) {
		t.Fatalf("expected resume_marker with from_session_id, got: %s", data)
	}
}

// TestSlugify guards against accidental regressions in filename safety.
func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"":                "",
		"task":            "task",
		"Search Code":     "search-code",
		"foo/bar.baz":     "foo-bar-baz",
		"   trim   me   ": "trim-me",
		"WAY-too-long-to-keep-its-full-length-OMG": "way-too-long-to-keep-its",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSessionIDIsMillisPlusUUID verifies new session ids and subagent
// filenames are prefixed with a parseable unix-millisecond timestamp at the
// time of creation, separated by '_' from the UUID suffix.
//
// Record fields (Record.Timestamp) keep the human-readable ISO format —
// see TestRecordTimestampIsISO.
func TestSessionIDIsMillisPlusUUID(t *testing.T) {
	before := time.Now().UnixMilli()
	id := NewSessionID()
	after := time.Now().UnixMilli()

	if !strings.Contains(id, "_") {
		t.Fatalf("session id must contain '_' separator, got %q", id)
	}
	parts := strings.SplitN(id, "_", 2)
	if len(parts) != 2 {
		t.Fatalf("session id should split into 2 parts on '_', got %q", id)
	}
	for _, r := range parts[0] {
		if r < '0' || r > '9' {
			t.Fatalf("session id timestamp prefix must be digits only, got %q", parts[0])
		}
	}
	if len(parts[1]) != 8 {
		t.Fatalf("session id uuid suffix should be 8 hex chars, got %q (len=%d)", parts[1], len(parts[1]))
	}

	ms := ParseLeadingTimestampMs(id)
	if ms < before || ms > after {
		t.Fatalf("ParseLeadingTimestampMs(%q) = %d, expected within [%d, %d]", id, ms, before, after)
	}

	// Subagent filename: "<unix_ms>_<slug>_<8 hex>.jsonl"
	fn := NewSubagentFilename("Search Code")
	if !strings.HasSuffix(fn, ".jsonl") {
		t.Fatalf("subagent filename must end with .jsonl, got %q", fn)
	}
	if !strings.Contains(fn, "_search-code_") {
		t.Fatalf("subagent filename should embed slugified name, got %q", fn)
	}
	if got := ParseLeadingTimestampMs(fn); got < before || got > after {
		t.Fatalf("ParseLeadingTimestampMs(%q) = %d, expected within [%d, %d]", fn, got, before, after)
	}
}

// TestRecordTimestampIsISO verifies that records written to messages.jsonl
// carry an ISO-8601 UTC string in the `timestamp` field, e.g.
// `2026-05-31T11:29:18.720Z`.
func TestRecordTimestampIsISO(t *testing.T) {
	dir := t.TempDir()
	sess, err := NewSession(dir, "v1")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	pid := NewPromptID()
	sess.Recorder.AppendUser(pid, anthropic.NewUserMessage(anthropic.NewTextBlock("hi")))

	data, err := os.ReadFile(sess.MessagesPath)
	if err != nil {
		t.Fatalf("read messages.jsonl: %v", err)
	}
	// New field name is "timestamp" with ISO content.
	if !strings.Contains(string(data), `"timestamp":"`) {
		t.Fatalf("expected `timestamp` field, got: %s", data)
	}
	// Old field name must be gone.
	if strings.Contains(string(data), `"timestamp_ms":`) {
		t.Fatalf("legacy `timestamp_ms` field must be removed, got: %s", data)
	}
	// Spot-check one ISO value: it should match `YYYY-MM-DDTHH:MM:SS.mmmZ`.
	// Try to parse the first record back and re-format its timestamp.
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if rec.Timestamp == "" {
			t.Fatalf("record has empty timestamp: %s", line)
		}
		// Spot-check ISO format: must be local-time wall clock with a
		// numeric offset (`+08:00`), not UTC `Z`. Try parsing with the
		// canonical layout used by NowISO.
		parsed, err := time.Parse("2006-01-02T15:04:05.000-07:00", rec.Timestamp)
		if err != nil {
			t.Fatalf("timestamp not ISO-8601 with numeric offset: %q (%v)", rec.Timestamp, err)
		}
		// Round-trip: formatting parsed back should equal the original.
		if got := parsed.Format("2006-01-02T15:04:05.000-07:00"); got != rec.Timestamp {
			t.Fatalf("round-trip mismatch: %q vs %q", got, rec.Timestamp)
		}
	}
}
