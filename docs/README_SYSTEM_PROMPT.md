# System Prompt Architecture - Quick Start Guide

Three documents have been created to help you understand how evo-agent builds its system prompt:

## 📚 Documents Created

1. **SYSTEM_PROMPT_ANALYSIS.md** — Comprehensive reference
   - Overview of the sequential injection pattern
   - All 6 sections of the final system prompt (with details)
   - Key observations and design patterns
   - Future extensibility points

2. **SYSTEM_PROMPT_FLOWCHART.md** — Visual diagrams
   - Initialization flow with ASCII diagrams
   - Agent loop usage pattern
   - Final system prompt structure
   - Subagent inheritance model
   - Memory lifecycle & constraints
   - Tool execution flow

3. **CODE_REFERENCE_GUIDE.md** — Implementation details
   - File map with line numbers
   - Step-by-step code walkthrough
   - Memory/skills/tool systems explained
   - Subagent prompt pattern with code
   - Constants and test references

## 🎯 Quick Answer: How is System Prompt Built?

### Sequential Injection in main.go (lines 45-86)

```
1. Base identity          → "You are a coding agent at {ProjectDir}."
2. + Agent.md             → Project guidance (optional, if file exists)
3. + Memories             → Persistent memories from .evo-agent/memory/
4. + Memory guidance      → When/when-not to save (const string)
5. + Skills catalog       → List of available skills (optional, if skills exist)
6. + Slash command intro  → Explanation of /skill-name syntax (conditional)
                  ↓
             STATIC PROMPT
                  ↓
Passed to every LLM call (same for entire session)
```

### Key Points

✓ **Static after startup** — System prompt is built once, then immutable  
✓ **Modular sections** — Each section added conditionally  
✓ **Agent.md optional** — User opts in by creating file in project root  
✓ **Memories optional** — Persist across sessions in `.evo-agent/memory/`  
✓ **Skills optional** — Load from `.evo-agent/skill/**/SKILL.md`  
✓ **Tool results ≠ system prompt** — Results added as user messages, not system  
✓ **Subagent inheritance** — Child agents get parent prompt + task-specific prompt  

## 🔍 Where to Look for Specific Things

| Question | Look Here |
|----------|-----------|
| How is system prompt assembled? | `main.go:45-86` |
| What happens in agent loop? | `agent/loop.go:88-165` |
| How are memories loaded? | `tools/memory.go:158-199` (LoadPrompt) |
| How are skills loaded? | `skills/registry.go:104-129` (Catalog) |
| How do subagents work? | `agent/subagent.go:19-83` |
| Memory file format? | `tools/memory.go:319-330` (frontmatter docs) |
| Skill file format? | `skills/registry.go:56-86` (parsing) |

## 🛠️ Common Tasks

### Add a new system prompt section
1. Add after line 69 (after memory guidance)
2. Follow pattern: `cfg.SystemMsg += "\n\n# Section Name\n\n" + content`
3. Consider making it optional with a condition

### Create a skill
1. Create `.evo-agent/skill/{skill-name}/SKILL.md`
2. Add frontmatter (name, description, etc.)
3. Skill appears in catalog at next startup

### Save a memory
1. User calls `remember` tool or `/remember` command
2. Subagent gets conversation + memory extraction prompt
3. Subagent writes `.evo-agent/memory/{name}.md`
4. Memory appears in NEXT session (not current)

### Understand tool execution
1. Model calls tool → `tools.Execute()`
2. Native tools: registry lookup → handler
3. MCP tools: prefixed `mcp__` → MCP router
4. Result appended as USER MESSAGE (not system)

## 📊 System Prompt Size Budget

```
Base identity              ~100 chars
Agent.md                   variable (user-controlled)
Memories                   variable (from .evo-agent/memory/)
Memory guidance            ~800 chars (const)
Skills catalog             variable (from .evo-agent/skill/)
Slash command intro        ~200 chars
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Total                      usually 2-10 KB
```

⚠️ Memory index (MEMORY.md) max ~200 lines, so memories truncate if too large.

## 🔄 Session Lifecycle

```
START
  ↓
config.Load()                      → base prompt
  ↓
Load Agent.md                      → + project guidance
  ↓
GlobalMemory.Init() + LoadPrompt() → + memories + guidance
  ↓
skills.Init() + Catalog()          → + skills + slash intro
  ↓
agent.New()                        → immutable system prompt set
  ↓
Agent.Loop() with static prompt    → each turn uses same system prompt
  ↓
Tool results added as user msgs    → conversation grows, prompt stays fixed
  ↓
(Optional: remember tool)          → writes to .evo-agent/memory/
  ↓
NEXT SESSION:                      → new memories appear in prompt
```

## 🎓 Key Concepts

### Immutable System Prompt
- Built once at startup
- Passed unchanged to every LLM turn
- Reduces context per-message
- Can't change guidance mid-session without restart

### Memory Extraction as Subagent
- Spawned to analyze conversation
- Writes memory files to disk
- Parent agent doesn't re-inject in current session
- New memories available in NEXT session

### Tool Results as User Messages
- Tool outputs NOT appended to system prompt
- Added to `Messages` array as user content blocks
- Keeps system prompt size fixed
- Allows unlimited tool turns without context explosion

### Skills as Declarative Catalog
- Loaded from `.evo-agent/skill/*/SKILL.md`
- Formatted into catalog for system prompt
- Agent uses `load_skill` tool to access full skill content
- Commands loaded from `.evo-agent/command/` but not in catalog

### Subagent Inheritance
- Child agents inherit: parent system prompt + task-specific prompt
- Ensures child understands project context
- Task-scoped execution with full project awareness

## 🚀 Next Steps

1. **Understand the pattern**: Read the flowchart in SYSTEM_PROMPT_FLOWCHART.md
2. **Review your project**: Check if you have Agent.md, .evo-agent/memory/, .evo-agent/skill/
3. **Trace execution**: Follow main.go → config.go → agent/loop.go
4. **Experiment**: Try adding a section or creating a skill

## 📝 Notes for Implementation

- **No dedicated "prompt builder" module** — directly in main.go with string concat
- **Order is explicit** — each `cfg.SystemMsg +=` statement defines order
- **Optional sections improve UX** — absent features don't add noise
- **Subagent pattern is powerful** — enables child agents with full project context
- **Tool results design is key** — keeps system prompt size predictable despite long interactions

