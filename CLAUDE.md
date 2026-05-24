# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Read First

Before making changes, read **[`docs/REFERENCE.md`](docs/REFERENCE.md)** — it contains:
- Key constants & limits (context thresholds, timeouts, sizes)
- File inventory with line counts
- Key data structures (`LoopState`, `CompactState`, `TodoItem`, `Event`)
- Step-by-step guides for adding tools and skills
- MCP config format with examples
- Startup sequence and agent loop flow
- Debugging tips

## Commands

All commands are run from the **project root** (`evo-agent/`), not from `src/`.

```bash
# Build
make build          # produces build/evo-agent

# Run (builds first)
make run            # TUI mode (default)
./build/evo-agent --plain   # plain-text REPL mode

# Tidy dependencies
make deps           # runs go mod tidy inside src/

# Test
cd src && go test ./...
cd src && go test ./internal/skills/...   # single package

# Vet
cd src && go vet ./...

# Clean
make clean
```

`MODEL_ID` must be set in the environment (or in `build/.env` / `.env`). Optionally set `ANTHROPIC_API_KEY` and `ANTHROPIC_BASE_URL` to override the endpoint.

## Architecture

### Top-level layout

```
src/          Go module root (module name: evo-agent)
  main.go     Entry point: flag parsing, config, MCP init, TUI vs plain dispatch
  internal/
    agent/    LLM loop, context compaction, subagent
    config/   Env-var configuration
    skills/   Prompt-injection skill catalog
    tools/    Tool registry + all built-in tools
    tui/      Bubble Tea TUI (model, render, styles, sink)
    ui/       Event types and output sinks
build/        Compiled binary output
```

### Agent loop (`internal/agent/`)

`agent.Loop()` drives the agentic turn cycle:
1. **MicroCompact** — truncates old tool-result blocks to a placeholder in-memory.
2. **LLM call** — sends `state.Messages` + tool schemas; appends `resp` to history.
3. **`tools.Execute()`** — iterates content blocks, emits UI events, dispatches each `tool_use` via the registry.
4. **Todo reminder injection** — if `todo` tool was not used for 3 consecutive rounds, appends an XML `<reminder>` text block to tool results.
5. **Manual compact** — if the model called the built-in `compact` tool, runs full LLM summarization.
6. Loop continues until no `tool_use` blocks remain in the response.

`CompactState` (carried on `LoopState`) persists `HasCompacted`, `LastSummary`, `RecentFiles` (FIFO-5), and `CompactCount` across turns. Auto-compact triggers at 50 000 estimated chars; full compaction writes a transcript to `.evo-agent/` then rebuilds history as a single summary message.

### Tool system (`internal/tools/`)

**Self-registering pattern**: every tool file has an `init()` that calls `Register(ToolDef{...})`. `Dispatch(name, input)` routes calls by name. `Tools()` returns all registered + MCP schemas for the API call.

Built-in tools: `bash`, `read_file`, `edit_file`, `write_file`, `compact`, `skill`, `todo`, `task`.

Large tool outputs (>30 000 chars) are persisted to `.evo-agent/tool-results/<id>.txt`; a 2 000-char preview placeholder is returned to the model instead.

**MCP** (`tools/mcp.go`): reads `.evo-agent/mcp.json` at startup. Supports three transports: `stdio` (subprocess), `sse` (persistent GET + POST), `streamableHttp` (stateless POST). Tool names are prefixed `mcp__{server}__{tool}`.

### Session planning (`internal/tools/todo.go`)

`GlobalTodo *todoManager` is a package-level singleton. The `todo` tool lets the model maintain a session plan (max 12 items, exactly 1 `in_progress` at a time). After each update, the handler emits `ui.EvTodo` so the TUI re-renders the live plan panel. `NoteRound(usedTodo bool)` / `Reminder()` implement the 3-round reminder interval injected in `agent.Loop`.

### Subagent (`internal/agent/subagent.go` + `internal/tools/task.go`)

The `task` tool lets the model delegate work to a child agent with fresh, isolated context. `RunSubagent(prompt)` drives a sub-loop (max 30 turns) using `ToolsExcept("task")` — the `task` tool is stripped to prevent recursive spawning. Only the last text block is returned to the parent; the child message history is GC'd on return.

**Import-cycle avoidance**: `agent` imports `tools`, so `tools/task.go` holds a private `subagentRunner` callback set via `RegisterSubagentRunner()`. `agent.New()` injects `ag.RunSubagent` at startup — the same pattern used by `GlobalTodo`.

### UI event bus (`internal/ui/`)

`EventSink` interface has a single method `Emit(Event)`. `globalSink` is swapped at startup:
- **TUI mode**: `tui.Sink` (buffered channel, non-blocking drop on full buffer).
- **Plain mode**: `TerminalSink` (writes ANSI directly to stdout).

`ui.Event` is a union struct tagged by `EventKind` (`EvThinking`, `EvText`, `EvToolCall`, `EvToolResult`, `EvSystem`, `EvTokens`, `EvDone`, `EvTodo`).

### Bubble Tea TUI (`internal/tui/`)

`tui.Run()` creates a `Sink`, calls `ui.SetSink(sink)`, then starts the Bubble Tea program. The agent goroutine and TUI communicate via two channels:
- `queryCh chan string` — user input (TUI → agent).
- `sink.Chan() <-chan ui.Event` — agent output (agent → TUI).

`Model.View()` renders only the **live interactive bottom area** (pending tool calls, todo panel, spinner, input, status bar). All completed conversation content is permanently committed to the terminal scroll buffer via `tea.Println`.

### Skills (`internal/skills/`)

Skills are Markdown files in a `skills/` directory. `skills.Init()` loads them; `skills.Catalog()` returns a summary injected into the system prompt. The `load_skill` tool lets the model load full skill content on demand.

### Configuration

`config.Load()` reads env vars: `MODEL_ID` (required), `ANTHROPIC_API_KEY`, `ANTHROPIC_BASE_URL`. `.env` files are loaded from the binary directory first, then from the working directory (cwd wins). `ProjectDir` defaults to cwd and seeds the system prompt.
