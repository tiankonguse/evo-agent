# Renaming Index: "session plan" → ? and "persistent plan" → ?

## Quick Summary
- **Total references: 63** across 13 files
- **Session plan references: 31**
- **Persistent plan references: 32**
- **User-visible occurrences: 4** (TUI header, descriptions, messages)

---

## 📋 Master File List with Line Numbers

### Tier 1: Critical User-Facing (Must Fix)
```
1. src/internal/tools/todo.go
   Lines: 21, 47, 53, 162, 210, 267, 272, 290, 295, 308, 325, 348 (13 total)
   Type: Comments, tool descriptions, error messages
   Impact: Users see these in tool help text

2. src/internal/tools/plan.go  
   Lines: 36, 37-51, 64, 71, 92, 548, 674, 679, 700, 705, 718, 749, 777, 801 (18 total)
   Type: PlanGuidance constant, tool descriptions, comments, reminders
   Impact: Users see in tool descriptions and system prompts
   ⭐ CRITICAL: Line 37-51 (PlanGuidance multi-line constant)

3. src/internal/tui/render.go
   Lines: 134, 148 (2 total) + Line 192 (1 persistent)
   Type: TUI header text (user-visible), comments
   Impact: Users see "▸ Session Plan" in live TUI display

4. README.md
   Lines: 19, 59, 143, 168, 328, 344 (6 total)
   Type: Feature descriptions, documentation
   Impact: First place users learn about features
```

### Tier 2: Important Infrastructure (Should Fix)
```
5. src/internal/prompt/builder.go
   Lines: 103, 145, 150, 180, 193 (5 total, persistent plan only)
   Type: Comments, method documentation
   Impact: Affects system prompt generation

6. src/internal/ui/events.go
   Lines: 18, 26, 34 (3 total)
   Type: Struct comments
   Impact: Developer documentation

7. src/internal/tui/model.go
   Lines: 320, 344, 349 (3 total)
   Type: Comments
   Impact: TUI logic documentation

8. src/main.go
   Lines: 65, 82 (2 total)
   Type: Comments
   Impact: Initialization documentation

9. src/internal/ui/terminal.go
   Lines: 61, 66 (2 total)
   Type: Comments
   Impact: Event system documentation
```

### Tier 3: Documentation (Nice to Have)
```
10. CLAUDE.md
    Line: 92 (1 total)
    Type: Developer guide
    
11. docs/PLANNING_SUMMARY.md
    Line: 16 (1 session + 1 persistent = 2 total)
    Type: Tool overview
    
12. docs/README_TOOL_ANALYSIS.md
    Lines: 54, 122, 299 (session plan) (3 total)
    Type: Analysis and diagrams
    
13. docs/TOOL_PATTERNS.md
    Lines: 45, 310 (session plan) + implicit persistent (2 total)
    Type: Architecture patterns
```

---

## 🔍 Line-by-Line Reference

### "session plan" Occurrences (31 total)

| File | Line | Type | Text |
|------|------|------|------|
| events.go | 18 | Comment | `// TodoItem is one entry in the session plan.` |
| terminal.go | 61 | Comment | `// EmitTodo broadcasts an updated session plan to the active sink.` |
| todo.go | 21 | Comment | `// todoItem is the internal representation of a session plan entry.` |
| todo.go | 47 | Comment | `// Create adds a new item to the session plan.` |
| todo.go | 53 | Error | `"session plan is full (max %d items)"` |
| todo.go | 162 | Message | `"Update your session plan (todo_update/todo_complete)"` |
| todo.go | 210 | Message | `"No session plan yet."` |
| todo.go | 267 | Tool Comment | `// todo_create: Create a session plan item` |
| todo.go | 272 | Tool Description | `"Add a new item to the session plan. Use for multi-step work (2+ steps)."` |
| todo.go | 290 | Tool Comment | `// todo_list: List all session plan items` |
| todo.go | 295 | Tool Description | `"List all items in the session plan with their status and IDs."` |
| todo.go | 308 | Tool Description | `"Get details of a specific session plan item by ID."` |
| todo.go | 325 | Tool Description | `"Update a session plan item's status, content, or active form."` |
| todo.go | 348 | Tool Description | `"Mark a session plan item as completed. Use as soon as you finish a step."` |
| render.go | 134 | Comment | `// renderTodoPanel renders the session plan as a compact bordered panel.` |
| render.go | 148 | UI Text | `todoHeaderStyle.Render("▸ Session Plan")` ⭐ **USER-VISIBLE** |
| model.go | 344 | Comment | `// Show session plan when items exist` |
| README.md | 19 | Docs | `maintain a live session plan` |
| README.md | 59 | Docs | `session plan, max 12 items` |
| README.md | 143 | Docs | `▸ Session Plan` ⭐ **IN EXAMPLE** |
| README.md | 168 | Docs | `Rewrite the current session plan` |
| README.md | 328 | Docs | `Session Planning — todo tool` |
| README.md | 344 | Docs | `Add session planning: todo tool` |
| CLAUDE.md | 92 | Docs | `session plan (max 12 items` |
| PLANNING_SUMMARY.md | 16 | Docs | `todo (session plan)` |
| README_TOOL_ANALYSIS.md | 54 | Docs | `session planning` |
| README_TOOL_ANALYSIS.md | 122 | Docs | `session plan` (in diagram) |
| README_TOOL_ANALYSIS.md | 299 | Docs | `GlobalTodo in todo.go (session plan)` |
| TOOL_PATTERNS.md | 45 | Docs | `### 3. Todo Manager: Session Planning` |
| TOOL_PATTERNS.md | 310 | Docs | `session plan` (in diagram) |

### "persistent plan" Occurrences (32 total)

| File | Line | Type | Text |
|------|------|------|------|
| main.go | 65 | Comment | `// Initialize persistent plan/task system` |
| main.go | 82 | Comment | `// Set persistent plan guidance and provider` |
| terminal.go | 66 | Comment | `// EmitPlan broadcasts updated persistent plan snapshots` |
| events.go | 26 | Comment | `// PlanTaskItem is one task in a persistent plan` |
| events.go | 34 | Comment | `// PlanSnapshot is the TUI-visible summary of an active persistent plan` |
| plan.go | 36 | Comment | `// PlanGuidance is the system-prompt text explaining when to use persistent plans` |
| plan.go | 37-51 | Constant | `const PlanGuidance = # Persistent Plans\nUse persistent plans...` ⭐ **CRITICAL** |
| plan.go | 64 | Comment | `// PlanManager manages persistent plans with task dependency graphs` |
| plan.go | 71 | Comment | `// GlobalPlan is the process-wide persistent plan manager` |
| plan.go | 92 | Comment | `// InitPlan sets the base directory for persistent plans` |
| plan.go | 548 | Message | `"You have an active persistent plan. Check plan_task_list"` |
| plan.go | 674 | Tool Comment | `// plan_create: Create a new persistent plan` |
| plan.go | 679 | Tool Description | `"Create a new persistent plan in .evo-agent/tasks/todo/<name>/"` |
| plan.go | 700 | Tool Comment | `// plan_list: List all persistent plans` |
| plan.go | 705 | Tool Description | `"List all persistent plans (active in todo/ and completed in done/)"` |
| plan.go | 718 | Tool Description | `"Create a new task in a persistent plan."` |
| plan.go | 749 | Tool Description | `"Update a task's status, owner, or dependencies in a persistent plan."` |
| plan.go | 777 | Tool Description | `"List all tasks in a persistent plan with status, dependencies"` |
| plan.go | 801 | Tool Description | `"Get full details of a specific task in a persistent plan"` |
| render.go | 192 | Comment | `// renderPlanPanel renders the persistent plan tasks` |
| model.go | 320 | Comment | `// Store updated persistent plan; View() will re-render it live` |
| model.go | 349 | Comment | `// Show persistent plan when active` |
| builder.go | 103 | Comment | `// PlanProvider abstracts access to the persistent plan system` |
| builder.go | 145 | Comment | `// SetPlanProvider sets the persistent plan provider` |
| builder.go | 150 | Comment | `// SetPlanGuidance sets the persistent plan workflow guidance text` |
| builder.go | 180 | Comment | `// When to use persistent plans` |
| builder.go | 193 | Comment | `// Active persistent plans status` |
| PLANNING_SUMMARY.md | 16 | Docs | `plan (persistent tasks)` |

---

## 🎯 Top Priority Changes

**Must update first (user-visible, highest impact):**

1. ✅ `plan.go:37-51` - PlanGuidance constant
2. ✅ `render.go:148` - TUI header "▸ Session Plan"
3. ✅ `todo.go:272, 295, 308, 325, 348` - Tool descriptions (5 tools)
4. ✅ `plan.go:679, 705, 718, 749, 777, 801` - Tool descriptions (6 tools)
5. ✅ `README.md` - All 6 occurrences

---

## ✏️ Suggested Replacement Strategy

### Conservative (Minimal Changes)
- "session plan" → **"task list"**
- "persistent plan" → **"plan"** (just remove "persistent")

### Balanced (Clear Distinction)
- "session plan" → **"task list"**
- "persistent plan" → **"project plan"**

### Progressive (More Descriptive)
- "session plan" → **"working todo"**
- "persistent plan" → **"tracked project"**

---

## 🚀 Implementation Phases

### Phase 1: Tool Definitions (Critical)
- [ ] Update PlanGuidance constant (plan.go:37-51)
- [ ] Update tool descriptions in todo.go
- [ ] Update tool descriptions in plan.go
- [ ] Update error/reminder messages

### Phase 2: UI/UX Layer
- [ ] Update TUI header text (render.go:148)
- [ ] Update remaining comments in render.go, model.go, events.go

### Phase 3: Documentation
- [ ] Update README.md (all 6 occurrences)
- [ ] Update CLAUDE.md
- [ ] Update docs/ files

### Phase 4: Cleanup & Testing
- [ ] Update remaining comments
- [ ] Search for any missed occurrences
- [ ] Run full test suite

---

## 📊 Statistics by Category

| Category | Count | Priority | Files |
|----------|-------|----------|-------|
| Tool Descriptions | 11 | 🔴 CRITICAL | 2 |
| PlanGuidance Constant | 1 | 🔴 CRITICAL | 1 |
| Comments | 20+ | 🟡 HIGH | 8 |
| Error/Reminder Messages | 4 | 🔴 CRITICAL | 2 |
| UI Header Text | 1 | 🔴 CRITICAL | 1 |
| Documentation | 15+ | 🟠 MEDIUM | 6 |
| **TOTAL** | **63** | - | **13** |

