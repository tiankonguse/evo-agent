# evo-agent

A step-by-step journey to implement an AI agent in Go.

Evo-Agent is a lightweight, tool-augmented AI agent written in Go. It leverages the Anthropic API to perform tasks in a local workspace through a ReAct (Reason + Act) loop — the agent reasons, calls tools, observes results, and repeats until the task is complete.

## Features

- **Bubble Tea TUI**: Inline (non-fullscreen) terminal UI — thinking blocks, tool call results, and text responses rendered as uniform blocks with a live status bar at the bottom
- **Slash Commands**: User-driven `/command` dispatch — type `/review`, `/deploy staging`, etc. for deterministic workflow triggers; supports shell-style arguments and template substitution (`$name`, `$0`, `$ARGUMENTS`)
- **System Prompt Builder**: Structured, section-based prompt assembly (`internal/prompt/`) with static/dynamic boundary for cache optimization; dependency-injected providers avoid import cycles; includes environment context (git, platform, shell, model, date)
- **Multi-tool Support**: bash, read_file, write_file, edit_file, compact, load_skill — all self-registering via `init()`
- **Self-registering Tool Pattern**: Adding a new tool only requires a single new file; no central registration needed
- **Table-driven Dispatch**: A global registry maps tool names to schemas and handlers
- **Multi-turn Reasoning**: Drives a loop of thought → action → observation until the model stops requesting tool calls
- **Context Compaction**: Three-layer strategy (placeholder micro-compact → LLM summarization → model-initiated compact) to handle unlimited-length sessions
- **MCP Client Support**: Connect to external MCP tool servers via `stdio`, `sse`, or `streamableHttp` transports; config loaded from `.evo-agent/mcp.json`
- **Skill System**: Two-layer on-demand knowledge — cheap catalog injected into system prompt; full skill body loaded only when needed via `load_skill`
- **Session Planning (todo)**: Built-in `todo` tool lets the model maintain a live session plan (max 12 items, exactly one `in_progress` at a time); a 3-round reminder is injected when the plan goes stale; the TUI renders a real-time plan panel at the bottom
- **Persistent Session Plan (plan_\*)**: Two-layer planning — the in-memory `todo` is for short steps, while the disk-backed `plan_*` tool family stores big tasks as a directory of JSON files under `.evo-agent/tasks/todo/<YYYY-MM-DD-name>/`; survives context compaction and process restarts; supports a task dependency graph (`blockedBy` / `blocks`, bidirectionally synced); active plan summary auto-injected into the system prompt; 5-round stale-reminder; on startup the active plans are printed and re-injected so the agent picks up exactly where it left off
- **Subagent (task tool)**: `task` tool spawns an isolated child agent with a fresh context to delegate complex subtasks; child shares the filesystem but not conversation history; only a text summary is returned to the parent; recursive spawning is prevented by stripping `task` from child's tool list; hard cap of 30 turns per subagent
- **Dump Prompts Debugging**: `/dump-prompts` toggle saves every API call (system prompt + messages) to `.evo-agent/dump-prompts/` as JSONL for prompt inspection and debugging
- **Session Persistence (`/resume`)**: Append-only transcript layer under `.evo-agent/sessions/<unix_ms>_<UUID>/` (`messages.jsonl` + `meta.json` sidecar + per-subagent sidechain files); every user turn, assistant response, tool result, and compact boundary is durably recorded as a single JSONL line; resume via `evo-agent --resume <id>` (CLI), `/resume <id>` (inline), or `/resume` (TUI dropdown picker showing date / token count / first prompt); on resume the loader replays post-boundary messages and prepends the most recent compact summary wrapped in `<previous-conversation-summary>` so the model sees a digest instead of a literal history; subagent conclusions surface as `<subagent-result name="…">…</subagent-result>` notes; on exit the agent prints the resume hint so the next session can pick up exactly where it left off

## Project Structure

```
src/
├── main.go                    # Entry point: TUI/plain mode, MCP init/shutdown, prompt builder, skill catalog, slash dispatch
├── go.mod
└── internal/
    ├── agent/
    │   ├── loop.go            # Agent struct, RunOneTurn, Loop, Run, RunQuery, RunQueryDirect
    │   ├── state.go           # LoopState, CompactState
    │   ├── compact.go         # MicroCompact, CompactHistory, SummarizeHistory, TrackRecentFile
    │   ├── subagent.go        # RunSubagent: isolated child agent, 30-turn cap, summary return
    │   ├── dump.go            # DumpAPICall, ToggleDumpPrompts: save API calls to JSONL for debugging
    │   └── transcripts.go     # WriteTranscript: save full history to .evo-agent/transcripts/
    ├── config/
    │   └── config.go          # Config struct, LoadEnv, Load
    ├── prompt/
    │   ├── builder.go         # Builder: section-based system prompt assembly with static/dynamic boundary
    │   └── builder_test.go    # Unit tests for prompt builder sections and ordering
    ├── session/
    │   ├── session.go         # Session struct, NewSession, AdoptSession (writes session_start)
    │   ├── recorder.go        # Recorder: append-only writes + meta.json sidecar
    │   ├── subagent_recorder.go # Sidechain recorder for subagent transcripts
    │   ├── loader.go          # LoadForResume: rebuild a runnable message slice
    │   ├── list.go            # ListSessions for the /resume picker
    │   ├── record.go          # Record envelope + Type* constants
    │   ├── ids.go             # NewSessionID, NewPromptID, NewSubagentFilename, slugify
    │   ├── git.go             # currentGitBranch — best-effort `git rev-parse --abbrev-ref HEAD`
    │   └── session_test.go    # Round-trip, compact-clip, sidechain, resume_marker tests
    ├── skills/
    │   ├── registry.go        # SkillManifest, Init, InitCommands, Catalog, Load, LookupForSlash
    │   ├── dispatch.go        # Dispatch, SlashResult, SlashNames — slash command entry point
    │   ├── provider.go        # Provider struct: satisfies prompt.SkillsProvider interface
    │   ├── args.go            # ParseArgs — shell-style argument splitting with quoting
    │   └── render.go          # RenderBody — template substitution ($name, $N, $ARGUMENTS)
    ├── tools/
    │   ├── tool.go            # ToolDef registry, Register, Tools, ToolsExcept, Dispatch, GenerateSchema
    │   ├── executor.go        # Execute: iterate content blocks, run tool calls
    │   ├── persist.go         # PersistLargeOutput: save large outputs to disk, return preview
    │   ├── mcp.go             # MCP client: stdio / sse / streamableHttp transports, InitMCP, ShutdownMCP
    │   ├── bash.go            # bash tool (run shell commands, 120s timeout)
    │   ├── read_file.go       # read_file tool (read file with optional line limit)
    │   ├── write_file.go      # write_file tool (write/create file with mkdir -p)
    │   ├── edit_file.go       # edit_file tool (exact-string replacement or create)
    │   ├── compact.go         # compact tool (model-initiated context compaction)
    │   ├── skill.go           # load_skill tool (load full skill body on demand)
    │   ├── todo.go            # todo tool (session plan, max 12 items, reminder injection)
    │   ├── plan.go            # persistent session plan: plan_create / plan_list / plan_task_create / plan_task_update / plan_task_list / plan_task_get / plan_complete; disk-backed task graph under .evo-agent/tasks/
    │   └── task.go            # task tool (spawn subagent), RegisterSubagentRunner callback
    ├── tui/
    │   ├── run.go             # Run(): create Sink, start Bubble Tea program
    │   ├── model.go           # Bubble Tea Model: Init/Update/View, event handling
    │   ├── blocks.go          # Block types (thinking/text/tool/system/user) and constructors
    │   ├── render.go          # renderThinking, renderToolCall, renderStatusBar, formatDuration
    │   ├── styles.go          # lipgloss styles and layout constants
    │   ├── sidebar.go         # SidebarInfo struct, truncate/shortenPath helpers
    │   └── sink.go            # Sink: ui.EventSink implementation backed by buffered channel
    └── ui/
        ├── terminal.go        # Print* functions routing through globalSink; ANSI constants
        ├── event.go           # Event struct and EventKind constants
        └── sink.go            # EventSink interface and globalSink registration
```

## Configuration

The agent is configured via environment variables (or a `.env` file):

| Variable              | Required | Description                                      |
|-----------------------|----------|--------------------------------------------------|
| `MODEL_ID`            | Yes      | The Anthropic model to use                       |
| `ANTHROPIC_API_KEY`   | Yes*     | Your Anthropic API key                           |
| `ANTHROPIC_BASE_URL`  | No       | Custom API endpoint (e.g. for proxies)           |

> \* If `ANTHROPIC_BASE_URL` is set, the API key may be optional depending on the proxy configuration.

Copy `.env.example` to `.env` and fill in your values:

```bash
cp .env.example .env
```

## Build & Run

```bash
# Build
make build

# Run tests
make test

# Or run directly
./build/evo-agent            # TUI mode (default)

# Plain-text mode (no TUI)
./build/evo-agent --plain 

# Resume a previous session
./build/evo-agent --resume <session-id>
./build/evo-agent --plain --resume <session-id>
```

## Usage

After starting the agent, type your request at the prompt. The TUI renders output inline — thinking blocks, tool calls, and text responses each appear as styled blocks separated by a blank line.

```
>> list all Go files in this workspace
>> read the file src/main.go and summarize it
>> create a new file hello.go with a Hello World program
>> exit
```

- **Enter** — send message
- **Ctrl+Enter / Alt+Enter** — insert newline in the input box
- **Ctrl+C** — quit
- Type `q` or `exit` to quit

### TUI Layout

```
 You: list all Go files                        ← user block
                                               ← blank line
 ▸ Thinking  🕐 1.2s                           ← thinking block (purple)
   Let me search for Go files…
                                               ← blank line
 ✓ bash  find . -name "*.go"  🕐 0.3s         ← tool call block
   Result:
   ./main.go
   ./internal/agent/loop.go
   …
                                               ← blank line
  Here are the Go files I found…              ← text block
                                               ← blank line
 🕐 2.1s                                       ← elapsed bar
────────────────────────────────────────────
 ▸ Session Plan                                ← todo panel (live, bottom)
 [x] Read existing files
 [>] Refactor module  (Refactoring)
 [ ] Add unit tests
 [ ] Update docs
 (1/4 completed)
────────────────────────────────────────────
 >> [input area]
────────────────────────────────────────────
 tokens:1234/200000(0.6%)  model:…  agent:…  skills:3  tools:6  mcp:0
 Enter send • Ctrl+Enter/Alt+Enter newline • ctrl+c quit
```

## Tools

### Built-in Tools

| Tool          | Description                                                     |
|---------------|-----------------------------------------------------------------|
| `bash`        | Run any shell command (timeout: 120s, max output: 50 000 chars) |
| `read_file`   | Read a file's contents with an optional line limit              |
| `write_file`  | Write (or overwrite) a file, creating parent directories        |
| `edit_file`   | Replace an exact string in a file, or create a new file         |
| `compact`     | Summarize the conversation history to free up context window; accepts an optional `focus` hint |
| `load_skill`  | Load the full body of a named skill into context; use before acting on tasks that need specialized instructions |
| `todo`        | Rewrite the current session plan (max 12 items, exactly one `in_progress`); refreshes the live TUI plan panel |
| `plan_create` | Create a new persistent session plan directory `.evo-agent/tasks/todo/<YYYY-MM-DD-name>/` with `plan.md` (analysis + approach + steps) |
| `plan_list`   | List all session plans — active in `todo/` and archived in `done/` — with task progress |
| `plan_task_create` | Add a task to a session plan; supports `blockedBy` to declare dependencies (the `blocks` field is auto-synced on the other side) |
| `plan_task_update` | Update a task's status (`pending` / `in_progress` / `completed` / `deleted`), owner, or dependency edges; completing a task auto-clears it from other tasks' `blockedBy` lists |
| `plan_task_list`   | List all tasks in a plan with status markers, ownership, and blocking info |
| `plan_task_get`    | Fetch full JSON of a single task (subject, description, status, dependencies, owner) |
| `plan_complete`    | Move a finished plan from `todo/` to `done/`; refuses to archive if any task is still pending or in_progress |
| `task`        | Spawn a subagent with fresh context to complete a subtask; shares filesystem but not history; returns a text summary; max 30 turns |

### MCP Tools

MCP tools are loaded automatically at startup from `.evo-agent/mcp.json`. Each tool is exposed to the model with a prefixed name: `mcp__{server}__{tool}`.

Configure servers in `.evo-agent/mcp.json`:

```json
{
  "mcpServers": {
    "my_server": {
      "type": "streamableHttp",
      "url": "https://example.com/mcp",
      "headers": {
        "Authorization": "Bearer <token>"
      },
      "disabled": false,
      "timeout": 30
    },
    "local_fs": {
      "type": "stdio",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
    }
  }
}
```

| Field       | Type    | Description                                           |
|-------------|---------|-------------------------------------------------------|
| `type`      | string  | Transport: `"stdio"`, `"sse"`, or `"streamableHttp"` |
| `disabled`  | boolean | Skip this server at startup (default: `false`)        |
| `timeout`   | integer | Request timeout in seconds (default: `30`)            |
| `command`   | string  | *(stdio only)* Subprocess command                     |
| `args`      | array   | *(stdio only)* Command-line arguments                 |
| `env`       | object  | *(stdio only)* Extra environment variables            |
| `url`       | string  | *(sse/streamableHttp)* Remote server URL              |
| `headers`   | object  | *(sse/streamableHttp)* Custom HTTP request headers    |

### Skills

Skills provide reusable, task-specific guidance using a two-layer model that keeps the system prompt small:

1. **Catalog** (always in system prompt) — just `name: description` for each skill
2. **Full body** (loaded on demand) — the model calls `load_skill` when it needs detailed instructions

Skill files live under `.evo-agent/skill/`:

```
.evo-agent/skill/
└── git-commit/
    └── SKILL.md
```

Each `SKILL.md` has a YAML frontmatter header followed by the skill body:

```markdown
---
name: git-commit
description: Best practices for writing git commit messages
---
Always use imperative mood. Keep subject line under 72 chars.
Format: <type>(<scope>): <subject>

Types: feat, fix, docs, refactor, test, chore
```

| Frontmatter field | Description                                             |
|-------------------|---------------------------------------------------------|
| `name`            | Skill identifier used with `load_skill`; falls back to the directory name if omitted |
| `description`     | One-line summary shown in the system prompt catalog     |
| `argument-hint`   | Hint shown in help for slash invocation (e.g. `[file]`) |
| `arguments`       | Named positional args (space or comma separated) for `$name` substitution |
| `user-invocable`  | Whether the user can invoke via `/slash` (default: `true`) |
| `disable-model-invocation` | If `true`, excluded from catalog — only user can invoke via `/slash` |

The `load_skill` output includes the absolute path to `SKILL.md` so the model can reference sibling files in the same directory:

```xml
<skill name="git-commit" path="/workspace/.evo-agent/skill/git-commit/SKILL.md">
...body...
</skill>
```

### Slash Commands

Slash commands provide **user-driven deterministic dispatch** — type `/name` in the input box to trigger a specific workflow without waiting for the LLM to decide.

Commands are stored as flat Markdown files in `.evo-agent/command/`:

```
.evo-agent/command/
├── review.md
├── deploy.md
└── hello.md
```

Each `.md` file uses the same frontmatter format as skills:

```markdown
---
name: hello
argument-hint: [name]
arguments: name
user-invocable: true
---

Say hello to $name in a friendly way.
```

**Argument substitution** — the command body supports template placeholders:

| Placeholder       | Description                                  |
|-------------------|----------------------------------------------|
| `$name`           | Named positional argument (from `arguments` frontmatter) |
| `$0`, `$1`, ...   | Positional argument by index                 |
| `$ARGUMENTS[N]`  | Explicit indexed argument                    |
| `$ARGUMENTS`      | Full raw argument string                     |

If no placeholder is present in the body, `ARGUMENTS: <raw>` is automatically appended.

**Dispatch priority**: commands take priority over skills when the same name exists in both.

**Key differences from skills:**

| | Skill | Command |
|---|---|---|
| Trigger | LLM calls `load_skill` autonomously | User types `/name` explicitly |
| In system prompt catalog | Yes (unless disabled) | Never |
| Storage | `.evo-agent/skill/<name>/SKILL.md` | `.evo-agent/command/<name>.md` |
| Determinism | LLM decides when to load | User decides when to invoke |

Usage examples:
```
/review src/main.go          ← trigger code review workflow
/deploy staging              ← trigger deployment
/hello World                 ← $name → "World"
```

## Adding a New Tool

1. Create `src/internal/tools/<name>.go`
2. Define an input struct with `jsonschema_description` tags
3. Call `Register(ToolDef{...})` inside an `init()` function

That's it — the tool is automatically available to the agent on next run.

## Session Persistence

Every run is durably recorded as an append-only JSONL transcript so a session
can be resumed across process restarts. See [`docs/session-persistence.md`](docs/session-persistence.md)
for the full design.

### On-disk layout

```
.evo-agent/sessions/<unix_ms>_<UUID>/
├── messages.jsonl                       # append-only event stream (user / assistant / tool result / compact_boundary / resume_marker / subagent_start | _end / session_start)
├── meta.json                            # sidecar: cumulative tokens, first prompt, branch, ts (rewritten after every append; cheap to list for the picker)
└── subagent/
    └── <unix_ms>_<name>_<UUID>.jsonl    # per-subagent sidechain transcript
```

The session id is `<unix_ms>_<8 hex>` (e.g. `1780227556183_b525857d`) so
`os.ReadDir` returns sessions in time order without an explicit sort.

### Write points in the agent loop

| Hook | Record type |
|---|---|
| `agent.RunQuery` / `RunQueryDirect` / `Run` (user turn entry) | `user` |
| `agent.Loop` after assistant response | `assistant` (with `input_tokens` / `output_tokens`) |
| `agent.Loop` after tool results | `user` (tool results travel as user role) |
| `agent.CompactHistory` after summary generation | `compact_boundary` |
| `task` tool spawn / return | `subagent_start` / `subagent_end` (parent) + full per-message records (sidechain) |

A `nil` recorder transparently disables persistence.

### Resume entry points

| Form | Where |
|---|---|
| `evo-agent --resume <id>` | CLI flag (TUI or `--plain`) |
| `/resume <id>` | Inline in the input box — intercepted client-side, never reaches the LLM |
| `/resume` (no args) | TUI dropdown picker; rows show `YYYY-MM-DD HH:MM   tokens=…   「first prompt…」` — ↑/↓ to select, Enter to submit, Esc to cancel |

`--resume` always opens a **new** session file and writes a `resume_marker`
record referencing the source id. The original transcript is never modified.

### Recovery rules

`session.LoadForResume` rebuilds a runnable `[]anthropic.MessageParam` slice:

1. Stream-scan `messages.jsonl`, collecting every record.
2. Find the index of the **last** `compact_boundary`, remembering its summary.
3. Drop every `user` / `assistant` record at indexes ≤ that boundary.
4. If a boundary existed, prepend a synthetic user message wrapping the summary in `<previous-conversation-summary>` tags.
5. Replay every post-boundary `user` / `assistant` `Message` field.
6. For every `subagent_end` after the boundary, append `<subagent-result name="…">…</subagent-result>` so the parent context still remembers the subagent's conclusion.

### Exit hint

When the process exits, a deferred print surfaces:

```
Resume this session with: evo-agent --resume <session-id>
```

## Blog

| Article | Description |
|---------|-------------|
| [01-loop](blog/01-loop.md) | ReAct Loop — how the agent thinks, acts, and observes in a cycle |
| [02-tools](blog/02-tools.md) | Tools — self-registering tool pattern and table-driven dispatch |
| [03-prompts](blog/03-prompts.md) | Prompts & Context — system prompt, messages history, and the two-layer loop |
| [04-compact](blog/04-compact.md) | Context Compaction — three-layer strategy for unlimited-length sessions |
| [05-mcp](blog/05-mcp.md) | MCP Client — connect external tool servers via stdio, sse, streamableHttp |
| [06-skill](blog/06-skill.md) | Skill System — two-layer on-demand knowledge injection |
| [07-tui](blog/07-tui.md) | Bubble Tea TUI — inline non-fullscreen terminal UI with live status bar |
| [08-todo](blog/08-todo.md) | Session Planning — todo tool, state constraints, reminder injection, TUI panel |
| [09-subagent](blog/09-subagent.md) | Subagent — task tool, isolated child agent, import-cycle avoidance, no recursive spawning |
| [10-command](blog/10-command.md) | Slash Commands — user-driven deterministic dispatch, command vs skill design, argument substitution |
| [11-auto-memory](blog/11-auto-memory.md) | Auto Memory — persistent memory system, remember tool, consolidation, memory guidance injection |
| [12-agent-markdown](blog/12-agent-markdown.md) | Agent.md — project guidance file, /init built-in command, go:embed built-in commands |
| [13-system-prompt](blog/13-systerm-prompt.md) | System Prompt — structured builder pattern, static/dynamic boundary, environment injection, dump-prompts debugging |
| [14-session-plan](blog/14-session-plan.md) | Persistent Session Plan — disk-backed task graph, two-layer planning (Memory Plan + Session Plan), `blockedBy`/`blocks` bidirectional dependency, startup recovery, system-prompt injection |

## Version History

| Version | Description |
|---------|-------------|
| **v0.15.0** | Add session persistence: new `internal/session` package writes an append-only `.evo-agent/sessions/<unix_ms>_<UUID>/messages.jsonl` transcript with a `meta.json` sidecar (cumulative tokens, first prompt, branch, ts) and per-subagent sidechain files under `subagent/`; `Recorder` hooks four points in the agent loop (user turn entry, assistant response, tool results, `compact_boundary`) plus subagent start/end markers; `LoadForResume` rebuilds a runnable `[]anthropic.MessageParam` slice — drops pre-boundary turns, prepends the most recent compact summary wrapped in `<previous-conversation-summary>` tags, and surfaces `<subagent-result>` notes for any post-boundary subagent conclusion; three resume entry points: `evo-agent --resume <id>` (CLI), `/resume <id>` (inline, client-side intercept that never reaches the LLM), and `/resume` (TUI dropdown picker showing date / token count / first prompt with ↑/↓ select); `agent.LoopState` carries `Recorder` + `PromptID` so a `nil` recorder transparently disables persistence; on resume a `resume_marker` is written to a fresh session file and the source transcript is never mutated; exit hint `Resume this session with: evo-agent --resume <id>` printed on process termination |
| **v0.14.0** | Add persistent session plan: `internal/tools/plan.go` introduces a disk-backed task graph at `.evo-agent/tasks/todo/<YYYY-MM-DD-name>/` (each plan = directory of `plan.md` + `task_N.json`); 7 new tools (`plan_create`, `plan_list`, `plan_task_create`, `plan_task_update`, `plan_task_list`, `plan_task_get`, `plan_complete`); two-layer planning model (in-memory `todo` for short steps, on-disk `plan_*` for big tasks); bidirectional dependency graph (`blockedBy` / `blocks`) with auto-sync on task creation/update and auto-clear on completion; single-active-plan invariant prevents concurrent in-progress plans; 5-round stale reminder when an active plan goes idle; `LoadPrompt()` injects active plan summary into the system prompt's `# Active Plans` section; `StartupSummary()` prints the active plan tree on launch; finished plans are archived from `todo/` to `done/` via `plan_complete`; survives context compaction and process restarts |
| **v0.13.0** | Add system prompt builder: `internal/prompt` package with `Builder` struct assembles prompt from independent sections; static/dynamic boundary (`DynamicBoundary`) separates cacheable content from per-session context; `MemoryProvider` and `SkillsProvider` interfaces for dependency injection; environment section injects runtime context (git, platform, shell, model, date); `/dump-prompts` toggle saves API calls to `.evo-agent/dump-prompts/*.jsonl` for debugging; `skills.Provider` adapter satisfies prompt interfaces |
| **v0.12.0** | Add `/init` built-in command and Agent.md loading: `/init` analyzes codebase structure and generates `Agent.md` project guidance file; `Agent.md` is read at startup and injected into system prompt; built-in commands embedded via `//go:embed` survive across clones; user commands in `.evo-agent/command/` override built-ins with the same name |
| **v0.11.0** | Add auto memory: persistent memory system (`MemoryManager`, `.evo-agent/memory/`); `remember` tool spawns extraction subagent to analyze conversation and persist user preferences, feedback, project facts, and references; `consolidate_memory` tool merges duplicates and prunes stale entries; memory guidance injected into system prompt; memories auto-loaded at startup and formatted into context; built-in commands (`/remember`, `/consolidate`) embedded via `//go:embed` |
| **v0.10.0** | Add slash command system: `Dispatch()` intercepts `/name` input; `InitCommands()` loads `.evo-agent/command/*.md`; shell-style `ParseArgs` with quoting; `RenderBody` template substitution (`$name`, `$0`, `$ARGUMENTS[N]`, `$ARGUMENTS`); commands take priority over skills; `RunQueryDirect` bypasses normal input processing |
| **v0.9.0** | Add subagent: `task` tool (`task.go`, `RegisterSubagentRunner`), `RunSubagent()` in `subagent.go` (30-turn isolated child agent), `ToolsExcept()` helper, `PersistLargeOutput` exported; `agent.New()` injects subagent runner |
| **v0.8.0** | Add session planning: `todo` tool (`todoManager`, max 12 items, single `in_progress` constraint, 3-round reminder injection); `EvTodo` event; live TUI plan panel (`renderTodoPanel`) |
| **v0.7.0** | Add Bubble Tea TUI (`internal/tui`): inline (non-fullscreen) output, thinking/text/tool blocks with uniform spacing, bottom status bar (tokens/model/agent/skills/tools/MCP), `ctrl+enter` newline via bubbletea v2 + Kitty Protocol |
| **v0.6.0** | Add two-layer skill system: `internal/skills` package (`Init`, `Catalog`, `Load`); `load_skill` tool in `tools/skill.go`; skill catalog auto-injected into system prompt; skills stored in `.evo-agent/skill/<name>/SKILL.md` |
| **v0.5.0** | Add MCP client support: `stdio`, `sse`, and `streamableHttp` transports; config from `.evo-agent/mcp.json`; `InitMCP`/`ShutdownMCP` in `main.go`; MCP tools auto-merged into `Tools()` and routed in `Dispatch()` |
| **v0.4.0** | Add context compaction: `CompactState`, `MicroCompact`, `CompactHistory`, `WriteTranscript`, and `compact` tool; `loop.go` integrates automatic and model-initiated compaction |
| **v0.3.0** | Refactor loop: move REPL into `loop.go` (`Run` method), add `TurnCount`/`TransitionReason` to `LoopState`, generate `SystemMsg` in `config.go` |
| **v0.2.0** | Add `read_file`, `write_file`, `edit_file` tools; introduce self-registering `init()` pattern and table-driven tool dispatch |
| **v0.1.0** | Initial release: ReAct loop + `bash` tool only |

## Dependencies

| Package                           | Purpose                              |
|-----------------------------------|--------------------------------------|
| `anthropic-sdk-go` v1.41.0        | Anthropic API client                 |
| `invopop/jsonschema` v0.13.0      | Reflect Go structs → JSON Schema     |
| `joho/godotenv` v1.5.1            | Load `.env` files                    |
| `charm.land/bubbletea/v2` v2.0.6  | TUI framework (Kitty Protocol, `ctrl+enter` support) |
| `charm.land/bubbles/v2` v2.1.0    | Textarea widget                      |
| `charm.land/lipgloss/v2` v2.0.3   | Terminal styling                     |
