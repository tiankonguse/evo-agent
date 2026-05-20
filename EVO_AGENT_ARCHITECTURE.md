# EVO-AGENT Architecture Overview

## Project Summary
**Go Module**: `evo-agent` (Go 1.26)
**Entry Point**: `src/main.go`
**Agent Loop**: `src/internal/agent/loop.go`

EVO-Agent is a **multi-turn LLM agent framework** that orchestrates Claude with tools, MCP servers, and skills. It's designed for autonomous coding tasks with context management, tool dispatch, and extensibility.

---

## 1. Directory Structure

```
src/
├── main.go                          # Entry point
├── evo-agent                        # Binary output
├── go.mod                           # Dependencies
├── go.sum
└── internal/
    ├── agent/                       # Agent loop & state management
    │   ├── loop.go                  # Main agent loop
    │   ├── state.go                 # LoopState, CompactState
    │   ├── compact.go               # Context compression & summarization
    │   └── transcripts.go           # Session recording
    ├── config/
    │   └── config.go                # Configuration from environment
    ├── tools/                       # Tool registry & dispatch system
    │   ├── tool.go                  # Core tool registry interface
    │   ├── executor.go              # Tool call execution & output rendering
    │   ├── mcp.go                   # MCP client implementation (stdio/SSE/HTTP)
    │   ├── skill.go                 # Skill loader tool
    │   ├── bash.go                  # Shell execution tool
    │   ├── read_file.go             # File reader tool
    │   ├── write_file.go            # File writer tool
    │   ├── edit_file.go             # File editor tool
    │   ├── persist.go               # Large output persistence
    │   └── compact.go               # Context compaction tool
    ├── ui/
    │   └── terminal.go              # Basic terminal output with ANSI colors
    └── skills/
        ├── registry.go              # Skill loader from .evo_agent/skill/**
        └── registry_test.go
```

---

## 2. Main Entry Point (`src/main.go`)

### Initialization Flow
```go
main()
  ├─ config.LoadEnv()                    // Load .env files (binary dir, then cwd)
  ├─ cfg := config.Load()                // Read MODEL_ID, API_KEY, BASE_URL, set SystemMsg
  ├─ client := anthropic.NewClient(...)  // Create Anthropic client
  ├─ tools.InitMCP()                     // Connect to MCP servers from .evo_agent/mcp.json
  ├─ skills.Init()                       // Scan .evo_agent/skill/**/SKILL.md
  ├─ cfg.SystemMsg += skills.Catalog()   // Inject skill list into system prompt
  ├─ tools.PrintToolList()               // Debug output
  └─ a.Run(os.Stdin)                     // Start REPL
```

### Configuration
- **MODEL_ID**: Required. LLM model to use (e.g., "claude-3-5-sonnet-20241022")
- **ANTHROPIC_API_KEY**: LLM API key
- **ANTHROPIC_BASE_URL**: Optional. Override API endpoint
- **SystemMsg**: Auto-generated from cwd; extended with skill catalog

---

## 3. Agent Loop (`src/internal/agent/loop.go`)

### REPL Structure
```
Run(io.Reader)
  ├─ Loop 1: Parse user query
  │   ├─ Loop(LoopState)                          // Run agent loop
  │   │   ├─ autoCompact()                        // Micro-compact + auto-compact if needed
  │   │   ├─ client.Messages.New()                // Call LLM
  │   │   ├─ Append response to history
  │   │   ├─ tools.Execute(resp.Content)          // Run tool calls
  │   │   ├─ manualCompact()                      // Check for "compact" tool call
  │   │   └─ Loop until stop_reason != "tool_use"
  │   ├─ Print final assistant text response
  │   └─ Loop 2: Read next query
```

### Key Structures

**LoopState** (`state.go`):
```go
type LoopState struct {
    Messages         []anthropic.MessageParam  // Conversation history
    TurnCount        int                       // Number of tool calls
    TransitionReason string                   // Why we transitioned
    CompactState     *CompactState            // Context management state
}

type CompactState struct {
    HasCompacted bool     // Whether compaction occurred
    LastSummary  string   // Generated summary
    RecentFiles  []string // Tracked file paths (FIFO, max 5)
    CompactCount int      // Compression count
}
```

### Message Flow
1. **User input** → `anthropic.NewUserMessage(query)`
2. **LLM call** → `Messages.New()` with:
   - `System`: SystemMsg (with skill catalog)
   - `Messages`: history
   - `Tools`: All registered + MCP tools
   - `MaxTokens`: 8000
3. **Tool execution** → `tools.Execute(resp.Content)`
4. **Result append** → `anthropic.NewUserMessage(toolResults...)`
5. **Loop until** `stop_reason == "end_turn"` (no more tool calls)

---

## 4. Tool System

### Tool Registry Pattern (`tool.go`)

```go
type ToolDef struct {
    Schema  anthropic.ToolParam
    Handler Handler  // func(json.RawMessage) (string, error)
}

// Register() called from each tool's init()
// Tools() returns all registered schemas + MCP tools
// Dispatch(name, input) routes to handler or MCP dispatcher
```

### Tool Registration Pattern
Each tool file implements:
```go
func init() {
    Register(ToolDef{
        Schema: anthropic.ToolParam{
            Name: "tool_name",
            Description: "...",
            InputSchema: GenerateSchema[InputStruct](),
        },
        Handler: func(input json.RawMessage) (string, error) {
            // Parse, execute, return
        },
    })
}
```

### Native Tools

| Tool | Purpose | File |
|------|---------|------|
| `read_file` | Read file contents (max 50KB) | `read_file.go` |
| `write_file` | Create/overwrite files | `write_file.go` |
| `edit_file` | String replace editing | `edit_file.go` |
| `bash` | Execute shell commands (120s timeout) | `bash.go` |
| `load_skill` | Inject skill body into context | `skill.go` |
| `compact` | Trigger LLM-based history compression | `compact.go` |

### Execution Flow (`executor.go`)

```
Execute(content []ContentBlockUnion) []ContentBlockParamUnion
  ├─ For each block:
  │   ├─ TextBlock   → ui.PrintText()
  │   ├─ ThinkingBlock → ui.PrintThinking()
  │   └─ ToolUseBlock:
  │       ├─ ui.PrintToolCall()
  │       ├─ Dispatch(name, input)  // Route to handler or MCP
  │       ├─ persistLargeOutput()   // Save >30KB to .evo_agent/tool-results/
  │       ├─ ui.PrintCommand()
  │       └─ Append ToolResultBlock to results
  └─ Return results for next message
```

### Output Persistence (`persist.go`)
- If tool output > 30KB:
  - Save to `.evo_agent/tool-results/{toolID}.txt`
  - Return preview (first 2000 chars) + pointer
  - Prevents context bloat

---

## 5. MCP Integration (`tools/mcp.go`)

### Supported Transports
1. **stdio**: `command` + `args` (process-based)
2. **streamableHttp**: `url` (stateless HTTP POST)
3. **sse**: `url` (persistent SSE connection + POST for requests)

### Configuration
Loaded from `.evo_agent/mcp.json`:
```json
{
  "mcpServers": {
    "serverName": {
      "type": "stdio|sse|streamableHttp",
      "disabled": false,
      "timeout": 30,
      "command": "...",
      "args": ["..."],
      "url": "...",
      "headers": {...},
      "env": {...}
    }
  }
}
```

### Connection Flow
```
InitMCP()
  ├─ Read .evo_agent/mcp.json (silently ignored if missing)
  ├─ For each enabled server:
  │   ├─ Start process or connect HTTP
  │   ├─ Call "initialize" JSON-RPC
  │   ├─ Send "notifications/initialized"
  │   ├─ Fetch "tools/list"
  │   └─ Store in mcpServers map
  └─ ShutdownMCP() on exit (kill processes, close connections)
```

### Tool Naming
MCP tools are exposed as: `mcp__{serverName}__{toolName}`

Example: `mcp__filesystem__read_file`, `mcp__git__diff`

### Tool Call Flow
```
DispatchMCP(name, input)
  ├─ Parse "mcp__serverName__toolName"
  ├─ Look up client in mcpServers
  └─ client.callTool(toolName, input)
```

---

## 6. Skills System (`skills/registry.go`)

### Loading
- Scans `.evo_agent/skill/**/SKILL.md`
- Extracts YAML frontmatter: `name`, `description`
- Stores full body for injection

### Manifest Format
```yaml
---
name: "my-skill"
description: "Short description"
---
Full skill body here...
Markdown format
```

### Integration
1. `skills.Init()` loads all skills
2. `skills.Catalog()` returns formatted list for system prompt:
   ```
   Skills available:
   - my-skill: Short description
   - other-skill: Another description
   ```
3. Agent can call `load_skill("my-skill")` to get full body:
   ```xml
   <skill name="my-skill" path="/abs/path/to/SKILL.md">
   Full skill body here...
   </skill>
   ```

---

## 7. Context Management

### Auto-Compaction (`compact.go`)

**Thresholds**:
- `CONTEXT_LIMIT = 50000` chars: Trigger full LLM summarization
- `KEEP_RECENT_RESULTS = 3`: Preserve last 3 tool results

**MicroCompact** (before LLM call):
- Compresses older tool results to `"[Earlier tool result compacted...]"`
- Preserves last user message's results (never compacted)
- Lightweight, no LLM cost

**CompactHistory** (full compaction):
- Calls LLM with summarization prompt
- Saves current transcript to disk
- Tracks recent files in state
- Returns single-message history: `"This was compacted..."{summary}`

### File Tracking
- `TrackRecentFile()` maintains FIFO list of 5 most recent files
- Injected into summary for quick re-opening

---

## 8. UI/Output System (`ui/terminal.go`)

### ANSI Color Codes
```go
ColorReset   = "\033[0m"
ColorGreen   = "\033[32m"     // Thinking blocks
ColorCyan    = "\033[36m"     // Text responses
ColorBlue    = "\033[34m"     // Tool calls
ColorYellow  = "\033[33m"     // Command preview
ColorMagenta = "\033[35m"     // Debug messages
ColorRed     = "\033[31m"     // Errors
```

### Output Functions
- `PrintThinking(text)`: Green thinking blocks
- `PrintText(text)`: Cyan text responses
- `PrintToolCall(name)`: Blue tool names
- `PrintCommand(cmd)`: Yellow command previews
- `PrintError(msg)`: Red error messages

### Current Limitations
- **No TUI features**: Plain stdout/stdin
- **Single column layout**: All output sequential
- **No rich formatting**: Just ANSI colors
- **No interaction widgets**: No forms, pickers, scrollable areas

---

## 9. Dependencies

```
github.com/anthropics/anthropic-sdk-go v1.41.0    // Claude API client
github.com/invopop/jsonschema v0.13.0             // JSON schema generation
github.com/joho/godotenv v1.5.1                   // .env file loading
```

---

## 10. Architecture Highlights

### Strengths
✅ **Modular tool system**: Self-registering tools, easy to add new ones
✅ **MCP support**: Multiple transports (stdio, SSE, HTTP)
✅ **Context management**: Auto-compaction, LLM summarization, file tracking
✅ **Skill injection**: Load specialized instructions dynamically
✅ **Output persistence**: Large results saved to disk, not forced into context
✅ **Clean separation**: UI, tools, agent loop, config all decoupled

### Design Patterns
- **Registry pattern**: Tools self-register via `init()`
- **Handler interface**: All tools implement `func(json.RawMessage) (string, error)`
- **State management**: LoopState + CompactState for persistence across turns
- **Graceful degradation**: Missing config files silently ignored (MCP, skills)

---

## 11. Opportunities for TUI Layer

### Current Output Model
```
main()
  └─ Agent.Run(stdin)
      └─ Loop: PrintText() + PrintError() + tools.PrintToolList()
```

### TUI Integration Points

1. **REPL Input**
   - Replace `bufio.Scanner` with TUI input widget
   - Support multi-line input, history, completion

2. **Message Display**
   - Panel for thinking blocks
   - Panel for assistant text
   - Scrollable history

3. **Tool Monitoring**
   - Show tool calls in real-time
   - Display tool output in separate panel
   - Show execution time & status

4. **State Dashboard**
   - Context size gauge
   - Token usage (input/output)
   - Compaction status
   - File tracking list

5. **Configuration UI**
   - Show loaded MCP servers
   - Show loaded skills
   - Display model info

### Recommended TUI Framework
- **Bubble Tea** (golang.org/x/exp/cmd/cue package) or
- **tcell** + custom layout
- **fyne** for native widgets

### Non-Breaking Integration
- Wrap `Agent.Run()` with TUI event loop
- Keep tool dispatch & execution unchanged
- Redirect stdout to TUI panels
- Capture colors from terminal.go for panel theming

---

## 12. Key Files to Understand

### Must-Read (In Order)
1. `src/main.go` - Initialization & REPL start
2. `src/internal/agent/loop.go` - Agent loop, message flow
3. `src/internal/tools/tool.go` - Tool registry interface
4. `src/internal/tools/executor.go` - Tool execution
5. `src/internal/tools/mcp.go` - MCP client details
6. `src/internal/agent/compact.go` - Context management

### Reference
- `src/internal/config/config.go` - Configuration loading
- `src/internal/skills/registry.go` - Skill loading
- `src/internal/ui/terminal.go` - Current output system

---

## 13. Quick Start for TUI Development

### Build Command
```bash
cd src && go build -o evo-agent
```

### Example Tool Output Hook
```go
// In executor.go, after tool execution:
uiDispatcher.ShowToolResult(toolName, output, duration)
```

### State Access
```go
// LoopState is passed to Loop(), accessible throughout
state.CompactState.CompactCount  // Check compactions
state.Messages                   // Access history
state.TurnCount                  // Current turn
```

### Extension Pattern
```go
// New tool in src/internal/tools/my_tool.go
func init() {
    Register(ToolDef{
        Schema: ...,
        Handler: func(input json.RawMessage) (string, error) {
            // Your logic
            return result, nil
        },
    })
}
```

