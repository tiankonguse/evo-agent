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
- **`/goal` Auto-continuation**: Set a session-scoped completion condition with `/goal <text>`; after every turn that ends with no tool calls, the same `MODEL_ID` is invoked as a yes/no judge and — when the condition isn't met — the loop synthesizes a `<goal-reminder>` user message and keeps working until satisfied or capped at 30 iterations. `/goal` (no args) shows status; `/goal clear` cancels (aliases `stop`/`off`/`reset`/`cancel`/`none`). The slash command auto-creates a persistent `.evo-agent/tasks/todo/<plan>/` so the work survives restarts; goal text is injected into every system prompt; goal lifecycle persists to the session transcript and is restored on `--resume`
- **Background Tasks (`bg_*`)**: Async execution lane for long-running shell commands (full builds, test suites, dev servers, file watchers); `bg_run` launches a command in its own goroutine + process group and returns a task id immediately so the agent loop can keep working while the command runs (300s hard cap per task, 50KB output capture); the next turn after completion automatically receives a synthetic `<background-results>` user message with status + preview + `output_file` path so the model sees outcomes without needing to poll; tasks are durably persisted under `.evo-agent/sessions/<sid>/runtime-tasks/{todo,done}/<id>/{task.json,output.log}` (atomic `os.Rename` on completion); on `--resume`, `Init()` rehydrates archived tasks and downgrades any leftover `running` records (from a crashed previous run) to `error`; companion tools `bg_list` / `bg_check` / `bg_cancel` (cancel sends `SIGKILL` to the whole pgrp); client-side `/bgtask`, `/bgtask <id>`, `/bgtask cancel <id>` slash commands inspect or kill tasks without consulting the LLM; live `bg:N run / M done` indicator in the TUI status bar
- **Scheduled Tasks (`cron_*`)**: Lets the model wake itself up at a future time using standard 5-field cron expressions. `cron_create` schedules a prompt; `cron_list` shows all tasks; `cron_delete` cancels by id. A 1-second background ticker matches every task's cron against the current minute (single-fire-per-minute guard prevents `* * * * *` from firing 60 times); on match, the task's prompt is queued and surfaces at the start of the next agent turn as a synthetic `<scheduled-task>` user message — the same notification pattern as `bg_run`. Two orthogonal flags: `recurring` (default `true` — repeat until explicit delete or 7-day auto-expiry; `false` = one-shot, deleted right after firing, with a `FireBy + 2min` miss-window guard) and `durable` (default `false` — session-only, in-memory; `true` persists to `.evo-agent/sessions/<id>/scheduled_tasks/tasks.json` and survives `--resume`). Deterministic 1-4 minute jitter on `:00` / `:30` targets keeps parallel sessions from firing the same `0 * * * *` job on the exact same wall-clock minute. Built-in `/loop` slash command wraps `cron_create` for the common "every N minutes" case (e.g. `/loop 5m /git-commit`, defaults to 10 min when interval is omitted)
- **Team Mode (`team_*`)**: Persistent named teammates that survive across compactions and process restarts — a multi-agent collaboration lane that complements one-shot subagents. Each teammate runs in its own goroutine with its own message history and inbox, all backed by `.evo-agent/team/` (`config.json` for the roster + `inbox/<name>.jsonl` per-recipient queue + `history/<name>.jsonl` per-teammate transcript). Six model-invocable tools: `team_spawn` (create or revive a teammate with a role + kickoff prompt; cap of 8 active members), `team_list` (every teammate with status `working`/`idle`/`shutdown` and last-active timestamp), `team_send_message` (append an envelope to a teammate's inbox — types: `message`/`broadcast`/`shutdown_request`/`shutdown_response`/`plan_approval`/`plan_approval_response`), `team_read_inbox` (drain the lead's own inbox manually; usually unnecessary since `agent.Loop` drains it at the top of every turn and synthesizes a `<team-inbox>` user message), `team_broadcast` (send the same body to every active teammate), `team_shutdown` (clean termination — record stays in config as `shutdown` for audit, history preserved on disk). Each teammate goroutine runs one work cycle per wake — hydrate history, drain inbox, run an LLM tool-use burst (cap 50 turns/wake), then either go idle or terminate; idle teammates have no live goroutine until a new inbox message wakes them, avoiding dead-thread channel pitfalls. Teammates cannot call `task` / `team_spawn` / `team_shutdown` (lead-only authority + no recursive spawning). When a teammate ends a turn naturally (no tool calls), it pushes a `TeamNotification` onto the lead's notification queue with the final text — the lead sees it as a `<team-notifications>` summary on its next turn. New `EvTeam` UI event renders a live team panel showing every member's role + status. Stale `working` records left from a crashed run are downgraded to `idle` on `Init()` so the next session sees a clean roster. Companion `/team` / `/team list` / `/team shutdown <name>` / `/team inbox <name>` slash commands inspect or operate the roster without consulting the LLM. Inspired by Claude Code's `TeamCreate` / `Agent` (with `team_name`) / `SendMessage` / `TeamDelete` tools

## Project Structure

```
src/
├── main.go                    # Entry point: TUI/plain mode, MCP init/shutdown, prompt builder, skill catalog, slash dispatch
├── go.mod
└── internal/
    ├── agent/
    │   ├── loop.go            # Agent struct, Loop, Run, RunQuery (caller pre-builds the user message)
    │   ├── state.go           # LoopState, CompactState
    │   ├── compact.go         # MicroCompact, CompactHistory, SummarizeHistory, TrackRecentFile
    │   ├── subagent.go        # RunSubagent: isolated child agent, 30-turn cap, summary return
    │   ├── bgtaskcmd.go       # /bgtask client-side slash command: list / show / cancel — never drives an LLM turn
    │   ├── teamcmd.go         # /team client-side slash command: list / shutdown / inbox — never drives an LLM turn
    │   ├── team.go            # team_* hooks for agent.Loop: drain lead inbox, drain team notifications, inject <team-inbox> + <team-notifications> user messages
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
    │   ├── bgtask.go          # background tasks: bg_run / bg_check / bg_list / bg_cancel; goroutine-based async execution with per-pgrp cancel + atomic todo/→done/ archive under .evo-agent/sessions/<sid>/runtime-tasks/
    │   ├── cron.go            # scheduled tasks: parseCron / matchCron / nextRun + 1-sec background ticker, single-fire-per-minute guard, durable tasks.json persistence, deterministic :00/:30 jitter, 7-day recurring auto-expiry
    │   ├── cron_tools.go      # cron_create / cron_list / cron_delete tool registrations
    │   ├── team.go            # team mode: TeamManager singleton, team_spawn / team_list / team_send_message / team_read_inbox / team_broadcast / team_shutdown; per-teammate goroutine, file-backed inbox/history under .evo-agent/team/
    │   ├── session_context.go # process-wide setter (SetSessionContext) so registry-dispatched tools can find the active session dir/id
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
| `bg_run`      | Launch a long-running shell command in a background goroutine; returns a task id immediately (300s hard cap, 50KB output capture); next turn after completion auto-injects a `<background-results>` user message with preview + `output_file` path |
| `bg_check`    | Inspect one background task by id (full JSON record), or omit `task_id` to list everything compactly |
| `bg_list`     | List every background task (running + archived) with status + preview |
| `bg_cancel`   | Kill a running task (`SIGKILL` on its process group) and archive as `cancelled`; idempotent for already-finished tasks |
| `cron_create` | Schedule a prompt to fire at a future time using a 5-field cron expression; `recurring` (default `true`, 7-day auto-expiry; `false` = one-shot) and `durable` (default `false` = session-only memory; `true` = persisted to disk, survives `--resume`) flags control lifetime |
| `cron_list`   | List every scheduled task — id, cron expression, recurring/one-shot, durable/session, last-fired timestamp, prompt preview |
| `cron_delete` | Cancel a scheduled task by id (idempotent for unknown ids) |
| `team_spawn`  | Create a persistent named teammate that runs in its own goroutine and survives across compactions; takes `name`, `role`, and a kickoff `prompt`; max 8 active members; reviving a `shutdown` member resets history |
| `team_list`   | List every teammate with role, status (`working`/`idle`/`shutdown`), and last-active timestamp |
| `team_send_message` | Send a message to a teammate's inbox (or `to='lead'`); recipient wakes immediately if idle; `msg_type` ∈ `message`/`broadcast`/`shutdown_request`/`shutdown_response`/`plan_approval`/`plan_approval_response` |
| `team_read_inbox`   | Drain pending messages from the lead's own inbox; rarely needed (the loop drains it automatically each turn) but useful for explicit sync points |
| `team_broadcast`    | Send the same body to every active teammate (shutdown ones are skipped) |
| `team_shutdown`     | Stop a teammate; record stays in config (`status=shutdown`) and history is preserved on disk |
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
| `agent.RunQuery` / `Run` (user turn entry) | `user` |
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
| [15-session-storage](blog/15-session-storage.md) | Session Persistence — append-only JSONL transcript, `meta.json` sidecar, per-subagent sidechain, `compact_boundary` clip-and-replay, three resume entry points (`--resume` / `/resume <id>` / `/resume` picker), exit hint |
| [16-goal](blog/16-goal.md) | `/goal` Auto-continuation — session-scoped completion condition, evaluator-driven loop continuation, `<goal-reminder>` synthesis, 30-iteration cap, status / set / clear forms, persistence across `--resume` |
| [17-background_tasks](blog/17-background_tasks.md) | Background Tasks — async execution lane via `bg_run` goroutines, per-pgrp cancellation, `<background-results>` injection at the start of the next turn, atomic `todo/`→`done/` archive, three-tier concurrency (`bash` / `subagent` / `bg_run`) |
| [18-loop](blog/18-loop.md) | Scheduled Tasks — 5-field cron expressions + 1-second background ticker, single-fire-per-minute guard, `<scheduled-task>` notification injected at the start of the next turn, recurring vs one-shot lifetimes (7-day auto-expiry, miss-window guard), session-only vs durable storage, deterministic jitter, `/loop` command wrapper
| [19-teammate](blog/19-teammate.md) | Team Mode (Teammates) — persistent named teammates that survive across compactions and restarts, per-teammate goroutine + file-backed inbox/history under `.evo-agent/team/`, six `team_*` tools (`spawn`/`list`/`send_message`/`read_inbox`/`broadcast`/`shutdown`), shared task list as the coordination substrate, asynchronous message bus instead of synchronous function calls, when-to-use boundary vs subagent (long-running collaboration vs one-shot exploration)

## Version History

| Version | Description |
|---------|-------------|
| **v0.19.1** | Add `/tools` picker + TUI sink hardening: new `internal/tools/disabled.go` introduces a per-project disable list persisted at `.evo-agent/disabled_tools.json` (sorted JSON array of tool names) — every code path that asks for the tool catalogue (lead loop, subagents via `ToolsExcept`, teammates) goes through `Tools()` which now filters via `IsDisabled()`, so a disabled tool is invisible to every LLM call without per-caller plumbing. Six API surfaces: `LoadDisabled(projectDir)` (rehydrate at startup, called from `main.go` after `InitMCP`), `IsDisabled(name)`, `SetDisabled(name, off)` (write-through to disk), `ResetDisabled()`, `DisabledList()`, `AllToolEntries()` (built-in + MCP roster annotated with source `builtin`/`mcp:<server>` and current disabled flag). New `internal/agent/toolscmd.go` adds the plain-REPL text grammar — `/tools` / `/tools list` / `/tools disable <name>` / `/tools off <name>` (alias) / `/tools enable <name>` / `/tools on <name>` (alias) / `/tools reset`. New TUI picker in `internal/tui/model.go`: typing exactly `/tools` opens an interactive dropdown (↑/↓ navigate · Space toggles disabled flag with immediate write-through · Enter/Esc closes) — `/tools` is also injected into the slash-completion list so it appears alongside `/resume`, `/goal`, etc. TUI sink hardening (`internal/tui/sink.go`): the previous non-blocking `Sink.Emit` silently dropped events on full buffer, and a dropped `EvDone` left `m.busy` stuck `true` forever ("TUI stuck not refreshing"); rewrite to backpressure semantics (try non-blocking send, fall back to blocking send), add `Close()` for shutdown to unblock producers, and an atomic `Dropped()` counter surfaced via `.evo-agent/tui-drops.log` + stderr if non-zero. Event batching (`Model.handleAgentEventBatch` + `listenForEvents` drain loop, capped at 256/batch): blocking first read then opportunistic drain coalesces N queued events into one Update→View pass and stitches all `tea.Println` strings into ONE Println cmd — bubbletea v2's printLineMessage channel is unbuffered with 60-FPS framerate gating, so N separate Printlns took O(N × frame_time) (>180s observed for 2.3s of agent work). Glamour markdown fix (`internal/tui/markdown.go`): switch from `WithAutoStyle()` (which writes OSC 11 to stdout and reads the reply directly from stdin, bypassing bubbletea's I/O loop and leaking bytes like `2828/2c2c/3434` into the textarea) to `WithStandardStyle("dark")`, overridable via `EVO_GLAMOUR_STYLE`. Tool result truncation (`internal/tui/render.go` + `styles.go`): replace row-based 10-line cap with rune-based 100-char cap (CJK-safe, sliced on rune boundary) plus row safety net; preview shows `… truncated (N chars total)`. Six new tests covering: `TestDisabledRoundTrip` / `TestToolsFilteringHidesDisabled` / `TestAllToolEntriesShape` (disable persistence + filter), `TestParseToolsCmd` (REPL grammar), `TestTruncateResult` (CJK rune slicing), `TestSinkBlocksWhenFull` (backpressure regression guard), `TestListenForEventsBatchesQueuedEvents` / `TestListenForEventsBlocksUntilEvent` (batch drain semantics) |
| **v0.19.0** | Add team mode (persistent teammates): new `internal/tools/team.go` introduces a process-wide `TeamManager` singleton plus six model-invocable tools — `team_spawn` (create / revive a named teammate with role + kickoff prompt; cap of 8 active members), `team_list` (every member with status `working`/`idle`/`shutdown` + last-active timestamp), `team_send_message` (append envelope to recipient's inbox; valid `msg_type` ∈ `message`/`broadcast`/`shutdown_request`/`shutdown_response`/`plan_approval`/`plan_approval_response`), `team_read_inbox` (drain the lead's inbox manually), `team_broadcast` (one body → every active teammate), `team_shutdown` (clean stop; record stays as `shutdown` for audit, history preserved). Disk layout `.evo-agent/team/{config.json, inbox/<name>.jsonl, history/<name>.jsonl}` survives compactions and `--resume`; stale `working` records left from a crashed run are auto-downgraded to `idle` on `Init()`. Per-teammate goroutine model: one goroutine = one work cycle (hydrate history → drain inbox → run an LLM tool-use burst capped at 50 turns/wake → either go idle or apply shutdown); idle teammates have NO live goroutine until a new inbox message wakes them via `wakeLocked`, which avoids the dead-thread-still-holding-channel pitfall. New `RegisterTeammateRunner` callback (mirrors `RegisterSubagentRunner`) so `tools/team.go` can drive an LLM call without importing `internal/agent`. Teammates cannot call `task` / `team_spawn` / `team_shutdown` (lead-only authority + no recursive spawning). Notifications: when a teammate ends a turn naturally it pushes a `TeamNotification` (with last text) onto a queue; `agent.Loop` calls `tools.GlobalTeam.DrainNotifications()` before each LLM call and synthesizes a `<team-notifications>` user message via `FormatTeamNotifications`; lead's own inbox is also drained each turn and emitted as `<team-inbox>`. New `EvTeam` UI event + `ui.EmitTeam(teamName, snapshot)`; TUI renders a live team panel showing role / status / last-active for every member. New `internal/agent/teamcmd.go` adds three pure client-side slash commands — `/team` (list), `/team shutdown <name>` (kill+archive), `/team inbox <name>` (debug-drain) — that never drive an LLM turn. System prompt gets a dedicated `# Persistent Teammates (Agent Teams)` guidance section telling the model when to reach for `team_spawn` (long-running multi-agent collaboration) vs `task` (one-shot exploration). `main.go` calls `tools.GlobalTeam.Init(projectDir)` after `session.Bootstrap` and `defer tools.GlobalTeam.Stop()` for clean goroutine shutdown. Inspired by Claude Code's `TeamCreate` / `Agent` (with `team_name`) / `SendMessage` / `TeamDelete` tools |
| **v0.18.0** | Add scheduled tasks (cron): new `internal/tools/cron.go` introduces a process-wide `CronScheduler` singleton with `parseCron` / `matchCron` / `nextRun` plus a 1-second background ticker goroutine; `cron_tools.go` registers three model-invocable tools — `cron_create` (schedule a prompt with a 5-field cron expression), `cron_list` (one-line-per-task summary including id / cron / recurring / durable / last-fired / prompt preview), `cron_delete` (idempotent cancel by id). Two orthogonal lifetime flags: `recurring` (default `true` — repeats until 7-day auto-expiry or explicit delete; `false` = fires once then immediately deleted with a `FireBy + 2min` miss-window guard so a missed wake-up doesn't fire late) and `durable` (default `false` — session-only in-memory; `true` = persisted to `.evo-agent/sessions/<id>/scheduled_tasks/tasks.json` so the task survives `--resume`). Single-fire-per-minute guard tracks the last-evaluated minute index (`hour*60 + minute`) so `* * * * *` fires once per minute, not once per second. Deterministic 1-4 minute forward jitter on tasks targeting `:00` / `:30` (keyed off cron-string hash) prevents two parallel sessions from firing the same `0 * * * *` job on the exact same wall-clock minute; off-minute crons get no jitter. Vixie-cron OR semantics when both day-of-month and day-of-week are constrained. 50-task cap per session matches Claude Code's `MAX_JOBS=50`. Notifications reuse the same pipeline as `bg_run`: matched tasks push a prompt to `notifQ`; `agent.Loop()` calls `tools.GlobalCron.DrainNotifications()` before each LLM call and synthesizes a `<scheduled-task>...</scheduled-task>` user message via `FormatCronNotifications`. `main.go` calls `tools.GlobalCron.Init(sess.Dir, sess.ID)` after `session.Bootstrap` and `defer tools.GlobalCron.Stop()` for clean ticker shutdown. New built-in `/loop` slash command wraps `cron_create` for the common "every N minutes" case (e.g. `/loop 5m /git-commit`, `/loop 30m check the deploy`, defaults to 10 min when interval is omitted). Inspired by Claude Code's `CronCreate` / `CronList` / `CronDelete` tools and Codex CLI's `/loop` command |
| **v0.17.0** | Add background tasks: new `internal/tools/bgtask.go` introduces a process-wide `BgTaskManager` singleton plus four model-invocable tools — `bg_run` (launch a goroutine + process group, return task id immediately), `bg_check` (one record or compact list), `bg_list` (every task with status + preview), `bg_cancel` (`SIGKILL` on the whole pgrp, archive as `cancelled`); 300s hard cap per task; output captured to a 50KB ring (`capWriter`) plus a 500-char preview kept on the record. Per-task storage at `.evo-agent/sessions/<sid>/runtime-tasks/{todo,done}/<id>/{task.json,output.log}`; status transitions `running` → `completed`/`timeout`/`error`/`cancelled` are committed by atomically `os.Rename`-ing the entire `<id>/` directory from `todo/` to `done/`. New `tools/session_context.go` exposes `SetSessionContext(dir,id)` so registry-dispatched tools can find the live session — wired in `main.go` after `session.Bootstrap`. Completion notifications are pushed to a `notifQ`; the agent loop calls `tools.GlobalBgTasks.DrainNotifications()` at the top of every turn and synthesizes a `<background-results>` user message (rendered by `FormatBgNotifications`) so the model sees outcomes without polling. `Init()` rehydrates archived tasks on `--resume` and downgrades stale `running` records (left over from a crashed previous run) to `error` with an explanatory preview. New `internal/agent/bgtaskcmd.go` adds three pure client-side slash commands — `/bgtask` (list), `/bgtask <id>` (show), `/bgtask cancel <id>` (kill+archive) — that never drive an LLM turn. New `EvBgTasks` UI event + `ui.EmitBgTasks(running, completed)`; TUI status bar shows `bg:N run / M done`. System prompt gets a dedicated `# Background Tasks` guidance section telling the model when to reach for `bg_run` (>~30s commands) vs synchronous `bash`. Inspired by Claude Code's `BashOutput`/`KillShell` and Codex CLI's `bg_run`/`bg_check`/`bg_cancel` |
| **v0.16.0** | Add `/goal` command: session-scoped completion condition that drives the loop to keep working until met. New `internal/goal` package with `Manager`/`State` singleton, `ParseVerdict`/`BuildEvalRequest`/`ContinuationPrompt` evaluator helpers, and `RunEvaluator` LLM wrapper. After every turn that ends with no tool calls, `(a *Agent).maybeContinueForGoal(state)` runs the same `MODEL_ID` as a yes/no judge; on `met=false` it synthesizes a `<goal-reminder>` user message embedding the persistent-plan summary and continues, capped at 30 evaluator-driven iterations. Three slash forms: `/goal` (status), `/goal clear` with aliases `stop|off|reset|cancel|none` (cancel), `/goal <text>` (set + auto-create `.evo-agent/tasks/todo/<plan>/`). New `prompt.GoalProvider` interface injects the active goal text into every system prompt build. New `EvGoal` UI event with seven kinds (`set|cleared|achieved|evaluating|continuing|capped|status`); TUI renders a `◎ /goal active · iter N/30` indicator above the input plus a `goal:<text>` chip in the status bar. New session record types (`goal_set`/`goal_cleared`/`goal_achieved`) survive `compact_boundary`; `LoadResult.Goal *RestoredGoal` rehydrates active goals on `--resume` (iter counter resets per Claude Code docs). Inspired by Claude Code's `/goal` and Codex CLI's `set_goal()` |
| **v0.15.0** | Add session persistence: new `internal/session` package writes an append-only `.evo-agent/sessions/<unix_ms>_<UUID>/messages.jsonl` transcript with a `meta.json` sidecar (cumulative tokens, first prompt, branch, ts) and per-subagent sidechain files under `subagent/`; `Recorder` hooks four points in the agent loop (user turn entry, assistant response, tool results, `compact_boundary`) plus subagent start/end markers; `LoadForResume` rebuilds a runnable `[]anthropic.MessageParam` slice — drops pre-boundary turns, prepends the most recent compact summary wrapped in `<previous-conversation-summary>` tags, and surfaces `<subagent-result>` notes for any post-boundary subagent conclusion; three resume entry points: `evo-agent --resume <id>` (CLI), `/resume <id>` (inline, client-side intercept that never reaches the LLM), and `/resume` (TUI dropdown picker showing date / token count / first prompt with ↑/↓ select); `agent.LoopState` carries `Recorder` + `PromptID` so a `nil` recorder transparently disables persistence; on resume a `resume_marker` is written to a fresh session file and the source transcript is never mutated; exit hint `Resume this session with: evo-agent --resume <id>` printed on process termination |
| **v0.14.0** | Add persistent session plan: `internal/tools/plan.go` introduces a disk-backed task graph at `.evo-agent/tasks/todo/<YYYY-MM-DD-name>/` (each plan = directory of `plan.md` + `task_N.json`); 7 new tools (`plan_create`, `plan_list`, `plan_task_create`, `plan_task_update`, `plan_task_list`, `plan_task_get`, `plan_complete`); two-layer planning model (in-memory `todo` for short steps, on-disk `plan_*` for big tasks); bidirectional dependency graph (`blockedBy` / `blocks`) with auto-sync on task creation/update and auto-clear on completion; single-active-plan invariant prevents concurrent in-progress plans; 5-round stale reminder when an active plan goes idle; `LoadPrompt()` injects active plan summary into the system prompt's `# Active Plans` section; `StartupSummary()` prints the active plan tree on launch; finished plans are archived from `todo/` to `done/` via `plan_complete`; survives context compaction and process restarts |
| **v0.13.0** | Add system prompt builder: `internal/prompt` package with `Builder` struct assembles prompt from independent sections; static/dynamic boundary (`DynamicBoundary`) separates cacheable content from per-session context; `MemoryProvider` and `SkillsProvider` interfaces for dependency injection; environment section injects runtime context (git, platform, shell, model, date); `/dump-prompts` toggle saves API calls to `.evo-agent/dump-prompts/*.jsonl` for debugging; `skills.Provider` adapter satisfies prompt interfaces |
| **v0.12.0** | Add `/init` built-in command and Agent.md loading: `/init` analyzes codebase structure and generates `Agent.md` project guidance file; `Agent.md` is read at startup and injected into system prompt; built-in commands embedded via `//go:embed` survive across clones; user commands in `.evo-agent/command/` override built-ins with the same name |
| **v0.11.0** | Add auto memory: persistent memory system (`MemoryManager`, `.evo-agent/memory/`); `remember` tool spawns extraction subagent to analyze conversation and persist user preferences, feedback, project facts, and references; `consolidate_memory` tool merges duplicates and prunes stale entries; memory guidance injected into system prompt; memories auto-loaded at startup and formatted into context; built-in commands (`/remember`, `/consolidate`) embedded via `//go:embed` |
| **v0.10.0** | Add slash command system: `Dispatch()` intercepts `/name` input; `InitCommands()` loads `.evo-agent/command/*.md`; shell-style `ParseArgs` with quoting; `RenderBody` template substitution (`$name`, `$0`, `$ARGUMENTS[N]`, `$ARGUMENTS`); commands take priority over skills; bypasses normal input processing by appending the rendered prompt directly to history before invoking the loop |
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
