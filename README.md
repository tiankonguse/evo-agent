# evo-agent

A step-by-step journey to implement an AI agent in Go.

Evo-Agent is a lightweight, tool-augmented AI agent written in Go. It leverages the Anthropic API to perform tasks in a local workspace through a ReAct (Reason + Act) loop — the agent reasons, calls tools, observes results, and repeats until the task is complete.

## Features

- **Multi-tool Support**: bash, read_file, write_file, edit_file, compact, load_skill — all self-registering via `init()`
- **Self-registering Tool Pattern**: Adding a new tool only requires a single new file; no central registration needed
- **Table-driven Dispatch**: A global registry maps tool names to schemas and handlers
- **Multi-turn Reasoning**: Drives a loop of thought → action → observation until the model stops requesting tool calls
- **Context Compaction**: Three-layer strategy (placeholder micro-compact → LLM summarization → model-initiated compact) to handle unlimited-length sessions
- **MCP Client Support**: Connect to external MCP tool servers via `stdio`, `sse`, or `streamableHttp` transports; config loaded from `.evo_agent/mcp.json`
- **Skill System**: Two-layer on-demand knowledge — cheap catalog injected into system prompt; full skill body loaded only when needed via `load_skill`
- **Colored CLI**: Clear terminal output distinguishing thinking, tool calls, responses, and errors

## Project Structure

```
src/
├── main.go                    # Entry point: input loop, history management, MCP init/shutdown, skill catalog
├── go.mod
└── internal/
    ├── agent/
    │   ├── loop.go            # Agent struct, RunOneTurn, Loop, Run (REPL)
    │   ├── state.go           # LoopState, CompactState
    │   ├── compact.go         # MicroCompact, CompactHistory, SummarizeHistory, TrackRecentFile
    │   └── transcripts.go     # WriteTranscript: save full history to .evo_agent/transcripts/
    ├── config/
    │   └── config.go          # Config struct, LoadEnv, Load
    ├── skills/
    │   └── registry.go        # SkillManifest, Init, Catalog, Load — two-layer skill system
    ├── tools/
    │   ├── tool.go            # ToolDef registry, Register, Tools, Dispatch, GenerateSchema
    │   ├── executor.go        # Execute: iterate content blocks, run tool calls
    │   ├── mcp.go             # MCP client: stdio / sse / streamableHttp transports, InitMCP, ShutdownMCP
    │   ├── bash.go            # bash tool (run shell commands, 120s timeout)
    │   ├── read_file.go       # read_file tool (read file with optional line limit)
    │   ├── write_file.go      # write_file tool (write/create file with mkdir -p)
    │   ├── edit_file.go       # edit_file tool (exact-string replacement or create)
    │   ├── compact.go         # compact tool (model-initiated context compaction)
    │   └── skill.go           # load_skill tool (load full skill body on demand)
    └── ui/
        └── terminal.go        # ANSI color helpers for terminal output
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
cd src && go run main.go
```

## Usage

After starting the agent, type your request at the prompt:

```
>> list all Go files in this workspace
>> read the file src/main.go and summarize it
>> create a new file hello.go with a Hello World program
>> exit
```

Type `q` or `exit` to quit.

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

### MCP Tools

MCP tools are loaded automatically at startup from `.evo_agent/mcp.json`. Each tool is exposed to the model with a prefixed name: `mcp__{server}__{tool}`.

Configure servers in `.evo_agent/mcp.json`:

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

Skill files live under `.evo_agent/skill/`:

```
.evo_agent/skill/
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

The `load_skill` output includes the absolute path to `SKILL.md` so the model can reference sibling files in the same directory:

```xml
<skill name="git-commit" path="/workspace/.evo_agent/skill/git-commit/SKILL.md">
...body...
</skill>
```

## Adding a New Tool

1. Create `src/internal/tools/<name>.go`
2. Define an input struct with `jsonschema_description` tags
3. Call `Register(ToolDef{...})` inside an `init()` function

That's it — the tool is automatically available to the agent on next run.

## Blog

| Article | Description |
|---------|-------------|
| [01-loop](blog/01-loop.md) | ReAct Loop — how the agent thinks, acts, and observes in a cycle |
| [02-tools](blog/02-tools.md) | Tools — self-registering tool pattern and table-driven dispatch |
| [03-prompts](blog/03-prompts.md) | Prompts & Context — system prompt, messages history, and the two-layer loop |
| [04-compact](blog/04-compact.md) | Context Compaction — three-layer strategy for unlimited-length sessions |

## Version History

| Version | Description |
|---------|-------------|
| **v0.6.0** | Add two-layer skill system: `internal/skills` package (`Init`, `Catalog`, `Load`); `load_skill` tool in `tools/skill.go`; skill catalog auto-injected into system prompt; skills stored in `.evo_agent/skill/<name>/SKILL.md` |
| **v0.5.0** | Add MCP client support: `stdio`, `sse`, and `streamableHttp` transports; config from `.evo_agent/mcp.json`; `InitMCP`/`ShutdownMCP` in `main.go`; MCP tools auto-merged into `Tools()` and routed in `Dispatch()` |
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
