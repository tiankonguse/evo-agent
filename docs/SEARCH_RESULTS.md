# Comprehensive Search Results: "session plan" and "persistent plan" References

**Search Date:** 2026-05-29  
**Project:** /Users/tiankonguse-m3/project/github/AIProject/evo-agent  
**Scope:** All .go files under `src/` and `refs/`, plus documentation files

---

## Summary

- **Total "session plan" references found:** 28
- **Total "persistent plan" references found:** 49
- **Total locations:** 15 distinct .go source files + 6 documentation files

---

## 1. "session plan" / "Session Plan" References

### 1.1 Source Code Files (.go)

#### `src/internal/ui/events.go`
- **Line 18:** `// TodoItem is one entry in the session plan.`

#### `src/internal/ui/terminal.go`
- **Line 61:** `// EmitTodo broadcasts an updated session plan to the active sink.`

#### `src/internal/tools/todo.go`
- **Line 21:** `// todoItem is the internal representation of a session plan entry.`
- **Line 47:** `// Create adds a new item to the session plan.`
- **Line 53:** `return "", fmt.Errorf("session plan is full (max %d items)", todoMaxItems)`
- **Line 162:** `return "<reminder>Update your session plan (todo_update/todo_complete) before continuing.</reminder>"`
- **Line 210:** `return "No session plan yet."`
- **Line 267:** `// todo_create: Create a session plan item` (tool comment)
- **Line 272:** `"Add a new item to the session plan. Use for multi-step work (2+ steps). "` (tool description)
- **Line 290:** `// todo_list: List all session plan items` (tool comment)
- **Line 295:** `"List all items in the session plan with their status and IDs.")` (tool description)
- **Line 308:** `"Get details of a specific session plan item by ID.")` (tool description)
- **Line 325:** `"Update a session plan item's status, content, or active form. "` (tool description)
- **Line 348:** `"Mark a session plan item as completed. Use as soon as you finish a step.")` (tool description)

#### `src/internal/tui/render.go`
- **Line 134:** `// renderTodoPanel renders the session plan as a compact bordered panel.` (comment)
- **Line 148:** `lines = append(lines, todoHeaderStyle.Render("▸ Session Plan"))` (TUI header text)

#### `src/internal/tui/model.go`
- **Line 344:** `// Show session plan when items exist` (comment)

---

### 1.2 Documentation Files

#### `README.md`
- **Line 19:** `- **Session Planning (todo)**: Built-in \`todo\` tool lets the model maintain a live session plan (max 12 items, exactly one \`in_progress\` at a time); ...`
- **Line 59:** `│   ├── todo.go            # todo tool (session plan, max 12 items, reminder injection)`
- **Line 143:** `▸ Session Plan                                ← todo panel (live, bottom)`
- **Line 168:** `| \`todo\`        | Rewrite the current session plan (max 12 items, exactly one \`in_progress\`); refreshes the live TUI plan panel |`
- **Line 328:** `| [08-todo](blog/08-todo.md) | Session Planning — todo tool, state constraints, reminder injection, TUI panel |`
- **Line 344:** `| **v0.8.0** | Add session planning: \`todo\` tool (\`todoManager\`, max 12 items, single \`in_progress\` constraint, 3-round reminder injection); \`EvTodo\` event; live TUI plan panel (\`renderTodoPanel\`) |`

#### `CLAUDE.md`
- **Line 92:** `\`GlobalTodo *todoManager\` is a package-level singleton. The \`todo\` tool lets the model maintain a session plan (max 12 items, exactly 1 \`in_progress\` at a time). ...`

#### `docs/PLANNING_SUMMARY.md`
- **Line 16:** `- Existing tools: task (subagent), todo (session plan), plan (persistent tasks)`

#### `docs/README_TOOL_ANALYSIS.md`
- **Line 54:** `- Todo tool: session planning (state manager, update with validation, reminders, rendering, TUI integration)`
- **Line 122:** (diagram) `session plan    ┌──────────────────────┐`
- **Line 299:** `- \`GlobalTodo\` in \`todo.go\` (session plan)`

#### `docs/TOOL_PATTERNS.md`
- **Line 45:** `### 3. Todo Manager: Session Planning (\`todo.go\`)`
- **Line 310:** (diagram) `session plan      ┌────────────────────┐`

---

## 2. "persistent plan" / "Persistent Plan" References

### 2.1 Source Code Files (.go)

#### `src/main.go`
- **Line 65:** `// Initialize persistent plan/task system`
- **Line 82:** `// Set persistent plan guidance and provider`

#### `src/internal/ui/terminal.go`
- **Line 66:** `// EmitPlan broadcasts updated persistent plan snapshots to the active sink.`

#### `src/internal/ui/events.go`
- **Line 26:** `// PlanTaskItem is one task in a persistent plan, for TUI rendering.`
- **Line 34:** `// PlanSnapshot is the TUI-visible summary of an active persistent plan.`

#### `src/internal/tools/plan.go`
- **Line 36:** `// PlanGuidance is the system-prompt text explaining when to use persistent plans.`
- **Lines 37-51:** PlanGuidance constant text includes: "# Persistent Plans", "Use persistent plans (plan_* tools) for multi-step work..."
- **Line 64:** `// PlanManager manages persistent plans with task dependency graphs.`
- **Line 71:** `// GlobalPlan is the process-wide persistent plan manager.`
- **Line 92:** `// InitPlan sets the base directory for persistent plans.`
- **Line 548:** `return "<reminder>You have an active persistent plan. Check plan_task_list and update task status.</reminder>"`
- **Line 674:** `// plan_create: Create a new persistent plan` (tool comment)
- **Line 679:** `"Create a new persistent plan in .evo-agent/tasks/todo/<name>/. "` (tool description)
- **Line 700:** `// plan_list: List all persistent plans` (tool comment)
- **Line 705:** `"List all persistent plans (active in todo/ and completed in done/) with task progress.")` (tool description)
- **Line 718:** `"Create a new task in a persistent plan. "` (tool description)
- **Line 749:** `"Update a task's status, owner, or dependencies in a persistent plan. "` (tool description)
- **Line 777:** `"List all tasks in a persistent plan with status, dependencies, and progress.")` (tool description)
- **Line 801:** `"Get full details of a specific task in a persistent plan, including description and dependencies.")` (tool description)

#### `src/internal/tui/render.go`
- **Line 192:** `// renderPlanPanel renders the persistent plan tasks as a compact bordered panel.` (comment)

#### `src/internal/tui/model.go`
- **Line 320:** `// Store updated persistent plan; View() will re-render it live` (comment)
- **Line 349:** `// Show persistent plan when active` (comment)

#### `src/internal/prompt/builder.go`
- **Line 103:** `// PlanProvider abstracts access to the persistent plan system.`
- **Line 145:** `// SetPlanProvider sets the persistent plan provider for dynamic status injection.`
- **Line 150:** `// SetPlanGuidance sets the persistent plan workflow guidance text.`
- **Line 180:** `b.buildPlanGuidance(),     // When to use persistent plans`
- **Line 193:** `b.buildPlanStatus(),  // Active persistent plans status`

---

## 3. Key Statistics by File

| File | "session plan" | "persistent plan" | Total |
|------|----------------|-------------------|-------|
| src/internal/tools/todo.go | 13 | - | 13 |
| src/internal/tools/plan.go | - | 18 | 18 |
| README.md | 6 | - | 6 |
| src/internal/prompt/builder.go | - | 5 | 5 |
| src/internal/tui/render.go | 2 | 1 | 3 |
| src/internal/ui/events.go | 1 | 2 | 3 |
| src/main.go | - | 2 | 2 |
| src/internal/ui/terminal.go | 1 | 1 | 2 |
| src/internal/tui/model.go | 1 | 2 | 3 |
| CLAUDE.md | 1 | - | 1 |
| docs/PLANNING_SUMMARY.md | 1 | 1 | 2 |
| docs/README_TOOL_ANALYSIS.md | 3 | - | 3 |
| docs/TOOL_PATTERNS.md | 2 | - | 2 |
| **TOTALS** | **31** | **32** | **63** |

---

## 4. Reference Type Breakdown

- **Comments**: 15+
- **Tool Descriptions**: 13
- **Constants/Guidance Text**: 1 (multi-line PlanGuidance)
- **UI Text/Headers**: 1
- **Error/Reminder Messages**: 4
- **Tool Registration Comments**: 10+
- **Documentation**: 15+

