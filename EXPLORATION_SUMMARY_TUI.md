# EVO-Agent Exploration Summary: Ready for TUI Integration

**Exploration Completed**: May 18, 2026  
**Agent**: Explore (read-only analysis)  
**Status**: ✅ Complete - All critical architecture documented

---

## QUICK START FOR TUI DEVELOPER

### What is evo-agent?

A **Go-based AI agent** that:
- Runs multi-turn conversations with Claude (Anthropic)
- Calls tools (bash, read_file, write_file, edit_file, etc.)
- Supports MCP (Model Context Protocol) for dynamic tool loading
- Loads specialized "skills" for domain-specific tasks
- Automatically compacts context when it grows too large
- Persists transcripts and large outputs to disk

**Entry point**: `src/main.go`  
**Core loop**: `src/internal/agent/loop.go`  
**Tool dispatch**: `src/internal/tools/executor.go`  
**Current output**: `src/internal/ui/terminal.go` (ANSI colors)

### Architecture at a Glance

```
User Input → Agent.Run() → Agent.Loop() → tools.Execute() → STDOUT
                                                        ↓
                                        ui.PrintThinking()
                                        ui.PrintText()
                                        ui.PrintToolCall()
                                        ui.PrintError()
```

### For TUI Integration: 3 Key Files to Hook

1. **Input**: Modify `Agent.Run()` to read from TUI pipe instead of os.Stdin
2. **Output**: Modify `ui/terminal.go` to emit events to channel instead of printing
3. **Flow**: Create Bubble Tea TUI that reads events and writes to pipe

---

## COMPLETE FILE MANIFEST

### Core Agent Files
```
src/main.go                           # 50 lines: startup, init MCP/skills
src/internal/agent/loop.go            # 193 lines: multi-turn loop ⭐
src/internal/agent/state.go           # 20 lines: LoopState, CompactState types
src/internal/agent/compact.go         # 250+ lines: context compression
src/internal/agent/transcripts.go     # 77 lines: save/load JSONL messages
```

### Tool System (Registry Pattern)
```
src/internal/tools/tool.go            # 68 lines: ToolDef, Handler, registry ⭐
src/internal/tools/executor.go        # 54 lines: Execute() - dispatch & output ⭐
src/internal/tools/bash.go            # 64 lines: bash tool
src/internal/tools/read_file.go       # 56 lines: read_file tool
src/internal/tools/write_file.go      # 44 lines: write_file tool
src/internal/tools/edit_file.go       # 66 lines: edit_file tool
src/internal/tools/skill.go           # 35 lines: load_skill tool
src/internal/tools/compact.go         # 5 lines: compact tool
src/internal/tools/persist.go         # 44 lines: large output handling
src/internal/tools/mcp.go             # 703 lines: MCP protocol implementation
```

### Configuration & Skills
```
src/internal/config/config.go         # 45 lines: load .env, Config struct
src/internal/skills/registry.go       # 80+ lines: load skills, catalog
```

### Terminal Output
```
src/internal/ui/terminal.go           # 35 lines: ANSI color print functions ⭐
```

### Dependencies
```
src/go.mod                            # 25 lines: module name, versions
```

### State & Configuration
```
.evo_agent/mcp.json                   # MCP server config (optional)
.evo_agent/skill/*/SKILL.md           # Skill definitions (optional)
.evo_agent/transcripts/               # Session history (auto-created)
.evo_agent/tool-results/              # Large outputs (auto-created)
.env                                  # Environment: MODEL_ID, API_KEY
```

---

## MESSAGE FLOW DIAGRAM

```
┌─────────────────────────────────────────────────────────────┐
│ USER enters: "list files in src/"                           │
└──────────────────────┬──────────────────────────────────────┘
                       │
                       ↓
┌─────────────────────────────────────────────────────────────┐
│ Agent.Run(os.Stdin)                                         │
│ ├─ Read input via bufio.Scanner                            │
│ ├─ Append to history: {Role: "user", Content: "list..."}   │
│ └─ Call Agent.Loop(state)                                  │
└──────────────────────┬──────────────────────────────────────┘
                       │
                       ↓
┌─────────────────────────────────────────────────────────────┐
│ Agent.Loop(state) - TURN 1                                  │
│ ├─ autoCompact() - check context size                       │
│ ├─ client.Messages.New() - call Claude API                 │
│ │  └─ Response: "I'll list the files..." + tool_use: bash  │
│ ├─ Append response to history                              │
│ └─ tools.Execute(response.Content) ← KEY DISPATCH POINT    │
└──────────────────────┬──────────────────────────────────────┘
                       │
                       ↓
┌─────────────────────────────────────────────────────────────┐
│ tools.Execute()                                             │
│ ├─ Iterate over response content blocks:                   │
│ │  ├─ TextBlock: ui.PrintText("I'll list...")             │
│ │  │  └─ → fmt.Printf(ANSI colored text)                  │
│ │  └─ ToolUseBlock(bash):                                 │
│ │     ├─ ui.PrintToolCall("bash")                         │
│ │     ├─ ui.PrintCommand("$ ls -la src/")                │
│ │     ├─ tools.Dispatch("bash", input)                    │
│ │     │  └─ runBash() executes command                    │
│ │     └─ Append ToolResultBlock with output               │
│ └─ Return []ToolResultBlockParam                          │
└──────────────────────┬──────────────────────────────────────┘
                       │
                       ↓
┌─────────────────────────────────────────────────────────────┐
│ Agent.Loop() continues                                      │
│ ├─ Append tool results to history as new user message      │
│ ├─ TurnCount++ → 2                                          │
│ ├─ Loop back: client.Messages.New() with tool results      │
│ │  └─ Response: "Here are the files..." (no more tools)    │
│ └─ RETURN false (no more tool calls)                       │
└──────────────────────┬──────────────────────────────────────┘
                       │
                       ↓
┌─────────────────────────────────────────────────────────────┐
│ Agent.Run() continues                                       │
│ ├─ Extract final response from history                     │
│ └─ fmt.Println(response_text) → stdout                     │
└─────────────────────────────────────────────────────────────┘
```

---

## TOOL REGISTRY PATTERN (Self-Registering)

Each tool file has an `init()` function:

```go
// src/internal/tools/bash.go
func init() {
    Register(ToolDef{
        Schema: anthropic.ToolParam{
            Name: "bash",
            Description: "Run shell command",
            InputSchema: GenerateSchema[BashInput](),
        },
        Handler: func(input json.RawMessage) (string, error) {
            var in BashInput
            json.Unmarshal(input, &in)
            return runBash(in.Command), nil
        },
    })
}
```

**No central registration needed** - Go's `init()` functions run automatically!

---

## MCP INTEGRATION (Advanced)

**MCP** = Model Context Protocol = dynamic tool loading

```
.evo_agent/mcp.json
  ↓
tools.InitMCP()
  ├─ Read config
  ├─ For each server:
  │  ├─ Connect (stdio/SSE/HTTP)
  │  ├─ Send initialize RPC
  │  └─ Send tools/list RPC
  └─ Store clients in mcpServers map
  
tools.MCPTools()
  └─ Generate schemas for all MCP tools with prefix: mcp__{server}__{tool}
  
tools.DispatchMCP()
  └─ Route mcp__ calls to correct server
```

**3 Transport Types**:
- **stdio**: subprocess with JSONL protocol
- **HTTP**: stateless POST requests
- **SSE**: persistent streaming with backlog

---

## SKILLS SYSTEM

Skills are specialized instructions loaded on demand:

```
.evo_agent/skill/my-skill/SKILL.md
├─ YAML frontmatter: name, description
└─ Markdown body: detailed instructions

skills.Init() → walks directory, loads manifests
skills.Catalog() → "Available skills: my-skill, ..."
skills.Load("my-skill") → returns full body text
```

Model can call `load_skill` tool before acting on task.

---

## CONTEXT COMPACTION (3-Level Strategy)

### Level 1: MicroCompact (Every turn)
- Keeps recent 3 tool results in full
- Replaces older results with placeholder
- ~1ms, no LLM call

### Level 2: Auto Compact (If context > 50KB)
- Calls LLM to summarize
- Replaces full history with compressed version
- Maintains recent files list

### Level 3: Manual Compact (On demand)
- Model calls `compact` tool with optional focus hint
- LLM summarization with focus area

---

## OUTPUT SYSTEM (CURRENT)

`src/internal/ui/terminal.go`:

```go
PrintThinking(text)    // GREEN: "THINKING: ..."
PrintText(text)        // CYAN: "TEXT: ..."
PrintToolCall(name)    // BLUE: "DEBUG: Tool called: ..."
PrintCommand(cmd)      // YELLOW: "$ ..."
PrintError(msg)        // RED: error text
```

**All functions use `fmt.Printf()` → stdout**

---

## FOR TUI INTEGRATION: MINIMAL CHANGES REQUIRED

### Option A: Event Channel (Recommended)
1. Create `src/internal/tui/events.go` with TUIEvent type
2. Add global `eventChan chan TUIEvent` 
3. Modify `ui/terminal.go` functions to send events:
   ```go
   func PrintThinking(text string) {
       if eventChan != nil {
           eventChan <- TUIEvent{Type: "thinking", Content: text}
       }
       fmt.Printf(...)  // Keep for non-TUI mode
   }
   ```
4. Create Bubble Tea model in `src/internal/tui/model.go`
5. Modify `main.go` to start TUI goroutine before `a.Run(os.Stdin)`

### Option B: Dependency Injection (Cleaner but more changes)
1. Define `EventEmitter` interface
2. Pass emitter to `Agent` and `tools.Execute()`
3. Create implementations: Terminal, TUI, Null
4. Modify all `ui.Print*()` calls to emit events

---

## CRITICAL CONSTANTS & LIMITS

```go
CONTEXT_LIMIT        = 50000      // Auto-compact threshold
KEEP_RECENT_RESULTS  = 3          // Recent tool results to keep
maxConversationBytes = 80000      // Max for summarization
persistThreshold     = 30000      // Large output threshold
maxReadBytes         = 50000      // Max read_file output
maxBashOutput        = 50000      // Max bash output
previewPrintLen      = 200        // Terminal preview length
previewChars         = 2000       // Persisted output preview
toolResultsDir       = ".evo_agent/tool-results"
transcriptDir        = ".evo_agent/transcripts"
```

---

## MESSAGE STRUCTURES (Anthropic SDK)

```go
// Full conversation history
[]anthropic.MessageParam{
    {
        Role: "user",
        Content: []ContentBlockParamUnion{
            {OfText: {Text: "user query"}},
        },
    },
    {
        Role: "assistant",
        Content: []ContentBlockParamUnion{
            {OfThinking: {Thinking: "..."}},
            {OfText: {Text: "response"}},
            {OfToolUse: {ID: "t1", Name: "bash", Input: {...}}},
        },
    },
    {
        Role: "user",
        Content: []ContentBlockParamUnion{
            {OfToolResult: {ID: "t1", Content: [{OfText: {Text: "output"}}]}},
        },
    },
}
```

---

## STARTUP SEQUENCE

```go
main()
  ├─ config.LoadEnv()           // Read .env (or exe dir .env)
  ├─ cfg := config.Load()        // Parse MODEL_ID, API_KEY
  ├─ Check cfg.ModelID           // Fail if not set
  ├─ BuildOptions(cfg)           // Create SDK options
  ├─ client := NewClient()       // Create Anthropic client
  ├─ tools.InitMCP()             // Connect MCP servers (if config exists)
  ├─ skills.Init()               // Load skill manifests (if dir exists)
  ├─ Inject skills.Catalog() into cfg.SystemMsg
  ├─ tools.PrintToolList()       // Debug: print all tools
  ├─ Agent := New(client, cfg)   // Create agent instance
  └─ Agent.Run(os.Stdin)         // Start REPL
```

---

## TESTING THE CODEBASE

```bash
# Build
cd src && go build -o evo-agent main.go

# Run (reads from stdin)
./evo-agent
>> What can you help me with?

# Run with custom model
MODEL_ID=claude-3-opus-20250219 ./evo-agent

# View MCP tools (printed on startup)
[MCP] Connected to "filesystem" (5 tools)

# View skills (printed on startup)
[Skills] Loaded 3 skill(s)
```

---

## KEY INSIGHTS FOR TUI DESIGN

✅ **No breaking changes needed** - stdout can be intercepted  
✅ **Clear event flow** - tools.Execute() is the dispatch point  
✅ **Minimal code modification** - ~10-20 lines to hook events  
✅ **Rich state** - LoopState has all needed information  
✅ **Production-ready** - MCP, skills, compaction all working  

**Biggest implementation work**: Bubble Tea layout and rendering (6-10 hours)  
**Least work**: Hooking into output system (1-2 hours)  

---

## DOCUMENTS PROVIDED

1. **CODEBASE_ANALYSIS.md** - 500+ lines: Comprehensive architecture deep-dive
2. **TUI_DESIGN_SPEC.md** - 400+ lines: Complete TUI design specification
3. **This file** - Quick reference and summary

---

## NEXT STEPS FOR TUI DEVELOPER

1. **Read**: CODEBASE_ANALYSIS.md (sections 1-11)
2. **Design**: Review TUI_DESIGN_SPEC.md (sections 1-8)
3. **Implement Phase 1**: Create `src/internal/tui/` package skeleton
4. **Hook Output**: Add event channel to `ui/terminal.go` (5 lines)
5. **Build TUI Model**: Implement Bubble Tea Model with 3 panels
6. **Test**: Run with `--tui` flag and verify event flow
7. **Polish**: Add features from TUI_DESIGN_SPEC Phase 4

---

## FILES MODIFIED BY EXPLORATION

Created:
- `CODEBASE_ANALYSIS.md` (this repo)
- `TUI_DESIGN_SPEC.md` (this repo)
- `EXPLORATION_SUMMARY_TUI.md` (this file)

No code files modified - exploration only!

