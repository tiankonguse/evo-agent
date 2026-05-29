# Quick Cheat Sheet: Renaming "session plan" and "persistent plan"

## The One-Minute Summary

**Total: 63 references across 13 files**

### Session Plan (31 refs)
- Mostly in `src/internal/tools/todo.go` (13)
- Also in `README.md` (6) and other docs

### Persistent Plan (32 refs)  
- Mostly in `src/internal/tools/plan.go` (18) ⭐ **CRITICAL**
- Also in `src/internal/prompt/builder.go` (5)

---

## The 5 Most Important Changes

```
1. plan.go:37-51     → PlanGuidance constant (system prompt)
2. render.go:148     → "▸ Session Plan" header (user sees this)
3. todo.go:272,295,308,325,348  → 5 tool descriptions
4. plan.go:679,705,718,749,777,801 → 6 tool descriptions  
5. README.md:all     → User documentation
```

---

## All Line Numbers at a Glance

### Session Plan (31 total)
```
events.go:18
terminal.go:61
todo.go: 21, 47, 53, 162, 210, 267, 272, 290, 295, 308, 325, 348
render.go: 134, 148
model.go: 344
README.md: 19, 59, 143, 168, 328, 344
CLAUDE.md: 92
PLANNING_SUMMARY.md: 16
README_TOOL_ANALYSIS.md: 54, 122, 299
TOOL_PATTERNS.md: 45, 310
```

### Persistent Plan (32 total)
```
main.go: 65, 82
terminal.go: 66
events.go: 26, 34
plan.go: 36, 37-51, 64, 71, 92, 548, 674, 679, 700, 705, 718, 749, 777, 801
render.go: 192
model.go: 320, 349
builder.go: 103, 145, 150, 180, 193
PLANNING_SUMMARY.md: 16
```

---

## Suggested Replacements

Pick ONE approach:

```
Option A: "task list" + "plan"
Option B: "task list" + "project plan"  
Option C: "working todo" + "tracked project"
```

---

## Files to Edit (Priority Order)

### Must Edit (User-facing)
- [ ] `src/internal/tools/plan.go` (18 refs, includes system prompt)
- [ ] `src/internal/tools/todo.go` (13 refs, tool descriptions)
- [ ] `README.md` (6 refs, user docs)
- [ ] `src/internal/tui/render.go` (3 refs, TUI header!)

### Should Edit (Infrastructure)
- [ ] `src/internal/prompt/builder.go` (5 refs)
- [ ] `src/internal/ui/events.go` (3 refs)
- [ ] `src/internal/tui/model.go` (3 refs)
- [ ] `src/main.go` (2 refs)
- [ ] `src/internal/ui/terminal.go` (2 refs)

### Nice to Edit (Documentation)
- [ ] `CLAUDE.md` (1 ref)
- [ ] `docs/PLANNING_SUMMARY.md` (2 refs)
- [ ] `docs/README_TOOL_ANALYSIS.md` (3 refs)
- [ ] `docs/TOOL_PATTERNS.md` (2 refs)

---

## Verification

After making changes, run:
```bash
grep -rn "session plan\|persistent plan" --include="*.go" src/ refs/ 2>/dev/null
```

Should return: **EMPTY** (no matches)

---

## Reference Documents

- **SEARCH_RESULTS.md** - Complete detailed listing
- **RENAME_QUICK_REFERENCE.md** - Full implementation guide
- **RENAME_INDEX.md** - Master index with tables

---

## Key Insights

✓ **Most critical file:** `src/internal/tools/plan.go` (contains PlanGuidance)
✓ **Most visible change:** Line 148 in `render.go` (TUI header)
✓ **User-impacting:** 11 tool descriptions across 2 files
✓ **Quick wins:** Comments are lowest risk, highest coverage
✓ **Total effort:** ~1-2 hours for thorough replacement

---

## Example Sed Commands (Use with caution!)

```bash
# Replace "session plan" with "task list" 
sed -i '' 's/session plan/task list/g' file.go

# Replace "persistent plan" with "project plan"
sed -i '' 's/persistent plan/project plan/g' file.go

# Replace capitalized versions
sed -i '' 's/Session Plan/Task List/g' file.go
sed -i '' 's/Persistent Plan/Project Plan/g' file.go

# VERIFY BEFORE COMMITTING!
grep -n "session plan\|persistent plan" file.go
```

---

## Testing After Changes

1. Build the project
2. Run with `-v` flag to see tool descriptions
3. Check TUI renders "Task List" instead of "Session Plan"
4. Verify README displays correctly
5. Run full test suite

---

## References Generated

This cheat sheet is part of a comprehensive search package:
- Generated: 2026-05-29
- Total documentation: ~750 lines
- All 63 occurrences documented with line numbers

