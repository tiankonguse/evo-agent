# evo-agent System Prompt Documentation Index

## 📋 Overview

This index maps all documentation related to evo-agent's system prompt architecture, helping you find exactly what you need.

## 📁 Documentation Files

### 1. **README_SYSTEM_PROMPT.md** ⭐ START HERE
**Best for**: Quick overview and navigation

Contains:
- Quick answer to "how is system prompt built?"
- Key points summary
- Where to look for specific things
- Common tasks checklists
- System prompt size budget
- Session lifecycle diagram

**Read this first** to get oriented.

---

### 2. **SYSTEM_PROMPT_ANALYSIS.md** 📖 COMPREHENSIVE REFERENCE
**Best for**: In-depth understanding of each section

Contains:
- Complete overview of sequential injection pattern
- Detailed breakdown of all 6 system prompt sections
- Key files & components table
- Subagent system prompt pattern
- Memory system details (types, frontmatter, index constraints)
- Static vs. dynamic prompting comparison
- Tool registration pattern
- Key observations and design patterns
- Future extensibility points

**Read this** when you need to understand how each piece works.

---

### 3. **SYSTEM_PROMPT_FLOWCHART.md** 📊 VISUAL DIAGRAMS
**Best for**: Understanding flow and relationships

Contains:
- ASCII flowchart of initialization sequence
- Agent loop diagram with LLM parameters
- Final system prompt structure visualization
- Subagent prompt inheritance model
- Tool execution flow diagram
- Memory management lifecycle
- Memory index constraints
- Key design patterns explained
- Implications for prompt engineering

**Read this** when you prefer diagrams over prose.

---

### 4. **CODE_REFERENCE_GUIDE.md** 💻 IMPLEMENTATION DETAILS
**Best for**: Code walkthroughs and debugging

Contains:
- File map with exact line numbers
- Step-by-step code walkthrough for each section
- Memory system implementation details
- Skills & commands system explanation
- Tool registration pattern with code
- Tool execution flow with code snippets
- Subagent system prompt pattern with code
- Context compaction system
- Constants reference
- Test references

**Read this** when you need to trace through actual code.

---

## 🎯 Quick Navigation by Task

### "I want to understand the architecture"
1. Start: **README_SYSTEM_PROMPT.md** (5 min)
2. Then: **SYSTEM_PROMPT_FLOWCHART.md** (diagrams, 10 min)
3. Deep: **SYSTEM_PROMPT_ANALYSIS.md** (details, 20 min)

### "I want to find where X happens"
Use **CODE_REFERENCE_GUIDE.md** → File Map section

### "I want to add a new section"
1. **CODE_REFERENCE_GUIDE.md** → Section 2 (Initial Config)
2. **CODE_REFERENCE_GUIDE.md** → Common pattern reference
3. **SYSTEM_PROMPT_ANALYSIS.md** → Future Extensibility Points

### "I need to fix something in memory loading"
1. **CODE_REFERENCE_GUIDE.md** → Section 3 (Load Persistent Memories)
2. **CODE_REFERENCE_GUIDE.md** → Memory System Details
3. **SYSTEM_PROMPT_ANALYSIS.md** → Memory System Details

### "I want to understand subagents"
1. **SYSTEM_PROMPT_FLOWCHART.md** → Subagent Prompt Inheritance diagram
2. **CODE_REFERENCE_GUIDE.md** → Subagent System Prompt Pattern
3. **SYSTEM_PROMPT_ANALYSIS.md** → Subagent System Prompt Pattern

### "I need to debug tool execution"
1. **SYSTEM_PROMPT_FLOWCHART.md** → Tool Execution Flow diagram
2. **CODE_REFERENCE_GUIDE.md** → Tool System section
3. **CODE_REFERENCE_GUIDE.md** → Tool Execution Flow with code

### "I want to create a skill"
1. **README_SYSTEM_PROMPT.md** → Common Tasks (Create a skill)
2. **CODE_REFERENCE_GUIDE.md** → Skills & Commands System
3. **CODE_REFERENCE_GUIDE.md** → Skill Frontmatter format

---

## 📚 Document Relationships

```
README_SYSTEM_PROMPT.md (index + quick start)
        │
        ├─→ SYSTEM_PROMPT_ANALYSIS.md (comprehensive details)
        ├─→ SYSTEM_PROMPT_FLOWCHART.md (visual diagrams)
        └─→ CODE_REFERENCE_GUIDE.md (implementation + code)
```

Each document is **self-contained** but cross-references the others.

---

## 🔑 Key Concepts Across All Documents

### Pattern: Sequential Injection
- **Where**: README (overview) → FLOWCHART (visual) → CODE (implementation)
- **Key insight**: System prompt built once at startup, then immutable

### Pattern: Optional Sections
- **Where**: README → ANALYSIS (design patterns) → CODE (conditionals)
- **Key insight**: Absent features don't add prompt noise

### Pattern: Subagent Inheritance
- **Where**: FLOWCHART (diagram) → ANALYSIS (pattern) → CODE (implementation)
- **Key insight**: Child agents get parent prompt + specialized task prompt

### Pattern: Memory Extraction
- **Where**: FLOWCHART (lifecycle) → ANALYSIS (memory types) → CODE (buildExtractionPrompt)
- **Key insight**: Memories written mid-session appear in NEXT session

### Pattern: Tool Results as User Messages
- **Where**: README → FLOWCHART (diagram) → ANALYSIS (observation)
- **Key insight**: Keeps system prompt size fixed despite long interactions

---

## 📍 File Locations Referenced

| Location | Purpose | Docs |
|----------|---------|------|
| `main.go:45-86` | System prompt assembly | All 4 |
| `config.go:41` | Base identity | CODE, ANALYSIS |
| `agent/loop.go:98-106` | Prompt usage in loop | All 4 |
| `agent/subagent.go:24` | Subagent prompt building | All 4 |
| `tools/memory.go:158-199` | LoadPrompt() | CODE, ANALYSIS |
| `skills/registry.go:104-129` | Catalog() | CODE, ANALYSIS |
| `.evo-agent/memory/` | Memory storage | All 4 |
| `.evo-agent/skill/` | Skills storage | ANALYSIS, CODE |
| `Agent.md` | Project guidance | README, ANALYSIS |

---

## 🧭 Reading Paths by Audience

### For Product Managers / Decision Makers
1. README_SYSTEM_PROMPT.md (Key Points section)
2. SYSTEM_PROMPT_FLOWCHART.md (Implications for Prompt Engineering)
3. Done — understand the trade-offs

### For Backend/Go Developers
1. CODE_REFERENCE_GUIDE.md (File Map)
2. CODE_REFERENCE_GUIDE.md (Step-by-Step Code Walkthrough)
3. SYSTEM_PROMPT_ANALYSIS.md (Key Observations)
4. Ready to modify code

### For Prompt Engineers
1. README_SYSTEM_PROMPT.md (all sections)
2. SYSTEM_PROMPT_FLOWCHART.md (Final System Prompt Structure)
3. SYSTEM_PROMPT_ANALYSIS.md (Memory System Details, Subagent Pattern)
4. Ready to tune prompts

### For QA / Testing
1. README_SYSTEM_PROMPT.md (Session Lifecycle)
2. CODE_REFERENCE_GUIDE.md (Test References)
3. SYSTEM_PROMPT_FLOWCHART.md (Memory Lifecycle)
4. Ready to write tests

### For New Team Members
1. README_SYSTEM_PROMPT.md (all sections)
2. SYSTEM_PROMPT_FLOWCHART.md (Initialization Flow + Agent Loop)
3. CODE_REFERENCE_GUIDE.md (scan File Map + Step-by-Step)
4. Understand the architecture

---

## ✅ Document Checklist

Each document should answer these questions:

- [x] What is the system prompt architecture?
- [x] How are sections ordered?
- [x] What is optional vs. required?
- [x] How do subagents work?
- [x] How are memories managed?
- [x] How are skills loaded?
- [x] Where is the code?
- [x] What are the design patterns?
- [x] How do tool results flow?
- [x] What are the constraints?

---

## 🎓 Learning Outcomes

After reading these documents, you should be able to:

**Basic** (README only)
- [ ] Describe the 6 sections of the system prompt
- [ ] Explain why system prompt is static
- [ ] Know where Agent.md goes
- [ ] Understand tool results aren't in system prompt

**Intermediate** (README + FLOWCHART)
- [ ] Draw the initialization sequence
- [ ] Explain subagent inheritance
- [ ] Describe memory lifecycle
- [ ] Understand memory index constraints
- [ ] Draw tool execution flow

**Advanced** (All documents)
- [ ] Read and modify main.go system prompt assembly
- [ ] Create a new system prompt section
- [ ] Debug memory loading issues
- [ ] Create a skill and add to catalog
- [ ] Understand subagent prompt building
- [ ] Trace tool execution from model call to result

---

## 📞 Questions & Answers

**Q: Where do I start?**  
A: Read **README_SYSTEM_PROMPT.md** first (5 min). Then dive deeper based on your needs.

**Q: Which document has code?**  
A: **CODE_REFERENCE_GUIDE.md** has the most code. **SYSTEM_PROMPT_FLOWCHART.md** has diagrams.

**Q: What if I only have 10 minutes?**  
A: Read **README_SYSTEM_PROMPT.md** entirely.

**Q: What if I have 1 hour?**  
A: Read README → FLOWCHART → CODE_REFERENCE (sections relevant to your task).

**Q: Can I read just one document?**  
A: Yes. Each is self-contained. But they reference each other, so reading multiple provides better context.

**Q: Are there other docs about evo-agent?**  
A: Yes — see **README.md** and **CLAUDE.md** in the project root (general project info, not system prompt specific).

---

## 📝 Version History

- **Created**: May 27, 2026
- **Documents**: 4 (README_SYSTEM_PROMPT, SYSTEM_PROMPT_ANALYSIS, SYSTEM_PROMPT_FLOWCHART, CODE_REFERENCE_GUIDE)
- **Total pages**: ~50 pages
- **Code references**: 50+ line-number citations
- **Diagrams**: 8 ASCII flowcharts

---

## 🔗 Cross-References

| Doc | References | Referenced By |
|-----|-----------|--------------|
| README_SYSTEM_PROMPT | All 3 others | Start here |
| SYSTEM_PROMPT_ANALYSIS | FLOWCHART, CODE | All paths |
| SYSTEM_PROMPT_FLOWCHART | ANALYSIS | Visual learners |
| CODE_REFERENCE_GUIDE | ANALYSIS, FLOWCHART | Developers |

---

## 🎯 Recommended Reading Order by Goal

```
Goal: "Quick overview"
  → README_SYSTEM_PROMPT (10 min)

Goal: "Understand architecture"
  → README_SYSTEM_PROMPT (10 min)
  → SYSTEM_PROMPT_FLOWCHART (15 min)
  → SYSTEM_PROMPT_ANALYSIS (20 min)

Goal: "Modify code"
  → CODE_REFERENCE_GUIDE (30 min)
  → SYSTEM_PROMPT_ANALYSIS (for design context, 20 min)

Goal: "Debug issue"
  → README_SYSTEM_PROMPT (locate problem area, 5 min)
  → CODE_REFERENCE_GUIDE (find code, 10 min)
  → SYSTEM_PROMPT_ANALYSIS (understand design, 15 min)

Goal: "Tune prompts"
  → README_SYSTEM_PROMPT (understand sections, 10 min)
  → SYSTEM_PROMPT_ANALYSIS (memory/skills/guidance, 15 min)
  → SYSTEM_PROMPT_FLOWCHART (structure diagram, 5 min)
```

---

## 💡 Tips for Using These Docs

1. **Use Ctrl+F** to search for specific file names or concepts
2. **Skim table of contents** before reading deeply
3. **Follow cross-references** to understand relationships
4. **Code snippets in CODE_REFERENCE_GUIDE** are actual code from the project
5. **Diagrams in FLOWCHART** use ASCII art — they're accurate, not stylized
6. **Examples are real** — drawn from actual codebase

---

## 🚀 Next Steps After Reading

1. **Run the agent** with debug logging to see system prompt assembly
2. **Create an Agent.md** in a test project
3. **Create a skill** in `.evo-agent/skill/`
4. **Trigger the remember tool** to see memory extraction
5. **Inspect .evo-agent/memory/` to see memory file format

---

**Last updated**: May 27, 2026  
**Total documentation**: 4 files, ~50 KB  
**Readability**: Designed for quick lookup and deep dives
