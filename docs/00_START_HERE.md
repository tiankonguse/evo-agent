# 🎯 START HERE: "session plan" / "persistent plan" Rename Search Results

**Generated:** 2026-05-29  
**Project:** evo-agent  
**Search Scope:** All `.go` files + documentation

---

## 📊 What You're Getting

A comprehensive search has been completed for all references to:
- **"session plan"** (31 occurrences)
- **"persistent plan"** (32 occurrences)

**Total: 63 references across 13 files**

You now have **5 comprehensive reference documents** (708 lines total) to guide your renaming effort.

---

## 📚 Reference Documents (Read in This Order)

### 1️⃣ **CHEAT_SHEET.md** ← START HERE (2 min read)
**Best for:** Quick reference, line numbers at a glance
- One-minute summary
- All 63 line numbers listed
- Priority files checklist
- Suggested replacements
- Verification command

### 2️⃣ **RENAME_QUICK_REFERENCE.md** (10 min read)
**Best for:** Understanding the scope and planning
- Files ranked by priority (Critical → Medium)
- Specific line numbers for each file
- Three replacement options explained
- 4-phase implementation plan
- Testing checklist

### 3️⃣ **RENAME_INDEX.md** (15 min read)
**Best for:** Complete reference while editing
- Tier 1-3 classification
- All 63 occurrences in table format
- Line-by-line breakdown
- Master file list with counts
- Statistics and roadmap

### 4️⃣ **SEARCH_RESULTS.md** (Reference)
**Best for:** Detailed analysis
- Complete categorized breakdown
- Reference type statistics
- File-by-file distribution
- Cross-reference map

### 5️⃣ **SEARCH_SUMMARY.txt** (Reference)
**Best for:** Executive overview
- Results summary
- Distribution visualization
- Key findings
- Next steps

---

## 🎯 Quick Start (5 Minutes)

1. **Open CHEAT_SHEET.md**
2. **Pick a replacement strategy:**
   - Option A: `"task list"` + `"plan"`
   - Option B: `"task list"` + `"project plan"`
   - Option C: `"working todo"` + `"tracked project"`
3. **Locate the 5 most critical files** (listed below)
4. **Make those changes first**
5. **Run the verification command** at the end

---

## 🔴 The 5 Critical Changes (Do These First!)

| Priority | File | Lines | What | Impact |
|----------|------|-------|------|--------|
| 1 | `plan.go` | 37-51 | PlanGuidance constant | System prompt |
| 2 | `render.go` | 148 | TUI header text | User sees this |
| 3 | `todo.go` | 272,295,308,325,348 | Tool descriptions (5) | Tool help text |
| 4 | `plan.go` | 679,705,718,749,777,801 | Tool descriptions (6) | Tool help text |
| 5 | `README.md` | 19,59,143,168,328,344 | Documentation (6) | User docs |

**These 5 changes cover 44 of the 63 total references.**

---

## 📋 Complete File List

**Tier 1 - CRITICAL (User-facing, must fix):**
- ✓ `src/internal/tools/todo.go` (13 refs)
- ✓ `src/internal/tools/plan.go` (18 refs) ⭐
- ✓ `README.md` (6 refs)
- ✓ `src/internal/tui/render.go` (3 refs)

**Tier 2 - HIGH (Infrastructure, should fix):**
- `src/internal/prompt/builder.go` (5 refs)
- `src/internal/ui/events.go` (3 refs)
- `src/internal/tui/model.go` (3 refs)
- `src/main.go` (2 refs)
- `src/internal/ui/terminal.go` (2 refs)

**Tier 3 - MEDIUM (Documentation, nice to fix):**
- `CLAUDE.md` (1 ref)
- `docs/PLANNING_SUMMARY.md` (2 refs)
- `docs/README_TOOL_ANALYSIS.md` (3 refs)
- `docs/TOOL_PATTERNS.md` (2 refs)

---

## 💡 Suggested Replacements

Choose ONE approach for consistency:

### Option A: Conservative (Simplest)
```
"session plan"   → "task list"
"persistent plan" → "plan"
```

### Option B: Clear Distinction (Recommended)
```
"session plan"   → "task list"
"persistent plan" → "project plan"
```

### Option C: Descriptive (Most explicit)
```
"session plan"   → "working todo"
"persistent plan" → "tracked project"
```

---

## ✅ Verification

After making your changes, run:
```bash
grep -rn "session plan\|persistent plan" --include="*.go" src/ refs/ 2>/dev/null
```

**Expected result:** No output (empty)

---

## 📈 By the Numbers

| Metric | Count |
|--------|-------|
| Total References | 63 |
| Files Affected | 13 |
| Go Source Files | 10 |
| Documentation Files | 3 |
| "session plan" refs | 31 |
| "persistent plan" refs | 32 |
| User-visible occurrences | 4 |
| Tool descriptions | 11 |
| Comments | 20+ |
| Error messages | 4 |

---

## 🚀 Implementation Path

### Phase 1: Critical (30 min)
Update the 5 most critical files listed above

### Phase 2: Infrastructure (30 min)
Update Tier 2 files (5 files)

### Phase 3: Documentation (30 min)
Update Tier 3 documentation files (4 files)

### Phase 4: Verification (15 min)
- Run grep to verify no old terms remain
- Build and test
- Check TUI display
- Verify tool descriptions

**Total time: ~2 hours for thorough replacement**

---

## 📖 How to Use These Documents

**For quick edits:**
→ Use **CHEAT_SHEET.md** (all line numbers at a glance)

**For planning:**
→ Use **RENAME_QUICK_REFERENCE.md** (priority order + phases)

**While editing:**
→ Keep **RENAME_INDEX.md** open (searchable master index)

**For details:**
→ Reference **SEARCH_RESULTS.md** or **SEARCH_SUMMARY.txt**

---

## 🎓 Key Insights

✓ **Most critical file:** `plan.go` (contains PlanGuidance system prompt)  
✓ **Most visible change:** TUI header in `render.go` line 148  
✓ **Easiest to miss:** Comments throughout codebase  
✓ **User-impacting:** Tool descriptions (11 total)  
✓ **Safe to do last:** Documentation strings

---

## ⚠️ Important Notes

- **PlanGuidance is special:** It's a multi-line constant with internal references
- **TUI header matters:** Users see "▸ Session Plan" in every session
- **Tool descriptions critical:** These appear in Claude's tool list
- **Case sensitivity:** Handle both lowercase and Title Case versions
- **Documentation:** Must stay consistent across all files

---

## 🔗 File Relationships

```
PlanGuidance (plan.go:37-51)
    ↓ references
System prompt builder (builder.go)
    ↓ feeds into
Claude's tool descriptions
    ↓ user sees
Tool help text

TUI Header (render.go:148)
    ↓ renders to
Live TUI display
    ↓ user sees
"▸ Session Plan" or new term
```

---

## 📞 Questions?

- **"Which file do I edit first?"** → Start with the 5 critical files in the table above
- **"What replacement term should I use?"** → Pick Option B if unsure
- **"How do I verify I got everything?"** → Run the grep command at the end
- **"What if I miss one?"** → The grep command will catch it

---

## 🎬 Let's Get Started

1. Open **CHEAT_SHEET.md**
2. Choose your replacement terms
3. Use **RENAME_INDEX.md** to find each line
4. Make the changes in priority order
5. Run verification at the end

Good luck! 🚀

---

**Generated:** 2026-05-29  
**Total documentation:** 708 lines across 5 files  
**All 63 references documented** with exact line numbers

