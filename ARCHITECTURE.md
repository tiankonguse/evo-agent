# System Architecture

## Overview

Evo-Agent is structured as a **ReAct (Reason + Act)** loop. The agent does not simply generate text — it interacts with the host operating system to gather information and make changes, then feeds observations back to the model until no more tool calls are requested.

## Components

### 1. Orchestrator (`internal/agent`)

The `Agent` struct manages multi-turn conversations.

- **`LoopState`** — mutable state for one run:
  - `Messages []anthropic.MessageParam` — full conversation history
  - `TurnCount int` — number of tool-call turns so far
  - `TransitionReason string` — why the loop continued (`"tool_result"`) or stopped (`""`)
  - `CompactState *CompactState` — context compaction state (shared across REPL turns)
- **`CompactState`** — tracks compaction across the session:
  - `HasCompacted bool` — whether compaction has occurred at least once
  - `LastSummary string` — text of the most recent generated summary
  - `RecentFiles []string` — up to 5 most recently read files (FIFO)
  - `CompactCount int` — total number of compactions performed
- **`RunOneTurn`** — sends history to the LLM, appends the response, executes tool calls, returns `true` if another turn is needed
- **`Loop`** — drives `RunOneTurn` in a `for` loop until it returns `false`; runs `MicroCompact` and `CompactHistory` before each LLM call
- **`Run`** — top-level REPL: reads user input, maintains persistent `history` and `CompactState`, calls `Loop`

### 2. Context Compaction (`internal/agent/compact.go`, `transcripts.go`)

A three-layer strategy to keep context size manageable across long sessions.

| Layer | Trigger | Mechanism | Cost |
|-------|---------|-----------|------|
| **MicroCompact** | Every LLM call | Replace older tool results with a one-line placeholder; keep the most recent `KEEP_RECENT_RESULTS` (3) intact | < 1 ms, no network |
| **CompactHistory** | Context > `CONTEXT_LIMIT` (50 000 chars) after MicroCompact | Write full transcript to disk, call LLM to generate a summary, replace all messages with one summary message | 1–5 s, one LLM call |
| **compact tool** | Model calls `compact` tool | Same as CompactHistory; model may supply a `focus` hint that is appended to the summary | 1–5 s, one LLM call |

Key functions:
- **`MicroCompact(messages, keepCount)`** — in-place placeholder replacement
- **`CompactHistory(client, model, messages, state, focus)`** — full summarisation + transcript write
- **`SummarizeHistory(client, model, messages)`** — calls LLM to produce a structured summary
- **`TrackRecentFile(state, path)`** — maintains `CompactState.RecentFiles` FIFO list
- **`WriteTranscript(messages)`** — saves JSONL snapshot to `.evo_agent/transcripts/<timestamp>.jsonl`

### 3. Tool Engine (`internal/tools`)

A self-registering, table-driven tool system.

- **`ToolDef`** — bundles an `anthropic.ToolParam` schema with a `Handler` func
- **`registry`** — a `map[string]ToolDef` populated at startup via `init()` functions
- **`Register(def ToolDef)`** — called from each tool file's `init()`; no central list needed
- **`Tools()`** — returns all registered schemas plus MCP tool schemas as `[]anthropic.ToolUnionParam` for the API call
- **`Dispatch(name, input)`** — routes `mcp__*` names to `DispatchMCP`; otherwise looks up the handler in the registry
- **`GenerateSchema[T]()`** — reflects a Go struct into `anthropic.ToolInputSchemaParam` using `invopop/jsonschema`
- **`Execute(content)`** — iterates API response content blocks, prints output, calls `Dispatch` for each `ToolUseBlock`, returns `[]ContentBlockParamUnion` tool results

#### Registered Tools

| Tool         | File           | What it does                                               |
|--------------|----------------|------------------------------------------------------------|
| `bash`       | `bash.go`      | Runs a shell command via `bash -c`, 120 s timeout, 50 KB cap |
| `read_file`  | `read_file.go` | Reads a file; optional `limit` truncates to N lines        |
| `write_file` | `write_file.go`| Writes content to a path, creating parent directories      |
| `edit_file`  | `edit_file.go` | Replaces the first exact occurrence of `old_str` with `new_str`; creates file if `old_str` is empty |
| `compact`    | `compact.go`   | Model-initiated context compaction; optional `focus` hint preserved in summary |
| `load_skill` | `skill.go`     | Loads the full body of a named skill; returns XML-wrapped content with the skill's file path |

### 4. Skill System (`internal/skills`)

A two-layer on-demand knowledge model that keeps the system prompt small.

- **`SkillManifest`** — lightweight metadata kept in memory: `Name`, `Description`
- **`skillDocument`** — full document: `Manifest`, `Body` (trimmed), `Path` (absolute path to `SKILL.md`)
- **`Init()`** — walks `.evo_agent/skill/**/SKILL.md` at startup; parses YAML frontmatter; populates the package-level `documents` map; missing directory silently ignored
- **`Catalog() string`** — returns a `"- name: description\n"` list for every loaded skill, sorted by name; returns `""` when no skills are loaded
- **`Load(name) string`** — returns `<skill name=… path=…>\nbody\n</skill>`; returns a human-readable error with available names when the skill is unknown
- **`parseFrontmatter(text)`** — extracts YAML front matter delimited by `---` using a compiled regexp; key–value pairs separated by `:` on each line

**Skill file layout:**

```
.evo_agent/skill/
└── <skill-name>/
    └── SKILL.md      # frontmatter (name, description) + body
```

**Import direction:** `main.go` → `internal/skills`; `internal/tools/skill.go` → `internal/skills`. The `skills` package imports nothing from `tools` — no circular dependency.

### 5. MCP Client (`internal/tools/mcp.go`)

Connects to external MCP (Model Context Protocol) tool servers at startup. Config is loaded from `.evo_agent/mcp.json` (missing file is silently ignored).

**Transports:**

| Type            | Connection model                                                                 |
|-----------------|---------------------------------------------------------------------------------|
| `stdio`         | Spawns a subprocess; communicates via line-delimited JSON-RPC over stdin/stdout |
| `streamableHttp`| Stateless POST per request; response may be JSON or SSE                         |
| `sse`           | Persistent GET SSE stream for responses; POST endpoint received in `endpoint` event |

**Interface:**
```go
type mcpClient interface {
    getTools() []mcpToolSpec
    callTool(toolName string, arguments json.RawMessage) (string, error)
    stop()
}
```

**Key functions:**
- **`InitMCP()`** — reads config, connects to each enabled server, prints `[MCP] Connected to "name" (N tools)`
- **`ShutdownMCP()`** — calls `stop()` on all clients (called via `defer` in `main.go`)
- **`MCPTools()`** — returns Anthropic tool schemas for all MCP tools, prefixed `mcp__{server}__{tool}`
- **`DispatchMCP(name, input)`** — parses the prefix, routes to the correct server's `callTool`

**Tool name convention:** `mcp__{serverName}__{toolName}`

### 6. Configuration (`internal/config`)

- **`LoadEnv()`** — loads `.env` from the binary's directory first, then from CWD (CWD takes precedence)
- **`Load()`** — reads `MODEL_ID`, `ANTHROPIC_API_KEY`, `ANTHROPIC_BASE_URL` from the environment and builds the system prompt dynamically with the current working directory

### 7. User Interface (`internal/ui`)

Routes agent output through an `EventSink` interface so the same agent code works in both TUI and plain-text modes.

- **`EventSink`** — interface with a single `Emit(Event)` method
- **`SetSink(s EventSink)`** — replaces the global sink at startup
- **`TerminalSink`** — default sink; writes ANSI-colored text directly to stdout
- **`Event` / `EventKind`** — typed event struct covering: `EvThinking`, `EvText`, `EvToolCall`, `EvToolResult`, `EvDone`, `EvSystem`, `EvTokens`
- **`Print*` helpers** — `PrintThinking`, `PrintText`, `PrintToolCall`, `PrintToolResult`, `PrintError`, `PrintSystem`, `PrintTokens`, `PrintDone` — all call `globalSink.Emit()`

### 8. TUI Layer (`internal/tui`)

An inline (non-fullscreen) Bubble Tea UI built on `charm.land/bubbletea/v2`.

**Design principles:**
- Output flows into the terminal scroll buffer via `tea.Println` — no viewport clipping, native mouse selection and scroll
- Each conversation unit (user message, thinking, text, tool call) is a **Block** printed permanently when complete
- `View()` only renders the live bottom area: pending tool spinners + input textarea + status bar + help bar

**Components:**

| File | Responsibility |
|------|---------------|
| `sink.go` | `Sink` — buffered channel-backed `ui.EventSink`; injected as global sink at startup |
| `blocks.go` | `Block` struct with `BlockKind` / `ToolStatus`; constructors for each block type |
| `model.go` | Bubble Tea `Model`: `Init`, `Update`, `View`; `handleKey`, `handleAgentEvent`; event → `tea.Println` pipeline |
| `render.go` | `renderThinking`, `renderToolCall`, `renderStatusBar`, `formatDuration` |
| `styles.go` | All `lipgloss` styles and the `defaultResultRows` constant |
| `sidebar.go` | `SidebarInfo` struct; `truncate` / `shortenPath` helpers |
| `run.go` | `Run()` — creates `Sink`, sets global sink, builds `Model`, starts program |

**Block spacing:** every `tea.Println` call appends `"\n"` so all block types are separated by exactly one blank line.

**Key bubbletea v2 details:**
- `View()` returns `tea.View` (wrapped with `tea.NewView(...)`)
- `tea.KeyPressMsg` replaces v1's `tea.KeyMsg`
- `ta.Focus()` returns `tea.Cmd`; called in `Init()` not `NewModel()`
- `ctrl+enter` / `alt+enter` distinguished via Kitty Keyboard Protocol

### 9. Entry Point (`main.go`)

- Loads config, creates the Anthropic client
- Calls `tools.InitMCP()` to connect MCP servers; defers `tools.ShutdownMCP()`
- Calls `skills.Init()` to scan `.evo_agent/skill/`; appends the skill catalog to `cfg.SystemMsg`
- `--plain` flag: runs `agent.Run(os.Stdin)` with the default `TerminalSink`
- TUI mode (default): builds `SidebarInfo`, starts agent goroutine reading from `queryCh`, calls `tui.Run(info, queryCh)`

## Data Flow

```
User Input (TUI textarea or stdin in --plain mode)
    │
    ▼
queryCh  ──►  Agent goroutine (main.go)
                    │
                    ▼  (per REPL turn)
            Agent.RunQuery(query, &history, &compactState, doneCh)
                    │
                    ▼
            Agent.Loop(state)
                    │
                    ▼
              MicroCompact(messages)          ← replace older tool results with placeholders
                    │
                    ▼
              EstimateContextSize > 50,000?
                    │ yes
                    ▼
              CompactHistory(...)             ← write transcript + LLM summary → 1 message
                    │
                    ▼
              Agent.RunOneTurn(state)
                    │
                    ├──► Anthropic API  (system prompt + tools + history)
                    │         │
                    │         ▼
                    │    Response (TextBlock / ThinkingBlock / ToolUseBlock)
                    │         │
                    ▼         ▼
              tools.Execute(content)
                    │
                    ├── ui.PrintThinking / PrintText / PrintToolCall / PrintToolResult
                    │       │
                    │       ▼
                    │   globalSink.Emit(Event)
                    │       │
                    │       ├── TUI mode: tui.Sink → buffered channel → Bubble Tea model
                    │       │       └── tea.Println(renderBlock(...)+"\n")  [permanent scroll buffer]
                    │       │
                    │       └── plain mode: TerminalSink → fmt.Printf to stdout
                    │
                    └──► tools.Dispatch(name, input)
                                │
                                ├── mcp__* → DispatchMCP → mcpClient.callTool
                                │
                                ├── bash / read_file / write_file / edit_file
                                │         (read_file also calls TrackRecentFile)
                                │
                                ├── load_skill → skills.Load(name)
                                │
                                └── compact → CompactHistory(focus=...)
                                        │
                                        ▼
                                  ToolResult appended to history
                                        │
                                        ▼
                                RunOneTurn returns true  ──► repeat
                                RunOneTurn returns false ──► done
                                        │
                                        ▼
                              ui.PrintDone()  ──►  tui.Model sets busy=false
```

## Tool Registration Pattern

Each tool file is fully self-contained:

```go
// internal/tools/my_tool.go
package tools

func init() {
    Register(ToolDef{
        Schema: anthropic.ToolParam{
            Name:        "my_tool",
            Description: anthropic.String("Does something useful."),
            InputSchema: GenerateSchema[MyToolInput](),
        },
        Handler: func(input json.RawMessage) (string, error) {
            var in MyToolInput
            json.Unmarshal(input, &in)
            return runMyTool(in)
        },
    })
}
```

Adding a new tool requires **no changes** to any existing file.
