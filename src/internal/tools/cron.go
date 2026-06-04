package tools

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// cron.go — Scheduled tasks (cron) for evo-agent.
//
// Lifted in shape from refs/ref.py and refs/cron*.ts:
//   - 5-field cron expressions: "minute hour day-of-month month day-of-week"
//   - Two persistence modes:
//       durable=true   → written to disk (per-session JSON), survives --resume
//       durable=false  → in-memory only, dies with the process
//   - Two trigger modes:
//       recurring=true (default) → repeats until 7-day expiry or explicit delete
//       recurring=false           → fires once then auto-deletes
//   - Background goroutine wakes every second and matches each task against
//     the wall clock with a one-minute granularity guard to avoid double-fire.
//   - On match, the task's prompt is enqueued; agent.Loop drains the queue at
//     the top of each turn and injects each prompt as a synthetic user message.
//
// Storage layout (per session):
//
//   .evo-agent/sessions/<sessID>/scheduled_tasks/
//     tasks.json          ← single JSON file, only durable tasks persisted
//
// Session-only (durable=false) tasks live in memory only and never touch disk.
// Their `agentId`-style routing is unnecessary here (single-agent CLI).

// ── Constants ───────────────────────────────────────────────────────────────

const (
	cronMaxJobs           = 50
	cronCheckInterval     = 1 * time.Second
	cronAutoExpiryDays    = 7
	cronTasksDirName      = "scheduled_tasks"
	cronTasksFileName     = "tasks.json"
	cronTaskIDByteLen     = 4 // → 8 hex chars
	cronJitterMinuteMatch = 30
	cronJitterMaxMinutes  = 4
	// cronOneShotGrace bounds how late a one-shot can fire after its
	// FireBy time before the scheduler gives up and deletes it. Two
	// minutes accommodates the up-to-1m worst-case tick latency plus the
	// jitter offset. After this window, the task is dropped without
	// firing — protects against durable one-shots pinned to a past date
	// re-firing on the next yearly cron match.
	cronOneShotGrace = 2 * time.Minute
)

// CronGuidance is appended to the system prompt by main.go so the model
// knows how to schedule prompts for future execution.
const CronGuidance = `# Scheduled Tasks (cron)

Use cron_create to schedule a prompt to run at a future time, either once
or on a recurring cadence. The model is responsible for translating the
user's natural-language request into a cron expression.

## Cron expression — 5 fields, local timezone

  minute  hour  day-of-month  month  day-of-week
  0-59    0-23  1-31          1-12   0-6 (0=Sun, 7=Sun alias)

Per-field syntax: "*", "*/N", "N", "N-M", "N,M,...". No L/W/?/name aliases.

## Mapping natural language to cron

One-shot reminders ("remind me at X", "at <time> do Y" — recurring=false):
  "remind me at 3pm to push the release"  → cron="0 15 <today_dom> <today_month> *", recurring=false
  "in 45 minutes, check CI"                → compute target wallclock and pin minute+hour+dom+month
  "tomorrow at 9am, run smoke tests"       → cron="0 9 <tomorrow_dom> <tomorrow_month> *", recurring=false

Recurring jobs ("every X", "daily at Y", "weekdays at Z" — recurring=true, the default):
  "every 5 minutes"                  → "*/5 * * * *"
  "every hour"                       → "7 * * * *"   (avoid :00 — see below)
  "every day at 9am"                 → "3 9 * * *"   (avoid :00 — see below)
  "weekdays at 9am"                  → "3 9 * * 1-5"
  "every Monday at noon"             → "0 12 * * 1"

## Avoid :00 and :30 minute marks when the user is approximate

When the user says "9am" without specifying exact, prefer minute 3 / 7 / 57
over minute 0. Two reasons: (1) every user landing on the same wall-clock
minute creates an inference spike; (2) the scheduler adds a 1-4 minute
forward jitter to tasks targeting :00 or :30 anyway, so explicit off-minutes
are more predictable. Only use :00 / :30 when the user names that exact
time ("at 9:00 sharp", "at half past").

## When to use durable=true

By default tasks are session-only — they live in process memory and die
when the agent exits. Use durable=true ONLY when the user explicitly asks
the task to persist across restarts ("keep doing this every day", "set
this up permanently"). Most "remind me in 5 minutes" / "check back in an
hour" requests should stay session-only.

## Runtime behavior

When a task fires, its prompt is enqueued and injected at the top of the
next agent turn as a synthetic <scheduled-task>...</scheduled-task> user
message — treat the embedded prompt as a fresh user request and act on it.

Recurring tasks auto-expire after 7 days (one final fire, then deleted) —
mention this when the user schedules a long-running recurring job.
One-shot tasks delete themselves after firing.

## Tools

  cron_create  — schedule a prompt (returns 8-char id)
  cron_list    — list every scheduled task (recurring + one-shot, all stores)
  cron_delete  — cancel a task by id
`

// ── Types ───────────────────────────────────────────────────────────────────

// CronTask is the JSON-persisted shape of a scheduled task.
type CronTask struct {
	ID         string `json:"id"`
	Cron       string `json:"cron"`
	Prompt     string `json:"prompt"`
	Recurring  bool   `json:"recurring"`
	Durable    bool   `json:"durable"`
	CreatedAt  int64  `json:"created_at_ms"`
	LastFired  int64  `json:"last_fired_ms,omitempty"`
	JitterMins int    `json:"jitter_mins,omitempty"`
	// FireBy is the first matching run computed at creation time. One-shot
	// tasks expire after this deadline (+ a grace period) without firing,
	// so a durable one-shot pinned to a past date can't zombie-fire on the
	// next yearly match. Zero for recurring tasks (no upper bound; they
	// rely on cronAutoExpiryDays instead).
	FireBy int64 `json:"fire_by_ms,omitempty"`
}

// cronNotification is queued when a task fires.
type cronNotification struct {
	ID     string
	Prompt string
	Cron   string
}

// CronScheduler is the process-wide singleton.
type CronScheduler struct {
	mu        sync.RWMutex
	sessionID string
	rootDir   string // <sessionDir>/scheduled_tasks
	tasks     map[string]*CronTask

	notifMu sync.Mutex
	notifQ  []cronNotification

	stopCh    chan struct{}
	stopped   bool
	lastCheck int // last "minute index" we evaluated (h*60+m)
}

// GlobalCron is the singleton wired into main.go's session bootstrap.
var GlobalCron = &CronScheduler{
	tasks:     map[string]*CronTask{},
	stopCh:    make(chan struct{}),
	lastCheck: -1,
}

// ── Cron expression parser ──────────────────────────────────────────────────
//
// Supports the standard 5-field cron subset:
//   minute (0-59) hour (0-23) day-of-month (1-31) month (1-12) day-of-week (0-6)
// Field syntax: "*", "*/N", "N", "N-M", "N,M,...". Day-of-week 7 is treated
// as Sunday (alias for 0).

type cronFields struct {
	minute, hour, dom, month, dow []int
}

type cronRange struct{ lo, hi int }

var cronRanges = []cronRange{
	{0, 59},
	{0, 23},
	{1, 31},
	{1, 12},
	{0, 6},
}

// ParseCron parses a 5-field cron expression. Returns nil on invalid input.
func parseCron(expr string) *cronFields {
	parts := strings.Fields(strings.TrimSpace(expr))
	if len(parts) != 5 {
		return nil
	}
	expanded := make([][]int, 5)
	for i := 0; i < 5; i++ {
		v := expandField(parts[i], cronRanges[i])
		if v == nil {
			return nil
		}
		expanded[i] = v
	}
	return &cronFields{
		minute: expanded[0],
		hour:   expanded[1],
		dom:    expanded[2],
		month:  expanded[3],
		dow:    expanded[4],
	}
}

func expandField(field string, r cronRange) []int {
	out := map[int]struct{}{}
	for _, raw := range strings.Split(field, ",") {
		part := raw
		step := 1
		if idx := strings.Index(part, "/"); idx >= 0 {
			s, err := strconv.Atoi(part[idx+1:])
			if err != nil || s < 1 {
				return nil
			}
			step = s
			part = part[:idx]
		}
		// Wildcard
		if part == "*" {
			for i := r.lo; i <= r.hi; i += step {
				out[i] = struct{}{}
			}
			continue
		}
		// Range N-M
		if idx := strings.Index(part, "-"); idx > 0 {
			lo, err1 := strconv.Atoi(part[:idx])
			hi, err2 := strconv.Atoi(part[idx+1:])
			if err1 != nil || err2 != nil {
				return nil
			}
			isDow := r.lo == 0 && r.hi == 6
			effHi := r.hi
			if isDow {
				effHi = 7
			}
			if lo < r.lo || hi > effHi || lo > hi {
				return nil
			}
			for i := lo; i <= hi; i += step {
				v := i
				if isDow && v == 7 {
					v = 0
				}
				out[v] = struct{}{}
			}
			continue
		}
		// Plain N
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil
		}
		// Day-of-week 7 → 0 (Sunday alias)
		if r.lo == 0 && r.hi == 6 && n == 7 {
			n = 0
		}
		if n < r.lo || n > r.hi {
			return nil
		}
		out[n] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	values := make([]int, 0, len(out))
	for v := range out {
		values = append(values, v)
	}
	sort.Ints(values)
	return values
}

// matchCron returns true iff the cron expression matches the given time.
// Uses standard vixie-cron semantics: when both day-of-month and day-of-week
// are constrained (neither is wildcard-full), a match on either is enough.
func matchCron(f *cronFields, t time.Time) bool {
	if f == nil {
		return false
	}
	if !contains(f.minute, t.Minute()) {
		return false
	}
	if !contains(f.hour, t.Hour()) {
		return false
	}
	if !contains(f.month, int(t.Month())) {
		return false
	}
	domWild := len(f.dom) == 31
	dowWild := len(f.dow) == 7
	dom := t.Day()
	// time.Weekday(): Sunday=0..Saturday=6 — matches cron numbering.
	dow := int(t.Weekday())
	switch {
	case domWild && dowWild:
		return true
	case domWild:
		return contains(f.dow, dow)
	case dowWild:
		return contains(f.dom, dom)
	default:
		return contains(f.dom, dom) || contains(f.dow, dow)
	}
}

func contains(s []int, n int) bool {
	for _, v := range s {
		if v == n {
			return true
		}
	}
	return false
}

// nextRun walks forward minute-by-minute (capped 366 days) and returns the
// first time strictly after `from` that matches the expression.
func nextRun(f *cronFields, from time.Time) (time.Time, bool) {
	if f == nil {
		return time.Time{}, false
	}
	t := from.Truncate(time.Minute).Add(time.Minute)
	max := 366 * 24 * 60
	for i := 0; i < max; i++ {
		if matchCron(f, t) {
			return t, true
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}, false
}

// ── Lifecycle ───────────────────────────────────────────────────────────────

// Init binds the scheduler to a session directory, loads any persisted
// durable tasks, and starts the background ticker. Idempotent.
//
// When sessionDir is empty (persistence disabled), the scheduler still
// starts its background ticker so session-only tasks created via
// cron_create can fire — they just never touch disk. This keeps
// cron_create's contract honest: a task scheduled in any mode actually
// fires.
func (s *CronScheduler) Init(sessionDir, sessionID string) error {
	var root string
	if sessionDir != "" {
		root = filepath.Join(sessionDir, cronTasksDirName)
		if err := os.MkdirAll(root, 0o755); err != nil {
			return fmt.Errorf("cron: mkdir: %w", err)
		}
	}

	s.mu.Lock()
	s.sessionID = sessionID
	s.rootDir = root
	s.tasks = map[string]*CronTask{}
	s.stopped = false
	s.stopCh = make(chan struct{})
	s.mu.Unlock()

	if root != "" {
		s.loadDurable()
	}
	go s.checkLoop()
	return nil
}

// Stop signals the background goroutine to exit. Safe to call multiple times.
func (s *CronScheduler) Stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	close(s.stopCh)
	s.mu.Unlock()
}

// Create schedules a new task. Returns the new id.
func (s *CronScheduler) Create(cronExpr, prompt string, recurring, durable bool) (string, error) {
	f := parseCron(cronExpr)
	if f == nil {
		return "", fmt.Errorf("invalid cron expression %q (expected 5 fields: M H DoM Mon DoW)", cronExpr)
	}
	if _, ok := nextRun(f, time.Now()); !ok {
		return "", fmt.Errorf("cron expression %q does not match any time in the next 366 days", cronExpr)
	}

	s.mu.Lock()
	if len(s.tasks) >= cronMaxJobs {
		s.mu.Unlock()
		return "", fmt.Errorf("too many scheduled jobs (max %d) — cancel one first", cronMaxJobs)
	}
	id, err := newCronTaskID()
	if err != nil {
		s.mu.Unlock()
		return "", err
	}
	t := &CronTask{
		ID:         id,
		Cron:       cronExpr,
		Prompt:     prompt,
		Recurring:  recurring,
		Durable:    durable,
		CreatedAt:  time.Now().UnixMilli(),
		JitterMins: computeJitter(cronExpr),
	}
	// Pin the firing window for one-shot tasks: a durable one-shot
	// pinned to "Feb 28 14:30" must NOT re-fire next year just because
	// the cron expression matches that calendar date again.
	if !recurring {
		if next, ok := nextRun(f, time.Now()); ok {
			t.FireBy = next.UnixMilli()
		}
	}
	s.tasks[id] = t
	s.mu.Unlock()

	if durable {
		s.saveDurable()
	}
	return id, nil
}

// Delete removes a task by id. Returns true if found.
func (s *CronScheduler) Delete(id string) bool {
	s.mu.Lock()
	t, ok := s.tasks[id]
	if !ok {
		s.mu.Unlock()
		return false
	}
	delete(s.tasks, id)
	wasDurable := t.Durable
	s.mu.Unlock()
	if wasDurable {
		s.saveDurable()
	}
	return true
}

// List returns a snapshot of every known task, sorted by created_at_ms asc.
func (s *CronScheduler) List() []CronTask {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]CronTask, 0, len(s.tasks))
	for _, t := range s.tasks {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out
}

// DrainNotifications returns and clears the pending notification queue.
// Called once per agent-loop iteration before the LLM call.
func (s *CronScheduler) DrainNotifications() []cronNotification {
	s.notifMu.Lock()
	defer s.notifMu.Unlock()
	out := s.notifQ
	s.notifQ = nil
	return out
}

// FormatCronNotifications renders a drained batch as a `<scheduled-task>`
// block ready to wrap in an anthropic.NewUserMessage.
func FormatCronNotifications(notifs []cronNotification) string {
	if len(notifs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<scheduled-task>\n")
	b.WriteString("The following scheduled task(s) just fired. Treat each prompt below as a fresh user request and act on it.\n")
	for _, n := range notifs {
		b.WriteString(fmt.Sprintf("\n[task %s — cron %q]\n%s\n", n.ID, n.Cron, n.Prompt))
	}
	b.WriteString("</scheduled-task>")
	return b.String()
}

// ── Background ticker ───────────────────────────────────────────────────────

func (s *CronScheduler) checkLoop() {
	ticker := time.NewTicker(cronCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case now := <-ticker.C:
			s.tickAt(now)
		}
	}
}

// tickAt evaluates all tasks against `now`. Exported style (lower-case) so
// tests in the same package could drive it deterministically without
// waiting on the system clock.
func (s *CronScheduler) tickAt(now time.Time) {
	idx := now.Hour()*60 + now.Minute()
	s.mu.Lock()
	if idx == s.lastCheck {
		s.mu.Unlock()
		return
	}
	s.lastCheck = idx

	type fired struct{ id, prompt, cron string }
	var notifs []fired
	var expired, oneshots []string

	for id, t := range s.tasks {
		// Auto-expire recurring tasks older than 7 days.
		if t.Recurring && time.Since(time.UnixMilli(t.CreatedAt)) > cronAutoExpiryDays*24*time.Hour {
			expired = append(expired, id)
			continue
		}
		// Auto-expire one-shot tasks whose firing window has passed
		// without a successful fire (e.g. agent was offline at the
		// scheduled minute). Without this guard, a durable one-shot
		// pinned to a past date would zombie-fire on the next yearly
		// cron match.
		if !t.Recurring && t.FireBy > 0 &&
			now.UnixMilli() > t.FireBy+int64(cronOneShotGrace/time.Millisecond) {
			expired = append(expired, id)
			continue
		}
		f := parseCron(t.Cron)
		if f == nil {
			continue // malformed; skip silently
		}
		// Apply jitter offset so two sessions don't fire on the exact same
		// :00 wall-clock minute.
		check := now
		if t.JitterMins > 0 {
			check = now.Add(-time.Duration(t.JitterMins) * time.Minute)
		}
		if !matchCron(f, check) {
			continue
		}
		t.LastFired = now.UnixMilli()
		notifs = append(notifs, fired{id: t.ID, prompt: t.Prompt, cron: t.Cron})
		if !t.Recurring {
			oneshots = append(oneshots, id)
		}
	}

	for _, id := range expired {
		delete(s.tasks, id)
	}
	for _, id := range oneshots {
		delete(s.tasks, id)
	}
	persistNeeded := len(expired)+len(oneshots) > 0 || len(notifs) > 0
	s.mu.Unlock()

	if len(notifs) > 0 {
		s.notifMu.Lock()
		for _, n := range notifs {
			s.notifQ = append(s.notifQ, cronNotification{ID: n.id, Prompt: n.prompt, Cron: n.cron})
		}
		s.notifMu.Unlock()
	}
	if persistNeeded {
		s.saveDurable()
	}
}

// ── Persistence ─────────────────────────────────────────────────────────────

func (s *CronScheduler) tasksFile() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.rootDir == "" {
		return ""
	}
	return filepath.Join(s.rootDir, cronTasksFileName)
}

type cronFile struct {
	Tasks []CronTask `json:"tasks"`
}

// loadDurable reads <root>/tasks.json (if present) and re-hydrates durable
// tasks. Tasks with invalid cron strings are silently dropped.
func (s *CronScheduler) loadDurable() {
	path := s.tasksFile()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var f cronFile
	if err := json.Unmarshal(data, &f); err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range f.Tasks {
		if parseCron(t.Cron) == nil {
			continue
		}
		// Force durable=true on rehydrate (the file only ever stores durables).
		t.Durable = true
		tt := t
		s.tasks[t.ID] = &tt
	}
}

// saveDurable writes only durable tasks to <root>/tasks.json. Best-effort:
// errors are swallowed because a failed write on one tick must not crash
// the agent.
func (s *CronScheduler) saveDurable() {
	path := s.tasksFile()
	if path == "" {
		return
	}
	s.mu.RLock()
	out := cronFile{}
	for _, t := range s.tasks {
		if t.Durable {
			out.Tasks = append(out.Tasks, *t)
		}
	}
	s.mu.RUnlock()
	sort.Slice(out.Tasks, func(i, j int) bool { return out.Tasks[i].CreatedAt < out.Tasks[j].CreatedAt })

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, append(data, '\n'), 0o644)
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func newCronTaskID() (string, error) {
	buf := make([]byte, cronTaskIDByteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// computeJitter returns a deterministic 1..cronJitterMaxMinutes offset when
// the task targets minute :00 or :30 (the human-rounding hot marks); zero
// otherwise. Stable across restarts because keyed off the cron string.
func computeJitter(cronExpr string) int {
	parts := strings.Fields(strings.TrimSpace(cronExpr))
	if len(parts) < 1 {
		return 0
	}
	minute, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0
	}
	if minute%cronJitterMinuteMatch != 0 {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(cronExpr))
	return int(h.Sum32()%uint32(cronJitterMaxMinutes)) + 1
}

// nowMs returns the current wall-clock time in epoch milliseconds. Lifted
// onto the scheduler so tests in the same package could swap it later.
func (s *CronScheduler) nowMs() int64 {
	return time.Now().UnixMilli()
}

// HumanCron returns a short human-readable rendering of a cron expression
// for display. Falls through to the raw string for patterns it doesn't
// special-case.
func HumanCron(cron string) string {
	parts := strings.Fields(strings.TrimSpace(cron))
	if len(parts) != 5 {
		return cron
	}
	min, hour, dom, month, dow := parts[0], parts[1], parts[2], parts[3], parts[4]
	// Every N minutes
	if hour == "*" && dom == "*" && month == "*" && dow == "*" {
		if strings.HasPrefix(min, "*/") {
			n := strings.TrimPrefix(min, "*/")
			return "every " + n + " minutes"
		}
		if min == "*" {
			return "every minute"
		}
		if m, err := strconv.Atoi(min); err == nil {
			return fmt.Sprintf("every hour at :%02d", m)
		}
	}
	// Daily at H:M
	if dom == "*" && month == "*" && dow == "*" {
		if m, err := strconv.Atoi(min); err == nil {
			if h, err := strconv.Atoi(hour); err == nil {
				return fmt.Sprintf("daily at %02d:%02d", h, m)
			}
		}
	}
	return cron
}
