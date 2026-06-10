# API Reference

## internal/agent

### `type LoopState struct`

Holds the mutable state of a single agent run.

```go
type LoopState struct {
    Messages         []anthropic.MessageParam
    TurnCount        int
    TransitionReason string
    CompactState     *CompactState
}
```

| Field              | Type                         | Description                                              |
|--------------------|------------------------------|----------------------------------------------------------|
| `Messages`         | `[]anthropic.MessageParam`   | Full conversation history (user + assistant turns)       |
| `TurnCount`        | `int`                        | Number of tool-call turns that have occurred             |
| `TransitionReason` | `string`                     | `"tool_result"` if loop continued, `""` when done        |
| `CompactState`     | `*CompactState`              | Compaction state shared across the whole REPL session    |

---

### `type CompactState struct`

Tracks context compaction across the session. Persists for the lifetime of the REPL (not reset between user prompts).

```go
type CompactState struct {
    HasCompacted bool
    LastSummary  string
    RecentFiles  []string
    CompactCount int
}
```

| Field          | Type       | Description                                              |
|----------------|------------|----------------------------------------------------------|
| `HasCompacted` | `bool`     | Whether at least one compaction has occurred             |
| `LastSummary`  | `string`   | Text of the most recently generated summary              |
| `RecentFiles`  | `[]string` | Up to 5 most recently read file paths (FIFO)             |
| `CompactCount` | `int`      | Total number of compactions performed in the session     |

---

### `func New(client *anthropic.Client, cfg *config.Config) *Agent`

Creates and returns a new `Agent`.

---

### `func (a *Agent) Run()`

Top-level REPL entry point. Reads user input in a loop, maintains persistent `history` and a single `CompactState` across all prompts, and calls `Loop` for each user message. Type `q` or `exit` to quit.

---

### `func (a *Agent) RunOneTurn(state *LoopState) bool`

Sends the current message history to the model, appends the response to `state.Messages`, executes any tool calls, and appends the tool results.

Returns `true` if tool calls were made (another turn needed), `false` otherwise.

---

### `func (a *Agent) Loop(state *LoopState)`

Drives `RunOneTurn` in a loop until it returns `false`. Before each call to `RunOneTurn`:

1. Runs `MicroCompact` to replace older tool results with placeholders.
2. Checks `EstimateContextSize`; if > `CONTEXT_LIMIT` (50 000), calls `CompactHistory`.

Modifies `state` in place.

---

### `func (a *Agent) RunSubagent(prompt string) string`

Spawns an isolated child agent to complete `prompt`. The child:

- Starts with a fresh `messages` slice (no parent history).
- Uses `ToolsExcept("task")` — `task` is stripped to prevent recursive spawning.
- Appends `"\nYou are a subagent. Complete the given task…"` to the system prompt.
- Runs for up to `subagentMaxTurns` (30) turns.
- Returns the last text block produced; returns `"(no summary)"` if the child emits no text.

The child's message history is local to `RunSubagent` and is GC'd on return. All tool calls inside the child go through `tools.Dispatch` and `tools.PersistLargeOutput` identically to the parent loop.

Called exclusively via the `subagentRunner` callback registered in `tools/task.go`.

---

## internal/agent — compaction

### Constants

```go
const (
    CONTEXT_LIMIT        = 50000 // Auto-compact threshold (characters)
    KEEP_RECENT_RESULTS  = 3     // Tool results kept intact by MicroCompact
    maxConversationBytes = 80000 // Max bytes passed to summarisation LLM
)
```

---

### `func EstimateContextSize(messages []anthropic.MessageParam) int`

Returns the approximate size of the message list in characters (JSON-serialised).

---

### `func MicroCompact(messages []anthropic.MessageParam, keepCount int) []anthropic.MessageParam`

Replaces the content of older tool-result blocks with a one-line placeholder, keeping the most recent `keepCount` results intact.

The last user message that contains tool results is **always** left untouched, regardless of `keepCount`, so the model always sees the current turn's results.

---

### `func SummarizeHistory(client *anthropic.Client, model string, messages []anthropic.MessageParam) (string, error)`

Calls the LLM to produce a structured summary of the conversation. Preserves: current goal, important findings and decisions, files read or changed, remaining work, user constraints.

Truncates input to `maxConversationBytes` if necessary.

---

### `func CompactHistory(client *anthropic.Client, model string, messages []anthropic.MessageParam, state *CompactState, focus string) ([]anthropic.MessageParam, error)`

Full compaction pipeline:

1. `WriteTranscript(messages)` — saves JSONL snapshot to disk.
2. `SummarizeHistory(...)` — generates LLM summary.
3. Appends `focus` hint and `state.RecentFiles` to the summary.
4. Updates `state` (`HasCompacted`, `LastSummary`, `CompactCount`).
5. Returns a new single-message list containing the summary.

---

### `func TrackRecentFile(state *CompactState, path string)`

Adds `path` to `state.RecentFiles`. Removes duplicates, then appends; trims to the 5 most recent entries.

---

## internal/agent — transcripts

### `func WriteTranscript(messages []anthropic.MessageParam) error`

Serialises `messages` to JSONL and writes the file to `.evo-agent/transcripts/<RFC3339-timestamp>.jsonl`. Creates the directory if it does not exist.

---

## internal/tools

### `type Handler`

```go
type Handler func(input json.RawMessage) (string, error)
```

Function signature every tool handler must implement.

---

### `type ToolDef struct`

```go
type ToolDef struct {
    Schema  anthropic.ToolParam
    Handler Handler
}
```

Bundles a tool's API schema with its runtime handler.

---

### `func Register(def ToolDef)`

Adds a `ToolDef` to the global registry. Call this from a tool file's `init()` function.

---

### `func Tools() []anthropic.ToolUnionParam`

Returns all registered tool schemas plus MCP tool schemas, ready to pass to the Anthropic API. Native tools come first, followed by MCP tools (prefixed `mcp__{server}__{tool}`).

---

### `func ToolsExcept(names ...string) []anthropic.ToolUnionParam`

Like `Tools()` but omits the named tools from the result. Used by `RunSubagent` to strip the `task` tool and prevent recursive subagent spawning.

---

### `func Dispatch(name string, input json.RawMessage) (string, error)`

Routes the call to the correct handler:
- Names prefixed `mcp__` are forwarded to `DispatchMCP`.
- All other names are looked up in the native tool registry.

Returns `("", nil)` if the tool is not found.

---

### `func GenerateSchema[T any]() anthropic.ToolInputSchemaParam`

Uses reflection (`invopop/jsonschema`) to build an `anthropic.ToolInputSchemaParam` from a Go struct. Annotate fields with `jsonschema_description:"..."` to provide descriptions.

---

### `func PersistLargeOutput(id, output string) string`

If `output` exceeds `persistThreshold` (30 000 chars), writes it to `.evo-agent/tool-results/<id>.txt` and returns a 2 000-char preview placeholder. Otherwise returns `output` unchanged. Called in both the parent executor (`executor.go`) and child subagent (`subagent.go`).

---

### `func RegisterSubagentRunner(fn func(prompt string) string)`

Registers the callback used by the `task` tool to spawn subagents. Called once by `agent.New()` at startup to inject `Agent.RunSubagent`. Uses a private package-level variable to avoid an `agent` → `tools` import cycle.

---

### `func Execute(content []anthropic.ContentBlockUnion) []anthropic.ContentBlockParamUnion`

Iterates over a model response's content blocks:
- `ThinkingBlock` → `ui.PrintThinking`
- `TextBlock` → `ui.PrintText`
- `ToolUseBlock` → `ui.PrintToolCall` + `ui.PrintCommand` + `Dispatch` + collect result

Returns the accumulated `ToolResultBlock` list to append to the next user message.

---

### Tool: `bash`

```go
type BashInput struct {
    Command string `json:"command"`
}
```

Runs `bash -c <command>` in the current working directory.
- Timeout: 120 seconds
- Max output: 50 000 characters
- Returns combined stdout + stderr

---

### Tool: `read_file`

```go
type ReadFileInput struct {
    FilePath string `json:"file_path"`         // absolute or cwd-relative
    Offset   int    `json:"offset,omitempty"`  // 1-indexed line to start at
    Limit    int    `json:"limit,omitempty"`   // max lines to read (0 = default 2000)
}
```

Re-implementation inspired by Claude Code's official `FileReadTool`. Reads a text file from the local filesystem and returns it in `cat -n` format (`%6d\t<content>\n`). Default cap is **2000 lines** / **256 KB**; provide `offset` + `limit` to read a window of a larger file.

Behaviors:

- **Empty file** → `<system-reminder>Warning: the file exists but the contents are empty.</system-reminder>`.
- **Offset out of range** → `<system-reminder>Warning: the file exists but is shorter than the provided offset (N). The file has M lines.</system-reminder>`.
- **Dedup**: if the same `(file_path, offset, limit)` is read again with mtime unchanged, returns a small `File unchanged since last read…` stub instead of resending the bytes (saves cache_creation tokens). The dedup entry is invalidated automatically by `edit_file` / `write_file` against the same path.
- **Binary refusal**: extensions like `.zip`, `.exe`, `.png`, `.pdf`, `.so` are rejected up front. Use the `bash` tool with file-specific utilities instead.
- **Device blocklist**: refuses `/dev/zero`, `/dev/random`, `/dev/stdin`, `/proc/<pid>/fd/{0,1,2}`, and the like — would block or never EOF.
- **CRLF normalization**: trailing `\r` is stripped before line-numbering.
- **File not found**: returns `read_file: File does not exist. Current working directory: …. Did you mean <similar>?` when a same-stem neighbor exists in the parent directory.

Also calls `TrackRecentFile` on the loop's `CompactState` for compaction-time hints.

---

### Tool: `write_file`

```go
type WriteFileInput struct {
    Path    string `json:"path"`
    Content string `json:"content"`
}
```

Writes `content` to `path`, creating all parent directories (`mkdir -p`). Returns a confirmation string with byte count.

---

### Tool: `edit_file`

```go
type EditFileInput struct {
    Path   string `json:"path"`
    OldStr string `json:"old_str"`
    NewStr string `json:"new_str"`
}
```

Replaces the **first** exact occurrence of `old_str` with `new_str` in the file at `path`.

Special cases:
- If the file does not exist and `old_str == ""`, the file is created with `new_str` as content (delegates to `write_file`)
- `old_str` and `new_str` must differ; `path` must not be empty

---

### Tool: `compact`

```go
type CompactInput struct {
    Focus string `json:"focus,omitempty"`
}
```

Model-initiated context compaction. The handler returns a placeholder string; actual compaction is performed by `loop.go` after detecting the `compact` tool call in the response.

`focus` — optional hint describing what information must be preserved in the generated summary.

---

### Tool: `load_skill`

```go
type loadSkillInput struct {
    Name string `json:"name"`
}
```

Loads the full body of the named skill from the skill registry. Returns the skill body wrapped in an XML tag with the skill's name and absolute file path:

```xml
<skill name="git-commit" path="/workspace/.evo-agent/skill/git-commit/SKILL.md">
...body...
</skill>
```

Returns a human-readable error string (containing `"Error"`) when the skill name is not found, along with the list of known skill names.

---

### Tool: `task`

```go
type TaskInput struct {
    Prompt      string `json:"prompt"`
    Description string `json:"description"`
}
```

Spawns a subagent with a fresh, isolated context to complete `prompt`. `description` is a one-line summary shown in the UI during execution.

- The subagent shares the filesystem but not conversation history.
- The `task` tool is stripped from the child's tool list (no recursive spawning).
- Hard cap: 30 turns per invocation (`subagentMaxTurns`).
- Returns the last text block produced by the child as a plain string summary.
- Returns `"(no summary)"` if the child produces no text output.

The handler calls the `subagentRunner` callback registered by `RegisterSubagentRunner`. Returns `"Error: subagent runner not initialized"` if called before `agent.New()`.

---

## internal/skills

### `type SkillManifest struct`

```go
type SkillManifest struct {
    Name        string
    Description string
}
```

Lightweight metadata for a single skill, kept in memory for fast catalog generation.

| Field         | Type     | Description                                  |
|---------------|----------|----------------------------------------------|
| `Name`        | `string` | Skill identifier used with `load_skill`      |
| `Description` | `string` | One-line summary shown in the system prompt  |

---

### `func Init()`

Scans `.evo-agent/skill/**/SKILL.md` in the current working directory. For each file found:

1. Reads and parses YAML frontmatter (`name`, `description`).
2. Falls back to the parent directory name if `name` is absent.
3. Resolves the absolute path via `filepath.Abs`.
4. Stores the document in the package-level `documents` map.

Missing skill directory is silently ignored (consistent with MCP config behaviour). Prints `[Skills] Loaded N skill(s)` when at least one skill is found.

---

### `func Catalog() string`

Returns a formatted, alphabetically sorted list of all loaded skills:

```
- git-commit: Best practices for writing git commit messages
- python-style: PEP 8 and idiomatic Python conventions
```

Returns `""` when no skills are loaded. Used by `main.go` to inject the catalog into the system prompt.

---

### `func Load(name string) string`

Returns the full skill body wrapped in an XML tag, ready to inject into context:

```xml
<skill name="git-commit" path="/workspace/.evo-agent/skill/git-commit/SKILL.md">
...body...
</skill>
```

Returns a human-readable error string (beginning with `"Error"`) when the skill name is unknown, including the list of available skill names.

---

## internal/tools — MCP

```go
type MCPServerConfig struct {
    Type        string            `json:"type"`
    Disabled    bool              `json:"disabled"`
    Timeout     int               `json:"timeout"`
    Description string            `json:"description"`
    Command     string            `json:"command"`
    Args        []string          `json:"args"`
    Env         map[string]string `json:"env"`
    URL         string            `json:"url"`
    Headers     map[string]string `json:"headers"`
}
```

| Field         | Description                                                          |
|---------------|----------------------------------------------------------------------|
| `Type`        | Transport: `"stdio"`, `"sse"`, or `"streamableHttp"` (required)     |
| `Disabled`    | Skip this server at startup                                          |
| `Timeout`     | Request timeout in seconds (default 30)                              |
| `Command`     | *(stdio)* Subprocess command                                         |
| `Args`        | *(stdio)* Command-line arguments                                     |
| `Env`         | *(stdio)* Extra environment variables overlaid on the current env    |
| `URL`         | *(sse/streamableHttp)* Remote server URL                             |
| `Headers`     | *(sse/streamableHttp)* Custom HTTP request headers                   |

---

### `type MCPConfig struct`

```go
type MCPConfig struct {
    MCPServers map[string]MCPServerConfig `json:"mcpServers"`
}
```

Top-level structure of `.evo-agent/mcp.json`.

---

### `func InitMCP()`

Reads `.evo-agent/mcp.json`, connects to each enabled server using the appropriate transport, and caches the client in the package-level `mcpServers` map. Missing config file is silently ignored. Prints `[MCP] Connected to "name" (N tools)` for each successful connection.

---

### `func ShutdownMCP()`

Calls `stop()` on all connected MCP clients. Should be called via `defer` in `main()`.

---

### `func MCPTools() []anthropic.ToolUnionParam`

Returns Anthropic tool schemas for all tools exposed by connected MCP servers. Each tool name is prefixed as `mcp__{serverName}__{toolName}`.

---

### `func DispatchMCP(name string, input json.RawMessage) (string, error)`

Parses the `mcp__{server}__{tool}` prefix from `name`, looks up the server in `mcpServers`, and calls `callTool(toolName, input)` on the matching client.

---

### `mcpClient` interface

```go
type mcpClient interface {
    getTools() []mcpToolSpec
    callTool(toolName string, arguments json.RawMessage) (string, error)
    stop()
}
```

Implemented by three transport types:

| Type               | Struct              | Transport mechanism                                             |
|--------------------|---------------------|-----------------------------------------------------------------|
| `stdio`            | `mcpProcess`        | Subprocess pipes; line-delimited JSON-RPC                       |
| `streamableHttp`   | `mcpHTTPClient`     | Stateless POST; response auto-detected as JSON or SSE           |
| `sse`              | `mcpSSEClient`      | Persistent GET SSE stream + POST; background goroutine for routing |

---

## internal/config

### `type Config struct`

```go
type Config struct {
    ModelID   string
    APIKey    string
    BaseURL   string
    SystemMsg string
}
```

| Field       | Env Variable           | Description                                      |
|-------------|------------------------|--------------------------------------------------|
| `ModelID`   | `MODEL_ID`             | Anthropic model identifier                       |
| `APIKey`    | `ANTHROPIC_API_KEY`    | API authentication key                           |
| `BaseURL`   | `ANTHROPIC_BASE_URL`   | Optional custom API endpoint                     |
| `SystemMsg` | *(generated)*          | System prompt including the current working directory |

---

### `func LoadEnv()`

Loads `.env` files in two passes:
1. From the directory of the running binary (e.g. `build/.env`)
2. From the current working directory (overrides step 1)

---

### `func Load() *Config`

Reads environment variables and returns a populated `*Config`. The `SystemMsg` field is generated dynamically with the current working directory injected.

---

## internal/ui

ANSI color helpers for terminal output.

### Constants

```go
const (
    ColorReset   = "\033[0m"
    ColorGreen   = "\033[32m"
    ColorCyan    = "\033[36m"
    ColorBlue    = "\033[34m"
    ColorYellow  = "\033[33m"
    ColorMagenta = "\033[35m"
    ColorRed     = "\033[31m"
)
```

### Functions

All `Print*` / `Emit*` helpers below route through `globalSink`, which is `TerminalSink{}` by default and swapped to `tui.Sink` when the TUI starts. The "Plain output" column shows what `TerminalSink` writes; the TUI consumes the same `Event` and renders it in the live view.

| Function                                                                                | Event kind     | Plain output                                          |
|-----------------------------------------------------------------------------------------|----------------|-------------------------------------------------------|
| `PrintThinking(text string)`                                                            | `EvThinking`   | Green `THINKING: <text>`                              |
| `PrintText(text string)`                                                                | `EvText`       | Cyan `<text>`                                         |
| `PrintToolCall(id, name, input string)`                                                 | `EvToolCall`   | Blue `DEBUG: Tool called: <name>`                     |
| `PrintToolResult(id, output string, isError bool)`                                      | `EvToolResult` | First 200 chars of `output` (no prefix)               |
| `PrintCommand(cmd string)`                                                              | *(no-op)*      | *(nothing — kept for API compatibility)*              |
| `PrintError(msg string)`                                                                | `EvSystem`     | Magenta `<msg>`                                       |
| `PrintSystem(msg string)`                                                               | `EvSystem`     | Magenta `<msg>`                                       |
| `PrintTokens(model string, in, out int64, stopReason, blockSummary string)`             | `EvTokens`     | Magenta `DEBUG: model=… in=… out=… stop=… blocks=[…]` |
| `PrintDone()`                                                                           | `EvDone`       | *(nothing in plain mode)*                             |
| `EmitTodo(items []TodoItem, topic string)`                                              | `EvTodo`       | Magenta `── TODO ──` block with status markers        |
| `EmitPlan(plans []PlanSnapshot)`                                                        | `EvPlan`       | *(plain mode prints nothing — TUI panel only)*        |
| `EmitGoal(ev Event)` / `PrintGoal(kind, text, reason, planName, iter, max int, setAtMs int64)` | `EvGoal` | Yellow/green/red one-liner per `GoalKind` (set/evaluating/continuing/achieved/cleared/capped/status) |
