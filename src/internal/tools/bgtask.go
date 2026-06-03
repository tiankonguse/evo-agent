package tools

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"evo-agent/internal/ui"
)

// bgtask.go — background-task manager + 4 model-invocable tools
// (bg_run / bg_check / bg_list / bg_cancel).
//
// Model: long-running shell commands run in their own goroutine + process
// group; the manager drains completion notifications into the agent loop
// so the model sees `<background-results>` as a synthetic user message at
// the top of the next turn.
//
// Storage layout (per session):
//
//   .evo-agent/sessions/<sessID>/runtime-tasks/
//     todo/<taskID>/{task.json,output.log}      ← still-running tasks
//     done/<taskID>/{task.json,output.log}      ← completed/cancelled/timeout/error
//
// State transitions move the entire <taskID>/ directory via os.Rename
// (atomic on every POSIX filesystem) so a task is never half-cataloged.

// ── Constants ───────────────────────────────────────────────────────────────

const (
	bgTaskTimeout      = 300 * time.Second // hard cap per task (matches ref.py)
	bgTaskOutputCap    = 50000             // bytes captured into output.log + record
	bgTaskPreviewCap   = 500               // chars retained on the record for status views
	bgTaskIDByteLen    = 4                 // 4 bytes → 8 hex chars
	bgRuntimeDirName   = "runtime-tasks"
	bgRuntimeTodo      = "todo"
	bgRuntimeDone      = "done"
	bgTaskRecordFile   = "task.json"
	bgTaskOutputFile   = "output.log"
)

// BgTaskGuidance is appended to the system prompt by main.go so the model
// knows when to reach for bg_run vs the synchronous bash tool.
const BgTaskGuidance = `# Background Tasks

Use bg_run for commands that take more than ~30s (long builds, full test suites, dev servers, watchers).
Use bash for fast, synchronous shell calls — bg_run has 300s timeout per task and the model can't read its output until the task completes.

After bg_run, you'll see a synthetic <background-results>...</background-results> user message at the start of the turn that follows completion. The output_file path inside that block points to the saved log under .evo-agent/sessions/<sid>/runtime-tasks/done/<id>/output.log — read it with read_file if you need the full output.

Tools: bg_run (start), bg_list (list all), bg_check (status of one), bg_cancel (kill+archive one).`

// ── Types ───────────────────────────────────────────────────────────────────

// BgTaskRecord is the JSON-persisted shape of one background task.
// IDs are 8 hex chars; status transitions: running → completed | timeout | error | cancelled.
type BgTaskRecord struct {
	ID           string `json:"id"`
	Command      string `json:"command"`
	Status       string `json:"status"`
	StartedAtMs  int64  `json:"started_at_ms"`
	FinishedAtMs int64  `json:"finished_at_ms,omitempty"`
	ExitCode     int    `json:"exit_code,omitempty"`
	Preview      string `json:"preview,omitempty"`     // first ~500 chars, whitespace-collapsed
	OutputFile   string `json:"output_file,omitempty"` // path relative to session dir
	Bucket       string `json:"-"`                     // "todo" | "done", derived at load
}

type bgNotification struct {
	ID         string
	Status     string
	Command    string
	Preview    string
	OutputFile string
}

// BgTaskManager is the process-wide singleton.
type BgTaskManager struct {
	mu        sync.RWMutex
	sessionID string
	rootDir   string                 // <sessionDir>/runtime-tasks
	tasks     map[string]*BgTaskRecord
	procs     map[string]*exec.Cmd
	cancels   map[string]context.CancelFunc

	notifMu sync.Mutex
	notifQ  []bgNotification
}

// GlobalBgTasks is the singleton wired into main.go's session bootstrap.
var GlobalBgTasks = &BgTaskManager{
	tasks:   map[string]*BgTaskRecord{},
	procs:   map[string]*exec.Cmd{},
	cancels: map[string]context.CancelFunc{},
}

// ── Lifecycle ───────────────────────────────────────────────────────────────

// Init binds the manager to a session directory. Creates todo/ + done/ if
// missing and rehydrates `tasks` from any pre-existing `task.json` files.
// On --resume, this lets `bg_list` immediately show the historical archive.
//
// Stale "running" records left over from a crashed previous run (the only
// way they'd land in todo/) are downgraded to "error" with a synthetic
// preview so the user can tell what happened.
func (m *BgTaskManager) Init(sessionDir, sessionID string) error {
	if sessionDir == "" {
		return nil
	}
	root := filepath.Join(sessionDir, bgRuntimeDirName)
	for _, sub := range []string{bgRuntimeTodo, bgRuntimeDone} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			return fmt.Errorf("bg: mkdir %s: %w", sub, err)
		}
	}

	m.mu.Lock()
	m.sessionID = sessionID
	m.rootDir = root
	m.tasks = map[string]*BgTaskRecord{}
	m.procs = map[string]*exec.Cmd{}
	m.cancels = map[string]context.CancelFunc{}
	m.mu.Unlock()

	// Rehydrate previously-seen tasks from disk so /bgtask + bg_list work
	// after --resume. Anything still in todo/ from a prior run had its
	// goroutine die with the process — surface that as "error".
	for _, bucket := range []string{bgRuntimeDone, bgRuntimeTodo} {
		entries, err := os.ReadDir(filepath.Join(root, bucket))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			recPath := filepath.Join(root, bucket, e.Name(), bgTaskRecordFile)
			data, err := os.ReadFile(recPath)
			if err != nil {
				continue
			}
			var rec BgTaskRecord
			if err := json.Unmarshal(data, &rec); err != nil {
				continue
			}
			rec.Bucket = bucket
			if bucket == bgRuntimeTodo && rec.Status == "running" {
				// Crashed previous-run leftover; salvage as error.
				rec.Status = "error"
				rec.Preview = "(task did not complete — agent exited while running)"
				rec.FinishedAtMs = time.Now().UnixMilli()
				m.archiveDownGrade(&rec)
			}
			m.mu.Lock()
			m.tasks[rec.ID] = &rec
			m.mu.Unlock()
		}
	}
	m.emitCounts()
	return nil
}

// archiveDownGrade moves a leftover todo/ dir to done/ and rewrites task.json.
// Caller does not hold the mutex.
func (m *BgTaskManager) archiveDownGrade(rec *BgTaskRecord) {
	src := filepath.Join(m.rootDir, bgRuntimeTodo, rec.ID)
	dst := filepath.Join(m.rootDir, bgRuntimeDone, rec.ID)
	_ = os.Rename(src, dst)
	rec.Bucket = bgRuntimeDone
	if rec.OutputFile != "" {
		rec.OutputFile = strings.Replace(rec.OutputFile,
			filepath.Join(bgRuntimeDirName, bgRuntimeTodo, rec.ID),
			filepath.Join(bgRuntimeDirName, bgRuntimeDone, rec.ID), 1)
	}
	_ = m.persistRecord(rec)
}

// ── Public API ──────────────────────────────────────────────────────────────

// Run launches a background task. Returns the task id.
// The actual subprocess runs in a goroutine; Run is non-blocking.
func (m *BgTaskManager) Run(command string) (string, error) {
	m.mu.RLock()
	root := m.rootDir
	m.mu.RUnlock()
	if root == "" {
		return "", fmt.Errorf("bg: no active session — set tools.SetSessionContext first")
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return "", fmt.Errorf("bg: command is required")
	}
	id, err := newBgTaskID()
	if err != nil {
		return "", err
	}

	taskDir := filepath.Join(root, bgRuntimeTodo, id)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return "", fmt.Errorf("bg: mkdir task dir: %w", err)
	}

	// Compute the session-relative output_file path so resume / printing
	// stays sane regardless of where the agent's cwd is.
	relOut := filepath.Join(bgRuntimeDirName, bgRuntimeTodo, id, bgTaskOutputFile)

	rec := &BgTaskRecord{
		ID:          id,
		Command:     command,
		Status:      "running",
		StartedAtMs: time.Now().UnixMilli(),
		OutputFile:  relOut,
		Bucket:      bgRuntimeTodo,
	}
	if err := m.persistRecord(rec); err != nil {
		return "", err
	}

	m.mu.Lock()
	m.tasks[id] = rec
	m.mu.Unlock()
	m.emitCounts()

	go m.execute(id, command, taskDir)

	short := command
	if len(short) > 80 {
		short = short[:80]
	}
	return fmt.Sprintf(
		"Background task %s started: %s\n(output_file=%s — fully streamed once the task completes)",
		id, short, relOut,
	), nil
}

// execute is the goroutine target. Runs cmd in its own process group, caps
// output, persists final state, archives to done/, and pushes a notification.
func (m *BgTaskManager) execute(id, command, taskDir string) {
	logPath := filepath.Join(taskDir, bgTaskOutputFile)
	logFile, _ := os.Create(logPath)
	if logFile != nil {
		defer logFile.Close()
	}

	ctx, cancel := context.WithTimeout(context.Background(), bgTaskTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	if cwd, err := os.Getwd(); err == nil {
		cmd.Dir = cwd
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// On timeout / Cancel, kill the whole process group so background "&"
	// children (e.g. server processes) are also reaped — same trick as
	// runBash in bash.go.
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}

	var memBuf bytes.Buffer
	memWriter := &capWriter{Buf: &memBuf, cap: bgTaskOutputCap}
	var writers []io.Writer
	writers = append(writers, memWriter)
	if logFile != nil {
		writers = append(writers, logFile)
	}
	mw := io.MultiWriter(writers...)
	cmd.Stdout = mw
	cmd.Stderr = mw

	m.mu.Lock()
	m.procs[id] = cmd
	m.cancels[id] = cancel
	m.mu.Unlock()

	runErr := cmd.Run()

	m.mu.Lock()
	delete(m.procs, id)
	delete(m.cancels, id)
	rec := m.tasks[id]
	m.mu.Unlock()
	if rec == nil {
		// Already cancelled and archived elsewhere; nothing to do.
		return
	}

	// If Cancel was already called externally (bg_cancel path), the record
	// status is "cancelled" — preserve it. Otherwise figure out the outcome.
	if rec.Status == "cancelled" {
		return // bg_cancel already handled persistence + notification
	}

	rec.FinishedAtMs = time.Now().UnixMilli()
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		rec.Status = "timeout"
	case runErr != nil:
		rec.Status = "error"
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			rec.ExitCode = exitErr.ExitCode()
		}
	default:
		rec.Status = "completed"
		rec.ExitCode = 0
	}
	rec.Preview = compactPreview(memBuf.String(), bgTaskPreviewCap)
	if rec.Preview == "" {
		rec.Preview = "(no output)"
	}

	m.archiveAndNotify(rec)
}

// Check returns a single task's details (id non-empty) or all tasks (id "").
func (m *BgTaskManager) Check(id string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if id != "" {
		rec, ok := m.tasks[id]
		if !ok {
			return fmt.Sprintf("bg: unknown task %q", id)
		}
		out, _ := json.MarshalIndent(map[string]interface{}{
			"id":             rec.ID,
			"status":         rec.Status,
			"command":        rec.Command,
			"started_at_ms":  rec.StartedAtMs,
			"finished_at_ms": rec.FinishedAtMs,
			"exit_code":      rec.ExitCode,
			"preview":        rec.Preview,
			"output_file":    rec.OutputFile,
		}, "", "  ")
		return string(out)
	}
	return m.renderListLocked()
}

// List returns a copy of every known task record (todo + done), sorted by
// started_at_ms ascending.
func (m *BgTaskManager) List() []BgTaskRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]BgTaskRecord, 0, len(m.tasks))
	for _, r := range m.tasks {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAtMs < out[j].StartedAtMs })
	return out
}

// Cancel kills a running task's process group and archives the record.
// Idempotent for already-finished tasks (returns an explanatory string).
func (m *BgTaskManager) Cancel(id string) (string, error) {
	m.mu.Lock()
	rec, ok := m.tasks[id]
	if !ok {
		m.mu.Unlock()
		return "", fmt.Errorf("bg: unknown task %q", id)
	}
	if rec.Status != "running" {
		m.mu.Unlock()
		return fmt.Sprintf("bg: task %s already %s — no action taken", id, rec.Status), nil
	}
	cancel := m.cancels[id]
	rec.Status = "cancelled"
	rec.FinishedAtMs = time.Now().UnixMilli()
	if rec.Preview == "" {
		rec.Preview = "(cancelled)"
	}
	m.mu.Unlock()

	if cancel != nil {
		cancel() // triggers cmd.Cancel (SIGKILL on the whole pgrp)
	}
	m.archiveAndNotify(rec)
	return fmt.Sprintf("bg: cancelled task %s", id), nil
}

// Counts returns (running, terminal). Terminal = anything not "running".
func (m *BgTaskManager) Counts() (running, completed int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, r := range m.tasks {
		if r.Status == "running" {
			running++
		} else {
			completed++
		}
	}
	return
}

// DrainNotifications returns + clears the pending completion queue. Called
// once per agent-loop iteration before the LLM call.
func (m *BgTaskManager) DrainNotifications() []bgNotification {
	m.notifMu.Lock()
	defer m.notifMu.Unlock()
	out := m.notifQ
	m.notifQ = nil
	return out
}

// FormatBgNotifications renders a drained batch as a `<background-results>`
// block ready to wrap in an anthropic.NewUserMessage.
func FormatBgNotifications(notifs []bgNotification) string {
	if len(notifs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<background-results>\n")
	for _, n := range notifs {
		b.WriteString(fmt.Sprintf(
			"[bg:%s] %s: %s (cmd=%s, output_file=%s)\n",
			n.ID, n.Status, n.Preview, truncForNotif(n.Command, 80), n.OutputFile,
		))
	}
	b.WriteString("</background-results>")
	return b.String()
}

// ── Internal helpers ────────────────────────────────────────────────────────

// archiveAndNotify persists final state, moves the dir from todo/ to done/,
// pushes a notification, and emits an EvBgTasks count update.
func (m *BgTaskManager) archiveAndNotify(rec *BgTaskRecord) {
	// Move dir.
	src := filepath.Join(m.rootDir, bgRuntimeTodo, rec.ID)
	dst := filepath.Join(m.rootDir, bgRuntimeDone, rec.ID)
	if _, err := os.Stat(src); err == nil {
		// Best-effort: a previous archive may have already happened (e.g.
		// race between Cancel + execute returning). Ignore errors — the
		// updated record write below is the source of truth.
		_ = os.Rename(src, dst)
	}
	rec.Bucket = bgRuntimeDone
	if rec.OutputFile != "" {
		rec.OutputFile = strings.Replace(rec.OutputFile,
			filepath.Join(bgRuntimeDirName, bgRuntimeTodo, rec.ID),
			filepath.Join(bgRuntimeDirName, bgRuntimeDone, rec.ID), 1)
	}
	_ = m.persistRecord(rec)

	m.notifMu.Lock()
	m.notifQ = append(m.notifQ, bgNotification{
		ID:         rec.ID,
		Status:     rec.Status,
		Command:    rec.Command,
		Preview:    rec.Preview,
		OutputFile: rec.OutputFile,
	})
	m.notifMu.Unlock()

	m.emitCounts()
}

func (m *BgTaskManager) persistRecord(rec *BgTaskRecord) error {
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	bucket := rec.Bucket
	if bucket == "" {
		bucket = bgRuntimeTodo
	}
	dir := filepath.Join(m.rootDir, bucket, rec.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, bgTaskRecordFile), data, 0o644)
}

func (m *BgTaskManager) emitCounts() {
	r, c := m.Counts()
	ui.EmitBgTasks(r, c)
}

// renderListLocked formats every known task as "<id>: [status] <cmd> -> <preview>".
// Caller must hold mu (R or W lock).
func (m *BgTaskManager) renderListLocked() string {
	if len(m.tasks) == 0 {
		return "No background tasks."
	}
	keys := make([]string, 0, len(m.tasks))
	for k := range m.tasks {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return m.tasks[keys[i]].StartedAtMs < m.tasks[keys[j]].StartedAtMs
	})
	var b strings.Builder
	for _, id := range keys {
		r := m.tasks[id]
		preview := r.Preview
		if r.Status == "running" {
			preview = "(running)"
		}
		cmd := r.Command
		if len(cmd) > 60 {
			cmd = cmd[:60] + "…"
		}
		b.WriteString(fmt.Sprintf("%s: [%s] %s -> %s\n", id, r.Status, cmd, preview))
	}
	return strings.TrimRight(b.String(), "\n")
}

// capWriter buffers up to `cap` bytes and silently drops anything beyond.
type capWriter struct {
	Buf *bytes.Buffer
	cap int
}

func (c *capWriter) Write(p []byte) (int, error) {
	if c.Buf.Len() >= c.cap {
		return len(p), nil // pretend we wrote everything
	}
	room := c.cap - c.Buf.Len()
	if room >= len(p) {
		return c.Buf.Write(p)
	}
	c.Buf.Write(p[:room])
	return len(p), nil
}

// compactPreview collapses runs of whitespace and trims to limit chars.
func compactPreview(s string, limit int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Collapse whitespace runs to a single space.
	var b strings.Builder
	wasSpace := false
	for _, r := range s {
		if r == '\n' || r == '\t' || r == '\r' || r == ' ' {
			if !wasSpace {
				b.WriteRune(' ')
				wasSpace = true
			}
			continue
		}
		b.WriteRune(r)
		wasSpace = false
	}
	out := b.String()
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func truncForNotif(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "…"
}

func newBgTaskID() (string, error) {
	buf := make([]byte, bgTaskIDByteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// ── Tool input schemas + registration ───────────────────────────────────────

type bgRunInput struct {
	Command string `json:"command" jsonschema_description:"Shell command to run in the background. Goes through 'bash -c'. 300s timeout per task."`
}

type bgCheckInput struct {
	TaskID string `json:"task_id,omitempty" jsonschema_description:"Optional task id to inspect; omit to list all."`
}

type bgListInput struct{}

type bgCancelInput struct {
	TaskID string `json:"task_id" jsonschema_description:"Task id to kill (SIGKILL on its process group) and archive as cancelled."`
}

func init() {
	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name: "bg_run",
			Description: anthropic.String(
				"Run a long-running shell command in a background goroutine and return its task id immediately. " +
					"Use for builds, test suites, dev servers — anything over ~30s. " +
					"The next turn after the task completes will receive a synthetic <background-results> message with the preview + output_file path."),
			InputSchema: GenerateSchema[bgRunInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in bgRunInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}
			return GlobalBgTasks.Run(in.Command)
		},
	})

	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name: "bg_check",
			Description: anthropic.String(
				"Inspect one background task by id (returns full JSON record), or omit task_id to list everything compactly."),
			InputSchema: GenerateSchema[bgCheckInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in bgCheckInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}
			return GlobalBgTasks.Check(in.TaskID), nil
		},
	})

	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name:        "bg_list",
			Description: anthropic.String("List every background task (running + archived) with status + preview."),
			InputSchema: GenerateSchema[bgListInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			return GlobalBgTasks.Check(""), nil
		},
	})

	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name: "bg_cancel",
			Description: anthropic.String(
				"Kill a running background task (SIGKILL on its process group) and archive the directory to done/ with status=cancelled. " +
					"Idempotent for already-finished tasks."),
			InputSchema: GenerateSchema[bgCancelInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in bgCancelInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}
			out, err := GlobalBgTasks.Cancel(in.TaskID)
			if err != nil {
				return "", err
			}
			return out, nil
		},
	})
}
