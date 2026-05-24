# evo-agent Skill System - Architecture & Design

## System Architecture

```
┌────────────────────────────────────────────────────────────────┐
│                          STARTUP PHASE                          │
├────────────────────────────────────────────────────────────────┤
│                                                                  │
│  main.go:40                                                      │
│  ├─ config.Load()                                                │
│  │  └─ cfg.SystemMsg = "[base system instructions]"             │
│  │                                                               │
│  ├─ skills.Init()                                                │
│  │  ├─ filepath.WalkDir(".evo-agent/skill")                      │
│  │  ├─ For each SKILL.md:                                        │
│  │  │  ├─ os.ReadFile(path)                                      │
│  │  │  ├─ parseFrontmatter(content)                              │
│  │  │  └─ documents[name] = skillDocument{...}                   │
│  │  └─ [Skills] Loaded N skill(s)                                │
│  │                                                               │
│  ├─ catalog := skills.Catalog()                                  │
│  │  └─ Formatted: "- name: description\n..."                     │
│  │                                                               │
│  └─ cfg.SystemMsg += "\nSkills available:\n" + catalog           │
│     └─ cfg.SystemMsg += "\nUse load_skill when..."               │
│                                                                  │
└────────────────────────────────────────────────────────────────┘

┌────────────────────────────────────────────────────────────────┐
│                     RUNTIME PHASE (Agent Loop)                  │
├────────────────────────────────────────────────────────────────┤
│                                                                  │
│  User Query                                                      │
│  └─ agent.Loop()                                                 │
│     ├─ client.Messages.New(                                      │
│     │  System: [cfg.SystemMsg with catalog],                     │
│     │  Messages: [...],                                          │
│     │  Tools: tools.Tools()  [includes load_skill]               │
│     │)                                                            │
│     │                                                             │
│     ├─ LLM Response (may include tool calls)                      │
│     │  ├─ Text response                                           │
│     │  └─ Tool use: {"name": "load_skill", "input": {...}}       │
│     │                                                             │
│     └─ tools.Execute(response)                                    │
│        └─ tools.Dispatch("load_skill", input)                     │
│           ├─ Unmarshals input → name                              │
│           ├─ skills.Load(name)                                    │
│           │  ├─ documents[name] lookup O(1)                       │
│           │  └─ Returns <skill>content</skill>                    │
│           └─ LLM receives result in context                       │
│                                                                  │
└────────────────────────────────────────────────────────────────┘
```

## Data Flow

```
Filesystem Layer
├─ .evo-agent/skill/git-commit/SKILL.md
├─ .evo-agent/skill/summarize-changes/SKILL.md
├─ .evo-agent/skill/codebase-visualizer/SKILL.md
└─ .evo-agent/skill/union-field-trace/SKILL.md
        │
        │ [Init time]
        │ filepath.WalkDir() + parseFrontmatter()
        ▼
In-Memory Layer
├─ documents["git-commit"] = SkillDocument{
│    Manifest: SkillManifest{
│      Name: "git-commit",
│      Description: "Best practices for writing git commit messages"
│    },
│    Body: "Always use imperative mood...",
│    Path: "/abs/path/to/SKILL.md"
│  }
├─ documents["summarize-changes"] = ...
├─ documents["codebase-visualizer"] = ...
└─ documents["union-field-trace"] = ...
        │
        │ [Startup time]
        │ Catalog() + SystemMsg injection
        ▼
System Prompt Layer
├─ [Original system instructions...]
└─ Skills available:
   - codebase-visualizer: Generate an interactive...
   - git-commit: Best practices for writing...
   - summarize-changes: Summarizes uncommitted changes...
   - union-field-trace: Analyze Union field values...
   
   Use load_skill when a task needs specialized instructions.
        │
        │ [Runtime - LLM sees catalog]
        │ LLM decides to call load_skill tool
        ▼
Tool Execution Layer
├─ LLM generates: {"name": "load_skill", "input": {"name": "git-commit"}}
├─ Handler unmarshals input
├─ skills.Load("git-commit") → O(1) map lookup
└─ Returns: <skill name="git-commit" path="...">content</skill>
        │
        │ [Tool result injected back to LLM]
        ▼
LLM Context
├─ Original query
├─ Skills catalog (in system prompt)
└─ Loaded skill content (in tool result)
   └─ LLM uses skill guidance to complete task
```

## Component Interaction

```
┌─────────────┐
│   main.go   │
└──────┬──────┘
       │ calls
       ▼
┌──────────────────────┐
│  skills.Init()       │ ◄─── Scans filesystem
│                      │
│ • filepath.WalkDir() │
│ • parseFrontmatter() │
│ • Build documents{}  │
└──────┬───────────────┘
       │
       ├─────────────────────────────────────┐
       │                                     │
       ▼                                     ▼
┌──────────────────────┐        ┌────────────────────────┐
│ skills.Catalog()     │        │ agent.New(cfg)         │
│                      │        │                        │
│ • Sort names         │        │ • Receives SystemMsg   │
│ • Format as bullets  │        │ • With catalog injected│
│ • Return string      │        │ • Registers tools      │
└─────────┬────────────┘        └────────────┬───────────┘
          │                                   │
          └────────────────┬──────────────────┘
                           │
                           ▼
                  ┌────────────────────┐
                  │  LLM sees catalog  │
                  │  in system prompt  │
                  └────────┬───────────┘
                           │
                    (User enters query)
                           │
                           ▼
                  ┌────────────────────┐
                  │  agent.Loop()      │
                  │  • LLM processes   │
                  │  • Sees catalog    │
                  │  • May call        │
                  │    load_skill tool │
                  └────────┬───────────┘
                           │
                           ▼
                  ┌────────────────────┐
                  │ tools.Dispatch()   │
                  │ ("load_skill")     │
                  └────────┬───────────┘
                           │
                           ▼
                  ┌────────────────────┐
                  │ skills.Load(name)  │◄─── O(1) map lookup
                  │                    │
                  │ • Find in docs{}   │
                  │ • Wrap in XML tags │
                  │ • Return to LLM    │
                  └────────┬───────────┘
                           │
                           ▼
                  ┌────────────────────┐
                  │ LLM receives skill │
                  │ in tool result     │
                  │ Completes task     │
                  └────────────────────┘
```

## State Management

### Global State (In-Memory)

```go
// src/internal/skills/registry.go
var documents = map[string]skillDocument{}

// Populated once at Init() time
// Read-only during runtime (no concurrent modifications)
// Persists for lifetime of agent process
```

**Thread Safety**: 
- Init() called once during startup (single-threaded)
- Runtime queries are read-only (concurrent-safe)

### Typical Memory Usage

```
Per skill:
├─ Manifest: ~100 bytes
│  ├─ Name: 20-50 bytes
│  └─ Description: 50-200 bytes
├─ Body: Content-dependent
│  ├─ Simple skill: 1-5 KB
│  ├─ Complex skill: 20-100 KB (e.g., union-field-trace ~50 KB)
└─ Path: 50-200 bytes

For 4 loaded skills: ~150-500 KB typical
```

## Execution Timeline

```
T=0ms         Startup begins
T=1ms         config.Load() complete
T=2ms         tools.InitMCP() complete
T=3ms         skills.Init() starts
T=4ms         ├─ WalkDir scans directories
T=5ms         ├─ Read SKILL.md files (I/O)
T=8ms         ├─ Parse frontmatter
T=9ms         └─ Build documents map (4 skills loaded)
T=10ms        skills.Catalog() called
T=11ms        SystemMsg concatenation
T=12ms        agent.New(cfg) complete
T=13ms        TUI initialized (if not --plain)
T=100ms       Agent ready, waiting for user input

[User enters query]

T=500ms       LLM calls load_skill("git-commit")
T=501ms       tools.Dispatch() finds handler
T=502ms       Unmarshals input
T=503ms       skills.Load() does O(1) map lookup
T=504ms       XML wrapping + return
T=505ms       Tool result injected to LLM
T=2000ms      LLM completes response
```

## Error Scenarios

```
Scenario 1: Missing .evo-agent/skill directory
├─ Init() called
├─ os.Stat(".evo-agent/skill") returns os.IsNotExist
└─ Early return (silently ignored)
   └─ Catalog() returns empty string
   └─ No skills injected to system prompt

Scenario 2: Unreadable SKILL.md file
├─ WalkDir encounters file
├─ os.ReadFile() returns error
├─ Error logged to stderr: "[Skills] Cannot read..."
└─ Continue walking (no fatal error)
   └─ Skill not added to documents{}

Scenario 3: LLM calls load_skill("unknown-skill")
├─ skills.Load("unknown-skill")
├─ Map lookup fails (not in documents{})
├─ knownNames() generates available list
└─ Return error: "Error: Unknown skill 'unknown-skill'. Available skills: ..."
   └─ LLM receives error as tool result
   └─ LLM can recover (retry with correct name or ask user)

Scenario 4: Malformed frontmatter
├─ parseFrontmatter() called
├─ Regex matches partial frontmatter
├─ Key-value parser handles gracefully
├─ Missing fields get defaults:
│  ├─ name → parent directory name
│  └─ description → "No description"
└─ Skill loaded with defaults (no error)

Scenario 5: SKILL.md has no frontmatter
├─ parseFrontmatter() finds no "---" markers
├─ Regex returns empty matches
├─ body = entire file content
├─ name = parent directory name
└─ description = "No description"
   └─ Skill loaded successfully (body is entire file)
```

## Performance Characteristics

```
Operation          Time      Space      Notes
─────────────────────────────────────────────────────────
Init()             1-5ms     ~200KB     Single scan, worst case
Catalog()          <1ms      ~1KB       O(n) sort + format
Load(name)         <0.1ms    Varies     O(1) map lookup + formatting
Names()            <0.1ms    ~1KB       O(n) iteration
─────────────────────────────────────────────────────────

Scaling:
├─ 4 skills: ~2ms Init, ~150KB memory
├─ 20 skills: ~8ms Init, ~500KB memory
├─ 100 skills: ~40ms Init, ~2MB memory
└─ 1000 skills: ~400ms Init, ~20MB memory
   (Not recommended; performance degrades with many small files)
```

## Design Patterns Used

### Pattern 1: Registry Pattern
```
Single global map (documents) acts as registry.
All queries go through public API functions.
Initialization is explicit (Init() must be called).
```

### Pattern 2: Strategy Pattern
```
Handler function assigned per tool (skill.go:Handler).
Each tool gets its own unmarshal/execute strategy.
New tools added by creating init() function.
```

### Pattern 3: Adapter Pattern
```
Tool handler adapts JSON input to skills.Load() call.
ToolInputSchema auto-generated from struct tags.
Marshaling/unmarshaling handled by handler.
```

### Pattern 4: Lazy Loading (Startup-Time)
```
Skills loaded once at startup (not on-demand).
Avoids I/O latency during runtime queries.
Trade-off: Startup slightly slower for faster runtime.
```

## Security Considerations

```
Input Validation
├─ Skill name from LLM: User-controlled
│  └─ Mitigation: Map lookup only (no path traversal)
│  └─ Mitigation: knownNames() lists safe names
│  └─ Error message shows available skills
└─ No risk of arbitrary file access

File Access
├─ Read-only access to SKILL.md files
├─ No write operations
├─ No execution of file content
└─ Safe for version control

Content Display
├─ Skill content wrapped in XML tags
├─ Presented as-is to LLM (no eval/exec)
├─ LLM is responsible for interpreting instructions
└─ No code injection from skill files
```

## Extensibility

```
Current
└─ 4 hardcoded skills (git-commit, summarize-changes, etc.)

Future Extensions
├─ Dynamic skill discovery (auto-scan subdirs)
├─ Skill versioning (.evo-agent/skill/name/v1/SKILL.md)
├─ Skill dependencies (meta: requires: [other-skill])
├─ Parametrized skills (load_skill("skill", {"param": "value"}))
├─ Skill validation (JSON schema for instructions)
├─ Skill metadata plugins (custom frontmatter handlers)
└─ Skill reload without restart (file watch + reload API)
```

## Comparison with Alternatives

| Approach | Pros | Cons | evo-agent Choice |
|----------|------|------|------------------|
| **File-based (YAML)** | Simple, version-controllable, discoverable | Static, no runtime updates | ✓ Chosen |
| **Database** | Dynamic, queryable, versionable | Requires schema, migration, deployment complexity | |
| **Hardcoded strings** | Fastest, no I/O | Not maintainable, version control unfriendly | |
| **HTTP endpoint** | Dynamic, shareable | Network latency, external dependency | |
| **Environment variables** | Simple, standard | Limited size, poor formatting | |

## Summary

The evo-agent skill system is a **lightweight, file-based registry** that:

1. **Scans at startup** (`Init()`) for all SKILL.md files
2. **Parses metadata** from YAML frontmatter
3. **Injects catalog** into system prompt
4. **Exposes load_skill** tool for runtime access
5. **Returns XML-wrapped** skills with path metadata
6. **Handles errors gracefully** (defaults, fallbacks, friendly messages)
7. **Maintains O(1) lookup** performance after initial scan
8. **Enables easy extension** via simple file-based convention

This design prioritizes **simplicity, discoverability, and version control** over dynamic features, making it ideal for team-based development workflows.

