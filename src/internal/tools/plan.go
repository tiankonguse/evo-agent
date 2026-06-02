package tools

import (
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

// ── PlanManager: persistent task graph on disk ───────────────────────────────
// Directory layout:
//   .evo-agent/tasks/
//     todo/2026-05-28-xxx/
//       plan.md          ← analysis + steps
//       task_1.json      ← individual task records
//       task_2.json
//     done/2026-05-28-xxx/
//       plan.md
//       task_1.json (completed)

const (
	tasksBaseDir         = ".evo-agent/tasks"
	tasksTodoDir         = "todo"
	tasksDoneDir         = "done"
	planReminderInterval = 5 // rounds without plan tool usage before reminder fires
)

// PlanGuidance is the system-prompt text explaining when to use session plans.
const PlanGuidance = `# Task Planning (Session Plan + Memory Plan)

Session Plan (plan_* tools) is for big tasks that persist on disk across context compressions:
- Create a session plan for the overall goal with plan_create
- Split into ordered tasks with plan_task_create (dependency graph)
- Each task in the session plan is a standalone deliverable

Memory Plan (todo_* tools) is for the small steps within ONE session plan task:
- When starting a session plan task, use todo_init to set the current sub-goal
- Break that task into 2-5 concrete steps with todo_create
- Mark steps in_progress/completed as you work through them

Workflow (big task → small steps):
1. plan_create — Define the big task: requirements, approach, architecture
2. plan_task_create — Split into ordered tasks with dependencies
3. For each task in order:
   a. plan_task_update(status="in_progress") — Claim the task
   b. todo_init(topic) — Set the sub-goal for this task
   c. todo_create — Break into small executable steps
   d. Execute steps, updating todo_complete as you go
   e. plan_task_update(status="completed") — Mark task done
4. plan_complete — Archive when all tasks are done`

// planTaskRecord is the on-disk JSON format for a single task.
type planTaskRecord struct {
	ID          int    `json:"id"`
	Subject     string `json:"subject"`
	Description string `json:"description"`
	Status      string `json:"status"` // pending, in_progress, completed, deleted
	BlockedBy   []int  `json:"blockedBy"`
	Blocks      []int  `json:"blocks"`
	Owner       string `json:"owner"`
}

// PlanManager manages session plans with task dependency graphs.
type PlanManager struct {
	mu                sync.RWMutex
	baseDir           string // resolved tasks/ path
	roundsSinceUpdate int    // rounds since last plan tool usage
}

// GlobalPlan is the process-wide session plan manager.
var GlobalPlan = &PlanManager{}

// planToolNames holds the registered plan tool names for precise identification.
var planToolNames = map[string]bool{}

// IsPlanTool returns true if the given tool name is a registered plan tool.
func IsPlanTool(name string) bool {
	return planToolNames[name]
}

// CheckPlanUsed scans content blocks and returns true if any plan tool was called.
func CheckPlanUsed(blocks []anthropic.ContentBlockUnion) bool {
	for _, block := range blocks {
		if block.Type == "tool_use" && planToolNames[block.Name] {
			return true
		}
	}
	return false
}

// InitPlan sets the base directory for session plans.
// Called from main.go at startup.
func InitPlan(projectDir string) {
	GlobalPlan.mu.Lock()
	defer GlobalPlan.mu.Unlock()
	GlobalPlan.baseDir = filepath.Join(projectDir, tasksBaseDir)
	// Ensure directories exist
	os.MkdirAll(filepath.Join(GlobalPlan.baseDir, tasksTodoDir), 0o755)
	os.MkdirAll(filepath.Join(GlobalPlan.baseDir, tasksDoneDir), 0o755)
}

// ── Internal helpers ─────────────────────────────────────────────────────────

func (pm *PlanManager) todoDir() string {
	return filepath.Join(pm.baseDir, tasksTodoDir)
}

func (pm *PlanManager) doneDir() string {
	return filepath.Join(pm.baseDir, tasksDoneDir)
}

func (pm *PlanManager) planDir(name string) string {
	return filepath.Join(pm.todoDir(), name)
}

func (pm *PlanManager) loadTask(planName string, taskID int) (*planTaskRecord, error) {
	path := filepath.Join(pm.planDir(planName), fmt.Sprintf("task_%d.json", taskID))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("task %d not found in plan %q", taskID, planName)
	}
	var task planTaskRecord
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, fmt.Errorf("failed to parse task %d: %v", taskID, err)
	}
	return &task, nil
}

func (pm *PlanManager) saveTask(planName string, task *planTaskRecord) error {
	dir := pm.planDir(planName)
	path := filepath.Join(dir, fmt.Sprintf("task_%d.json", task.ID))
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (pm *PlanManager) nextTaskID(planName string) int {
	dir := pm.planDir(planName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 1
	}
	maxID := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "task_") && strings.HasSuffix(e.Name(), ".json") {
			var id int
			fmt.Sscanf(e.Name(), "task_%d.json", &id)
			if id > maxID {
				maxID = id
			}
		}
	}
	return maxID + 1
}

func (pm *PlanManager) listTaskFiles(planName string) ([]*planTaskRecord, error) {
	dir := pm.planDir(planName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("plan %q not found", planName)
	}
	var tasks []*planTaskRecord
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "task_") || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var task planTaskRecord
		if err := json.Unmarshal(data, &task); err != nil {
			continue
		}
		tasks = append(tasks, &task)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	return tasks, nil
}

// clearDependency removes completedID from all tasks' BlockedBy lists.
func (pm *PlanManager) clearDependency(planName string, completedID int) {
	tasks, err := pm.listTaskFiles(planName)
	if err != nil {
		return
	}
	for _, task := range tasks {
		changed := false
		newBlocked := make([]int, 0, len(task.BlockedBy))
		for _, id := range task.BlockedBy {
			if id == completedID {
				changed = true
			} else {
				newBlocked = append(newBlocked, id)
			}
		}
		if changed {
			task.BlockedBy = newBlocked
			pm.saveTask(planName, task)
		}
	}
}

// ── Public operations ────────────────────────────────────────────────────────

// Create creates a new plan directory with plan.md.
func (pm *PlanManager) Create(name, planContent string) (string, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	dir := pm.planDir(name)
	if _, err := os.Stat(dir); err == nil {
		return "", fmt.Errorf("plan %q already exists", name)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create plan directory: %v", err)
	}
	planPath := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(planPath, []byte(planContent), 0o644); err != nil {
		return "", fmt.Errorf("failed to write plan.md: %v", err)
	}
	return fmt.Sprintf("Plan created: %s\nPath: %s", name, dir), nil
}

// CreateForGoal is a client-callable helper that creates a plan associated
// with a /goal command. It builds a minimal plan.md from the goal text and
// approach hint and calls Create.
//
// Used by the /goal slash command in main.go to spin up a persistent plan
// alongside the in-memory goal so the work survives session restarts.
func (pm *PlanManager) CreateForGoal(name, goalText, approach string) (string, error) {
	body := fmt.Sprintf(`# Plan: %s

## Goal
%s

## Approach
%s

## Tasks
(empty — use plan_task_create to add tasks; the agent will populate this as it
breaks the goal into steps)

## Source
This plan was created by the /goal slash command. Created at %s.
`,
		name,
		strings.TrimSpace(goalText),
		strings.TrimSpace(approach),
		time.Now().Format(time.RFC3339),
	)
	return pm.Create(name, body)
}

// DerivePlanName slugifies a goal description into a plan directory name in
// the project's `YYYY-MM-DD-<slug>` convention. Exported so the /goal slash
// command can compute the same name it will pass to CreateForGoal.
func DerivePlanName(goalText string) string {
	date := time.Now().Format("2006-01-02")
	slug := slugify(goalText)
	if slug == "" {
		slug = "goal"
	}
	if len(slug) > 40 {
		slug = slug[:40]
	}
	return date + "-" + slug
}

// slugify lowercases, replaces non-alphanumeric runs with single dashes, and
// trims dashes from the ends. Keeps ASCII-only output so directory names
// stay portable across filesystems.
func slugify(s string) string {
	var b strings.Builder
	prevDash := true // suppresses leading dashes
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := b.String()
	out = strings.TrimRight(out, "-")
	return out
}

// List lists all plans in todo/ and done/ directories.
func (pm *PlanManager) List() string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var lines []string

	// List todo plans
	todoEntries, _ := os.ReadDir(pm.todoDir())
	if len(todoEntries) > 0 {
		lines = append(lines, "## Active Plans (todo)")
		for _, e := range todoEntries {
			if !e.IsDir() {
				continue
			}
			tasks, _ := pm.listTaskFiles(e.Name())
			completed := 0
			total := len(tasks)
			for _, t := range tasks {
				if t.Status == "completed" {
					completed++
				}
			}
			progress := ""
			if total > 0 {
				progress = fmt.Sprintf(" [%d/%d tasks done]", completed, total)
			}
			lines = append(lines, fmt.Sprintf("  [>] %s%s", e.Name(), progress))
		}
	}

	// List done plans
	doneEntries, _ := os.ReadDir(pm.doneDir())
	if len(doneEntries) > 0 {
		lines = append(lines, "## Completed Plans (done)")
		for _, e := range doneEntries {
			if !e.IsDir() {
				continue
			}
			lines = append(lines, fmt.Sprintf("  [x] %s", e.Name()))
		}
	}

	if len(lines) == 0 {
		return "No plans."
	}
	return strings.Join(lines, "\n")
}

// TaskCreate creates a new task record in the given plan.
func (pm *PlanManager) TaskCreate(planName, subject, description string, blockedBy []int) (string, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	dir := pm.planDir(planName)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return "", fmt.Errorf("plan %q does not exist", planName)
	}

	id := pm.nextTaskID(planName)
	task := &planTaskRecord{
		ID:          id,
		Subject:     subject,
		Description: description,
		Status:      "pending",
		BlockedBy:   blockedBy,
		Blocks:      []int{},
		Owner:       "",
	}
	if task.BlockedBy == nil {
		task.BlockedBy = []int{}
	}

	// Bidirectional: update blocking tasks' Blocks list
	for _, blockerID := range blockedBy {
		blocker, err := pm.loadTask(planName, blockerID)
		if err == nil {
			blocker.Blocks = append(blocker.Blocks, id)
			pm.saveTask(planName, blocker)
		}
	}

	if err := pm.saveTask(planName, task); err != nil {
		return "", err
	}

	data, _ := json.MarshalIndent(task, "", "  ")
	return string(data), nil
}

// TaskUpdate updates a task's status, owner, or dependencies.
func (pm *PlanManager) TaskUpdate(planName string, taskID int, status, owner string, addBlockedBy, addBlocks []int) (string, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	task, err := pm.loadTask(planName, taskID)
	if err != nil {
		return "", err
	}

	if owner != "" {
		task.Owner = owner
	}
	if status != "" {
		switch status {
		case "pending", "in_progress", "completed", "deleted":
		default:
			return "", fmt.Errorf("invalid status: %q", status)
		}
		// Enforce single-active plan: only one plan can have in_progress tasks
		if status == "in_progress" {
			active := pm.activePlanName()
			if active != "" && active != planName {
				return "", fmt.Errorf("plan %q already has in_progress tasks — complete it before starting work in %q", active, planName)
			}
		}
		task.Status = status
		if status == "completed" {
			pm.clearDependency(planName, taskID)
		}
	}
	if len(addBlockedBy) > 0 {
		existing := make(map[int]bool)
		for _, id := range task.BlockedBy {
			existing[id] = true
		}
		for _, id := range addBlockedBy {
			if !existing[id] {
				task.BlockedBy = append(task.BlockedBy, id)
				existing[id] = true
			}
		}
	}
	if len(addBlocks) > 0 {
		existing := make(map[int]bool)
		for _, id := range task.Blocks {
			existing[id] = true
		}
		for _, id := range addBlocks {
			if !existing[id] {
				task.Blocks = append(task.Blocks, id)
				existing[id] = true
				// Bidirectional: update blocked tasks
				blocked, err := pm.loadTask(planName, id)
				if err == nil {
					hasIt := false
					for _, b := range blocked.BlockedBy {
						if b == taskID {
							hasIt = true
							break
						}
					}
					if !hasIt {
						blocked.BlockedBy = append(blocked.BlockedBy, taskID)
						pm.saveTask(planName, blocked)
					}
				}
			}
		}
	}

	if err := pm.saveTask(planName, task); err != nil {
		return "", err
	}

	data, _ := json.MarshalIndent(task, "", "  ")
	return string(data), nil
}

// TaskList lists all tasks in a plan with status summary.
func (pm *PlanManager) TaskList(planName string) (string, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	tasks, err := pm.listTaskFiles(planName)
	if err != nil {
		return "", err
	}
	if len(tasks) == 0 {
		return fmt.Sprintf("No tasks in plan %q.", planName), nil
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("# Plan: %s", planName))
	completed := 0
	for _, t := range tasks {
		marker := map[string]string{
			"pending":     "[ ]",
			"in_progress": "[>]",
			"completed":   "[x]",
			"deleted":     "[-]",
		}[t.Status]
		if marker == "" {
			marker = "[?]"
		}
		blocked := ""
		if len(t.BlockedBy) > 0 {
			blocked = fmt.Sprintf(" (blocked by: %v)", t.BlockedBy)
		}
		owner := ""
		if t.Owner != "" {
			owner = fmt.Sprintf(" owner=%s", t.Owner)
		}
		lines = append(lines, fmt.Sprintf("%s #%d: %s%s%s", marker, t.ID, t.Subject, owner, blocked))
		if t.Status == "completed" {
			completed++
		}
	}
	lines = append(lines, fmt.Sprintf("(%d/%d completed)", completed, len(tasks)))
	return strings.Join(lines, "\n"), nil
}

// TaskGet returns full details of a single task.
func (pm *PlanManager) TaskGet(planName string, taskID int) (string, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	task, err := pm.loadTask(planName, taskID)
	if err != nil {
		return "", err
	}
	data, _ := json.MarshalIndent(task, "", "  ")
	return string(data), nil
}

// Complete moves a finished plan from todo/ to done/.
func (pm *PlanManager) Complete(planName string) (string, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	src := pm.planDir(planName)
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return "", fmt.Errorf("plan %q not found in todo/", planName)
	}

	// Verify all tasks are completed or deleted
	tasks, _ := pm.listTaskFiles(planName)
	for _, t := range tasks {
		if t.Status != "completed" && t.Status != "deleted" {
			return "", fmt.Errorf("task #%d (%s) is still %s — complete all tasks first", t.ID, t.Subject, t.Status)
		}
	}

	dst := filepath.Join(pm.doneDir(), planName)
	if err := os.Rename(src, dst); err != nil {
		return "", fmt.Errorf("failed to move plan to done/: %v", err)
	}
	return fmt.Sprintf("Plan %q completed and moved to done/.", planName), nil
}

// ── Prompt integration ──────────────────────────────────────────────────────

// LoadPrompt returns a system-prompt injectable summary of active plans.
// Returns "" if no active plans exist.
func (pm *PlanManager) LoadPrompt() string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	entries, _ := os.ReadDir(pm.todoDir())
	if len(entries) == 0 {
		return ""
	}

	var lines []string
	lines = append(lines, "# Active Plans")
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		tasks, _ := pm.listTaskFiles(e.Name())
		completed, inProgress, blocked, pending := 0, 0, 0, 0
		var currentTask string
		for _, t := range tasks {
			switch t.Status {
			case "completed":
				completed++
			case "in_progress":
				inProgress++
				currentTask = t.Subject
			default:
				if len(t.BlockedBy) > 0 {
					blocked++
				} else {
					pending++
				}
			}
		}
		total := len(tasks)
		progress := fmt.Sprintf("[%d/%d done]", completed, total)
		line := fmt.Sprintf("- %s %s", e.Name(), progress)
		if currentTask != "" {
			line += fmt.Sprintf(" current: %q", currentTask)
		} else if pending > 0 {
			line += fmt.Sprintf(" (%d ready to start)", pending)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// NoteRound tracks plan tool usage per agent turn.
// If usedPlan is true the counter resets; otherwise it increments.
func (pm *PlanManager) NoteRound(usedPlan bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if usedPlan {
		pm.roundsSinceUpdate = 0
	} else {
		pm.roundsSinceUpdate++
	}
}

// Reminder returns a non-empty string when the plan hasn't been refreshed
// for planReminderInterval rounds and there are active tasks, otherwise "".
func (pm *PlanManager) Reminder() string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if !pm.hasActiveTasks() || pm.roundsSinceUpdate < planReminderInterval {
		return ""
	}
	return "<reminder>You have an active session plan. Check plan_task_list and update task status.</reminder>"
}

// hasActiveTasks returns true if any plan in todo/ has tasks with status in_progress.
// Must be called with at least a read lock held.
func (pm *PlanManager) hasActiveTasks() bool {
	return pm.activePlanName() != ""
}

// activePlanName returns the name of the plan with in_progress tasks, or "".
// Must be called with at least a read lock held.
func (pm *PlanManager) activePlanName() string {
	entries, err := os.ReadDir(pm.todoDir())
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		tasks, _ := pm.listTaskFiles(e.Name())
		for _, t := range tasks {
			if t.Status == "in_progress" {
				return e.Name()
			}
		}
	}
	return ""
}

// ActivePlan returns the current active plan name (has in_progress tasks).
// Thread-safe public version.
func (pm *PlanManager) ActivePlan() string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.activePlanName()
}

// Snapshot returns a copy of active plans for safe concurrent read by the TUI.
func (pm *PlanManager) Snapshot() []ui.PlanSnapshot {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	entries, err := os.ReadDir(pm.todoDir())
	if err != nil || len(entries) == 0 {
		return nil
	}

	var plans []ui.PlanSnapshot
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		tasks, _ := pm.listTaskFiles(e.Name())
		if len(tasks) == 0 {
			continue
		}
		var items []ui.PlanTaskItem
		for _, t := range tasks {
			items = append(items, ui.PlanTaskItem{
				ID:        t.ID,
				Subject:   t.Subject,
				Status:    t.Status,
				BlockedBy: t.BlockedBy,
			})
		}
		plans = append(plans, ui.PlanSnapshot{
			Name:  e.Name(),
			Tasks: items,
		})
	}
	return plans
}

// emitPlanUpdate broadcasts the current plan state to the TUI.
func (pm *PlanManager) emitPlanUpdate() {
	ui.EmitPlan(pm.Snapshot())
}

// StartupSummary returns a formatted string for printing at startup.
// Returns "" if no active plans exist.
func (pm *PlanManager) StartupSummary() string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	entries, _ := os.ReadDir(pm.todoDir())
	if len(entries) == 0 {
		return ""
	}

	var lines []string
	lines = append(lines, "\n[Session Plan] Active tasks:")
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		tasks, _ := pm.listTaskFiles(e.Name())
		if len(tasks) == 0 {
			lines = append(lines, fmt.Sprintf("  ▸ %s (no tasks yet)", e.Name()))
			continue
		}
		completed := 0
		for _, t := range tasks {
			if t.Status == "completed" {
				completed++
			}
		}
		lines = append(lines, fmt.Sprintf("  ▸ %s [%d/%d done]", e.Name(), completed, len(tasks)))
		for _, t := range tasks {
			marker := map[string]string{
				"pending":     "[ ]",
				"in_progress": "[>]",
				"completed":   "[x]",
				"deleted":     "[-]",
			}[t.Status]
			if marker == "" {
				marker = "[?]"
			}
			blocked := ""
			if len(t.BlockedBy) > 0 {
				blocked = fmt.Sprintf(" (blocked by %v)", t.BlockedBy)
			}
			lines = append(lines, fmt.Sprintf("    %s #%d: %s%s", marker, t.ID, t.Subject, blocked))
		}
	}
	return strings.Join(lines, "\n")
}

// ── Tool input schemas ──────────────────────────────────────────────────────

type planCreateInput struct {
	Name    string `json:"name"    jsonschema_description:"Plan name in format YYYY-MM-DD-description (e.g. 2026-05-28-add-auth). If empty, auto-generates from today's date."`
	Content string `json:"content" jsonschema_description:"The plan.md content: analysis, approach, and step-by-step execution plan."`
}

type planListInput struct{}

type planTaskCreateInput struct {
	Plan        string `json:"plan"                    jsonschema_description:"Plan name (directory name under .evo-agent/tasks/todo/)."`
	Subject     string `json:"subject"                 jsonschema_description:"Short task title."`
	Description string `json:"description,omitempty"   jsonschema_description:"Detailed task description."`
	BlockedBy   []int  `json:"blockedBy,omitempty"     jsonschema_description:"IDs of tasks that must complete before this one can start."`
}

type planTaskUpdateInput struct {
	Plan         string `json:"plan"                jsonschema_description:"Plan name."`
	TaskID       int    `json:"task_id"             jsonschema_description:"ID of the task to update."`
	Status       string `json:"status,omitempty"    jsonschema_description:"New status: pending, in_progress, completed, or deleted."`
	Owner        string `json:"owner,omitempty"     jsonschema_description:"Set when a subagent claims the task."`
	AddBlockedBy []int  `json:"addBlockedBy,omitempty" jsonschema_description:"Additional task IDs that block this task."`
	AddBlocks    []int  `json:"addBlocks,omitempty"    jsonschema_description:"Additional task IDs that this task blocks."`
}

type planTaskListInput struct {
	Plan string `json:"plan" jsonschema_description:"Plan name to list tasks for. If empty, lists all plans."`
}

type planTaskGetInput struct {
	Plan   string `json:"plan"    jsonschema_description:"Plan name."`
	TaskID int    `json:"task_id" jsonschema_description:"Task ID to retrieve."`
}

type planCompleteInput struct {
	Plan string `json:"plan" jsonschema_description:"Plan name to mark as complete and move to done/."`
}

// ── Tool registration ───────────────────────────────────────────────────────

func init() {
	// Collect plan tool names for precise identification via IsPlanTool()
	names := []string{"plan_create", "plan_list", "plan_task_create", "plan_task_update", "plan_task_list", "plan_task_get", "plan_complete"}
	for _, n := range names {
		planToolNames[n] = true
	}

	// plan_create: Create a new session plan
	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name: "plan_create",
			Description: anthropic.String(
				"Create a new session plan in .evo-agent/tasks/todo/<name>/. " +
					"The plan.md file should contain: requirements analysis, approach, and step-by-step tasks. " +
					"After creating the plan, use plan_task_create to add individual executable tasks."),
			InputSchema: GenerateSchema[planCreateInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in planCreateInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}
			if in.Name == "" {
				in.Name = time.Now().Format("2006-01-02") + "-plan"
			}
			result, err := GlobalPlan.Create(in.Name, in.Content)
			if err == nil {
				GlobalPlan.emitPlanUpdate()
			}
			return result, err
		},
	})

	// plan_list: List all session plans
	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name: "plan_list",
			Description: anthropic.String(
				"List all session plans (active in todo/ and completed in done/) with task progress."),
			InputSchema: GenerateSchema[planListInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			return GlobalPlan.List(), nil
		},
	})

	// plan_task_create: Create a task in a plan
	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name: "plan_task_create",
			Description: anthropic.String(
				"Create a new task in a session plan. " +
					"Set blockedBy to establish the dependency graph (task won't start until dependencies complete)."),
			InputSchema: GenerateSchema[planTaskCreateInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in planTaskCreateInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}
			if in.Plan == "" {
				in.Plan = GlobalPlan.ActivePlan()
			}
			if in.Plan == "" {
				return "", fmt.Errorf("plan name is required (no active plan found)")
			}
			if in.Subject == "" {
				return "", fmt.Errorf("subject is required")
			}
			result, err := GlobalPlan.TaskCreate(in.Plan, in.Subject, in.Description, in.BlockedBy)
			if err == nil {
				GlobalPlan.emitPlanUpdate()
			}
			return result, err
		},
	})

	// plan_task_update: Update a task's status or dependencies
	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name: "plan_task_update",
			Description: anthropic.String(
				"Update a task's status, owner, or dependencies in a session plan. " +
					"When status is set to 'completed', the task is automatically removed from other tasks' blockedBy lists."),
			InputSchema: GenerateSchema[planTaskUpdateInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in planTaskUpdateInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}
			if in.Plan == "" {
				in.Plan = GlobalPlan.ActivePlan()
			}
			if in.Plan == "" {
				return "", fmt.Errorf("plan name is required (no active plan found)")
			}
			result, err := GlobalPlan.TaskUpdate(in.Plan, in.TaskID, in.Status, in.Owner, in.AddBlockedBy, in.AddBlocks)
			if err == nil {
				GlobalPlan.emitPlanUpdate()
			}
			return result, err
		},
	})

	// plan_task_list: List tasks in a plan
	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name: "plan_task_list",
			Description: anthropic.String(
				"List all tasks in a session plan with status, dependencies, and progress."),
			InputSchema: GenerateSchema[planTaskListInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in planTaskListInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}
			if in.Plan == "" {
				in.Plan = GlobalPlan.ActivePlan()
			}
			if in.Plan == "" {
				// No active plan, list all plans overview
				return GlobalPlan.List(), nil
			}
			return GlobalPlan.TaskList(in.Plan)
		},
	})

	// plan_task_get: Get full details of a task
	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name: "plan_task_get",
			Description: anthropic.String(
				"Get full details of a specific task in a session plan, including description and dependencies."),
			InputSchema: GenerateSchema[planTaskGetInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in planTaskGetInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}
			if in.Plan == "" {
				in.Plan = GlobalPlan.ActivePlan()
			}
			if in.Plan == "" {
				return "", fmt.Errorf("plan name is required (no active plan found)")
			}
			return GlobalPlan.TaskGet(in.Plan, in.TaskID)
		},
	})

	// plan_complete: Move plan from todo/ to done/
	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name: "plan_complete",
			Description: anthropic.String(
				"Mark a plan as complete and move it from .evo-agent/tasks/todo/ to .evo-agent/tasks/done/. " +
					"All tasks must be completed or deleted before the plan can be moved."),
			InputSchema: GenerateSchema[planCompleteInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in planCompleteInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}
			if in.Plan == "" {
				in.Plan = GlobalPlan.ActivePlan()
			}
			if in.Plan == "" {
				return "", fmt.Errorf("plan name is required (no active plan found)")
			}
			result, err := GlobalPlan.Complete(in.Plan)
			if err == nil {
				GlobalPlan.emitPlanUpdate()
			}
			return result, err
		},
	})
}
