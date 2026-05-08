# API Reference

## internal/agent

### `type LoopState struct`

Holds the mutable state of a single agent run.

```go
type LoopState struct {
    Messages         []anthropic.MessageParam
    TurnCount        int
    TransitionReason string
}
```

| Field              | Type                         | Description                                              |
|--------------------|------------------------------|----------------------------------------------------------|
| `Messages`         | `[]anthropic.MessageParam`   | Full conversation history (user + assistant turns)       |
| `TurnCount`        | `int`                        | Number of tool-call turns that have occurred             |
| `TransitionReason` | `string`                     | `"tool_result"` if loop continued, `""` when done        |

---

### `func New(client *anthropic.Client, cfg *config.Config) *Agent`

Creates and returns a new `Agent`.

---

### `func (a *Agent) RunOneTurn(state *LoopState) bool`

Sends the current message history to the model, appends the response to `state.Messages`, executes any tool calls, and appends the tool results.

Returns `true` if tool calls were made (another turn needed), `false` otherwise.

---

### `func (a *Agent) Loop(state *LoopState)`

Drives `RunOneTurn` in a loop until it returns `false`. Modifies `state` in place.

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
