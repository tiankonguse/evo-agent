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

Serialises `messages` to JSONL and writes the file to `.evo_agent/transcripts/<RFC3339-timestamp>.jsonl`. Creates the directory if it does not exist.

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

Returns all registered tool schemas, ready to pass to the Anthropic API.

---

### `func Dispatch(name string, input json.RawMessage) (string, error)`

Looks up `name` in the registry and calls its handler with `input`. Returns `("", nil)` if the tool is not found.

---

### `func GenerateSchema[T any]() anthropic.ToolInputSchemaParam`

Uses reflection (`invopop/jsonschema`) to build an `anthropic.ToolInputSchemaParam` from a Go struct. Annotate fields with `jsonschema_description:"..."` to provide descriptions.

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
    Path  string `json:"path"`
    Limit int    `json:"limit,omitempty"`
}
```

Reads the file at `path`. If `limit > 0`, truncates to the first `limit` lines and appends a `"... (N more lines)"` note. Max 50 000 characters.

Also calls `TrackRecentFile` in the loop's `CompactState` when invoked.

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

| Function                   | Color   | Output format                    |
|----------------------------|---------|----------------------------------|
| `PrintThinking(text string)` | Green   | `THINKING: <text>`             |
| `PrintText(text string)`     | Cyan    | `TEXT: <text>`                 |
| `PrintToolCall(name string)` | Blue    | `DEBUG: Tool called: <name>`   |
| `PrintCommand(cmd string)`   | Yellow  | `$ <cmd>`                      |
| `PrintError(msg string)`     | Red     | `<msg>`                        |
