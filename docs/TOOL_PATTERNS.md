# Evo-Agent Tool Architecture Patterns

Quick reference for understanding the tool system in evo-agent.

## The 5 Core Patterns

### 1. Tool Registry (`tool.go`)
- **What**: Central registry for all tools
- **How**: Each tool calls `Register()` in its `init()` function
- **Why**: Decoupled, modular, no central file needed

```go
var registry = map[string]ToolDef{}

func Register(def ToolDef) {
    registry[def.Schema.Name] = def
}

func Dispatch(name string, input json.RawMessage) (string, error) {
    if d, ok := registry[name]; ok {
        return d.Handler(input)
    }
    return "", nil
}
```

### 2. Task Tool: Subagent Delegation (`task.go`)
- **What**: Spawn a subagent with fresh context
- **How**: Callback injection to avoid import cycles
- **Why**: Keep parent context clean, delegate exploration

```go
// In task.go (defines the callback var)
var subagentRunner func(systemPrompt string, messages []MessageParam) string

// In agent.New() (wires it at runtime)
tools.RegisterSubagentRunner(func(sysPrompt string, msgs []MessageParam) string {
    return a.RunSubagent(sysPrompt, msgs)
})

// In handler
return subagentRunner(sysPrompt, messages), nil
```

### 3. Todo Manager: Session Planning (`todo.go`)
- **What**: Session-scoped plan with stale detection
- **How**: Singleton with round counter for reminders
- **Why**: Keep agent focused on current task

```go
type todoManager struct {
    mu                sync.RWMutex
    items             []todoItem
    roundsSinceUpdate int
}

var GlobalTodo = &todoManager{}

// After 3 turns without update, inject reminder
if reminder := GlobalTodo.Reminder(); reminder != "" {
    toolResults = append(toolResults, NewTextBlock(reminder))
}
```

### 4. Plan Manager: Persistent Tasks (`plan.go`)
- **What**: Persistent task graph on disk
- **How**: JSON files in `.tasks/todo/` and `.tasks/done/`
- **Why**: Task tracking across sessions with dependencies

```
.tasks/
  todo/
    2026-05-28-auth/
      plan.md
      task_1.json (pending, blockedBy: [])
      task_2.json (pending, blockedBy: [1])
  done/
    2026-05-27-login/
      plan.md
```

**Bidirectional deps:** When task 2 has `blockedBy: [1]`, task 1 gets `blocks: [2]`

### 5. Executor: Tool Dispatch (`executor.go`)
- **What**: Single function that processes all tool calls
- **How**: Iterate response blocks, dispatch tools, collect results
- **Why**: Centralized error handling + output persistence

```go
func Execute(content []ContentBlockUnion) []ContentBlockParamUnion {
    for _, block := range content {
        case ToolUseBlock:
            output, err := Dispatch(block.Name, block.Input)
            output = PersistLargeOutput(block.ID, output)
            results = append(results, NewToolResultBlock(block.ID, output, err != nil))
    }
    return results
}
```

---

## Schema Generation Pattern

Auto-generate Anthropic tool schemas from Go structs:

```go
type TaskInput struct {
    Prompt      string `json:"prompt" jsonschema_description:"Task description"`
    Description string `json:"description" jsonschema_description:"UI summary"`
}

Register(ToolDef{
    Schema: anthropic.ToolParam{
        Name: "task",
        Description: anthropic.String("..."),
        InputSchema: GenerateSchema[TaskInput](),  // ← Auto-generated
    },
    Handler: func(input json.RawMessage) (string, error) {
        var in TaskInput
        json.Unmarshal(input, &in)  // ← Same struct
        return process(in), nil
    },
})
```

---

## Agent Loop Integration

```
1. autoCompact()                           ← Manage context size
2. client.Messages.New()                   ← Call LLM
3. Append response to history              ← Build conversation
4. toolResults := Execute(response)        ← Dispatch tools
5. Check todo usage → inject reminder      ← Stale detection
6. Append results as user message          ← Keep loop going
7. Return to step 1 if tools were called
```

Key: Tool results become the next **user message**, not assistant message.

---

## Thread Safety

| State | Sync Mechanism | Why |
|-------|---|---|
| `GlobalTodo` | `sync.RWMutex` | Multiple readers (TUI + agent loop), exclusive writers |
| `GlobalPlan` | `sync.RWMutex` | File operations must serialize |
| `Messages` | Append-only | Last writer wins |
| `CompactState` | Immutable | Passed by value |

---

## Initialization Sequence

```go
func main() {
    // Order matters!
    config.LoadEnv()
    client := anthropic.NewClient()
    
    tools.InitMCP()                    // 1. MCP server setup
    tools.GlobalMemory.Init(projectDir)    // 2. Load memories
    tools.InitPlan(projectDir)         // 3. Initialize .tasks/
    
    skills.Init()                      // 4. Load slash commands
    
    a := agent.New(client, cfg, builder)   // 5. ← Registers subagent callback
    
    a.Run()                            // 6. Start REPL/TUI
}
```

**Critical:** `agent.New()` must be called **after** tool system is ready, but it registers the callback that tools need.

---

## Adding a New Tool

Create `src/internal/tools/mytool.go`:

```go
package tools

import (
    "encoding/json"
    "github.com/anthropics/anthropic-sdk-go"
)

type MyInput struct {
    Query string `json:"query" jsonschema_description:"Search query"`
}

func init() {
    Register(ToolDef{
        Schema: anthropic.ToolParam{
            Name:        "my_tool",
            Description: anthropic.String("What it does"),
            InputSchema: GenerateSchema[MyInput](),
        },
        Handler: func(input json.RawMessage) (string, error) {
            var in MyInput
            if err := json.Unmarshal(input, &in); err != nil {
                return "", err
            }
            return doSomething(in), nil
        },
    })
}
```

That's it! Automatically discovered via `init()`.

---

## Common Patterns

### Stateless Tool
```go
Handler: func(input json.RawMessage) (string, error) {
    var in Input
    json.Unmarshal(input, &in)
    return compute(in), nil  // Pure function
}
```

### Tool with Singleton State
```go
type MyManager struct {
    mu sync.RWMutex
    data Map
}

var GlobalMyManager = &MyManager{}

Handler: func(input json.RawMessage) (string, error) {
    return GlobalMyManager.Operation(input), nil
}
```

### Tool with Validation
```go
Handler: func(input json.RawMessage) (string, error) {
    var in Input
    json.Unmarshal(input, &in)
    
    if in.Value < 0 {
        return "", fmt.Errorf("must be non-negative")
    }
    
    return process(in), nil
}
```

---

## Key Files

| File | Purpose | Key Types |
|------|---------|-----------|
| `tool.go` | Registry + dispatch | `ToolDef`, `Handler`, `Register()`, `Dispatch()` |
| `task.go` | Subagent spawning | `TaskInput`, callback registration |
| `todo.go` | Session planning | `todoManager`, `GlobalTodo`, round tracking |
| `plan.go` | Persistent tasks | `planTaskRecord`, `PlanManager`, disk layout |
| `executor.go` | Tool execution | `Execute()`, output persistence |
| `loop.go` | Agent loop | `Loop()`, todo reminder injection |

---

## Error Handling

| Scenario | Handling |
|----------|----------|
| Invalid tool input | Return `(msg, error)` → model sees error |
| Tool execution fails | Catch in `Execute()` → set `isError=true` flag |
| Missing tool | Return `("", nil)` → silent (model might retry) |
| Uninitialized dependency | Return `("Error: ...", nil)` → message visible to model |

---

## Performance Considerations

- **Micro-compact**: O(n) on recent results, fast
- **Full compact**: LLM call, expensive, only when needed
- **Large output persistence**: Avoid bloating message history
- **Todo reminders**: Injected after 3 turns without update

---

## Evo-Agent Architecture Summary

```
┌─────────────────────────────────────────────────────────────┐
│                   AGENT LOOP                                │
│  (loop.go: autoCompact → LLM → Execute → Inject Results)   │
└────────────────────────────────────────────────────────────┐
                            │
        ┌───────────────────┼───────────────────┐
        │                   │                   │
        v                   v                   v
    TOOLS.DISPATCH()    TOOL RESULTS        STATE MANAGERS
        │               (executor.go)           │
        ├─→ [tool.go]                  ┌───────┼────────┐
        │   Registry                   │       │        │
        ├─→ [task.go]          GlobalTodo  GlobalPlan  GlobalMemory
        │   task dispatch              (in-mem)  (disk)  (SQLite)
        ├─→ [todo.go]
        │   session plan      ┌────────────────────┐
        ├─→ [plan.go]         │   INITIALIZATION   │
        │   tasks             │   (main.go)        │
        ├─→ [bash.go]         │                    │
        │   execution         Config → MCP → Memory
        └─→ [read_file.go]        → Plan → Skills
            ... (10+ more)        → Agent.New()
```

---

Generated: 2026-05-28  
For full analysis, see: `evo-agent-tool-patterns-analysis.md`
