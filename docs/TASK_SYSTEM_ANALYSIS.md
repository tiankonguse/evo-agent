# EVO-AGENT Task System Analysis

## Executive Summary

The evo-agent project has a **well-designed task/subagent system** built on these core principles:

1. **Tool Registration Pattern** — All tools self-register in `init()` functions
2. **Task Tool** — Spawns lightweight subagents with fresh context but shared filesystem
3. **Subagent Architecture** — Child agents get all tools except "task" (prevents recursion)
4. **Callback Pattern** — Avoids import cycles using `RegisterSubagentRunner`

---

## 1. TOOL REGISTRATION PATTERN

### File: `src/internal/tools/tool.go`

**Core Structures:**
```go
type Handler func(input json.RawMessage) (string, error)

type ToolDef struct {
    Schema  anthropic.ToolParam
    Handler Handler
}

var registry = map[string]ToolDef{}
```

**Registration API:**
- `Register(def ToolDef)` — Add a tool to the global registry
- `Tools()` — Returns all tool schemas (built-in + MCP tools) ready for Anthropic API
- `Dispatch(name string, input json.RawMessage)` — Route tool calls to handlers
- `ToolsExcept(exclude ...string)` — Returns all tools except specified ones
- `GenerateSchema[T]()` — Reflect struct tags to build input schema

**Each tool file pattern:**
```go
func init() {
    Register(ToolDef{
        Schema: anthropic.ToolParam{
            Name: "tool_name",
            Description: anthropic.String("..."),
            InputSchema: GenerateSchema[InputStruct](),
        },
        Handler: func(input json.RawMessage) (string, error) {
            var in InputStruct
            if err := json.Unmarshal(input, &in); err != nil {
                return "", err
            }
            // Implementation
            return result, err
        },
    })
}
```

**Key Pattern Details:**
- Tools use struct field tags: `jsonschema_description:"..."`
- Schema generation is automatic via reflection
- MCP tools are routed separately (prefix: `mcp__`)
- Tools can be excluded from specific contexts (e.g., task tool removed for subagents)

---

## 2. EXISTING TASK TOOL

### File: `src/internal/tools/task.go`

**Purpose:** Spawn a subagent with fresh context to delegate complex subtasks

**Input Schema:**
```go
type TaskInput struct {
    Prompt      string // Full task description for the subagent
    Description string // One-line summary shown in UI
}
```

**Implementation:**
```go
var subagentRunner func(systemPrompt string, messages []anthropic.MessageParam) string

func RegisterSubagentRunner(fn func(...) string) {
    subagentRunner = fn  // Called once at agent startup
}

Handler: func(input json.RawMessage) (string, error) {
    var in TaskInput
    if err := json.Unmarshal(input, &in); err != nil {
        return "", err
    }
    if subagentRunner == nil {
        return "Error: subagent runner not initialized", nil
    }
    
    sysPrompt := "You are a subagent. Complete the given task..."
    messages := []anthropic.MessageParam{
        anthropic.NewUserMessage(anthropic.NewTextBlock(in.Prompt)),
    }
    return subagentRunner(sysPrompt, messages), nil
}
```

**Key Characteristics:**
- System prompt is simple and generic
- Only passes user's task prompt in messages (fresh context)
- Uses callback pattern to avoid import cycle (agent ← tools)
- Returns only final text summary (subagent context is discarded)

---

## 3. SUBAGENT ARCHITECTURE

### File: `src/internal/agent/subagent.go`

**Entry Point:** `Agent.RunSubagent(systemPrompt, messages) string`

**Loop Structure:**
```
for turn := 0; turn < 30; turn++ {
    1. Call Claude API with all tools EXCEPT "task"
    2. Print response to UI (system/tokens/blocks)
    3. For each content block:
       - Text: capture as lastText, print to UI
       - ToolUse: dispatch to handler, collect results
    4. If no tool calls: BREAK (subagent done)
    5. Append results as user message, continue loop
}
return lastText  // Final text summary only
```

**Tool Set Configuration:**
```go
childTools := tools.ToolsExcept("task")  // Remove task tool from subagents
subSystem := a.prompt.Build() + "\n" + systemPrompt
```

**Why remove "task" tool:**
- Prevents infinite recursion of subagent spawning
- Subagents are designed to be leaf nodes
- Parent agent delegates complex work; subagents complete it

**Turn Limit:** 30 turns (prevents runaway subagents)

**Output Handling:**
- Last text block is captured and returned to parent
- Large outputs are persisted to disk via `PersistLargeOutput()`
- Only the summary goes back; subagent context is discarded

---

## 4. INTEGRATION: How It All Connects

### File: `src/internal/agent/loop.go`

**Initialization:**
```go
// At agent startup, register the subagent runner callback
tools.RegisterSubagentRunner(func(systemPrompt string, messages []anthropic.MessageParam) string {
    return a.RunSubagent(systemPrompt, messages)
})
```

**Call Flow:**
```
Agent.Execute() [main loop]
  ↓
Claude API response includes tool use "task"
  ↓
tools.Dispatch("task", input)
  ↓
task.go Handler unmarshals TaskInput
  ↓
subagentRunner(sysPrompt, messages)  [callback invoked]
  ↓
Agent.RunSubagent(sysPrompt, messages)
  ↓
Subagent loop (max 30 turns)
  - Calls tools (except "task")
  - Collects results
  - Returns when done
  ↓
Return lastText to parent
  ↓
Parent continues with summary
```

---

## 5. RELATED TOOLS & PATTERNS

### Memory Tool (`src/internal/tools/memory.go`)

**Two tool entries:**
- `remember` — Spawn subagent to extract and save memories
- `consolidate_memory` — Spawn subagent to merge/cleanup memories

**Pattern:**
- Uses subagent to do file I/O (read_file, write_file, edit_file)
- Builds custom system prompts with extraction/consolidation instructions
- Reloads memory state after subagent completes
- Global `GlobalMemory` manager persists memories across sessions

### Todo Tool (`src/internal/tools/todo.go`)

**Purpose:** Session plan tracking (not multi-session persistent)

**Features:**
- Max 12 items, only 1 can be in_progress
- Tracks "rounds since update" for reminders
- Syncs with TUI via `ui.EmitTodo()`
- Built-in validation (status enum, constraints)

### Bash Tool (`src/internal/tools/bash.go`)

**Implementation:**
- Timeout: 120 seconds
- Process group management to kill background processes
- Output capped at 50KB
- Error handling for timeouts

---

## 6. SCHEMA GENERATION PATTERN

**From tool.go:**
```go
func GenerateSchema[T any]() anthropic.ToolInputSchemaParam {
    reflector := jsonschema.Reflector{
        AllowAdditionalProperties: false,
        DoNotReference:            true,
    }
    var v T
    schema := reflector.Reflect(v)
    return anthropic.ToolInputSchemaParam{
        Properties: schema.Properties,
    }
}
```

**Usage in each tool:**
```go
type InputStruct struct {
    Field string `json:"field" jsonschema_description:"Description here"`
}

InputSchema: GenerateSchema[InputStruct]()
```

**Benefits:**
- Single source of truth (Go struct = schema)
- Automatic description from tags
- No manual JSON schema maintenance
- Type-safe unmarshaling

---

## 7. KEY FILES SUMMARY

| File | Purpose | Key Exports |
|------|---------|-------------|
| `tool.go` | Tool registry & schema generation | `Register()`, `Tools()`, `Dispatch()`, `ToolsExcept()` |
| `task.go` | Spawn subagents for subtasks | Task tool with `RegisterSubagentRunner()` |
| `subagent.go` | Subagent execution loop | `Agent.RunSubagent()` |
| `memory.go` | Persistent memory extraction & consolidation | `remember` & `consolidate_memory` tools, `GlobalMemory` manager |
| `todo.go` | Session plan management | `todo` tool, `GlobalTodo` manager |
| `bash.go` | Shell command execution | `bash` tool with timeout/process group handling |
| `read_file.go` | File reading with line limits | `read_file` tool |
| `write_file.go` | File writing with mkdir | `write_file` tool |
| `edit_file.go` | File content replacement | `edit_file` tool |

---

## 8. DESIGN PATTERNS & BEST PRACTICES OBSERVED

### ✅ Patterns Used

1. **Callback Pattern** — Avoids import cycles (tools → agent)
2. **Registry Pattern** — Self-registering tools via `init()`
3. **Generic Schema Generation** — DRY principle applied to input schemas
4. **Tool Exclusion** — Context-aware tool availability (subagents exclude "task")
5. **Singleton Managers** — `GlobalTodo`, `GlobalMemory` for session state
6. **Subagent Discipline** — Max turns, no recursive spawning, context discarding

### 🎯 Design Goals

- **Clean separation** — Tools don't import agent; agent registers tools
- **Composability** — Tools can be dynamically included/excluded
- **Scalability** — New tools only require adding a new file
- **Safety** — Recursive spawning prevented, timeouts enforced
- **Persistence** — Memory and todo state managed across turns/sessions

---

## 9. EXTENSION POINTS FOR NEW TASKS

To add a new structured task tool following this pattern:

1. **Create a new file** in `src/internal/tools/` (e.g., `new_task.go`)
2. **Define input struct** with `jsonschema_description` tags
3. **Implement Handler** that processes input and returns string result
4. **Register in init()** via `Register(ToolDef{...})`
5. **Optional: Use subagent** if you need complex orchestration
   - Call `subagentRunner(sysPrompt, messages)` with custom prompt
   - Subagent will have all tools except "task" and "new_task" (if excluded)

Example template:
```go
package tools

import (
    "encoding/json"
    "github.com/anthropics/anthropic-sdk-go"
)

type MyTaskInput struct {
    Field string `json:"field" jsonschema_description:"What this field does"`
}

func init() {
    Register(ToolDef{
        Schema: anthropic.ToolParam{
            Name: "my_task",
            Description: anthropic.String("Description of the task"),
            InputSchema: GenerateSchema[MyTaskInput](),
        },
        Handler: func(input json.RawMessage) (string, error) {
            var in MyTaskInput
            if err := json.Unmarshal(input, &in); err != nil {
                return "", err
            }
            // Implementation
            return "result", nil
        },
    })
}
```

