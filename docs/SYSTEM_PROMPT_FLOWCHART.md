# evo-agent System Prompt Architecture - Visual Flow

## Initialization Flow (main.go)

```
┌─────────────────────────────────────────────────────────────────┐
│                     STARTUP SEQUENCE                             │
└─────────────────────────────────────────────────────────────────┘

1. config.Load()
   └─> cfg.SystemMsg = "You are a coding agent at {ProjectDir}."
       
2. Load Agent.md (optional)
   └─> if exists: cfg.SystemMsg += "# Project Guidance (Agent.md)\n" + content
   
3. tools.GlobalMemory.Init() + LoadPrompt()
   └─> reads .evo-agent/memory/*.md files
   └─> cfg.SystemMsg += "\n\n# Memories (persistent across sessions)\n" + formatted
   
4. Append MemoryGuidance constant
   └─> cfg.SystemMsg += tools.MemoryGuidance
   └─> (When/when-not to save memories)
   
5. skills.Init()
   └─> reads .evo-agent/skill/**/SKILL.md files
   └─> cfg.SystemMsg += "\nSkills available:\n" + catalog
   
6. Append slash command intro (conditional)
   └─> if len(slashNames) > 0:
   └─> cfg.SystemMsg += "\n\nSlash commands: /<skill-name>..."

7. Create Agent with built system prompt
   └─> a := agent.New(&client, cfg)
   └─> cfg.SystemMsg is now IMMUTABLE for this session
```

## Agent Loop (agent/loop.go)

```
┌──────────────────────────────────────────────────────────┐
│  For each turn:                                          │
└──────────────────────────────────────────────────────────┘

1. Apply micro-compaction (if needed)
   └─> state.Messages = MicroCompact(state.Messages, ...)

2. Call LLM
   ┌──────────────────────────────────────────────────────┐
   │ a.client.Messages.New(                               │
   │   Model: cfg.ModelID,                                │
   │   System: [{Text: a.cfg.SystemMsg}] ← STATIC PROMPT │
   │   Messages: state.Messages ← CONVERSATION HISTORY   │
   │   Tools: tools.Tools(),                              │
   │   MaxTokens: 8000,                                   │
   │ )                                                    │
   └──────────────────────────────────────────────────────┘

3. Process response
   ├─> Extract tool calls
   ├─> Execute tools
   ├─> Track file reads (for compaction)
   ├─> Inject todo reminders (if due)
   └─> Append tool results as USER MESSAGE (not system)

4. Continue if tool calls exist, else stop
```

## System Prompt Structure (Final State)

```
┌─────────────────────────────────────────────────────────────────┐
│                    FINAL SYSTEM PROMPT                          │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│ [1] BASE IDENTITY                                              │
│     └─ "You are a coding agent at /path/to/project"            │
│                                                                 │
│ [2] PROJECT GUIDANCE (Agent.md)     [OPTIONAL]                │
│     └─ # Project Guidance (Agent.md                            │
│        └─ {user-written project-specific instructions}         │
│                                                                 │
│ [3] PERSISTENT MEMORIES             [OPTIONAL]                │
│     └─ # Memories (persistent across sessions)                 │
│        ├─ ## [user]                                            │
│        │  └─ ### Memory Name: Description                      │
│        │     └─ {memory content}                               │
│        ├─ ## [feedback]                                        │
│        │  └─ ### Memory Name: Description                      │
│        │     └─ {memory content}                               │
│        ├─ ## [project]                                         │
│        │  └─ ### Memory Name: Description                      │
│        │     └─ {memory content}                               │
│        └─ ## [reference]                                       │
│           └─ ### Memory Name: Description                      │
│              └─ {memory content}                               │
│                                                                 │
│ [4] MEMORY GUIDANCE                 [CONSTANT]                 │
│     └─ ## Memory guidance                                      │
│        ├─ When to save memories (user, feedback, ...)          │
│        └─ When NOT to save (derivable code, secrets, ...)      │
│                                                                 │
│ [5] SKILLS CATALOG                  [OPTIONAL]                │
│     └─ Skills available:                                       │
│        ├─ - skill-1 [arg]: Description                         │
│        ├─ - skill-2: Description                               │
│        └─ Use load_skill when a task needs...                  │
│                                                                 │
│ [6] SLASH COMMANDS INTRO            [CONDITIONAL]              │
│     └─ Slash commands: /<skill-name> (e.g., /git-commit)       │
│        └─ ... is shorthand for users to invoke a skill...      │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘

Note: This prompt is IMMUTABLE during the session.
      Tool results and reminders are injected as USER MESSAGES, not system prompt.
```

## Subagent Prompt Inheritance

```
┌─────────────────────────────────────┐
│   Parent Agent System Prompt         │
│   (Base + Agent.md + Memories +     │
│    Guidance + Skills + Slash Cmds)  │
└────────────────┬────────────────────┘
                 │
                 │ Subagent spawned via remember/consolidate/etc.
                 ├─> subSystem = a.cfg.SystemMsg + "\n" + task_prompt
                 │
                 ▼
┌─────────────────────────────────────┐
│   Subagent System Prompt            │
│   (Full parent prompt + task)       │
│   Example task: memory extraction   │
└─────────────────────────────────────┘

Pattern:
- Subagents inherit FULL parent context
- Append specialized task instructions
- Allows child agents to understand project while being task-focused
```

## Tool Execution Flow (Not System Prompt)

```
Model generates tool call
        ↓
tools.Execute(toolCall)
        ↓
    ┌───┴────────────────────────────────────┐
    │                                        │
    ▼                                        ▼
[NATIVE TOOL]                        [MCP TOOL (mcp__ prefix)]
Registry lookup                      MCP router
    ↓                                    ↓
Handler executes                    MCP server call
    ↓                                    ↓
Output → Tool result block          Output → Tool result block
    ↓                                    ↓
    └────────────────────┬───────────────┘
                         │
                         ▼
           Append as USER MESSAGE (not system)
                         ↓
           Next turn includes in Messages array
                    (not in System)
```

## Memory Management Cycle

```
┌────────────────────────────────────────────────────────────┐
│                    MEMORY LIFECYCLE                        │
└────────────────────────────────────────────────────────────┘

STARTUP:
  1. GlobalMemory.Init(projectDir)
     └─> scan .evo-agent/memory/*.md
  
  2. LoadAll()
     └─> parse frontmatter for each memory file
     └─> store in-memory: map[name]memoryEntry
  
  3. LoadPrompt()
     └─> format memories for system prompt injection
     └─> group by type (user, feedback, project, reference)

MID-SESSION:
  1. User calls /remember or agent detects valuable info
     └─> remember tool spawns memory extraction subagent
  
  2. Subagent receives:
     └─> Full conversation history
     └─> Memory directory path
     └─> List of existing memories
  
  3. Subagent uses write_file to create/update .md files
     └─> Creates: .evo-agent/memory/{name}.md with frontmatter
     └─> Updates: .evo-agent/memory/MEMORY.md index
  
  4. Parent agent calls GlobalMemory.LoadAll()
     └─> Reloads all memories from disk
     └─> But SystemMsg is NOT re-built
     └─> New memories only appear in NEXT SESSION

TEARDOWN:
  .evo-agent/memory/ persists to disk
  └─> Available for next agent session
```

## Memory Index Constraints

```
┌─────────────────────────────────────────────────┐
│        MEMORY.md Index File Rules               │
├─────────────────────────────────────────────────┤
│                                                 │
│ Purpose:      Dashboard/index of memories      │
│               (NOT storage of content)          │
│                                                 │
│ Format:       One line per memory entry         │
│               Example: - [Title](file.md) —     │
│               one-line hook                     │
│                                                 │
│ Max lines:    ~200 lines (soft limit)           │
│               Hard truncation warning at 334    │
│               in memory.go                      │
│                                                 │
│ Line length:  Aim for ~150 chars per line      │
│                                                 │
│ Content:      NEVER write memory content here   │
│               Only links + one-line summaries   │
│                                                 │
│ Metadata:     NO frontmatter in MEMORY.md       │
│               Frontmatter only in memory files  │
│                                                 │
└─────────────────────────────────────────────────┘
```

## Key Design Patterns

### Pattern 1: Static System Prompt
```
Built once at startup → Immutable during session
Reduces per-message overhead
Pro: Consistent context across turns
Con: Can't change guidance mid-session without restart
```

### Pattern 2: Optional Sections
```
Agent.md, Memories, Skills catalog all optional
Present: section is injected
Absent: section skipped (no prompt noise)
```

### Pattern 3: Subagent Composition
```
Parent system prompt + specialized task prompt
Ensures child agents understand project context
Allows focused execution for specific tasks
```

### Pattern 4: Memory Extraction as Tool
```
remember tool spawns subagent
Subagent has write_file access
Parent doesn't re-inject updated memories until next session
Keeps context window predictable
```

### Pattern 5: Tool Results as User Messages
```
Tool outputs are NOT appended to system prompt
Appended as USER MESSAGE content blocks
Keeps system prompt size fixed
Allows unlimited tool turns without context blowup
```

## Implications for Prompt Engineering

1. **Order matters**: System prompt sections are in fixed order (base → Agent.md → memories → guidance → skills)
2. **Section conflicts**: If Agent.md repeats memory guidance, both appear (concatenated)
3. **Memory persistence**: Memories saved mid-session only appear in NEXT session (not current)
4. **Skills are dynamic**: If you add/remove SKILL.md files and restart, skills change
5. **Agent.md is static**: Changes to Agent.md require agent restart
6. **Context budget is fixed**: System prompt size is determined at startup; doesn't grow during session
