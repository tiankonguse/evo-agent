# EVO-Agent Codebase: Comprehensive Architecture Overview

**Date**: May 18, 2026  
**Project**: evo-agent (Go-based AI Agent with MCP, Skills, and Tool Support)  
**Goal**: Design a TUI layer on top of existing codebase

---

## 1. PROJECT STRUCTURE & DEPENDENCIES

### Go Module
- **Module Name**: `evo-agent`
- **Go Version**: 1.26
- **Main Dependencies**:
  - `github.com/anthropics/anthropic-sdk-go` (v1.41.0) - Claude API client
  - `github.com/invopop/jsonschema` (v0.13.0) - JSON schema generation for tool input validation
  - `github.com/joho/godotenv` (v1.5.1) - Environment variable loading

### Directory Structure
```
evo-agent/
├── src/
│   ├── main.go                           # Entry point
│   ├── go.mod                            # Module definition
│   └── internal/
│       ├── agent/                        # Agent loop & state management
│       │   ├── loop.go                   # Core Agent.Loop() - multi-turn chat
│       │   ├── state.go                  # LoopState, CompactState structs
│       │   ├── compact.go                # Context compaction logic
│       │   └── transcripts.go            # Message persistence (JSONL)
│       ├── tools/                        # Tool registry & execution
│       │   ├── tool.go                   # ToolDef, Handler, registry interface
│       │   ├── executor.go               # Execute() - dispatches tool calls
│       │   ├── bash.go                   # bash tool
│       │   ├── read_file.go              # read_file tool
│       │   ├── write_file.go             # write_file tool
│       │   ├── edit_file.go              # edit_file tool
│       │   ├── mcp.go                    # MCP client & protocol (stdio, SSE, HTTP)
│       │   ├── skill.go                  # load_skill tool
│       │   ├── compact.go                # compact tool
│       │   └── persist.go                # Large output persistence
│       ├── skills/                       # Skill registry (YAML manifests)
│       │   └── registry.go               # Load & catalog skills
│       ├── config/                       # Configuration
│       │   └── config.go                 # Load .env, API keys, model ID
│       └── ui/                           # Terminal output (ANSI colors)
│           └── terminal.go               # Print* functions
├── .evo_agent/                           # Agent state directory
│   ├── mcp.json                          # MCP server configurations
│   ├── skill/                            # Skill definitions (.md files)
│   ├── tool-results/                     # Persisted large tool outputs
│   └── transcripts/                      # JSONL message history
└── .env                                  # Configuration (MODEL_ID, API_KEY)
```

---

## 2. MESSAGE FLOW & ARCHITECTURE

### High-Level Flow

```
┌──────────────────────────────────────────────────────┐
│                    main.go                           │
│  1. Load .env (MODEL_ID, ANTHROPIC_API_KEY)         │
│  2. Initialize Anthropic SDK client                 │
│  3. Load MCP servers from .evo_agent/mcp.json       │
│  4. Load skills from .evo_agent/skill/SKILL.md      │
│  5. Create Agent instance                           │
│  6. Call Agent.Run(os.Stdin) ← REPL start           │
└──────────────────────────────────────────────────────┘
                         ↓
┌──────────────────────────────────────────────────────┐
│              Agent.Run() - REPL Loop                 │
│  INPUT: Reads from bufio.Scanner(os.Stdin)          │
│  ┌────────────────────────────────────────────────┐ │
│  │ for each user input (until "q" or "exit"):     │ │
│  │  1. Append user message to history[]           │ │
│  │  2. Call Agent.Loop(state)                     │ │
│  │  3. Extract & print final assistant response   │ │
│  └────────────────────────────────────────────────┘ │
│  OUTPUT: fmt.Println() direct to stdout             │
└──────────────────────────────────────────────────────┘
                         ↓
┌──────────────────────────────────────────────────────┐
│        Agent.Loop() - Multi-turn Tool Loop           │
│  Input: LoopState{Messages, TurnCount, CompactState}│
│                                                      │
│  REPEAT until stop_reason != "tool_use":            │
│   1. autoCompact(state)                             │
│      ├─ MicroCompact: compress old tool results    │
│      └─ Check context size, trigger full compact   │
│   2. client.Messages.New()                          │
│      ├─ Send messages + tools to Claude API        │
│      ├─ Print DEBUG info (tokens, model, reason)   │
│      └─ Append response to state.Messages           │
│   3. manualCompact(state, response.Content)         │
│      └─ Check for "compact" tool call              │
│   4. tools.Execute(response.Content, compactState)  │
│      ├─ Iterate over content blocks                │
│      ├─ Print thinking/text/tool calls via ui.*    │
│      ├─ Dispatch tool calls → ToolDef.Handler()    │
│      └─ Return []ToolResultBlockParam               │
│   5. Append tool results as new user message        │
│   6. TurnCount++                                    │
│                                                      │
│  RETURN: false if no tool calls, true if continuing│
└──────────────────────────────────────────────────────┘
                         ↓
┌──────────────────────────────────────────────────────┐
│         tools.Execute() - Tool Dispatcher            │
│  Input: response.Content (ContentBlockUnion[])       │
│  Output: []ToolResultBlockParam                      │
│                                                      │
│  FOR each content block:                            │
│   ├─ ThinkingBlock → ui.PrintThinking()            │
│   ├─ TextBlock → ui.PrintText()                    │
│   └─ ToolUseBlock:                                 │
│      ├─ ui.PrintToolCall(name)                    │
│      ├─ ui.PrintCommand(formatted call)           │
│      ├─ Dispatch via tools.Dispatch(name, input)  │
│      │  ├─ if mcp__* → DispatchMCP()             │
│      │  └─ else → registry[name].Handler()       │
│      ├─ persistLargeOutput() if output > 30KB    │
│      └─ Append ToolResultBlock to results        │
└──────────────────────────────────────────────────────┘
```

### Message History Structure

Each turn accumulates messages:
```
history = [
  {Role: "user", Content: [{OfText: {Text: "user query"}}]},
  {Role: "assistant", Content: [
    {OfThinking: {Thinking: "..."}},
    {OfText: {Text: "response text"}},
    {OfToolUse: {ID: "t1", Name: "bash", Input: {...}}}
  ]},
  {Role: "user", Content: [{OfToolResult: {ID: "t1", Content: [{OfText: "output"}}]}}
]
```

---

## 3. TOOL SYSTEM

### Tool Registry Pattern

All tools use **self-registering init functions**:

```go
type ToolDef struct {
    Schema  anthropic.ToolParam    // Anthropic API schema
    Handler Handler               // func(json.RawMessage) (string, error)
}

type Handler func(input json.RawMessage) (string, error)

var registry = map[string]ToolDef{}

func Register(def ToolDef) {
    registry[def.Schema.Name] = def
}

func Dispatch(name string, input json.RawMessage) (string, error) {
    if strings.HasPrefix(name, "mcp__") {
        return DispatchMCP(name, input)
    }
    if d, ok := registry[name]; ok {
        return d.Handler(input)
    }
    return "", nil
}
```

### Built-in Tools (6)

| Tool | Input | Output |
|------|-------|--------|
| `bash` | `{command: string}` | stdout/stderr combined (max 50KB) |
| `read_file` | `{path: string, limit?: int}` | File contents (max 50KB) |
| `write_file` | `{path: string, content: string}` | "Wrote N bytes to path" |
| `edit_file` | `{path: string, old_str: string, new_str: string}` | "Edited path" |
| `load_skill` | `{name: string}` | Full skill body text |
| `compact` | `{focus?: string}` | Summarization result |

### Input Schema Generation

Uses reflection with `jsonschema` package:
```go
func GenerateSchema[T any]() anthropic.ToolInputSchemaParam {
    // Reflects struct tags: `jsonschema_description:"..."`
    // Returns {Properties, Required} for Anthropic API
}
```

### Output Handling

- **Small outputs** (≤30KB): Returned in-memory
- **Large outputs** (>30KB): 
  - Persisted to `.evo_agent/tool-results/{toolID}.txt`
  - Returns placeholder with file path + 2KB preview
  - Model can re-run tool if full detail needed

---

## 4. MCP (MODEL CONTEXT PROTOCOL) INTEGRATION

### MCP Configuration

Loaded from `.evo_agent/mcp.json`:

```json
{
  "mcpServers": {
    "server_name": {
      "type": "stdio|sse|streamableHttp",
      "disabled": false,
      "timeout": 30,
      "description": "...",
      "command": "node mcp-server.js",    // stdio only
      "args": ["--option"],                // stdio only
      "env": {"VAR": "value"},             // stdio only
      "url": "https://...",                // sse/streamableHttp only
      "headers": {"Auth": "..."}           // sse/streamableHttp only
    }
  }
}
```

### Transport Types

1. **stdio** (process-based):
   - Start subprocess
   - Send/receive JSON-RPC over stdin/stdout
   - Synchronous line-by-line protocol

2. **streamableHttp** (stateless):
   - Each request is independent HTTP POST
   - Response: JSON or SSE stream
   - Simple, no connection state

3. **SSE** (persistent streaming):
   - Persistent GET for incoming SSE stream
   - Background goroutine reads responses
   - Separate POST requests for RPC calls
   - Maps request IDs to response channels

### Tool Naming

MCP tools are exposed as: `mcp__{server_name}__{tool_name}`

Example: `mcp__filesystem__read_file` → calls `read_file` on `filesystem` server

### Initialization Flow

```
InitMCP() calls:
  1. Read .evo_agent/mcp.json
  2. For each enabled server:
     a. Connect via transport (stdio/SSE/HTTP)
     b. Send initialize RPC with protocol version "2024-11-05"
     c. Send tools/list RPC
     d. Store client in mcpServers map
     e. Print "[MCP] Connected to 'name' (N tools)"
  3. MCPTools() builds Anthropic schemas for all MCP tools
  4. tools.Execute() routes mcp__ prefixes to DispatchMCP()
  5. ShutdownMCP() stops all transports on exit
```

---

## 5. SKILLS SYSTEM

### Skill Discovery & Loading

Loaded from `.evo_agent/skill/*/SKILL.md`:

```
.evo_agent/skill/
├── my-skill/
│   └── SKILL.md              ← Parsed for frontmatter + body
└── another-skill/
    └── SKILL.md
```

### Skill Document Format

```yaml
---
name: "my-skill"
description: "Brief description"
---

# Full skill instructions in markdown
...detailed task-specific prompts...
```

### Skill Registry API

```go
func Init() {
    // Walk .evo_agent/skill/**/SKILL.md
    // Parse frontmatter (YAML) + body
    // Store in documents map
}

func Catalog() string {
    // Returns formatted list for system prompt
    // "Available skills: my-skill, another-skill, ..."
}

func Load(name string) string {
    // Returns full skill body text
    // Called by load_skill tool
}
```

### Integration in System Prompt

In `main.go`:
```go
cfg.SystemMsg += "\nSkills available:\n" + skills.Catalog() +
    "\nUse load_skill when a task needs specialized instructions before you act."
```

---

## 6. CONTEXT COMPACTION

### Three-Level Strategy

#### Level 1: MicroCompact (Automatic)
- Triggered before every LLM API call
- Keeps recent N (=3) tool results in full
- Replaces older results with placeholder: `"[Earlier tool result compacted. Re-run the tool if you need full detail.]"`
- Does NOT require LLM call
- Runs in ~1ms

#### Level 2: Auto Compact (Automatic)
- Triggered if context size > 50,000 chars after MicroCompact
- Calls LLM to summarize full conversation
- Replaces history with compressed version
- Prints: `"[auto compact triggered: N chars]"`

#### Level 3: Manual Compact (On-Demand)
- Model calls `compact` tool with optional `focus` hint
- Summarizes conversation with focus area if provided
- Replaces history with result

### Compaction State

```go
type CompactState struct {
    HasCompacted bool      // Whether any compaction occurred
    LastSummary  string    // The last generated summary
    RecentFiles  []string  // Recently accessed file paths (FIFO, max 5)
    CompactCount int       // Count of compression operations
}
```

### Constants

```go
const (
    CONTEXT_LIMIT        = 50000      // Auto-compact threshold
    KEEP_RECENT_RESULTS  = 3          // Recent tool results to keep
    maxConversationBytes = 80000      // Max for summarization LLM
    persistThreshold     = 30000      // Large output threshold
)
```

---

## 7. TRANSCRIPT & PERSISTENCE

### Message History Persistence

**File**: `.evo_agent/transcripts/transcript_{timestamp}.jsonl`

```go
func WriteTranscript(messages []anthropic.MessageParam) {
    // Writes one message per line (JSONL format)
    // Filename: transcript_1716057600.jsonl
    // Each line: {complete message JSON}
}

func LoadTranscript(path string) ([]anthropic.MessageParam, error) {
    // Reads JSONL file
    // Parses each line as anthropic.MessageParam
}
```

**Use Case**: Save session history for debugging/audit

### Large Output Persistence

**Directory**: `.evo_agent/tool-results/`

- Tool results > 30KB saved to disk
- File named: `{toolUseID}.txt`
- Model receives preview + file path reference
- Prevents context explosion from large outputs

---

## 8. TERMINAL UI & OUTPUT

### Current Output Rendering

`src/internal/ui/terminal.go` provides ANSI color-coded printing:

```go
// ANSI color constants
ColorReset   = "\033[0m"
ColorGreen   = "\033[32m"    // Thinking
ColorCyan    = "\033[36m"    // Text output
ColorBlue    = "\033[34m"    // Tool calls
ColorYellow  = "\033[33m"    // Commands
ColorMagenta = "\033[35m"    // Debug/metadata
ColorRed     = "\033[31m"    // Errors

// Output functions
PrintThinking(text string)         // Green: "THINKING: ..."
PrintText(text string)             // Cyan: "TEXT: ..."
PrintToolCall(name string)         // Blue: "DEBUG: Tool called: ..."
PrintCommand(cmd string)           // Yellow: "$ ..."
PrintError(msg string)             // Red: error message
```

### Current Flow

```
stdout:
  >> user input prompt
  THINKING: model reasoning
  TEXT: assistant response
  DEBUG: Tool called: bash
  $ ls -la
  (tool output)
  [auto compact triggered: 12345 chars]
  DEBUG: Model used: claude-3-5-sonnet, Tokens: 1000, stop_reason: tool_use
```

---

## 9. CONFIGURATION & STARTUP

### Environment Variables (from .env)

```env
MODEL_ID=claude-3-5-sonnet-20241022
ANTHROPIC_API_KEY=sk-ant-...
ANTHROPIC_BASE_URL=https://api.anthropic.com/v1  # Optional
```

### Config Struct

```go
type Config struct {
    ModelID   string  // LLM model identifier
    APIKey    string  // Anthropic API key
    BaseURL   string  // Optional custom endpoint
    SystemMsg string  // System prompt (injected with skills catalog)
}
```

### Startup Sequence

```go
func main() {
    1. config.LoadEnv()           // Read .env files
    2. cfg := config.Load()        // Parse environment
    3. Check cfg.ModelID set       // Fail if not set
    4. tools.InitMCP()             // Connect MCP servers
    5. defer tools.ShutdownMCP()   // Cleanup on exit
    6. skills.Init()               // Load skill manifests
    7. Inject skills.Catalog() into cfg.SystemMsg
    8. tools.PrintToolList()       // Debug: print all available tools
    9. Create Anthropic SDK client with opts
    10. a := agent.New(client, cfg)
    11. a.Run(os.Stdin)            // Start REPL
}
```

---

## 10. KEY EVENTS/CALLBACKS FOR TUI INTEGRATION

### Events Emitted During Agent.Loop()

1. **Before LLM Call**: `autoCompact()` → context size check
2. **LLM Call**: Model request with tokens, model name
3. **Tool Dispatch**: Each tool name + input parameters
4. **Tool Execution**: Tool output + errors
5. **Compaction**: Auto/manual compaction triggered
6. **Turn Completion**: `TurnCount` incremented

### Output Points (Interception Opportunities for TUI)

| Function | Called By | Output Type | TUI Destination |
|----------|-----------|------------|-----------------|
| `PrintThinking()` | `tools.Execute()` | Thinking text | History panel |
| `PrintText()` | `tools.Execute()` | Assistant text | History panel |
| `PrintToolCall()` | `tools.Execute()` | Tool name | Tool status |
| `PrintCommand()` | `tools.Execute()` | Tool input | History panel |
| `PrintError()` | `tools.Execute()` / `Agent.Loop()` | Error message | Status/Error panel |
| `fmt.Printf()` in Loop | `Agent.Loop()` | DEBUG info, tokens | Status panel |

---

## 11. DATA STRUCTURES FOR TUI INTEGRATION

### Current Message Structure (Anthropic SDK)

```go
type MessageParam struct {
    Role     string                                  // "user" | "assistant"
    Content  []ContentBlockParamUnion               // Text, ToolUse, ToolResult
}

type ContentBlockParamUnion struct {
    OfText       *TextBlockParam
    OfToolUse    *ToolUseBlockParam
    OfToolResult *ToolResultBlockParam
}

type TextBlockParam struct {
    Text string
}

type ToolUseBlockParam struct {
    ID    string                 // Unique ID for this tool call
    Name  string                 // Tool name (bash, read_file, etc.)
    Input json.RawMessage        // Tool input JSON
}

type ToolResultBlockParam struct {
    ID      string                // References ToolUseBlockParam.ID
    Content []ContentBlockParamUnion  // Text results
    IsError bool
}
```

### LoopState (Mutable State)

```go
type LoopState struct {
    Messages         []anthropic.MessageParam   // Full conversation history
    TurnCount        int                       // Number of tool call turns
    TransitionReason string                    // "tool_result" | ""
    CompactState     *CompactState             // Compaction tracking
}
```

---

## 12. SUGGESTED TUI INTEGRATION POINTS

### Minimal Integration (No Code Changes Required)

1. **Redirect stdout/stderr** → capture ANSI output
2. **Route to Bubble Tea panels** → render in TUI layout
3. **Buffer input** → read from TUI instead of os.Stdin
4. **Create event channel** → broadcast tool calls/results to TUI

### Example TUI Panel Events

```go
type TUIEvent struct {
    Type      string    // "thinking", "text", "tool_call", "tool_result", "error", "debug"
    Timestamp time.Time
    Content   string
    ToolName  string    // For tool_call/tool_result
    ToolID    string    // For tool_result
}
```

### Recommended TUI Layout

```
┌─────────────────────────────────────────────────────┐
│ EVO-AGENT TUI                                       │
├─────────────────────────────────────────────────────┤
│                                                     │
│  ┌─────────────────────┐  ┌───────────────────┐   │
│  │   Message History   │  │   Tool Status     │   │
│  │   [thinking]        │  │   Current: bash   │   │
│  │   [response]        │  │   Tokens: 1000    │   │
│  │   [tool: bash]      │  │   Context: 45KB   │   │
│  │   [output preview]  │  │   Turn: 3         │   │
│  │                     │  │                   │   │
│  └─────────────────────┘  └───────────────────┘   │
├─────────────────────────────────────────────────────┤
│ Input: >> [typing here...]                         │
├─────────────────────────────────────────────────────┤
│ Status: Processing... (turn 3/5) | Ctrl+C to quit  │
└─────────────────────────────────────────────────────┘
```

---

## 13. IMPORTANT PATTERNS & GOTCHAS

### Pattern: Self-Registering Tools
- Each tool `init()` calls `tools.Register(ToolDef)`
- No central registry needed
- Adding a tool: create new file, implement init()

### Pattern: CompactState Threading
- Passed as `interface{}` to tools.Execute() to avoid circular imports
- Cast to `*CompactState` inside tool handlers if needed
- Persists across turns within Agent.Run()

### Gotcha: MicroCompact Preserves Recent
- MicroCompact ALWAYS preserves all results in last tool-result user message
- Older results compressed to placeholders
- This prevents losing current turn's data before model sees it

### Gotcha: Tool Output Truncation
- `read_file` / `bash` max 50KB returned
- Large outputs persisted to disk
- Model informed via placeholder with file path

### Gotcha: MCP JSON-RPC Protocol
- Requires JSONL format (one request per line)
- Sync call-response (ID tracking)
- SSE transport needs background reader goroutine
- All transports timeout after 30s (configurable)

---

## 14. CRITICAL FILES FOR TUI DESIGNER

| File | Lines | Key Concepts |
|------|-------|--------------|
| `src/main.go` | 50 | Startup flow, config, client init |
| `src/internal/agent/loop.go` | 193 | Multi-turn loop, tool dispatch |
| `src/internal/tools/executor.go` | 54 | Output routing, tool execution |
| `src/internal/tools/tool.go` | 68 | Tool registry interface |
| `src/internal/ui/terminal.go` | 35 | Current ANSI output functions |
| `src/internal/tools/mcp.go` | 703 | MCP client (complex!) |
| `src/internal/agent/compact.go` | 250+ | Compaction logic |
| `src/internal/agent/state.go` | 20 | State structures |

---

## 15. RECOMMENDED TUI TECH STACK

- **Bubble Tea** (charmbracelet): High-level TUI framework
  - Event-driven model (tea.Model, tea.Cmd)
  - Composable components (text input, viewport, etc.)
  - ANSI color support
  
- **tcell** (lower level): Direct terminal control
  - More control over rendering
  - Useful for custom layouts

- **lipgloss**: Styling library (pairs well with Bubble Tea)

---

## 16. NEXT STEPS FOR TUI IMPLEMENTATION

1. **Create `src/internal/tui/` package** with:
   - `model.go` - Bubble Tea Model struct
   - `events.go` - Event channel types
   - `output.go` - Output redirection
   - `layout.go` - Rendering logic

2. **Modify `main.go`** to:
   - Add `--tui` flag
   - Redirect stdout to TUI event channel
   - Pass TUI input channel to Agent.Run()

3. **Hook into `tools.Execute()`** via:
   - Global event channel to broadcast events
   - TUI subscribes and renders

4. **Implement message history panel** with:
   - Scrollable view
   - Color-coded by type (thinking/text/tool/error)
   - Timestamps

5. **Implement tool status panel** with:
   - Current tool name & execution time
   - Token usage
   - Context size meter
   - Turn counter

6. **Implement input panel** with:
   - Text input widget
   - Command history (up/down)
   - Syntax highlighting (optional)

---

## CONCLUSION

The evo-agent codebase is **highly modular and TUI-friendly**:

✅ **No breaking changes needed** - stdout/stderr can be intercepted  
✅ **Clear event flow** - tools.Execute() outputs are predictable  
✅ **Composable tools** - Tool registry makes adding/monitoring easy  
✅ **Rich state** - LoopState provides all needed information  
✅ **Production-ready** - MCP, skills, compaction all working  

A TUI layer can be added as a thin wrapper around Agent.Run() with minimal changes to core logic.

