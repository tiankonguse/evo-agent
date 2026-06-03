# evo-agent — Architecture & Reference

Single-source **architectural** reference for evo-agent. Read this **before making changes**.

This document explains the *what* and *why* — system layout, design patterns, control flow. For **exact Go signatures, struct field tables, tool input schemas, and constant values** see [`API_REFERENCE.md`](./API_REFERENCE.md). For external SDK / skills format see [`anthropic-sdk-go.md`](./anthropic-sdk-go.md), [`skills.md`](./skills.md).

---

## 1. Project Overview

evo-agent is a Go-based coding agent with a Bubble Tea TUI and plain-text REPL. It drives the Anthropic Messages API in a tool-use loop, supports user-defined skills/commands, persistent memories, persistent task plans, and MCP servers.

### Top-level layout

```
src/                       Go module root (module name: evo-agent)
  main.go                  Entry: flag parsing, config, MCP init, TUI vs plain dispatch
  internal/
    agent/                 LLM loop, context compaction, subagent
      loop.go              Loop() — agentic turn cycle
      subagent.go          RunSubagent() — child agent with fresh context
    config/                Env-var configuration (config.go)
    skills/                Skill + slash-command registry, dispatcher
      registry.go          Init(), InitCommands(), Catalog(), Load(), LookupForSlash()
      dispatch.go          Dispatch(input) — slash-command pipeline
      builtin.go           //go:embed of builtin_commands/*.md
    tools/                 Tool registry + all built-in tools
      tool.go              Register(), Dispatch(), Tools(), GenerateSchema[T]()
      executor.go          Execute() — iterates content blocks, persists large output
      bash.go read_file.go edit_file.go write_file.go
      compact.go skill.go todo.go task.go
      memory.go            remember + consolidate_memory tools, GlobalMemory
      plan.go              Persistent task plan tools (.evo-agent/tasks/)
      mcp.go               MCP transport (stdio / sse / streamableHttp)
    tui/                   Bubble Tea TUI (model, render, styles, sink)
    ui/                    Event types and output sinks
build/                     Compiled binary output
.evo-agent/                Per-project config root
  command/*.md             User-defined slash commands (flat)
  skill/<name>/SKILL.md    User-defined skills (nested)
  memory/                  Persistent memories (per-type .md files + MEMORY.md index)
  mcp.json                 MCP server config
  tool-results/<id>.txt    Persisted large tool output
.evo-agent/tasks/          Persistent task plans
  todo/<plan-name>/        Active plans (plan.md + task_N.json)
  done/<plan-name>/        Archived plans
Agent.md                   Optional project guidance, injected into system prompt
```

---

## 2. Startup Sequence (`main.go`)

```
config.Load()                              // SystemMsg = "You are a coding agent at <cwd>."
  ↓
read Agent.md (if exists)                  // append "# Project Guidance (Agent.md)\n\n" + body
  ↓
tools.InitMCP()                            // load .evo-agent/mcp.json, spawn/connect
  ↓
tools.GlobalMemory.Init(projectDir)        // scan .evo-agent/memory/, parse frontmatter
  ↓
tools.InitPlan(projectDir)                 // mkdir .evo-agent/tasks/todo .evo-agent/tasks/done
  ↓
append memPrompt   = GlobalMemory.LoadPrompt()
append            tools.MemoryGuidance     // const string about when/when-not to save
  ↓
skills.Init() / skills.InitCommands()      // walk .evo-agent/skill/**/SKILL.md and .evo-agent/command/*.md
append "Skills available:\n" + skills.Catalog() + load_skill instructions
append slash-commands intro (if SlashNames non-empty)
  ↓
agent.New(client, cfg, builder)            // registers subagent callback (see §6)
  ↓
TUI mode  → tui.Run()
Plain mode → plain REPL
```

`cfg.SystemMsg` is **fully built and immutable** after this point. Tool results, todo reminders, and live state are added to `state.Messages` (user messages), never to system prompt.

### System Prompt Sections (final order)

| # | Section | Source | Conditional |
|---|---------|--------|-------------|
| 1 | Base identity | `config.go:41` `"You are a coding agent at {cwd}."` | always |
| 2 | Project Guidance | `Agent.md` | only if file exists |
| 3 | Persistent Memories | `tools.GlobalMemory.LoadPrompt()` (grouped by type: user / feedback / project / reference) | only if any memories |
| 4 | Memory Guidance constant | `tools.MemoryGuidance` (when/when-not-to-save rules) | always |
| 5 | Skills Catalog | `skills.Catalog()` — bullet list, excludes `disable-model-invocation: true` | only if any model-invocable skills |
| 6 | Slash Commands intro | hard-coded explanation of `/<skill-name>` shorthand | only if `len(SlashNames) > 0` |

### Subagent prompt inheritance (`agent/subagent.go:24`)

```go
subSystem := a.cfg.SystemMsg + "\n" + systemPrompt
```

Subagents inherit the full parent prompt + a specialized task prompt. They run with `tools.ToolsExcept("task")` to prevent recursive spawning. Max 30 turns.

---

## 3. Agent Loop (`agent/loop.go`)

```
agent.Loop(state) {
  for {
    1. MicroCompact(state.Messages, KEEP_RECENT_RESULTS)   // truncate old tool-result blocks in-memory
       autoCompact() — full LLM summarization if context > 50k chars

    2. resp = client.Messages.New(System: cfg.SystemMsg, Messages: state.Messages,
                                  Tools: tools.Tools(), MaxTokens: 8000)
       state.Messages = append(state.Messages, resp)         // assistant turn

    3. toolResults = tools.Execute(resp.Content)             // §4
       — iterates blocks, emits ui.EvToolCall / EvToolResult
       — Dispatch(name, input) → handler (or DispatchMCP if mcp__-prefixed)
       — large output (>30k chars) persisted to .evo-agent/tool-results/<id>.txt
         and replaced with 2k-char preview placeholder

    4. usedTodo = (any tool_use block had Name == "todo")
       GlobalTodo.NoteRound(usedTodo)
       if reminder := GlobalTodo.Reminder(); reminder != "" {
         toolResults = append(toolResults, NewTextBlock(reminder))   // 3-round nudge
       }

    5. if model called the built-in `compact` tool → CompactHistory()
       — writes full transcript to .evo-agent/, rebuilds history as one summary message

    6. if no tool_use blocks → return                         // turn complete
       else state.Messages = append(state.Messages, NewUserMessage(toolResults...))
  }
}
```

### `LoopState` & `CompactState`

`CompactState` is carried on `LoopState` and persists for the lifetime of the REPL session (not reset between user prompts). It tracks: whether compaction has happened, the last summary, the FIFO-5 of recently-read files, total compaction count.

Auto-compact triggers at **`CONTEXT_LIMIT` (50 000 chars)** — see [`API_REFERENCE.md` › *internal/agent — compaction*](./API_REFERENCE.md) for exact field types and `EstimateContextSize` / `MicroCompact` / `CompactHistory` signatures. Full compact writes a JSONL transcript to `.evo-agent/transcripts/<RFC3339>.jsonl`, then rebuilds `state.Messages` as a single summary user message.

---

## 4. Tool System (`internal/tools/`)

### Self-registering pattern

Every tool file has an `init()` that calls `Register(ToolDef{...})`. No central registry list — packages auto-register when imported.

The four core entry points:

| API | Purpose |
|-----|---------|
| `Register(def ToolDef)` | Add a tool to the global `registry` map |
| `Tools()` | All registered schemas + `MCPTools()`, ready for the Anthropic API |
| `ToolsExcept(names...)` | Same as `Tools()` minus listed names — subagent uses this to strip `task` |
| `Dispatch(name, input)` | Route by name; `mcp__`-prefix → `DispatchMCP`, else handler lookup |

Exact signatures, return types, and the `Handler` / `ToolDef` definitions are in [`API_REFERENCE.md` › *internal/tools*](./API_REFERENCE.md).

### Schema generation

Auto-generated from Go struct tags:

```go
type TaskInput struct {
    Prompt      string `json:"prompt" jsonschema_description:"Task description"`
    Description string `json:"description" jsonschema_description:"UI summary"`
}

InputSchema: GenerateSchema[TaskInput]()
```

### Built-in tools

One-line summaries — for full input schemas (`BashInput`, `ReadFileInput`, etc.) and exact behaviour (timeout values, output caps, special cases) see [`API_REFERENCE.md` › *internal/tools*](./API_REFERENCE.md).

| Tool | File | Purpose |
|------|------|---------|
| `bash` | bash.go | Shell execution |
| `read_file` | read_file.go | Read with line-limit; tracks `RecentFiles` for compaction |
| `write_file` | write_file.go | Write with mkdir |
| `edit_file` | edit_file.go | First-occurrence exact-string replacement |
| `compact` | compact.go | Model-initiated full history summarization |
| `load_skill` | skill.go | Load full skill body at runtime |
| `todo_init` / `todo_create` / `todo_list` / `todo_get` / `todo_update` / `todo_complete` | todo.go | Session plan (max 12, exactly 1 in_progress) |
| `task` | task.go | Spawn subagent with fresh context |
| `remember` | memory.go | Spawn extraction subagent; reload memories after |
| `consolidate_memory` | memory.go | Spawn consolidation subagent |
| `plan_create` / `plan_list` / `plan_task_create` / `plan_task_update` / `plan_task_list` / `plan_task_get` / `plan_complete` | plan.go | Persistent task plans (see §7) |
| `bg_run` / `bg_check` / `bg_list` / `bg_cancel` | bgtask.go | Background tasks — long-running shell commands in their own goroutine + process group; results drained as `<background-results>` user message before each LLM call. See §7.5. |
| `mcp__<server>__<tool>` | mcp.go | Routed via `DispatchMCP` |

### Execute pipeline (`tools/executor.go`)

```go
func Execute(content []ContentBlockUnion) []ContentBlockParamUnion {
    for _, block := range content {
        case ToolUseBlock:
            output, err := Dispatch(block.Name, block.Input)
            output = PersistLargeOutput(block.ID, output)        // >30 KB → file
            results = append(results,
                NewToolResultBlock(block.ID, output, err != nil))
    }
    return results
}
```

### Adding a new tool

Create `src/internal/tools/<name>.go`:

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

Auto-discovered via package init().

---

## 5. Skills & Slash Commands (`internal/skills/`)

Skills and commands are Markdown files with YAML frontmatter. Skills live nested under `.evo-agent/skill/<name>/SKILL.md`; commands live flat under `.evo-agent/command/<name>.md`. **Built-in commands** (e.g., `/init`) are embedded in the binary via `//go:embed builtin_commands/*.md` and loaded last by `LoadBuiltinCommands()`. User commands with the same name override built-ins.

### File format

```yaml
---
name: my-skill
description: What this skill does
argument-hint: "[arg1] [arg2]"        # optional UI hint
arguments: arg1, arg2                  # optional named args (space- or comma-separated)
user-invocable: true                   # default true (false = model-only via load_skill)
disable-model-invocation: false        # default false (true = excluded from catalog, slash-only)
---

Skill body. Use $arg1, $ARGUMENTS, $ARGUMENTS[0], $0, etc.
```

### Frontmatter fields

| Field | Required | Default | Meaning |
|-------|----------|---------|---------|
| `name` | yes | filename | unique id |
| `description` | no | "No description" | shown in catalog/help |
| `argument-hint` | no | — | UI hint string |
| `arguments` | no | — | named args for `$name` substitution |
| `user-invocable` | no | true | user can `/foo` |
| `disable-model-invocation` | no | false | excluded from `Catalog()` |

### Loading

- `Init()` — `filepath.WalkDir(.evo-agent/skill)` → parse → `skillDocuments` map
- `InitCommands()` — `os.ReadDir(.evo-agent/command)` → parse → `commandDocuments` map; then `LoadBuiltinCommands()` overlays embedded
- `Catalog()` — formatted bullet list of model-invocable skills (filters `disable-model-invocation: true`)
- `Load(name)` — returns full skill body wrapped in XML tags (used by `load_skill` tool)

### Dispatch pipeline (`skills/dispatch.go`)

```
User: "/hello Alice"
  ↓ Validate: starts with "/" + letter (avoid file paths like /usr/bin)
  ↓ Parse: name="hello", rawArgs="Alice"
  ↓ LookupForSlash("hello")          // commands first, then skills
  ↓ Verify doc.Manifest.UserInvocable
  ↓ ParseArgs("Alice")               // shell-style quoting
  ↓ RenderBody(body, argNames, args, rawArgs)
  ↓ Wrap: <skill name="hello" source="slash" type="command">…</skill>
  ↓ Return SlashResult{Found, Prompt, Content, Name}

In agent goroutine (main.go):
  history = append(history,
    NewUserMessage(NewTextBlock(result.Prompt), NewTextBlock(result.Content)))
  a.RunQuery(&history, …)            // history already updated by caller
```

### Argument substitution precedence (`render.go`)

1. `$ARGUMENTS[N]` → `args[N]`
2. `$<name>` → named arg (when `arguments:` declares them)
3. `$N` → positional shorthand
4. `$ARGUMENTS` → full raw string
5. **Fallback**: if no placeholder, append `\nARGUMENTS: <rawArgs>` to body

### Priority rules

- Commands take priority over skills with the same name in `LookupForSlash`
- `Catalog()` excludes skills where `disable-model-invocation: true`
- Default: skills are model-invocable AND user-invocable; commands are user-only (not in catalog)

### Adding a skill / command

```bash
# Skill (in catalog, model can load_skill)
mkdir -p .evo-agent/skill/my-skill
cat > .evo-agent/skill/my-skill/SKILL.md <<'EOF'
---
name: my-skill
description: Does X
---
Step 1: …
EOF

# Command (slash-only, not in catalog)
cat > .evo-agent/command/my-cmd.md <<'EOF'
---
name: my-cmd
arguments: target
---
Do something to $target.
EOF
```

Restart agent — auto-discovered.

---

## 6. Subagent (`agent/subagent.go` + `tools/task.go`)

### Why a callback?

`agent` imports `tools`, so `tools/task.go` cannot import `agent`. The pattern:

```go
// tools/task.go
var subagentRunner func(systemPrompt string, messages []anthropic.MessageParam) string

func RegisterSubagentRunner(fn func(string, []anthropic.MessageParam) string) {
    subagentRunner = fn
}

// task tool handler
Handler: func(input json.RawMessage) (string, error) {
    var in TaskInput
    json.Unmarshal(input, &in)
    if subagentRunner == nil {
        return "Error: subagent runner not initialized", nil
    }
    sysPrompt := "You are a subagent. Complete the given task..."
    msgs := []anthropic.MessageParam{
        anthropic.NewUserMessage(anthropic.NewTextBlock(in.Prompt)),
    }
    return subagentRunner(sysPrompt, msgs), nil
}

// agent.New() (called from main after tools loaded)
tools.RegisterSubagentRunner(a.RunSubagent)
```

The same pattern is used by `GlobalTodo`, `GlobalMemory`, `GlobalPlan`.

### `Agent.RunSubagent`

```
subSystem = cfg.SystemMsg + "\n" + systemPrompt
childTools = tools.ToolsExcept("task")              // strip task to prevent recursion

for turn := 0; turn < 30; turn++ {
    resp = client.Messages.New(System: subSystem, Messages: subMessages,
                               Tools: childTools, MaxTokens: 8000)
    capture last text block as lastText
    dispatch tool_use blocks → toolResults
    if no tool_use → break
    subMessages = append(subMessages, NewUserMessage(toolResults...))
}
return lastText      // only the final summary returns; child history GC'd
```

### Memory subagents

`memory.go` builds two specialized prompts and reuses `subagentRunner`:

- `buildExtractionPrompt(memDir, existing)` — used by `remember` tool. Receives conversation history; subagent reads/writes/edits memory files. After completion, `GlobalMemory.LoadAll()` reloads.
- `buildConsolidatePrompt(...)` — used by `consolidate_memory` tool.

---

## 7. Persistent Task System (`tools/plan.go`)

Durable, file-based task management that survives context compression and session restarts.

### Layout

```
.evo-agent/tasks/
  todo/
    2026-05-28-add-auth/
      plan.md             # requirements + approach + steps
      task_1.json
      task_2.json
  done/
    2026-05-20-fix-login/
      plan.md
      task_*.json
```

Plan name convention: `YYYY-MM-DD-description` (chronological + descriptive).

### Task record

```json
{
  "id": 1,
  "subject": "Design auth schema",
  "description": "Create migration for users table",
  "status": "pending",
  "blockedBy": [],
  "blocks": [2, 3],
  "owner": ""
}
```

Status: `pending` | `in_progress` | `completed` | `deleted`.

### Tools

| Tool | Description |
|------|-------------|
| `plan_create` | Create plan dir + plan.md |
| `plan_list` | List active + completed plans with progress |
| `plan_task_create` | Add task; supports `blockedBy: [...]` |
| `plan_task_update` | Change status/owner/deps |
| `plan_task_list` | List tasks in a plan |
| `plan_task_get` | Full details of one task |
| `plan_complete` | Move plan from `todo/` to `done/` (requires all tasks completed/deleted) |

### Dependency semantics

- Setting `blockedBy: [1]` on task 2 also adds `2` to task 1's `blocks` list (bidirectional).
- Marking task 1 `completed` removes `1` from every other task's `blockedBy`. Newly empty `blockedBy` = ready.
- Thread-safe via `sync.RWMutex`.

### Initialization

`tools.InitPlan(cfg.ProjectDir)` creates `.evo-agent/tasks/todo/` and `.evo-agent/tasks/done/` if missing.

### Session-scoped vs persistent

| Aspect | `todo` (session) | `plan_*` (persistent) |
|--------|------------------|------------------------|
| Storage | in-memory `GlobalTodo` | files in `.evo-agent/tasks/` |
| Lifetime | one session | survives restarts/compaction |
| Max items | 12 | unlimited |
| Status | adds `cancelled` | adds `deleted` |
| Reminder | 3-round stale-check | none |
| Sync to TUI | yes (`ui.EvTodo`) | no |

---

## 7.5 Background Tasks (`tools/bgtask.go`)

Long-running shell commands run asynchronously in their own goroutine + process group. Pattern adapted from `refs/ref.py:BackgroundManager`. Storage scoped to the active session so `--resume` can list historical tasks; cancel and timeout move the task directory atomically from `todo/` to `done/` via `os.Rename`.

### Layout

```
.evo-agent/sessions/<sessID>/runtime-tasks/
  todo/<taskID>/                     # 8 hex chars
    task.json                        # metadata
    output.log                       # stdout + stderr (capped at 50 KB)
  done/<taskID>/                     # completed | timeout | error | cancelled
    task.json
    output.log
```

`task.json` fields: `id`, `command`, `status`, `started_at_ms`, `finished_at_ms`, `exit_code`, `preview` (≤500 chars), `output_file` (relative to session dir).

### Tools

| Tool | Description |
|------|-------------|
| `bg_run` | Start a command in the background; returns `Background task <id> started: …` |
| `bg_check` | Inspect one task by id (full JSON record); empty `task_id` lists all |
| `bg_list` | Compact list of every known task |
| `bg_cancel` | SIGKILL the process group, archive as `cancelled` |

### Notification injection

At the top of each agent-loop iteration (after `autoCompact`, before `SendMessage`), `tools.GlobalBgTasks.DrainNotifications()` pulls all completion events into a `<background-results>...</background-results>` user message. This lands in the conversation history (and the session transcript via `Recorder.AppendUser`) so the model sees outcomes without polling.

### Slash command

`/bgtask` is intercepted client-side in `agent/repl.go:handleTurn` (never drives an LLM turn):

```
/bgtask                  list every task
/bgtask <id>             show one task's full record
/bgtask cancel <id>      kill + archive a running task
```

### Initialization

`tools.SetSessionContext(sess.Dir, sess.ID)` + `tools.GlobalBgTasks.Init(sess.Dir, sess.ID)` are called from `main.go` immediately after `a.AttachSession(sess)`. `Init` mkdirs `todo/`+`done/`, rehydrates known tasks from disk, and downgrades any leftover `running` records (left behind by a crashed run) to `error`.

### Status bar

The TUI status bar always shows `bg: N run / M done` (even at 0/0). Updates flow through `ui.EmitBgTasks(running, completed)` → `Event{Kind: EvBgTasks}` → TUI `model.handleAgentEvent` writes `m.info.BgRunning/BgCompleted`.

---

## 8. Memory System (`tools/memory.go`)

### Storage

```
.evo-agent/memory/
  MEMORY.md               # index, ≤200 lines, one line per memory: "- [name](file.md) — hint"
  <name>.md               # individual memory with frontmatter
```

### Memory file format

```yaml
---
name: my-memory
description: One-line hook (≤150 chars)
type: user | feedback | project | reference
---

Memory body content.
```

### Types

| Type | When | Example |
|------|------|---------|
| `user` | role / preferences / knowledge | "User is Go expert, learning Rust" |
| `feedback` | corrections + confirmations | "Always use -v flag for debugging" |
| `project` | non-obvious project facts | "Auth rewrite driven by compliance" |
| `reference` | external resource pointers | "OnCall dashboard: grafana.internal/d/X" |

### When NOT to save

- Anything derivable from code
- Temporary task state
- Secrets or credentials
- Git history / recent changes
- Debugging solutions

### LoadPrompt format

`GlobalMemory.LoadPrompt()` returns memories grouped by type:

```
# Memories (persistent across sessions)

## user
### name: description
content...

## feedback
…
```

### Index constraints (`MEMORY.md`)

- Max **200 lines** (soft limit; truncation warning injected via prompt at memory.go:333)
- One line per memory, ~150 chars max
- Index-only — never write memory **content** into MEMORY.md
- Updated by the `remember` subagent, not the parent agent

### Lifecycle

1. Agent calls `remember` (or user invokes `/remember`)
2. `subagentRunner` invoked with `buildExtractionPrompt(memDir, existing)`
3. Subagent uses read_file/write_file/edit_file to update `.evo-agent/memory/`
4. Parent calls `GlobalMemory.LoadAll()` → in-memory cache refreshed
5. Memories appear in **next session's** system prompt (current prompt is immutable)

---

## 9. UI Event Bus (`internal/ui/`) & TUI (`internal/tui/`)

### EventSink

```go
type EventSink interface {
    Emit(Event)
}

var globalSink EventSink   // swapped at startup
```

- **TUI mode**: `tui.Sink` — buffered channel, non-blocking drop on full
- **Plain mode**: `TerminalSink` — writes ANSI directly to stdout

### `ui.Event` kinds

`EvThinking`, `EvText`, `EvToolCall`, `EvToolResult`, `EvSystem`, `EvTokens`, `EvDone`, `EvTodo`.

### TUI architecture

`tui.Run()` creates a `Sink`, calls `ui.SetSink(sink)`, starts Bubble Tea. Two channels bridge agent goroutine and UI:

```
queryCh   chan string         // user input  (TUI → agent)
sink.Chan() <-chan ui.Event   // agent output (agent → TUI)
```

`Model.View()` renders only the **live interactive bottom area** (pending tool calls, todo panel, spinner, input, status bar). Completed conversation content is permanently committed to the terminal scroll buffer via `tea.Println`.

---

## 10. MCP Integration (`tools/mcp.go`)

`tools.InitMCP()` reads `.evo-agent/mcp.json` at startup. Supports three transports:

| Transport | Description |
|-----------|-------------|
| `stdio` | Subprocess; line-delimited JSON-RPC over pipes |
| `sse` | Persistent GET (event stream) + POST (requests); background goroutine for routing |
| `streamableHttp` | Stateless POST per request; response auto-detected as JSON or SSE |

Tool names are prefixed `mcp__{server}__{tool}`. `tools.Dispatch` routes any `mcp__`-prefixed name to `DispatchMCP`. Missing `.evo-agent/mcp.json` is silently ignored. Disabled servers (`disabled: true` in config) are skipped at startup.

For the exact `MCPServerConfig` / `MCPConfig` JSON schema, the `mcpClient` interface, and `MCPTools()` / `DispatchMCP()` signatures see [`API_REFERENCE.md` › *internal/tools — MCP*](./API_REFERENCE.md).

---

## 11. Configuration (`internal/config/config.go`)

`Config` carries `ModelID` (required), `APIKey`, `BaseURL`, the dynamically-built `SystemMsg`, and the LLM-provider switches `ProviderID` / `OpenAIAPIKey` / `OpenAIBaseURL`. `config.LoadEnv()` reads `.env` files in two passes: first from the binary directory (e.g. `build/.env`), then from the cwd (overrides the first). `config.Load()` then reads env vars and returns a populated `*Config` — see [`API_REFERENCE.md` › *internal/config*](./API_REFERENCE.md) for the field table.

| Env var | Field | Required | Meaning |
|---------|-------|----------|---------|
| `MODEL_ID` | `ModelID` | yes | Passed through unchanged to the active provider (e.g. `claude-sonnet-4-5`, `gpt-4o-mini`). |
| `PROVIDER_ID` | `ProviderID` | no (default `anthropic`) | `anthropic` → Anthropic Messages API. `openai` → OpenAI Chat Completions (and any compatible gateway). Anything else is rejected at startup. |
| `ANTHROPIC_API_KEY` | `APIKey` | when `PROVIDER_ID=anthropic` and the endpoint enforces auth | Falls back to literal `"dummy"` so requests against permissive proxies still work. |
| `ANTHROPIC_BASE_URL` | `BaseURL` | no | Custom Anthropic-compatible endpoint. Setting this also unsets `ANTHROPIC_AUTH_TOKEN` so the explicit API key wins. |
| `OPENAI_API_KEY` | `OpenAIAPIKey` | when `PROVIDER_ID=openai` | Used as `Authorization: Bearer …`. Validated by `llm.New` — empty key fails fast. |
| `OPENAI_BASE_URL` | `OpenAIBaseURL` | no (default `https://api.openai.com`) | Override for OpenAI-compatible providers (DeepSeek, Qwen, OpenRouter, Ollama, …). The adapter posts to `<base>/v1/chat/completions`. |

---

## 11.7 LLM Provider Abstraction (`internal/llm/`)

Single boundary that lets the agent talk to either the Anthropic Messages API or the OpenAI Chat Completions API while keeping `anthropic.MessageNewParams` / `*anthropic.Message` as the canonical internal types. **Translation is runtime-only** — session JSONL on disk continues to be stored in anthropic shape regardless of the active provider.

### Files

| File | Role |
|------|------|
| `internal/llm/provider.go` | `Provider` interface, `Config` struct, `New(cfg) (Provider, error)` factory. |
| `internal/llm/anthropic.go` | `anthropicProvider` — thin pass-through to the official `anthropic-sdk-go`; behaviour byte-identical to the pre-refactor flow. |
| `internal/llm/openai.go` | `openaiProvider` — owns its own `*http.Client`, posts to `<base>/v1/chat/completions`, surfaces non-2xx bodies verbatim. |
| `internal/llm/wire.go` | Private OpenAI request/response structs. |
| `internal/llm/translate.go` | Pure functions `paramsToOpenAI`, `openAIToMessage`, `mapFinishReason`. |
| `internal/llm/translate_test.go` | Table-driven mappings (both directions) including the critical `ToParam()` round-trip assertion. |
| `internal/llm/openai_test.go` | `httptest.NewServer`-based end-to-end test. |

### Provider interface

```go
type Provider interface {
    SendMessage(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error)
}
```

Returning `*anthropic.Message` (not a custom canonical type) means the four LLM call sites — `agent/loop.go:140`, `agent/subagent.go:69`, `agent/compact.go` (`SummarizeHistory`/`CompactHistory`), `goal/evaluator_llm.go` — keep using `resp.ToParam()`, `resp.Content` walking via `block.AsAny()`, and `resp.Usage` / `resp.StopReason` unchanged.

### Synthesis trick (critical)

The OpenAI adapter cannot field-fill `*anthropic.Message`: the SDK's `ContentBlockUnion.AsAny()` and `Message.ToParam()` both rely on the unexported `JSON.raw` field that only `apijson.UnmarshalRoot` populates. Field-filling silently produces zero-valued blocks and corrupts history within one round-trip.

The adapter therefore **JSON-marshals an Anthropic-shaped `map[string]any` and feeds it to `(*anthropic.Message).UnmarshalJSON`**, letting the SDK rebuild every internal cache as if the bytes had come off the wire from the real API. Cost is one extra marshal/unmarshal per LLM turn. See `openAIToMessage` in `translate.go`.

### Anthropic ↔ OpenAI translation table (high-level)

| Anthropic (`MessageNewParams`) | OpenAI (`/v1/chat/completions`) |
|---|---|
| `Model` | `model` |
| `MaxTokens` | `max_tokens` |
| `System []TextBlockParam` | leading `{role:"system", content:<joined with \n\n>}` |
| user msg with `OfText` | `{role:"user", content}` |
| user msg with multiple `OfToolResult` | one `{role:"tool", tool_call_id, content}` per result, encounter order |
| user msg with `OfToolResult` + trailing `OfText` (reminder) | tool messages first, then `{role:"user", content:<reminder>}` |
| assistant `OfText` + `OfToolUse` | `{role:"assistant", content, tool_calls:[…]}` |
| `OfThinking` / `OfRedactedThinking` | dropped silently |
| `Tools[i].OfTool` | `{type:"function", function:{name, description, parameters:<input_schema>}}` |
| non-`OfTool` tool variants (server tools, web search, code exec) | dropped silently |
| `CacheControl` | dropped silently |
| `Temperature`, `TopP`, `StopSequences` | passthrough |
| `TopK`, `ToolChoice` (when unset) | dropped |

| OpenAI response | Anthropic |
|---|---|
| `choices[0].message.content` | one `TextBlock` (when non-empty) |
| `choices[0].message.refusal` | text block + `StopReasonRefusal` |
| `choices[0].message.tool_calls[]` | one `ToolUseBlock` per call; `arguments` JSON-string parsed back into `Input` |
| `choices[0].finish_reason` | `stop`→`end_turn`, `tool_calls`/`function_call`→`tool_use`, `length`→`max_tokens`, `content_filter`→`refusal`, else→`end_turn` |
| `usage.prompt_tokens` / `completion_tokens` | `Usage.InputTokens` / `OutputTokens` |
| empty content + no tool_calls | one empty text block (Anthropic shape requires non-empty `Content`) |

For exact field rules and edge cases see `internal/llm/translate.go` and the table-driven tests in `translate_test.go` / `openai_test.go`.

### Plumbing across the agent

| Site | Before | After |
|---|---|---|
| `Agent` struct | `client *anthropic.Client` | `provider llm.Provider` |
| `agent.New` signature | `New(client *anthropic.Client, …)` | `New(provider llm.Provider, …)` |
| `agent/loop.go:140` | `a.client.Messages.New(…)` | `a.provider.SendMessage(…)` |
| `agent/subagent.go:69` | same | same |
| `agent/compact.go` (`SummarizeHistory`, `CompactHistory`) | first arg `*anthropic.Client` | first arg `llm.Provider` |
| `goal/evaluator_llm.go` (`RunEvaluator`) | `client *anthropic.Client` | `provider llm.Provider` |
| `main.go` | `client := anthropic.NewClient(BuildOptions(cfg)...)` | `provider, err := llm.New(llm.Config{…})` |

`BuildOptions` was deleted; its body now lives inside `internal/llm/anthropic.go:newAnthropicProvider` verbatim.

---

## 11.5 Session Persistence (`internal/session/`)

Append-only JSONL transcripts under `.evo-agent/sessions/{ts}-{UUID}/` survive process exit and power `/resume`. Full design in [`session-persistence.md`](./session-persistence.md).

Key points:

- **Per-session directory**: `.evo-agent/sessions/<unix_ms>_<8 hex>/` (e.g. `1780227556183_b525857d`) holding `messages.jsonl` (event stream), `meta.json` (cumulative tokens + first prompt for the picker), `subagent/<unix_ms>_<name>_<8 hex>.jsonl` (sidechain transcripts). Lexical order = chronological order; `_` separates the numeric ms prefix from the UUID suffix.
- **Record envelope**: every JSONL line carries `type`, `timestamp` (ISO-8601 local wall-clock with numeric offset, ms precision, e.g. `"2026-05-31T19:58:42.705+08:00"`), `agent_version`, `session_id`, `cwd`, `prompt_id`, `git_branch`, plus type-specific fields.
- **Record types**: `session_start`, `user`, `assistant`, `compact_boundary`, `resume_marker`, `subagent_start`, `subagent_end` — same envelope (timestamp, agent_version, session_id, cwd, prompt_id, git_branch), type-specific fields.
- **Recovery rule**: `LoadForResume` finds the **last** `compact_boundary`, drops every user/assistant record before it, prepends a synthetic user message wrapping the boundary's `summary` in `<previous-conversation-summary>` tags, and surfaces each post-boundary `subagent_end.result` as a `<subagent-result>` user block.
- **Entry points**: `evo-agent --resume <id>` (CLI), `/resume` (TUI dropdown picker, sorted newest first with token totals + first prompt), `/resume <id>` (inline, intercepted client-side). Exit always prints `Resume this session with: evo-agent --resume <id>`.
- **Hooks**: `agent.LoopState.{Recorder,PromptID}` carry the recorder per turn; `agent.CompactHistory(... recorder, promptID)` writes the boundary; `tools.RegisterNamedSubagentRunner` lets the task tool's `description` label subagent files.

---

## 11.6 `/goal` Command (`internal/goal/`)

Session-scoped completion condition that drives the loop to keep working until met. Inspired by Claude Code's `/goal` and Codex CLI's `set_goal()`.

### Lifecycle

```
/goal <condition>         → goal.Global.Set(...) + tools.GlobalPlan.CreateForGoal(...) + driving turn
/goal                     → emit EvGoal{status} (no LLM call)
/goal clear|stop|off|...  → goal.Global.Clear() + AppendGoalCleared
agent.Loop() (no tool_use)
  └─ a.maybeContinueForGoal(state):
       Snapshot active goal → goal.RunEvaluator(client, ModelID, ...)
       Met=true  → goal.Global.Achieve() + AppendGoalAchieved + EvGoal{achieved} + return false
       Met=false → append <goal-reminder> user msg + IncIter + EvGoal{continuing} + return true
       Iter≥Max  → EvGoal{capped} + Clear + return false
```

### Files

| File | Purpose |
|------|---------|
| `internal/goal/goal.go` | `Manager`, `State`, `Global` singleton (mirrors `tools/todo.go:GlobalTodo` pattern) |
| `internal/goal/evaluator.go` | `ParseVerdict`, `BuildEvalRequest`, `ContinuationPrompt` (pure, unit-testable) |
| `internal/goal/evaluator_llm.go` | `RunEvaluator(ctx, client, modelID, goalText, msgs) Verdict` — wraps `client.Messages.New` |
| `internal/agent/goal.go` | `(a *Agent) maybeContinueForGoal(state) bool` — sole interception point |
| `internal/agent/goalcmd.go` | `ParseGoalCmd`, `(a *Agent) HandleGoalCmd(...)` — shared client-side dispatch |
| `internal/skills/builtin_commands/goal.md` | help-list manifest (dispatch is intercepted client-side; this exists for `/help`) |

### Persistence

- New record types in `session/record.go`: `TypeGoalSet`, `TypeGoalCleared`, `TypeGoalAchieved` carrying `GoalText`, `GoalReason`, `GoalPlanName`.
- New recorder methods: `AppendGoalSet`, `AppendGoalCleared`, `AppendGoalAchieved`.
- `LoadResult.Goal *RestoredGoal` populated by `loader.go`'s second-pass scan over **all** records (goal survives `compact_boundary`). On `--resume`, `main.go` calls `goal.Global.Set(text, planName)` so the iter counter resets.

### System-prompt integration

`prompt.GoalProvider` interface (mirrors `PlanProvider`); `goal.Global` satisfies it. `BuildSections` injects `<active-goal>...</active-goal>` after `buildPlanStatus()` so the model sees the goal every turn — no message-history pollution.

### UI

`ui.EvGoal` with `GoalKind ∈ {set, cleared, achieved, evaluating, continuing, capped, status}`. `TerminalSink` prints one-line status; TUI maintains `goalActive/goalText/goalIter` fields and renders a `◎ /goal active · iter N/30 · …` indicator above the input plus a `goal:<text>` chip in the status bar.

### Plan integration

`/goal <text>` auto-creates a persistent plan at `.evo-agent/tasks/todo/YYYY-MM-DD-<slug>/` via `tools.GlobalPlan.CreateForGoal(name, goalText, approach)`. The continuation prompt embeds `tools.GlobalPlan.StartupSummary()` so each evaluator-driven turn carries plan context.

### Constants

| Value | Where | Meaning |
|-------|-------|---------|
| `30` | `goal.DefaultMaxIter` | evaluator-driven continuation cap (aligned with `subagentMaxTurns`) |
| `6` | `goal.EvalRecentTurns` | trailing messages excerpted for the evaluator |
| `256` | `goal.evaluatorMaxTokens` | `MaxTokens` for the evaluator LLM call |
| `2000` | `goal/evaluator.go` | per-block transcript truncation cap |

---

## 12. Constants & Limits

| Value | Identifier | Where | Meaning |
|-------|------------|-------|---------|
| `50000` | `CONTEXT_LIMIT` | agent/loop.go | Auto-compact trigger (estimated chars) |
| `3` | `KEEP_RECENT_RESULTS` | agent/loop.go | Tool results kept intact by `MicroCompact` |
| `80000` | `maxConversationBytes` | agent/loop.go | Max bytes passed to summarisation LLM |
| `30` | `subagentMaxTurns` | agent/subagent.go | Subagent hard turn cap |
| `8000` | (literal) | loop.go, subagent.go | `MaxTokens` per LLM call |
| `30000` | `persistThreshold` | tools/executor.go | Large-output threshold → `.evo-agent/tool-results/<id>.txt` |
| `2000` | (literal) | tools/executor.go | Preview-placeholder length for persisted output |
| `12` | (literal) | tools/todo.go | Max session-todo items |
| `3` | (literal) | tools/todo.go | Rounds without `todo` tool use → reminder injection |
| `200` | `maxIndexLines` | tools/memory.go | MEMORY.md soft line cap |
| `150` | (soft) | tools/memory.go | Per-line cap for MEMORY.md entries |
| `120 s` | (literal) | tools/bash.go | Bash timeout |
| `50 000` chars | (literal) | tools/bash.go, read_file.go | Bash + read_file output cap |
| `200000` | (literal) | — | Claude API context window (informational) |
| `300 s` | `bgTaskTimeout` | tools/bgtask.go | Background task per-task timeout |
| `50 000` chars | `bgTaskOutputCap` | tools/bgtask.go | Bytes of stdout+stderr captured into `output.log` |
| `500` chars | `bgTaskPreviewCap` | tools/bgtask.go | Preview retained on the JSON record + status views |
| `8` hex | derived from `bgTaskIDByteLen=4` | tools/bgtask.go | Background task ID length |

---

## 13. File Map (line-number index)

| Component | File | Lines | Notes |
|-----------|------|-------|-------|
| Config struct + base prompt | `internal/config/config.go` | 12-18, 41 | `SystemMsg` field; "You are a coding agent at …" |
| Main initialization | `main.go` | 45-86 | Sequential injection into `cfg.SystemMsg` |
| Slash-command dispatch in TUI | `main.go` | 114-144 | Two-block message → `RunQuery` |
| Agent loop | `internal/agent/loop.go` | 88-165 | `Loop()`; system prompt at line 101 |
| Tool result reminder injection | `internal/agent/loop.go` | 143-156 | Todo reminder as user text block |
| Subagent | `internal/agent/subagent.go` | 19-83 | Combined prompt at line 24 |
| Memory loading | `internal/tools/memory.go` | 55-242 | `Init()`, `LoadAll()`, `LoadPrompt()` |
| Memory extraction prompt | `internal/tools/memory.go` | 246-346 | `buildExtractionPrompt` |
| Memory consolidation prompt | `internal/tools/memory.go` | 350-376 | `buildConsolidatePrompt` |
| Memory guidance const | `internal/tools/memory.go` | 24-42 | `MemoryGuidance` |
| Skills loading | `internal/skills/registry.go` | 39-99, 104-129 | `Init()`, `Catalog()` |
| Slash dispatcher | `internal/skills/dispatch.go` | 18-88 | `Dispatch(input)` → `SlashResult` |
| Tool registry | `internal/tools/tool.go` | 11-50 | `Register`, `Tools`, `Dispatch`, `ToolsExcept`, `GenerateSchema[T]` |
| Tool executor | `internal/tools/executor.go` | — | `Execute()`, large-output persistence |

---

## 14. Common Tasks Cheat Sheet

### Add a tool
→ §4 "Adding a new tool". Drop a file in `src/internal/tools/`, restart.

### Add a skill
→ §5 "Adding a skill / command". Drop SKILL.md in `.evo-agent/skill/<name>/`.

### Add a slash command
→ §5. Drop `.md` in `.evo-agent/command/`.

### Add a builtin command (shipped in binary)
→ Drop `.md` in `internal/skills/builtin_commands/`. Loaded via `//go:embed`. User overrides take priority.

### Modify the system prompt
→ Edit `main.go:45-86`. Keep order in §2 in mind. To add a new dynamic section, follow the pattern `cfg.SystemMsg += "\n\n# …\n" + body`.

### Add a new persistent task
→ Use `plan_create` then `plan_task_create`. Manual: `.evo-agent/tasks/todo/<plan>/task_N.json`.

### Make a memory persist
→ Call `remember` tool (or user invokes `/remember`) — extraction subagent writes to `.evo-agent/memory/`. Re-load happens automatically; visible in **next** session's system prompt.

### Add an MCP server
→ Edit `.evo-agent/mcp.json`. Schema in [`API_REFERENCE.md` › *internal/tools — MCP*](./API_REFERENCE.md). Three transports: `stdio` (Command/Args/Env), `sse`/`streamableHttp` (URL/Headers).

---

## 15. Debugging Tips

- **System prompt content**: print `cfg.SystemMsg` after `main.go:86` and inspect.
- **Skill not in catalog**: check `disable-model-invocation` in frontmatter; check `Init()` logs for parse errors.
- **Tool not dispatched**: confirm `init()` ran (package must be imported transitively); search for `Register(ToolDef{Name: "<name>"`).
- **Subagent never spawns**: ensure `agent.New()` runs after the tools package is loaded — `RegisterSubagentRunner` must be called before any `task`/`remember` invocation.
- **Memory write didn't appear**: memories are visible only in the **next** session's prompt. Inspect `.evo-agent/memory/MEMORY.md` and individual files to confirm subagent wrote them.
- **MEMORY.md > 200 lines**: triggers truncation warning in extraction prompt; consolidation needed.
- **Tool output truncated**: check `.evo-agent/tool-results/<tool_use_id>.txt` for full content (large-output persistence).
- **Auto-compact not triggering**: `EstimateContextSize` is a char-count heuristic; threshold is 50 000.

---

## 16. Related Files

| File | Role | When to read |
|------|------|--------------|
| `CLAUDE.md` (project root) | Claude Code-specific guidance | Working in Claude Code |
| `Agent.md` (project root, optional) | Injected into system prompt §2 | Editing project guidance |
| [`API_REFERENCE.md`](./API_REFERENCE.md) | Exact Go signatures, struct field tables, tool input schemas, ANSI color constants | Implementing or calling an API; verifying a constant value |
| [`anthropic-sdk-go.md`](./anthropic-sdk-go.md) | Vendored Anthropic SDK reference | SDK call details |
| [`skills.md`](./skills.md) | External skills format spec | Authoring skills |
