# Quick Reference: Rename "session plan" and "persistent plan"

## Overview
- **Total occurrences to update: 63**
- **Files to modify: 13**
- **Type of changes: Comments, strings, tool descriptions, documentation**

## Files Ranked by Priority (# of occurrences)

### 🔴 CRITICAL (High-impact, user-facing)
1. **`src/internal/tools/todo.go`** - 13 occurrences
   - Tool descriptions (5 tools)
   - Tool comments
   - Error messages
   - Status text
   
2. **`src/internal/tools/plan.go`** - 18 occurrences
   - PlanGuidance constant (multi-line, critical)
   - Tool descriptions (6 tools)
   - Tool comments
   - Reminder messages
   - Comments

3. **`README.md`** - 6 occurrences
   - Feature descriptions
   - Tool documentation
   - Version history

### 🟡 HIGH (Important, affecting core logic)
4. **`src/internal/prompt/builder.go`** - 5 occurrences (persistent plan only)
   - Comments and guidance text
   
5. **`src/internal/tui/render.go`** - 3 occurrences
   - **Line 148: UI Header text** ← User-visible!
   - Comments

6. **`src/internal/ui/events.go`** - 3 occurrences
   - Struct definition comments

7. **`src/internal/tui/model.go`** - 3 occurrences
   - Comments

### 🟠 MEDIUM (Supporting documentation/comments)
8. **`src/main.go`** - 2 occurrences
   - Comments

9. **`src/internal/ui/terminal.go`** - 2 occurrences
   - Function comments

10. **`CLAUDE.md`** - 1 occurrence
    - Project documentation

11. **`docs/PLANNING_SUMMARY.md`** - 2 occurrences
    - Tool comparison

12. **`docs/README_TOOL_ANALYSIS.md`** - 3 occurrences
    - Diagrams and descriptions

13. **`docs/TOOL_PATTERNS.md`** - 2 occurrences
    - Section headers and diagrams

---

## Specific Line Numbers for Targeted Editing

### Session Plan References (31 total)
```
src/internal/ui/events.go:18
src/internal/ui/terminal.go:61
src/internal/tools/todo.go:21, 47, 53, 162, 210, 267, 272, 290, 295, 308, 325, 348
src/internal/tui/render.go:134, 148 ⭐ (UI text)
src/internal/tui/model.go:344
README.md:19, 59, 143, 168, 328, 344
CLAUDE.md:92
docs/PLANNING_SUMMARY.md:16
docs/README_TOOL_ANALYSIS.md:54, 122, 299
docs/TOOL_PATTERNS.md:45, 310
```

### Persistent Plan References (32 total)
```
src/main.go:65, 82
src/internal/ui/terminal.go:66
src/internal/ui/events.go:26, 34
src/internal/tools/plan.go:36, 37-51 ⭐ (PlanGuidance), 64, 71, 92, 548, 674, 679, 700, 705, 718, 749, 777, 801
src/internal/tui/render.go:192
src/internal/tui/model.go:320, 349
src/internal/prompt/builder.go:103, 145, 150, 180, 193
docs/PLANNING_SUMMARY.md:16
```

---

## Suggested Replacement Terms

### Option 1: More Descriptive
- "session plan" → **"task list"** (emphasizes transient, in-session nature)
- "persistent plan" → **"project plan"** (emphasizes durability across sessions)

### Option 2: Consistency
- "session plan" → **"working list"** (lighter, session-focused)
- "persistent plan" → **"tracked plan"** (emphasizes persistence)

### Option 3: Unify Terminology
- "session plan" → **"quick todo"** (differentiates from persistent)
- "persistent plan" → **"plan"** (simplify since only one type remains)

---

## Key Considerations for Renaming

### Strings to Replace (Highest Priority)
1. **PlanGuidance constant** (plan.go:37-51)
   - Contains "# Persistent Plans" heading
   - Contains "Use persistent plans (plan_* tools)"
   - Must maintain parallel structure with "session" vs "persistent" distinction

2. **Tool Descriptions** (All tool registration sites)
   - `todo_create`, `todo_list`, `todo_get`, `todo_update`, `todo_complete`
   - `plan_create`, `plan_list`, `plan_task_create`, `plan_task_update`, `plan_task_list`, `plan_task_get`

3. **UI Header Text** (render.go:148)
   - `"▸ Session Plan"` is displayed in live TUI
   - Users see this in every session

4. **Error Messages** (User-visible)
   - "session plan is full (max %d items)"
   - "You have an active persistent plan"

### Comments (Lower Priority but Important)
- All `//` comments explaining the distinction
- Struct definition comments
- Function documentation

### Documentation (Must Update)
- README.md - user-facing reference
- CLAUDE.md - developer guide
- All docs/ files for consistency

---

## Implementation Strategy

### Phase 1: Core Functionality
1. Update `src/internal/tools/plan.go` (PlanGuidance, tool descriptions)
2. Update `src/internal/tools/todo.go` (tool descriptions, error messages)
3. Test tool descriptions are correct in Claude's tool list

### Phase 2: UI/UX
4. Update `src/internal/tui/render.go` (header text)
5. Update event/model comments
6. Test TUI rendering shows correct text

### Phase 3: Documentation
7. Update README.md (user-facing)
8. Update CLAUDE.md (developer guide)
9. Update all docs/ files for consistency

### Phase 4: Comments & Clean-up
10. Update remaining comments throughout codebase
11. Search for any missed occurrences
12. Run tests to verify nothing broke

---

## Testing Checklist

- [ ] Claude receives updated tool descriptions
- [ ] Tool help text displays correctly
- [ ] TUI header renders with new terminology
- [ ] README displays correctly
- [ ] No broken documentation links
- [ ] All comments updated consistently
- [ ] Search for old terms returns no results

