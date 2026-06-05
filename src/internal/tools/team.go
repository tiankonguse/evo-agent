// Package tools — team.go
//
// Persistent named teammate roster ("agent teams"). Each teammate runs
// inside its own goroutine with its own message history; communication
// happens through file-backed JSONL inboxes under .evo-agent/team/.
//
// Reference shape (lifted from refs/ref.py:s15 + refs/ref.md):
//
//   spawn -> work (LLM loop) -> idle -> wake on inbox arrival -> work -> ...
//                                                              -> shutdown
//
// Storage layout:
//
//   .evo-agent/team/
//     config.json                ← persistent registry (members + statuses)
//     inbox/<name>.jsonl         ← append-only, drained on read
//     inbox/lead.jsonl           ← lead's own inbox (drained at top of agent loop)
//     history/<name>.jsonl       ← teammate's full message history (resume on wake)
//
// Concurrency model:
//   - Single sync.RWMutex guards config + threads map.
//   - Each teammate goroutine owns its own messages slice.
//   - inbox/history file appends use a per-name file lock (chunkMu) — but
//     since each name only has one writer (the goroutine itself for history,
//     SendMessage callers serialized by a global appendMu for inbox), we
//     keep the locking coarse: appendMu for all writes.
//   - notifQ is guarded by notifMu.
//
// Compared to subagent (tools/task.go):
//   - subagent: one-shot, max 30 turns, returns final text, child history GC'd.
//   - teammate: long-lived, no turn cap; transitions working↔idle on each
//     LLM "end_turn" stop reason; child history persists on disk.

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"evo-agent/internal/ui"
)

// ── Constants ───────────────────────────────────────────────────────────────

const (
	teamBaseDirName    = ".evo-agent/team"
	teamInboxDirName   = "inbox"
	teamHistoryDirName = "history"
	teamConfigFile     = "config.json"

	// teamMaxMembers caps the number of teammates that can coexist (active or
	// idle). 8 is well above ref.md's 3-5 recommendation but bounds runaway
	// goroutine creation when the lead misuses team_spawn.
	teamMaxMembers = 8

	// teamMaxTurnsPerWake bounds how many LLM round-trips a single wake-up
	// can trigger before the goroutine forces an idle pause. Mirrors
	// subagentMaxTurns in spirit but is per-wake, not per-lifetime.
	teamMaxTurnsPerWake = 50

	// teamLeadName is the reserved inbox owner that the lead reads in its
	// own loop. Cannot be used as a teammate name.
	teamLeadName = "lead"

	// teamDefaultName for the team itself (single-team CLI; matches ref.py).
	teamDefaultName = "default"
)

// validMsgTypes mirrors refs/ref.py:VALID_MSG_TYPES exactly. Lead and
// teammates use these to tag inbox messages so each side can route them.
var validMsgTypes = map[string]bool{
	"message":                true,
	"broadcast":              true,
	"shutdown_request":       true,
	"shutdown_response":      true,
	"plan_approval":          true,
	"plan_approval_response": true,
}

// teamReservedToolsForTeammates is the set of tools teammates cannot call.
// Prevents recursive spawning + lets the lead retain authoritative control
// over team membership and shutdown.
var teamReservedToolsForTeammates = []string{
	"task",          // subagent — teammates already are sub-roles
	"team_spawn",    // only the lead can grow the team
	"team_shutdown", // only the lead can terminate teammates
}

// TeamGuidance is appended to the lead system prompt by main.go so the
// model knows when persistent teammates beat one-shot subagents.
const TeamGuidance = `# Persistent Teammates (Agent Teams)

The team_* tools spawn long-lived named teammates that live on disk under
.evo-agent/team/ and survive across compactions and restarts. They are NOT
the same as the task tool (one-shot subagent):

  task tool          → spawn -> run -> return summary -> destroyed
  team_spawn         → spawn -> work -> idle -> wake -> work -> shutdown

Use teammates when you need:
  - Multiple agents collaborating in parallel for a long-running effort
  - A named expert role you want to consult repeatedly (e.g. reviewer, qa)
  - Cross-cutting work where teammates message each other directly

Use the task tool (NOT teammates) for:
  - One-off explorations or single-shot research
  - Tasks that fit in a single turn — coordination overhead isn't worth it

## Tools

  team_spawn         — create or revive a named teammate
  team_list          — list every teammate with status (working/idle/shutdown)
  team_send_message  — send a message to a teammate's inbox
  team_read_inbox    — pull pending messages from your own (lead) inbox
  team_broadcast     — send the same message to every teammate
  team_shutdown      — gracefully stop a teammate (history is preserved)

When a teammate finishes its current turn (LLM stops without tool calls)
it goes idle and posts a notification on the lead's inbox. The next agent
loop turn surfaces those notifications to you automatically.`

// ── Types ───────────────────────────────────────────────────────────────────

// teammateRecord is the on-disk JSON record for one teammate.
type teammateRecord struct {
	Name         string `json:"name"`
	Role         string `json:"role"`
	Status       string `json:"status"`        // working | idle | shutdown
	SystemPrompt string `json:"system_prompt"` // captured at first spawn
	SpawnedAtMs  int64  `json:"spawned_at_ms"`
	LastActiveMs int64  `json:"last_active_ms"`
}

// teamConfig is the on-disk shape of config.json.
type teamConfig struct {
	TeamName string             `json:"team_name"`
	Members  []*teammateRecord  `json:"members"`
}

// teammateThread bundles the channel(s) the goroutine listens on. Owned
// exclusively by the goroutine after spawn. Only shutdownCh is needed —
// wake-up is handled by starting a fresh goroutine via wakeLocked when no
// thread is registered (matches refs/ref.py: each work cycle = one Python
// thread, idle = thread exits).
type teammateThread struct {
	shutdownCh chan struct{} // closed by Shutdown / Stop
}

// InboxMessage is one envelope sitting in inbox/<name>.jsonl. Mirrors
// ref.py's send/read shape: type+from+content+timestamp + extra fields.
type InboxMessage struct {
	Type      string         `json:"type"`
	From      string         `json:"from"`
	Content   string         `json:"content"`
	Timestamp float64        `json:"timestamp"` // unix seconds
	Extra     map[string]any `json:"extra,omitempty"`
}

// TeamNotification is what the goroutine pushes onto notifQ when a teammate
// transitions state. Lead's loop drains them at the top of each turn.
type TeamNotification struct {
	Name   string
	Role   string
	Status string // "idle" | "shutdown" | "error"
	Reason string // optional detail (last text block, error message)
	At     time.Time
}

// TeammateRunner is the agent-package-injected callback that performs ONE
// LLM call. tools/team.go owns the outer loop (history, tool dispatch,
// idle/shutdown transitions) so the runner stays minimal — it only
// translates (system + messages + tools) → *anthropic.Message.
type TeammateRunner func(ctx context.Context, systemPrompt string, messages []anthropic.MessageParam, tools []anthropic.ToolUnionParam) (*anthropic.Message, error)

var teammateRunner TeammateRunner

// RegisterTeammateRunner is called once by agent.New() to inject the LLM
// caller. tools/team.go cannot import internal/agent (cycle), so we use
// the same callback pattern as RegisterSubagentRunner.
func RegisterTeammateRunner(fn TeammateRunner) { teammateRunner = fn }

// ── TeamManager ─────────────────────────────────────────────────────────────

// TeamManager is the singleton that owns the team config, inbox files,
// teammate goroutines, and notification queue.
type TeamManager struct {
	mu sync.RWMutex

	baseDir    string // .evo-agent/team
	inboxDir   string // .evo-agent/team/inbox
	historyDir string // .evo-agent/team/history
	cfgPath    string

	cfg     *teamConfig
	threads map[string]*teammateThread

	appendMu sync.Mutex // serialize all inbox/history file appends

	notifMu sync.Mutex
	notifQ  []TeamNotification

	wg sync.WaitGroup // tracks live goroutines for graceful Stop
}

// GlobalTeam is the process-wide team manager. Initialised by main.go via
// (*TeamManager).Init(projectDir).
var GlobalTeam = &TeamManager{}

// Init creates / opens the .evo-agent/team/ tree, loads (or seeds)
// config.json, and rehydrates teammate records. It does NOT auto-restart
// goroutines for previously idle members — the lead must call team_spawn
// or team_send_message to wake them, which avoids surprise LLM traffic
// at startup.
//
// Stale "working" records left behind by a crashed run are downgraded to
// "idle" so the lead's next turn sees a clean roster.
func (m *TeamManager) Init(projectDir string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.baseDir = filepath.Join(projectDir, teamBaseDirName)
	m.inboxDir = filepath.Join(m.baseDir, teamInboxDirName)
	m.historyDir = filepath.Join(m.baseDir, teamHistoryDirName)
	m.cfgPath = filepath.Join(m.baseDir, teamConfigFile)
	m.threads = map[string]*teammateThread{}

	for _, d := range []string{m.baseDir, m.inboxDir, m.historyDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("team: mkdir %s: %w", d, err)
		}
	}

	cfg, err := m.loadConfigLocked()
	if err != nil {
		return err
	}
	for _, mem := range cfg.Members {
		if mem.Status == "working" {
			mem.Status = "idle"
		}
	}
	m.cfg = cfg
	if err := m.saveConfigLocked(); err != nil {
		return err
	}
	return nil
}

// Stop gracefully terminates every running teammate goroutine and waits
// for them to finish their current LLM call before returning. Called by
// main.go via defer at shutdown.
func (m *TeamManager) Stop() {
	m.mu.Lock()
	for _, th := range m.threads {
		select {
		case <-th.shutdownCh:
			// already closed
		default:
			close(th.shutdownCh)
		}
	}
	// Drop the threads map under the lock to prevent late SendMessage
	// calls from waking dying goroutines.
	m.threads = map[string]*teammateThread{}
	m.mu.Unlock()
	m.wg.Wait()
}

// ── Config persistence ──────────────────────────────────────────────────────

func (m *TeamManager) loadConfigLocked() (*teamConfig, error) {
	data, err := os.ReadFile(m.cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &teamConfig{TeamName: teamDefaultName, Members: []*teammateRecord{}}, nil
		}
		return nil, fmt.Errorf("team: read config: %w", err)
	}
	cfg := &teamConfig{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("team: parse config: %w", err)
	}
	if cfg.TeamName == "" {
		cfg.TeamName = teamDefaultName
	}
	if cfg.Members == nil {
		cfg.Members = []*teammateRecord{}
	}
	return cfg, nil
}

func (m *TeamManager) saveConfigLocked() error {
	data, err := json.MarshalIndent(m.cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("team: marshal config: %w", err)
	}
	tmp := m.cfgPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("team: write config: %w", err)
	}
	if err := os.Rename(tmp, m.cfgPath); err != nil {
		return fmt.Errorf("team: rename config: %w", err)
	}
	return nil
}

func (m *TeamManager) findMemberLocked(name string) *teammateRecord {
	for _, mem := range m.cfg.Members {
		if mem.Name == name {
			return mem
		}
	}
	return nil
}

// ── Spawn / Wake / Shutdown ─────────────────────────────────────────────────

// Spawn creates a new teammate or revives an idle/shutdown one. The
// teammate goroutine is started immediately and the prompt is queued on
// its inbox so the very next LLM turn picks it up.
//
// Returns a one-line status the model can show the user.
func (m *TeamManager) Spawn(name, role, prompt string) (string, error) {
	if teammateRunner == nil {
		return "", fmt.Errorf("team: runner not registered (agent.New() must run first)")
	}
	if err := validateTeammateName(name); err != nil {
		return "", err
	}
	if strings.TrimSpace(role) == "" {
		return "", fmt.Errorf("team: role is required")
	}
	if strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("team: prompt is required")
	}

	m.mu.Lock()
	if m.cfg == nil {
		m.mu.Unlock()
		return "", fmt.Errorf("team: not initialised — call Init() first")
	}
	mem := m.findMemberLocked(name)
	now := time.Now().UnixMilli()
	if mem == nil {
		// Cap only counts non-shutdown members; shutdowns are tombstones.
		active := 0
		for _, mm := range m.cfg.Members {
			if mm.Status != "shutdown" {
				active++
			}
		}
		if active >= teamMaxMembers {
			m.mu.Unlock()
			return "", fmt.Errorf("team: max %d active teammates reached", teamMaxMembers)
		}
		mem = &teammateRecord{
			Name:         name,
			Role:         role,
			SystemPrompt: buildTeammateSystemPrompt(name, role),
			SpawnedAtMs:  now,
		}
		m.cfg.Members = append(m.cfg.Members, mem)
	} else {
		// Reviving — keep history but refresh role if changed.
		if role != "" {
			mem.Role = role
		}
		if mem.Status == "shutdown" {
			// Reset history on full revive (shutdown is intended as a hard
			// stop; reviving from shutdown should not pollute the new
			// session with stale conversation).
			_ = os.Remove(m.historyPathLocked(name))
			mem.SystemPrompt = buildTeammateSystemPrompt(name, role)
			mem.SpawnedAtMs = now
		}
	}
	mem.Status = "working"
	mem.LastActiveMs = now

	// Persist BEFORE starting the goroutine + sending the inbox kickoff,
	// so that even if the process is killed mid-spawn we don't leak a
	// goroutine without a config record.
	if err := m.saveConfigLocked(); err != nil {
		m.mu.Unlock()
		return "", err
	}

	// Drop any lingering thread (e.g. from an idle goroutine that hasn't
	// fully exited yet). The new goroutine takes ownership.
	if old, ok := m.threads[name]; ok {
		select {
		case <-old.shutdownCh:
		default:
			close(old.shutdownCh)
		}
	}
	th := &teammateThread{
		shutdownCh: make(chan struct{}),
	}
	m.threads[name] = th
	systemPrompt := mem.SystemPrompt
	teamName := m.cfg.TeamName
	m.mu.Unlock()

	// Append the kickoff prompt to the teammate's inbox so the goroutine
	// sees it on the very first wake.
	if _, err := m.SendMessage(teamLeadName, name, prompt, "message", nil); err != nil {
		return "", err
	}

	m.wg.Add(1)
	go m.runTeammate(name, systemPrompt, th)

	m.emitTeamEvent()
	ui.PrintSystem(fmt.Sprintf("[team:%s] spawned %s (role=%s)", teamName, name, role))
	return fmt.Sprintf("Spawned teammate %q (role=%s, team=%s).", name, role, teamName), nil
}

// Shutdown signals the named teammate to exit cleanly. Returns an error
// if the teammate doesn't exist. A shutdown teammate's record stays in
// config (status=shutdown) so future team_list calls can see it.
func (m *TeamManager) Shutdown(name string) (string, error) {
	if name == teamLeadName {
		return "", fmt.Errorf("team: cannot shutdown lead")
	}
	m.mu.Lock()
	mem := m.findMemberLocked(name)
	if mem == nil {
		m.mu.Unlock()
		return "", fmt.Errorf("team: no such teammate %q", name)
	}
	if mem.Status == "shutdown" {
		m.mu.Unlock()
		return fmt.Sprintf("Teammate %q already shutdown.", name), nil
	}
	mem.Status = "shutdown"
	if err := m.saveConfigLocked(); err != nil {
		m.mu.Unlock()
		return "", err
	}
	if th, ok := m.threads[name]; ok {
		select {
		case <-th.shutdownCh:
		default:
			close(th.shutdownCh)
		}
		delete(m.threads, name)
	}
	m.mu.Unlock()

	m.pushNotif(TeamNotification{Name: name, Role: mem.Role, Status: "shutdown", At: time.Now()})
	m.emitTeamEvent()
	ui.PrintSystem(fmt.Sprintf("[team] %s shutdown", name))
	return fmt.Sprintf("Shutdown teammate %q.", name), nil
}

// wakeLocked re-arms a teammate's goroutine. If a goroutine is still live
// for this teammate (e.g. mid-burst), it will pick up the new inbox
// message on its next iteration — no signal needed. If the previous
// goroutine has exited (idle teammate, no thread registered) we start a
// fresh one that hydrates history and drains the inbox. Caller holds m.mu.
func (m *TeamManager) wakeLocked(name string) {
	mem := m.findMemberLocked(name)
	if mem == nil || mem.Status == "shutdown" {
		return
	}
	if _, ok := m.threads[name]; ok {
		// Goroutine is alive; it will see the new inbox message on its
		// next iteration via ReadInbox().
		return
	}
	// Idle (no live goroutine) → start a fresh one.
	th := &teammateThread{shutdownCh: make(chan struct{})}
	m.threads[name] = th
	mem.Status = "working"
	_ = m.saveConfigLocked()
	systemPrompt := mem.SystemPrompt
	m.wg.Add(1)
	go m.runTeammate(name, systemPrompt, th)
}

// ── Inbox / messaging ───────────────────────────────────────────────────────

// SendMessage appends an envelope to inbox/<to>.jsonl and, if the recipient
// is a known teammate, pings its goroutine via wakeLocked. Returns a short
// status string.
//
// Special recipient "lead" routes to the lead's own inbox file; the lead's
// agent.Loop drains it at the top of each turn.
func (m *TeamManager) SendMessage(from, to, content, msgType string, extra map[string]any) (string, error) {
	if msgType == "" {
		msgType = "message"
	}
	if !validMsgTypes[msgType] {
		return "", fmt.Errorf("team: invalid msg_type %q (valid: %s)", msgType, joinSortedKeys(validMsgTypes))
	}
	if strings.TrimSpace(to) == "" {
		return "", fmt.Errorf("team: recipient (to) is required")
	}
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("team: content is required")
	}

	m.mu.RLock()
	if m.baseDir == "" {
		m.mu.RUnlock()
		return "", fmt.Errorf("team: not initialised")
	}
	// Validate recipient exists (lead is always valid).
	if to != teamLeadName {
		if mem := m.findMemberLocked(to); mem == nil {
			m.mu.RUnlock()
			return "", fmt.Errorf("team: no such teammate %q", to)
		}
	}
	inboxPath := filepath.Join(m.inboxDir, to+".jsonl")
	m.mu.RUnlock()

	env := InboxMessage{
		Type:      msgType,
		From:      from,
		Content:   content,
		Timestamp: float64(time.Now().UnixNano()) / 1e9,
		Extra:     extra,
	}
	line, err := json.Marshal(env)
	if err != nil {
		return "", fmt.Errorf("team: marshal envelope: %w", err)
	}

	m.appendMu.Lock()
	f, err := os.OpenFile(inboxPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		m.appendMu.Unlock()
		return "", fmt.Errorf("team: open inbox: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()
		m.appendMu.Unlock()
		return "", fmt.Errorf("team: write inbox: %w", err)
	}
	_ = f.Close()
	m.appendMu.Unlock()

	// Wake the recipient (if it's a teammate). Lead doesn't have a
	// goroutine — its inbox is drained synchronously by agent.Loop.
	if to != teamLeadName {
		m.mu.Lock()
		m.wakeLocked(to)
		m.mu.Unlock()
	}

	return fmt.Sprintf("Sent %s to %s.", msgType, to), nil
}

// ReadInbox drains every pending message from the named inbox. The file
// is truncated atomically after a successful read so callers won't see
// the same envelope twice.
func (m *TeamManager) ReadInbox(name string) ([]InboxMessage, error) {
	m.mu.RLock()
	if m.baseDir == "" {
		m.mu.RUnlock()
		return nil, fmt.Errorf("team: not initialised")
	}
	inboxPath := filepath.Join(m.inboxDir, name+".jsonl")
	m.mu.RUnlock()

	m.appendMu.Lock()
	defer m.appendMu.Unlock()

	data, err := os.ReadFile(inboxPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("team: read inbox: %w", err)
	}
	if len(data) == 0 {
		return nil, nil
	}
	var out []InboxMessage
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var msg InboxMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			// Skip malformed lines but keep the rest.
			ui.PrintError(fmt.Sprintf("[team] skip malformed inbox line for %s: %v", name, err))
			continue
		}
		out = append(out, msg)
	}
	// Truncate after successful parse.
	if err := os.WriteFile(inboxPath, nil, 0o644); err != nil {
		return out, fmt.Errorf("team: truncate inbox: %w", err)
	}
	return out, nil
}

// Broadcast sends content to every member except the sender. Skips
// shutdown members. msg_type is fixed to "broadcast".
func (m *TeamManager) Broadcast(from, content string) (string, error) {
	m.mu.RLock()
	if m.cfg == nil {
		m.mu.RUnlock()
		return "", fmt.Errorf("team: not initialised")
	}
	targets := make([]string, 0, len(m.cfg.Members))
	for _, mem := range m.cfg.Members {
		if mem.Name == from || mem.Status == "shutdown" {
			continue
		}
		targets = append(targets, mem.Name)
	}
	m.mu.RUnlock()

	count := 0
	for _, t := range targets {
		if _, err := m.SendMessage(from, t, content, "broadcast", nil); err == nil {
			count++
		}
	}
	return fmt.Sprintf("Broadcast to %d teammate(s).", count), nil
}

// ── Snapshots / prompt injection ────────────────────────────────────────────

// List returns a formatted multi-line string describing every teammate.
// Used by team_list and the /team list slash command.
func (m *TeamManager) List() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cfg == nil || len(m.cfg.Members) == 0 {
		return "No teammates."
	}
	out := []string{fmt.Sprintf("Team: %s", m.cfg.TeamName)}
	now := time.Now().UnixMilli()
	for _, mem := range m.cfg.Members {
		ago := ""
		if mem.LastActiveMs > 0 {
			ago = " — last active " + humanMs(now-mem.LastActiveMs) + " ago"
		}
		out = append(out, fmt.Sprintf("  %s (%s): %s%s", mem.Name, mem.Role, mem.Status, ago))
	}
	return strings.Join(out, "\n")
}

// Snapshot returns a copy of the current member roster for the TUI / EvTeam.
func (m *TeamManager) Snapshot() (string, []ui.TeammateSnapshot) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cfg == nil {
		return "", nil
	}
	out := make([]ui.TeammateSnapshot, 0, len(m.cfg.Members))
	for _, mem := range m.cfg.Members {
		out = append(out, ui.TeammateSnapshot{
			Name:         mem.Name,
			Role:         mem.Role,
			Status:       mem.Status,
			LastActiveMs: mem.LastActiveMs,
		})
	}
	return m.cfg.TeamName, out
}

// LoadPrompt returns the system-prompt string injected by prompt.Builder
// so the lead always sees its current roster. Empty when no members.
func (m *TeamManager) LoadPrompt() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cfg == nil || len(m.cfg.Members) == 0 {
		return ""
	}
	out := []string{fmt.Sprintf("# Active Team (%s)", m.cfg.TeamName)}
	now := time.Now().UnixMilli()
	for _, mem := range m.cfg.Members {
		if mem.Status == "shutdown" {
			out = append(out, fmt.Sprintf("- %s (%s): shutdown", mem.Name, mem.Role))
			continue
		}
		ago := ""
		if mem.LastActiveMs > 0 {
			ago = " — last active " + humanMs(now-mem.LastActiveMs) + " ago"
		}
		out = append(out, fmt.Sprintf("- %s (%s): %s%s", mem.Name, mem.Role, mem.Status, ago))
	}
	out = append(out, "", "Use team_send_message to assign work; team_shutdown when done.")
	return strings.Join(out, "\n")
}

// ── Notifications ───────────────────────────────────────────────────────────

func (m *TeamManager) pushNotif(n TeamNotification) {
	m.notifMu.Lock()
	m.notifQ = append(m.notifQ, n)
	m.notifMu.Unlock()
}

// DrainNotifications returns and clears the pending status events. Called
// at the top of agent.Loop so the model sees idle/shutdown/error events
// without polling.
func (m *TeamManager) DrainNotifications() []TeamNotification {
	m.notifMu.Lock()
	defer m.notifMu.Unlock()
	if len(m.notifQ) == 0 {
		return nil
	}
	out := m.notifQ
	m.notifQ = nil
	return out
}

// emitTeamEvent broadcasts the current roster to the TUI. Safe to call
// from any goroutine; the sink is non-blocking.
func (m *TeamManager) emitTeamEvent() {
	name, snap := m.Snapshot()
	ui.EmitTeam(name, snap)
}

// ── Goroutine: the teammate loop ────────────────────────────────────────────

// runTeammate is the per-wake teammate goroutine. One goroutine = one
// work cycle: hydrate history, drain inbox, run an LLM tool-use burst
// until either no inbox + no tool calls remain (→ idle) or shutdown.
//
// Wake-up is handled by wakeLocked starting a fresh goroutine when no
// thread is registered for this name — there is no long-lived sleep loop
// in this function, which avoids the dead-thread-still-holding-channel
// pitfall that bit the first version of this code.
func (m *TeamManager) runTeammate(name, systemPrompt string, th *teammateThread) {
	defer m.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			ui.PrintError(fmt.Sprintf("[team:%s] panic: %v", name, r))
			m.markIdle(name, "error", fmt.Sprintf("panic: %v", r))
		}
	}()

	// Hydrate history from disk (resume case).
	messages := m.loadHistory(name)

	for {
		// Honour shutdown signals between iterations.
		select {
		case <-th.shutdownCh:
			m.applyShutdown(name)
			return
		default:
		}

		inbox, err := m.ReadInbox(name)
		if err != nil {
			ui.PrintError(fmt.Sprintf("[team:%s] inbox read failed: %v", name, err))
			m.markIdle(name, "error", err.Error())
			return
		}
		if len(inbox) == 0 {
			// No work pending → return to idle. wakeLocked will spawn a
			// fresh goroutine when the next inbox message arrives.
			m.markIdle(name, "idle", "")
			return
		}

		// Append inbox as a single user message (JSON so the model can
		// route by type/from).
		inboxText, _ := json.MarshalIndent(inbox, "", "  ")
		userMsg := anthropic.NewUserMessage(anthropic.NewTextBlock("<inbox>" + string(inboxText) + "</inbox>"))
		messages = append(messages, userMsg)
		m.appendHistory(name, userMsg)

		// Honour shutdown_request inbox messages immediately. The fancy
		// "teammate may reject" protocol from ref.md is left for v2.
		shutdown := false
		for _, im := range inbox {
			if im.Type == "shutdown_request" {
				ui.PrintSystem(fmt.Sprintf("[team:%s] shutdown_request from %s", name, im.From))
				ack := anthropic.NewAssistantMessage(anthropic.NewTextBlock("Acknowledged shutdown."))
				messages = append(messages, ack)
				m.appendHistory(name, ack)
				if _, err := m.SendMessage(name, im.From, "shutdown_response: ok", "shutdown_response", nil); err != nil {
					ui.PrintError(fmt.Sprintf("[team:%s] shutdown_response send failed: %v", name, err))
				}
				shutdown = true
				break
			}
		}
		if shutdown {
			m.applyShutdown(name)
			return
		}

		// Run the LLM tool-use burst. Returns false if shutdown was
		// observed mid-burst.
		if !m.runWorkBurst(name, systemPrompt, &messages, th) {
			return
		}

		// After the burst, loop back: drain any inbox messages that
		// arrived during the burst, otherwise idle out.
	}
}

// runWorkBurst runs up to teamMaxTurnsPerWake LLM round-trips, executing
// tool calls between them. Returns true when the burst ended naturally
// (LLM stop_reason != tool_use), false when a shutdown was observed.
func (m *TeamManager) runWorkBurst(name, systemPrompt string, messagesPtr *[]anthropic.MessageParam, th *teammateThread) bool {
	tools := m.teammateTools()

	for turn := 0; turn < teamMaxTurnsPerWake; turn++ {
		select {
		case <-th.shutdownCh:
			return false
		default:
		}

		resp, err := teammateRunner(context.Background(), systemPrompt, *messagesPtr, tools)
		if err != nil {
			ui.PrintError(fmt.Sprintf("[team:%s] LLM error: %v", name, err))
			m.markIdle(name, "error", err.Error())
			return false
		}
		assistantMsg := resp.ToParam()
		*messagesPtr = append(*messagesPtr, assistantMsg)
		m.appendHistory(name, assistantMsg)

		// Capture last text + execute tool_use blocks. We do our own
		// dispatch (rather than tools.Execute) so we can prefix UI output
		// with the teammate's name, matching the [subagent] convention in
		// agent/subagent.go.
		var toolResults []anthropic.ContentBlockParamUnion
		var lastText string
		for _, blk := range resp.Content {
			switch v := blk.AsAny().(type) {
			case anthropic.TextBlock:
				lastText = v.Text
				ui.PrintText(fmt.Sprintf("[team:%s] %s", name, v.Text))

			case anthropic.ThinkingBlock:
				ui.PrintThinking(fmt.Sprintf("[team:%s] %s", name, v.Thinking))

			case anthropic.ToolUseBlock:
				inputRaw := v.JSON.Input.Raw()
				ui.PrintToolCall(v.ID, fmt.Sprintf("[team:%s] %s", name, v.Name), inputRaw)

				inputBytes, _ := json.Marshal(v.Input)
				out, dispErr := Dispatch(v.Name, inputBytes)
				isError := dispErr != nil
				if isError {
					out = dispErr.Error()
				} else {
					out = PersistLargeOutput(v.ID, out)
				}
				ui.PrintToolResult(v.ID, out, isError)
				toolResults = append(toolResults, anthropic.NewToolResultBlock(v.ID, out, isError))
			}
		}

		// Refresh last-active timestamp on every LLM round-trip.
		m.touchLastActive(name)

		if len(toolResults) == 0 {
			// Natural turn end — push a notification so the lead sees the
			// final summary even if the teammate didn't message it directly.
			if lastText != "" {
				m.pushNotif(TeamNotification{
					Name:   name,
					Role:   m.roleOf(name),
					Status: "idle",
					Reason: lastText,
					At:     time.Now(),
				})
			} else {
				m.pushNotif(TeamNotification{Name: name, Role: m.roleOf(name), Status: "idle", At: time.Now()})
			}
			return true
		}

		userMsg := anthropic.NewUserMessage(toolResults...)
		*messagesPtr = append(*messagesPtr, userMsg)
		m.appendHistory(name, userMsg)
	}

	// Burst cap hit; pretend it's an idle-with-warning.
	ui.PrintSystem(fmt.Sprintf("[team:%s] burst cap %d hit, going idle", name, teamMaxTurnsPerWake))
	m.pushNotif(TeamNotification{Name: name, Role: m.roleOf(name), Status: "idle", Reason: "burst cap reached", At: time.Now()})
	return true
}

// teammateTools returns the set of tools a teammate can call. Reserved
// names (task, team_spawn, team_shutdown) are stripped to prevent
// recursive spawning and protect lead authority.
func (m *TeamManager) teammateTools() []anthropic.ToolUnionParam {
	return ToolsExcept(teamReservedToolsForTeammates...)
}

// markIdle persists status=idle, refreshes last_active_ms, removes the
// thread entry, and emits a UI/team event. The goroutine returning is
// what actually frees its stack — markIdle just records the state.
func (m *TeamManager) markIdle(name, status, reason string) {
	m.mu.Lock()
	mem := m.findMemberLocked(name)
	if mem == nil {
		m.mu.Unlock()
		return
	}
	if mem.Status != "shutdown" {
		mem.Status = "idle"
	}
	mem.LastActiveMs = time.Now().UnixMilli()
	if err := m.saveConfigLocked(); err != nil {
		ui.PrintError(fmt.Sprintf("[team:%s] save config: %v", name, err))
	}
	delete(m.threads, name)
	m.mu.Unlock()
	if status == "error" {
		m.pushNotif(TeamNotification{Name: name, Role: m.roleOf(name), Status: "error", Reason: reason, At: time.Now()})
	}
	m.emitTeamEvent()
}

// applyShutdown finalizes a teammate's record after it acknowledges a
// shutdown request from inside its own goroutine. Caller must NOT hold m.mu.
func (m *TeamManager) applyShutdown(name string) {
	m.mu.Lock()
	mem := m.findMemberLocked(name)
	if mem != nil {
		mem.Status = "shutdown"
		mem.LastActiveMs = time.Now().UnixMilli()
		_ = m.saveConfigLocked()
	}
	delete(m.threads, name)
	m.mu.Unlock()
	m.pushNotif(TeamNotification{Name: name, Status: "shutdown", At: time.Now()})
	m.emitTeamEvent()
}

func (m *TeamManager) roleOf(name string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if mem := m.findMemberLocked(name); mem != nil {
		return mem.Role
	}
	return ""
}

func (m *TeamManager) touchLastActive(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if mem := m.findMemberLocked(name); mem != nil {
		mem.LastActiveMs = time.Now().UnixMilli()
		_ = m.saveConfigLocked()
	}
}

// ── History persistence ─────────────────────────────────────────────────────

func (m *TeamManager) historyPathLocked(name string) string {
	return filepath.Join(m.historyDir, name+".jsonl")
}

func (m *TeamManager) historyPath(name string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.historyPathLocked(name)
}

// loadHistory reads the on-disk transcript and returns the rebuilt slice.
// Empty / missing file → empty slice. Used when the goroutine starts up.
func (m *TeamManager) loadHistory(name string) []anthropic.MessageParam {
	path := m.historyPath(name)
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			ui.PrintError(fmt.Sprintf("[team:%s] history read: %v", name, err))
		}
		return nil
	}
	if len(data) == 0 {
		return nil
	}
	var out []anthropic.MessageParam
	for i, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var msg anthropic.MessageParam
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			ui.PrintError(fmt.Sprintf("[team:%s] history line %d malformed: %v", name, i, err))
			continue
		}
		out = append(out, msg)
	}
	return out
}

func (m *TeamManager) appendHistory(name string, msg anthropic.MessageParam) {
	path := m.historyPath(name)
	line, err := json.Marshal(msg)
	if err != nil {
		ui.PrintError(fmt.Sprintf("[team:%s] history marshal: %v", name, err))
		return
	}
	m.appendMu.Lock()
	defer m.appendMu.Unlock()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		ui.PrintError(fmt.Sprintf("[team:%s] history open: %v", name, err))
		return
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		ui.PrintError(fmt.Sprintf("[team:%s] history write: %v", name, err))
	}
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func validateTeammateName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("team: name is required")
	}
	if name == teamLeadName {
		return fmt.Errorf("team: %q is reserved", teamLeadName)
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return fmt.Errorf("team: name contains invalid char %q (allowed: a-zA-Z0-9-_)", r)
		}
	}
	if len(name) > 32 {
		return fmt.Errorf("team: name too long (max 32 chars)")
	}
	return nil
}

func buildTeammateSystemPrompt(name, role string) string {
	return fmt.Sprintf(
		"You are '%s', role: %s. You are a persistent teammate inside an evo-agent agent team. "+
			"Your inbox is delivered to you as a <inbox>...</inbox> user message at the start of each wake. "+
			"Use team_send_message to reply to the lead or another teammate. When your task is finished, "+
			"emit a final text summary and stop calling tools — you will go idle and the lead will see your "+
			"summary as a notification. Never call task / team_spawn / team_shutdown — those are lead-only.",
		name, role,
	)
}

func humanMs(ms int64) string {
	if ms <= 0 {
		return "just now"
	}
	sec := ms / 1000
	switch {
	case sec < 60:
		return fmt.Sprintf("%ds", sec)
	case sec < 3600:
		return fmt.Sprintf("%dm", sec/60)
	default:
		return fmt.Sprintf("%dh", sec/3600)
	}
}

func joinSortedKeys(set map[string]bool) string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// FormatTeamInbox renders a slice of inbox messages as a synthetic
// <team-inbox> user-message body for injection into the lead loop.
func FormatTeamInbox(msgs []InboxMessage) string {
	if len(msgs) == 0 {
		return ""
	}
	body, err := json.MarshalIndent(msgs, "", "  ")
	if err != nil {
		return fmt.Sprintf("<team-inbox>(failed to marshal: %v)</team-inbox>", err)
	}
	return "<team-inbox>" + string(body) + "</team-inbox>"
}

// FormatTeamNotifications renders a slice of TeamNotifications as a
// synthetic <team-notifications> user-message body.
func FormatTeamNotifications(notifs []TeamNotification) string {
	if len(notifs) == 0 {
		return ""
	}
	lines := []string{"<team-notifications>"}
	for _, n := range notifs {
		base := fmt.Sprintf("- %s (%s) → %s", n.Name, n.Role, n.Status)
		if n.Reason != "" {
			// Trim huge reasons (LLM final-text dumps) to keep the inbox notice compact.
			r := n.Reason
			if len(r) > 800 {
				r = r[:800] + "…"
			}
			base += ": " + r
		}
		lines = append(lines, base)
	}
	lines = append(lines, "</team-notifications>")
	return strings.Join(lines, "\n")
}

// ── Tool registration ───────────────────────────────────────────────────────

type teamSpawnInput struct {
	Name   string `json:"name"   jsonschema_description:"Unique teammate name (a-zA-Z0-9-_, max 32 chars). Becomes the inbox file name."`
	Role   string `json:"role"   jsonschema_description:"Short role label, e.g. 'coder', 'reviewer', 'qa'."`
	Prompt string `json:"prompt" jsonschema_description:"Initial task description sent as the first inbox message."`
}

type teamListInput struct{}

type teamSendMessageInput struct {
	To      string `json:"to"               jsonschema_description:"Recipient teammate name (or 'lead')."`
	Content string `json:"content"          jsonschema_description:"Message body."`
	MsgType string `json:"msg_type,omitempty" jsonschema_description:"Envelope type: message (default), shutdown_request, plan_approval, plan_approval_response, broadcast, shutdown_response."`
}

type teamReadInboxInput struct{}

type teamBroadcastInput struct {
	Content string `json:"content" jsonschema_description:"Body sent to every active teammate (skipping shutdown ones)."`
}

type teamShutdownInput struct {
	Name string `json:"name" jsonschema_description:"Teammate name to shut down. History is preserved on disk."`
}

func init() {
	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name: "team_spawn",
			Description: anthropic.String(
				"Spawn a persistent named teammate that runs in its own goroutine and survives across compactions. " +
					"Use for parallel collaboration with named roles (e.g. coder + reviewer + qa). " +
					"For single-shot delegation prefer the task tool instead."),
			InputSchema: GenerateSchema[teamSpawnInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in teamSpawnInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}
			return GlobalTeam.Spawn(in.Name, in.Role, in.Prompt)
		},
	})

	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name:        "team_list",
			Description: anthropic.String("List every teammate with role, status (working/idle/shutdown), and last-active timestamp."),
			InputSchema: GenerateSchema[teamListInput](),
		},
		Handler: func(_ json.RawMessage) (string, error) {
			return GlobalTeam.List(), nil
		},
	})

	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name: "team_send_message",
			Description: anthropic.String(
				"Send a message to a teammate's inbox. The recipient wakes immediately if idle. " +
					"Use msg_type=shutdown_request to ask a teammate to wind down (it will reply with shutdown_response and go to shutdown). " +
					"Address the lead with to='lead'."),
			InputSchema: GenerateSchema[teamSendMessageInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in teamSendMessageInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}
			return GlobalTeam.SendMessage(teamLeadName, in.To, in.Content, in.MsgType, nil)
		},
	})

	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name: "team_read_inbox",
			Description: anthropic.String(
				"Drain pending messages from the lead's own inbox. " +
					"Normally unnecessary — agent.Loop drains it automatically at the top of every turn — " +
					"but useful for explicit sync points (e.g. waiting on a plan_approval_response)."),
			InputSchema: GenerateSchema[teamReadInboxInput](),
		},
		Handler: func(_ json.RawMessage) (string, error) {
			msgs, err := GlobalTeam.ReadInbox(teamLeadName)
			if err != nil {
				return "", err
			}
			if len(msgs) == 0 {
				return "(empty)", nil
			}
			body, _ := json.MarshalIndent(msgs, "", "  ")
			return string(body), nil
		},
	})

	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name:        "team_broadcast",
			Description: anthropic.String("Send the same message to every active teammate (skipping shutdown ones)."),
			InputSchema: GenerateSchema[teamBroadcastInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in teamBroadcastInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}
			return GlobalTeam.Broadcast(teamLeadName, in.Content)
		},
	})

	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name:        "team_shutdown",
			Description: anthropic.String("Stop a teammate. The record stays in config (status=shutdown) and the history is preserved on disk."),
			InputSchema: GenerateSchema[teamShutdownInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in teamShutdownInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}
			return GlobalTeam.Shutdown(in.Name)
		},
	})
}
