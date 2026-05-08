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
- **`RunOneTurn`** — sends history to the LLM, appends the response, executes tool calls, returns `true` if another turn is needed
- **`Loop`** — drives `RunOneTurn` in a `for` loop until it returns `false`

### 2. Tool Engine (`internal/tools`)

A self-registering, table-driven tool system.

- **`ToolDef`** — bundles an `anthropic.ToolParam` schema with a `Handler` func
- **`registry`** — a `map[string]ToolDef` populated at startup via `init()` functions
- **`Register(def ToolDef)`** — called from each tool file's `init()`; no central list needed
- **`Tools()`** — returns all registered schemas as `[]anthropic.ToolUnionParam` for the API call
- **`Dispatch(name, input)`** — looks up and calls the handler for a given tool name
- **`GenerateSchema[T]()`** — reflects a Go struct into `anthropic.ToolInputSchemaParam` using `invopop/jsonschema`
- **`Execute(content)`** — iterates API response content blocks, prints output, calls `Dispatch` for each `ToolUseBlock`, returns `[]ContentBlockParamUnion` tool results

#### Registered Tools

| Tool         | File           | What it does                                               |
|--------------|----------------|------------------------------------------------------------|
| `bash`       | `bash.go`      | Runs a shell command via `bash -c`, 120 s timeout, 50 KB cap |
| `read_file`  | `read_file.go` | Reads a file; optional `limit` truncates to N lines        |
| `write_file` | `write_file.go`| Writes content to a path, creating parent directories      |
| `edit_file`  | `edit_file.go` | Replaces the first exact occurrence of `old_str` with `new_str`; creates file if `old_str` is empty |

### 3. Configuration (`internal/config`)

- **`LoadEnv()`** — loads `.env` from the binary's directory first, then from CWD (CWD takes precedence)
- **`Load()`** — reads `MODEL_ID`, `ANTHROPIC_API_KEY`, `ANTHROPIC_BASE_URL` from the environment and builds the system prompt dynamically with the current working directory

### 4. User Interface (`internal/ui`)

Provides ANSI-colored terminal output to separate different agent output types.

| Function         | Color   | Purpose                              |
|------------------|---------|--------------------------------------|
| `PrintThinking`  | Green   | Model's extended thinking            |
| `PrintText`      | Cyan    | Model's text response                |
| `PrintToolCall`  | Blue    | Tool name being invoked              |
| `PrintCommand`   | Yellow  | Tool call with arguments             |
| `PrintError`     | Red     | Errors from tools or the API         |

### 5. Entry Point (`main.go`)

- Loads config, creates the Anthropic client and `Agent`
- Maintains a persistent `history []anthropic.MessageParam` across prompts in the same session
- Appends the user message to history, creates a fresh `LoopState`, calls `Loop`, then reads the final assistant message back out for display

## Data Flow

```
User Input
    │
    ▼
main.go ──► Agent.Loop(state)
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
                ├── PrintThinking / PrintText / PrintCommand
                │
                └──► tools.Dispatch(name, input)
                            │
                            ▼
                      bash / read_file / write_file / edit_file
                            │
                            ▼
                      ToolResult appended to history
                            │
                            ▼
                    RunOneTurn returns true  ──► repeat
                    RunOneTurn returns false ──► done
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
