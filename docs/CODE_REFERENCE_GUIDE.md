# evo-agent System Prompt - Code Reference Guide

Quick lookup for where each piece of the system prompt is built.

## File Map

| Component | File | Lines | Purpose |
|-----------|------|-------|---------|
| **Config struct** | `internal/config/config.go` | 12-18, 41 | Defines `SystemMsg` field; creates base prompt |
| **Main initialization** | `main.go` | 45-86 | Sequential injection of all prompt sections |
| **Agent loop** | `internal/agent/loop.go` | 88-165 | Uses `a.cfg.SystemMsg` in every LLM call (line 101) |
| **Subagent spawning** | `internal/agent/subagent.go` | 19-83 | Builds subagent prompt: `a.cfg.SystemMsg + "\n" + systemPrompt` (line 24) |
| **Memory loading** | `internal/tools/memory.go` | 55-242 | Loads memories, formats `LoadPrompt()`, defines `MemoryGuidance` |
| **Skills loading** | `internal/skills/registry.go` | 39-129 | Loads skills, formats `Catalog()` for prompt |
| **Memory extraction prompt** | `internal/tools/memory.go` | 246-346 | `buildExtractionPrompt()` — sent to remember subagent |
| **Memory consolidation prompt** | `internal/tools/memory.go` | 350-376 | `buildConsolidatePrompt()` — sent to consolidate subagent |

## Step-by-Step System Prompt Build

### 1. Initial Config (config.go)

```go
// config.go:41
func Load() *Config {
    cwd, _ := os.Getwd()
    return &Config{
        ModelID:    os.Getenv("MODEL_ID"),
        APIKey:     os.Getenv("ANTHROPIC_API_KEY"),
        BaseURL:    os.Getenv("ANTHROPIC_BASE_URL"),
        ProjectDir: cwd,
        SystemMsg:  fmt.Sprintf("You are a coding agent at %s.", cwd),  // ← BASE
    }
}
```

**Result**: `cfg.SystemMsg = "You are a coding agent at /path/to/project."`

---

### 2. Load Agent.md (main.go:49-50)

```go
// main.go:49-50
if agentMd, err := os.ReadFile(filepath.Join(cfg.ProjectDir, "Agent.md")); err == nil {
    cfg.SystemMsg += "\n\n# Project Guidance (Agent.md)\n\n" + string(agentMd)
}
```

**Conditions**:
- File: `{ProjectDir}/Agent.md`
- Optional: Silently skipped if file doesn't exist
- User controls content

**Result**: Appends project guidance (if file exists)

---

### 3. Load Persistent Memories (main.go:65-68)

```go
// main.go:65-68
tools.GlobalMemory.Init(cfg.ProjectDir)
if memPrompt := tools.GlobalMemory.LoadPrompt(); memPrompt != "" {
    cfg.SystemMsg += "\n\n" + memPrompt
}
```

**Behind the scenes** (`tools/memory.go`):

```go
// memory.go:91-100
func (m *MemoryManager) Init(projectDir string) {
    m.mu.Lock()
    m.dir = filepath.Join(projectDir, memorySubdir)  // .evo-agent/memory/
    m.mu.Unlock()
    
    os.MkdirAll(m.dir, 0o755)
    m.LoadAll()  // Scan and parse all .md files
}

// memory.go:158-199
func (m *MemoryManager) LoadPrompt() string {
    // Returns formatted memories organized by type
    // Groups: [user], [feedback], [project], [reference]
    // Format: "# Memories (persistent...)\n\n## [type]\n### name: desc\n{content}"
}
```

**Result**: Appends formatted memories (if any exist)

---

### 4. Append Memory Guidance Constant (main.go:69)

```go
// main.go:69
cfg.SystemMsg += tools.MemoryGuidance

// tools/memory.go:24-42
const MemoryGuidance = `
## Memory guidance

When to save memories (use the remember tool):
- User states a preference ("I like tabs", ...) → type: user
- User corrects your approach ("don't do X", ...) → type: feedback
- You learn a project fact NOT easily inferred from code → type: project
- You learn where an external resource lives → type: reference

When NOT to save:
- Anything easily derivable from code
- Temporary task state
- Secrets or credentials
- Git history or recent changes
- Debugging solutions
`
```

**Result**: Appends unchanging guidance text

---

### 5. Load Skills Catalog (main.go:74-76)

```go
// main.go:74-76
skills.Init()
if catalog := skills.Catalog(); catalog != "" {
    cfg.SystemMsg += "\nSkills available:\n" + catalog +
        "\nUse load_skill when a task needs specialized instructions before you act."
}
```

**Behind the scenes** (`skills/registry.go`):

```go
// skills/registry.go:39-99
func Init() {
    skillsDir := filepath.Join(".evo-agent", "skill")
    if _, err := os.Stat(skillsDir); os.IsNotExist(err) {
        return  // Silently skip if dir doesn't exist
    }
    
    filepath.WalkDir(skillsDir, func(path string, d os.DirEntry, err error) error {
        if d.Name() != "SKILL.md" {
            return nil
        }
        // Parse frontmatter: name, description, argument-hint, disable-model-invocation
        // Store in skillDocuments map
    })
    
    InitCommands()  // Also load commands from .evo-agent/command/
}

// skills/registry.go:104-129
func Catalog() string {
    // Format: "- skill-name [arg]: description\n- ..."
    // Only includes skills where disable-model-invocation != true
    // Returns "" if no skills loaded or all disabled
}
```

**Result**: Appends skills list (if skills exist and are model-invocable)

---

### 6. Append Slash Command Intro (main.go:80-85)

```go
// main.go:80-85
slashNames := skills.SlashNames()
if len(slashNames) > 0 {
    cfg.SystemMsg += "\n\nSlash commands: /<skill-name> (e.g., /git-commit) is shorthand for users " +
        "to invoke a skill. When executed, the skill content is expanded into a full prompt. " +
        "Use the load_skill tool to load skills programmatically. " +
        "IMPORTANT: Only use load_skill for skills listed above - do not guess or invent skill names."
}
```

**Conditions**:
- Only added if `slashNames` is non-empty
- `SlashNames()` returns names of user-invocable commands/skills

**Result**: Appends explanation of `/` syntax (conditional)

---

### 7. Agent Loop Usage (agent/loop.go:98-106)

```go
// agent/loop.go:88-165
func (a *Agent) Loop(state *LoopState) bool {
    for {
        a.autoCompact(state)  // Micro-compact before LLM call
        
        resp, err := a.client.Messages.New(context.Background(), 
            anthropic.MessageNewParams{
                Model: anthropic.Model(a.cfg.ModelID),
                System: []anthropic.TextBlockParam{
                    {Text: a.cfg.SystemMsg},  // ← STATIC PROMPT EVERY TURN
                },
                Messages:  state.Messages,    // ← CONVERSATION (grows)
                Tools:     tools.Tools(),
                MaxTokens: 8000,
            })
        
        // Process response, execute tools
        // Append tool results as USER MESSAGE (not system)
        state.Messages = append(state.Messages, 
            anthropic.NewUserMessage(toolResults...))
    }
}
```

**Key insight**: `a.cfg.SystemMsg` is **static** for entire session. Tool results and reminders are added to `state.Messages` (user messages), not to system prompt.

---

## Memory System Details

### Memory File Format

```yaml
---
name: {{memory_name}}
description: {{one-line description}}
type: {{user | feedback | project | reference}}
---

{{memory content here}}
```

### Memory Index (MEMORY.md)

```markdown
- [Memory 1](memory_1.md) — Short description here
- [Memory 2](memory_2.md) — Another short description
```

**Constraints**:
- Max ~200 lines (soft limit, truncation warning)
- One line per memory
- No frontmatter in index
- Index is loaded but never updated by agent (subagent updates it)

### Memory Types Reference

| Type | When to Save | Example |
|------|--------------|---------|
| **user** | User role/preferences/knowledge | "User is Go expert, learning Rust" |
| **feedback** | User corrections OR confirmations | "Always use -v flag for debugging" |
| **project** | Non-obvious project facts | "Auth rewrite driven by compliance" |
| **reference** | External resource pointers | "OncCall dashboard at grafana.internal/d/X" |

---

## Skills & Commands System

### Skill Loading

```
.evo-agent/skill/{skill_name}/SKILL.md
    ↓
Parse frontmatter (name, description, disable-model-invocation, etc.)
    ↓
Store in skillDocuments map
    ↓
Catalog() formats for system prompt (excludes disabled)
```

### Skill Frontmatter

```yaml
---
name: my-skill
description: What this skill does
disable-model-invocation: false  # default: false (model can invoke via load_skill)
user-invocable: true             # default: true (user can invoke via /my-skill)
argument-hint: "[arg1] [arg2]"  # optional
arguments: arg1, arg2             # optional
---

Skill content here...
```

### Command vs Skill

| Aspect | Skill | Command |
|--------|-------|---------|
| **Storage** | `.evo-agent/skill/{name}/SKILL.md` | `.evo-agent/command/{name}.md` |
| **In catalog** | Yes (if `disable-model-invocation` != true) | No |
| **In system prompt** | Yes (if model-invocable) | No |
| **User slash trigger** | Yes (`/skill-name`) | Yes (`/command-name`) |
| **Model can invoke** | Yes via `load_skill` tool | No |

---

## Tool System

### Tool Registration Pattern

Each tool file has an `init()` function that registers itself:

```go
// internal/tools/some_tool.go
package tools

func init() {
    Register(ToolDef{
        Schema: anthropic.ToolParam{
            Name: "tool_name",
            Description: anthropic.String("..."),
            InputSchema: GenerateSchema[InputType](),
        },
        Handler: func(input json.RawMessage) (string, error) {
            var in InputType
            json.Unmarshal(input, &in)
            // Execute tool logic
            return output, nil
        },
    })
}
```

### Tool Execution Flow

```go
// tools/tool.go:32-40
func Tools() []anthropic.ToolUnionParam {
    out := make([]anthropic.ToolUnionParam, 0, len(registry))
    for _, d := range registry {
        tool := d.Schema
        out = append(out, anthropic.ToolUnionParam{OfTool: &tool})
    }
    out = append(out, MCPTools()...)  // Add MCP tools
    return out
}

// tools/tool.go:45-50
func Dispatch(name string, input json.RawMessage) (string, error) {
    if strings.HasPrefix(name, "mcp__") {
        return DispatchMCP(name, input)  // Route to MCP
    }
    if d, ok := registry[name]; ok {
        return d.Handler(input)  // Execute registered handler
    }
    return "", fmt.Errorf("unknown tool: %s", name)
}
```

---

## Subagent System Prompt Pattern

### Memory Extraction Subagent

```go
// tools/memory.go:246-346
func buildExtractionPrompt(memoryDir, existingMemories string) string {
    // Returns a specialized prompt for memory extraction
    // Subagent receives:
    //   - Memory directory path
    //   - List of existing memories
    //   - Memory types & guidelines
    //   - Available tools (read_file, write_file, edit_file, bash)
}

// tools/memory.go:401-441
if subagentRunner == nil {
    return "Error: subagent runner not initialized", nil
}

memDir := GlobalMemory.Dir()
sysPrompt := buildExtractionPrompt(memDir, existing)

// Conversation history is passed to subagent
messages := getConversationMessages()
subMessages := append(messages, 
    anthropic.NewUserMessage(anthropic.NewTextBlock(trigger)))

// Spawn child agent with inherited parent context
result := subagentRunner(sysPrompt, subMessages)

// Reload memories after subagent finishes
GlobalMemory.LoadAll()
```

### Subagent Invocation

```go
// agent/subagent.go:19-83
func (a *Agent) RunSubagent(systemPrompt string, 
    messages []anthropic.MessageParam) string {
    
    // Line 24: Combine parent prompt + task prompt
    subSystem := a.cfg.SystemMsg + "\n" + systemPrompt
    
    // Line 28-35: Loop up to 30 turns
    for turn := 0; turn < subagentMaxTurns; turn++ {
        resp, err := a.client.Messages.New(context.Background(), 
            anthropic.MessageNewParams{
                Model:     anthropic.Model(a.cfg.ModelID),
                System:    []anthropic.TextBlockParam{{Text: subSystem}},
                Messages:  subMessages,
                Tools:     childTools,  // Excludes "task" tool
                MaxTokens: 8000,
            })
    }
    
    return lastText  // Return final summary
}
```

---

## Context Compaction System

### Micro-Compaction (Implicit)

```go
// agent/loop.go:37-38
func (a *Agent) autoCompact(state *LoopState) {
    state.Messages = MicroCompact(state.Messages, KEEP_RECENT_RESULTS)
    
    contextSize := EstimateContextSize(state.Messages)
    if contextSize <= CONTEXT_LIMIT {
        return
    }
    
    // Trigger full LLM-based summarization if needed
    newMessages, err := CompactHistory(...)
}
```

### Todo Reminder Injection (User Message)

```go
// agent/loop.go:143-156
usedTodo := false
for _, block := range resp.Content {
    if block.Type == "tool_use" && block.Name == "todo" {
        usedTodo = true
        break
    }
}
tools.GlobalTodo.NoteRound(usedTodo)
if reminder := tools.GlobalTodo.Reminder(); reminder != "" {
    // Injected as USER MESSAGE, not system prompt
    toolResults = append(toolResults, anthropic.NewTextBlock(reminder))
}
```

---

## Constants

### Memory Subdir
```go
// tools/memory.go:17-19
const (
    memorySubdir    = ".evo-agent/memory"
    memoryIndexFile = "MEMORY.md"
    maxIndexLines   = 200
)
```

### Agent Loop
```go
// main.go:24-25
const (
    contextLimit = 200000  // Claude's context window
)

// agent/subagent.go:14
const subagentMaxTurns = 30
```

---

## Test References

Memory & skills loading are tested in:
- `internal/skills/registry_test.go` — Catalog() and skill loading
- `internal/skills/dispatch_test.go` — Skill dispatch
- Memory functions tested in memory.go itself

