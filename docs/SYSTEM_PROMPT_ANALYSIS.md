# evo-agent System Prompt Architecture Analysis

## Overview
The evo-agent Go project uses a **sequential injection pattern** where system prompt sections are built up during initialization in `main.go`, then passed to the agent loop. The prompt is **static after initialization** — not dynamically modified during the agent loop.

## Current System Prompt Flow

### 1. **Initial Setup** (`main.go:45-76`)

```
START: config.SystemMsg = "You are a coding agent at {ProjectDir}."
       ↓
STEP 1: Load Agent.md (if exists in project root)
        cfg.SystemMsg += "\n\n# Project Guidance (Agent.md)\n\n" + agentMd_content
       ↓
STEP 2: Load persistent memories from disk
        memPrompt := tools.GlobalMemory.LoadPrompt()
        cfg.SystemMsg += "\n\n" + memPrompt
        cfg.SystemMsg += tools.MemoryGuidance (const string)
       ↓
STEP 3: Load skills catalog
        catalog := skills.Catalog()  // formatted list of model-invocable skills
        cfg.SystemMsg += "\nSkills available:\n" + catalog + 
                         "\nUse load_skill when a task needs specialized instructions..."
       ↓
STEP 4: Add slash command introduction
        cfg.SystemMsg += "\n\nSlash commands: /<skill-name> (e.g., /git-commit) is shorthand..."
       ↓
FINAL:  cfg.SystemMsg is fully built and immutable
```

### 2. **Agent Loop Usage** (`internal/agent/loop.go:98-106`)

The built system prompt is passed to every LLM call:

```go
resp, err := a.client.Messages.New(context.Background(), anthropic.MessageNewParams{
    Model: anthropic.Model(a.cfg.ModelID),
    System: []anthropic.TextBlockParam{
        {Text: a.cfg.SystemMsg},  // ← Static prompt passed here
    },
    Messages:  state.Messages,
    Tools:     tools.Tools(),
    MaxTokens: 8000,
})
```

## System Prompt Sections (In Order)

### Section 1: Base Identity
**Location:** `config.go:41`
```
"You are a coding agent at {ProjectDir}."
```
- Minimal, sets context
- Provides working directory awareness

### Section 2: Project Guidance (Agent.md)
**Location:** `main.go:49-50`
- **Optional**: Only loaded if `Agent.md` exists in project root
- **Format**: Markdown file with user-written guidance
- **Purpose**: Project-specific rules, architectural decisions, conventions
- **Example**: Build instructions, code style requirements, architectural patterns
- **Moderation**: User controls content — agent should not modify it

### Section 3: Persistent Memories
**Location:** `main.go:65-68`
- **Source**: `.evo-agent/memory/` directory
- **Format**: YAML frontmatter + markdown body (per memory file)
- **Frontmatter fields**: `name`, `description`, `type`, `content`
- **Types**: `user`, `feedback`, `project`, `reference`
- **Organization**: Grouped by type in `LoadPrompt()` output
- **Max lines in MEMORY.md index**: 200 lines (truncation warning at line 333 of memory.go)
- **Populated by**: `remember` tool spawns a subagent to extract memories from conversation

### Section 4: Memory Guidance (Constant)
**Location:** `main.go:69` + `tools/memory.go:24-42`
- **Content**: When/when-not-to save memories (user, feedback, project, reference types)
- **Immutable constant**: Guides agent on memory save eligibility
- **Does not change per project or session**

### Section 5: Skills Catalog
**Location:** `main.go:74-76`
- **Source**: `.evo-agent/skill/**/SKILL.md` files
- **Format**: Bullet list with name, optional argument hint, description
- **Filter**: Only includes skills with `disable-model-invocation != true`
- **Per-skill frontmatter**: `name`, `description`, `argument-hint`, `arguments`, `disable-model-invocation`
- **Purpose**: Tell agent which skills exist and how to invoke them via `load_skill` tool

### Section 6: Slash Commands Introduction
**Location:** `main.go:80-85`
- **Conditional**: Only added if `len(slashNames) > 0`
- **Content**: Explanation of `/skill-name` syntax as user shorthand
- **Clarification**: Model should use `load_skill` tool, not `/` syntax directly

## Key Files & Components

| File | Role |
|------|------|
| `config.go` | Config struct with `SystemMsg` field; initial base prompt |
| `main.go` | Sequential injection of all sections into `cfg.SystemMsg` |
| `agent/loop.go` | Passes static `a.cfg.SystemMsg` to every LLM call |
| `agent/subagent.go:24` | Subagents get: `a.cfg.SystemMsg + "\n" + systemPrompt` (see below) |
| `tools/memory.go` | `LoadPrompt()` formats memories; `MemoryGuidance` const |
| `skills/registry.go:104` | `Catalog()` formats model-invocable skills |

## Subagent System Prompt Pattern

**Location:** `agent/subagent.go:24`

```go
subSystem := a.cfg.SystemMsg + "\n" + systemPrompt
```

- Subagents **inherit the parent agent's full system prompt** (base + Agent.md + memories + guidance + skills)
- **Then append** a specialized `systemPrompt` parameter (e.g., for memory extraction or file editing)
- This ensures subagents have full context of the project while being task-scoped

## Memory System Details

### Memory Types & Guidance
From `tools/memory.go:158-199`:

1. **user**: User role, goals, knowledge, preferences
2. **feedback**: User-given guidance on approach (corrections + confirmations)
3. **project**: Non-obvious project facts (initiatives, deadlines, compliance rules)
4. **reference**: External resource pointers (dashboards, ticket boards, docs URLs)

### Memory Frontmatter Format
```yaml
---
name: {{memory_name}}
description: {{one-line description — used for relevance filtering}}
type: {{user | feedback | project | reference}}
---

{{memory content}}
```

### Memory Index (MEMORY.md)
- **Purpose**: Index/dashboard, not storage
- **Max 200 lines** (soft limit in prompt, hard truncation warning)
- **Format**: One line per memory, ~150 chars max
- **Example**: `- [Title](file.md) — one-line hook`
- **Never** write memory content into MEMORY.md — it's index-only

## Static vs. Dynamic Prompting

| Aspect | Current |
|--------|---------|
| **When built** | Initialization (once at startup) |
| **When passed to LLM** | Every message in the loop |
| **Modified during loop** | NO — system prompt is immutable |
| **Can change mid-session** | Only via memories (remember tool spawns subagent, then reloads) |
| **Context cost** | Fixed per session (no growing prompt) |

## Tool Registration Pattern

**Location:** Each tool file's `init()` function

```go
func init() {
    Register(ToolDef{
        Schema: anthropic.ToolParam{
            Name: "tool_name",
            Description: anthropic.String("..."),
            InputSchema: GenerateSchema[InputType](),
        },
        Handler: func(input json.RawMessage) (string, error) {
            // tool logic
        },
    })
}
```

- Tools are **discovered via init() side effects** (no explicit registry loop)
- `tools.Tools()` returns all registered schemas + MCP tools
- MCP tools prefixed `mcp__` are routed to `DispatchMCP()`

## Key Observations

1. **No prompt builder module**: System prompt is assembled directly in `main.go` with string concatenation
2. **Section order is explicit**: The sequential `cfg.SystemMsg +=` statements define the order
3. **Memory index truncation**: MEMORY.md max ~200 lines enforced by prompt injection (line 334 of memory.go)
4. **Skills are optional**: If no skills exist or all are disabled, catalog section is skipped
5. **Agent.md is optional**: Project can opt-in by creating Agent.md file
6. **Subagent inheritance**: Child agents get full parent prompt + specialized task prompt
7. **No dynamic system prompt injection**: Todo reminders and tool results are injected as **user messages**, not system prompt

## Future Extensibility Points

If you wanted to add new system prompt sections:

1. **After memory guidance** (around line 69): Best place for new static guidance
2. **After skills** (around line 76): Good for external context (docs, conventions)
3. **In separate module**: Could extract `buildSystemPrompt()` function to dedicated file

Example pattern:
```go
// In new file internal/prompt/builder.go
func BuildSystemPrompt(cfg *config.Config) string {
    var sections []string
    sections = append(sections, cfg.SystemMsg)  // base
    if agentMd, err := os.ReadFile(...); err == nil {
        sections = append(sections, "# Project Guidance\n" + string(agentMd))
    }
    sections = append(sections, "# Custom Section\n" + someContent)
    return strings.Join(sections, "\n\n")
}
```

