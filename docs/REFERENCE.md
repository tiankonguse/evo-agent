# evo-agent Developer Reference

Consolidated reference for constants, data structures, how-to guides, and debugging.  
See `CLAUDE.md` for the high-level architecture overview.

---

## Key Constants & Limits

| Constant | Value | Location | Notes |
|----------|-------|----------|-------|
| `CONTEXT_LIMIT` | 50 000 chars | `agent/compact.go` | Triggers auto-compact |
| `KEEP_RECENT_RESULTS` | 3 | `agent/compact.go` | MicroCompact keeps this many full results |
| `maxConversationBytes` | 80 000 | `agent/compact.go` | Max input to LLM summarizer |
| `persistThreshold` | 30 000 chars | `tools/persist.go` | Large outputs saved to disk |
| `maxReadBytes` | 50 000 | `tools/read_file.go` | read_file output cap |
| `maxBashOutput` | 50 000 | `tools/bash.go` | bash output cap |
| `bashTimeout` | 120 s | `tools/bash.go` | Shell command timeout |
| `previewChars` | 2 000 | `tools/persist.go` | Preview returned for persisted output |
| `todoMaxItems` | 12 | `tools/todo.go` | Max session-plan entries |
| `todoReminderInterval` | 3 rounds | `tools/todo.go` | Rounds before reminder injected |
| `MaxTokens` | 8 000 | `agent/loop.go` | Per-request output token budget |

---

## File Inventory

| File | ~LOC | Responsibility |
|------|------|----------------|
| `src/main.go` | 60 | Entry point, flag parsing, TUI vs plain dispatch |
| `src/internal/agent/loop.go` | 220 | Agent loop, auto-compact, todo reminder, manual compact |
| `src/internal/agent/compact.go` | 260 | MicroCompact, CompactHistory, LLM summarizer |
| `src/internal/agent/state.go` | 25 | `LoopState`, `CompactState` structs |
| `src/internal/agent/transcripts.go` | 80 | Write/read JSONL transcripts |
| `src/internal/tools/tool.go` | 70 | `ToolDef`, `Register`, `Dispatch`, `Tools()` |
| `src/internal/tools/executor.go` | 60 | `Execute()` — iterates content blocks, emits UI events |
| `src/internal/tools/mcp.go` | 710 | Full MCP client: stdio, SSE, streamableHTTP |
| `src/internal/tools/todo.go` | 120 | `todoManager`, `GlobalTodo`, reminder logic, EvTodo emit |
| `src/internal/tools/bash.go` | 65 | bash tool (120 s timeout, 50 KB cap) |
| `src/internal/tools/read_file.go` | 60 | read_file tool |
| `src/internal/tools/write_file.go` | 45 | write_file tool (auto mkdir -p) |
| `src/internal/tools/edit_file.go` | 70 | edit_file tool (exact-string replace) |
| `src/internal/tools/skill.go` | 35 | load_skill tool |
| `src/internal/tools/compact.go` | 10 | compact tool registration |
| `src/internal/tools/persist.go` | 45 | Large output persistence |
| `src/internal/config/config.go` | 50 | Env-var config, `.env` loading |
| `src/internal/skills/registry.go` | 85 | Skill manifest loading, catalog |
| `src/internal/ui/terminal.go` | 65 | `Print*` helpers, ANSI constants, `EmitTodo` |
| `src/internal/ui/sink.go` | ~50 | `EventSink` interface, `TerminalSink`, `globalSink` |
| `src/internal/ui/events.go` | ~50 | `Event`, `EventKind`, `TodoItem` types |
| `src/internal/tui/model.go` | 315 | Bubble Tea root model, `handleAgentEvent` |
| `src/internal/tui/render.go` | 195 | `renderToolCall`, `renderThinking`, `renderTodoPanel`, `renderStatusBar` |
| `src/internal/tui/styles.go` | 105 | Lipgloss style definitions |
| `src/internal/tui/sink.go` | ~60 | `tui.Sink` (buffered channel → Bubble Tea) |

---

## Key Data Structures

### LoopState (`agent/state.go`)
```go
type LoopState struct {
    Messages         []anthropic.MessageParam
    TurnCount        int
    CompactState     *CompactState
    TransitionReason string // "" = end_turn, "tool_result" = more turns
}
```

### CompactState (`agent/state.go`)
```go
type CompactState struct {
    HasCompacted bool
    LastSummary  string
    RecentFiles  []string // FIFO, max 5
    CompactCount int
}
```

### TodoItem (`ui/events.go`)
```go
type TodoItem struct {
    ID         string
    Content    string
    Status     string // "pending" | "in_progress" | "completed"
    ActiveForm string // present-continuous label shown in TUI panel
}
```

### Event (`ui/events.go`)
```go
type EventKind int
const (
    EvThinking EventKind = iota
    EvText
    EvToolCall
    EvToolResult
    EvSystem
    EvTokens
    EvDone
    EvTodo
)

type Event struct {
    Kind         EventKind
    Text         string
    // EvToolCall
    ToolID, ToolName, ToolInput string
    // EvToolResult
    ResultID, ResultOutput string
    ResultError            bool
    // EvTokens
    Model                  string
    InputTokens, OutputTokens int64
    StopReason             string
    // EvTodo
    TodoItems []TodoItem
}
```

### SidebarInfo (`tui/model.go`)
```go
type SidebarInfo struct {
    Model, AgentName string
    InputTokens, OutputTokens, ContextLimit int64
    Skills, Tools, MCPServers []string
}
```

---

## Startup Sequence

```
main()
  ├─ flag.Parse()                 // --plain flag
  ├─ config.LoadEnv()             // loads .env (binary dir first, then cwd)
  ├─ cfg := config.Load()         // MODEL_ID required, API_KEY optional
  ├─ tools.InitMCP()              // reads .evo-agent/mcp.json, connects servers
  ├─ skills.Init()                // walks .evo-agent/skill/**/SKILL.md
  ├─ cfg.SystemMsg += Catalog()   // injects skill list into system prompt
  ├─ client := anthropic.NewClient(opts...)
  ├─ ag := agent.New(client, cfg)
  └─ if --plain:
       ag.Run(os.Stdin)           // blocking ANSI REPL
     else:
       tui.Run(ag, sidebarInfo)   // sets sink, starts Bubble Tea program
           └─ goroutine: ag.RunQuery() per user turn
```

---

## Environment Variables

| Variable | Required | Default | Notes |
|----------|----------|---------|-------|
| `MODEL_ID` | **Yes** | — | e.g. `claude-3-5-sonnet-20241022` |
| `ANTHROPIC_API_KEY` | No | SDK default | Overrides SDK env lookup |
| `ANTHROPIC_BASE_URL` | No | Anthropic API | Custom proxy endpoint |

`.env` files are searched: binary directory first, then current working directory. The `cwd` `.env` wins on conflict.

---

## How to Add a New Tool

### Step 1 — Create `src/internal/tools/mytool.go`

```go
package tools

import "encoding/json"

type myToolInput struct {
    Target string `json:"target" jsonschema_description:"What to process"`
}

func init() {
    Register(ToolDef{
        Schema: newSchema("my_tool", "One-line description.", myToolInput{}),
        Handler: func(input json.RawMessage) (string, error) {
            var in myToolInput
            if err := json.Unmarshal(input, &in); err != nil {
                return "", err
            }
            // implementation
            return "result: " + in.Target, nil
        },
    })
}
```

`newSchema` (or `GenerateSchema[T]()` depending on the helper used in the codebase) auto-generates the JSON Schema from the struct tags.

### Step 2 — Rebuild

```bash
make build
```

### Step 3 — Done

The tool is now listed in the system prompt and available to the model. No registration elsewhere needed.

---

## How to Add a Skill

1. Create directory `.evo-agent/skill/my-skill/`
2. Add `SKILL.md` with YAML frontmatter:

```markdown
---
name: my-skill
description: One-sentence summary shown in the skill catalog
---

Full instructions for the skill go here. You can reference files
using relative paths and include example prompts.
```

3. Restart the agent. The skill appears in `skills.Catalog()` which is injected into the system prompt. The model calls `load_skill my-skill` when it needs the full body.

---

## MCP Configuration (`.evo-agent/mcp.json`)

```json
{
  "mcpServers": {
    "filesystem": {
      "type": "stdio",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
      "env": { "EXTRA_VAR": "value" },
      "timeout": 30
    },
    "remote-api": {
      "type": "streamableHttp",
      "url": "https://api.example.com/mcp",
      "headers": { "Authorization": "Bearer TOKEN" },
      "timeout": 60
    },
    "streaming": {
      "type": "sse",
      "url": "https://sse.example.com/mcp",
      "disabled": false
    }
  }
}
```

Tools are automatically prefixed: `mcp__{server}__{tool}`. If the file is missing or a server fails to connect, the agent continues with remaining tools.

---

## TUI Key Bindings

| Key | Action |
|-----|--------|
| `Enter` | Send message |
| `Ctrl+Enter` / `Alt+Enter` | Insert newline in input |
| `Ctrl+C` / `Ctrl+D` | Quit |
| `q` / `exit` (as sole input) | Quit |

---

## Debugging Tips

```bash
# Run in plain mode (no TUI, direct ANSI output)
./build/evo-agent --plain

# Check tools loaded (printed at startup in plain mode or visible in status bar)
# Status bar shows: tools:N  mcp:N

# Read the full transcript of a session
cat .evo-agent/transcripts/YYYY-MM-DD_HHmmss.txt

# Inspect a persisted large tool output
cat .evo-agent/tool-results/<tool-id>.txt

# Test a specific tool invocation via the agent
>> bash echo "hello"
>> read_file src/main.go

# Run tests
cd src && go test ./...
cd src && go test ./internal/tools/...   # one package
```

Common startup messages to watch:
- `[MCP] Connected to "name" (N tools)` — MCP server OK
- `[MCP] ... error ...` — MCP server failed (agent still runs)
- `[Skills] Loaded N skill(s)` — skills OK
- `[auto compact triggered: N chars]` — context was compacted

---

## Agent Loop Flow (Detailed)

```
agent.Loop(state):
  for {
    1. MicroCompact(state.Messages, keepRecent=3)
       → replaces old ToolResult blocks with "[result truncated]"
    2. if EstimateContextSize > 50000:
         CompactHistory() → LLM summarizes → single summary message
    3. client.Messages.New(model, system, messages, tools, maxTokens=8000)
    4. state.Messages = append(state.Messages, resp.ToParam())
    5. ui.PrintTokens(model, inputTok, outputTok, stopReason)
    6. tools.Execute(resp.Content, compactState):
         for block in resp.Content:
           EvThinking  → ui.PrintThinking()
           EvText      → ui.PrintText()
           EvToolCall  → ui.PrintToolCall() → Dispatch() → result
           EvDone      → ui.PrintDone()
         return []ToolResultBlockParam
    7. if no tool results: return false (done)
    8. Todo reminder:
         if !usedTodo for 3 rounds: append XML <reminder> to results
    9. state.Messages = append(state.Messages, NewUserMessage(toolResults...))
   10. if resp contains "compact" tool_use:
         CompactHistory(focus=hint)
  }
```
