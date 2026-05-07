# API Reference

## internal/agent

### `type LoopState struct`
Holds the state of a single interaction session.
- `Messages`: A slice of `anthropic.MessageParam`.
- `TurnCount`: Integer tracking how many tool-calls have occurred.

### `func (a *Agent) Loop(state *LoopState)`
The main execution loop that continues calling `RunOneTurn` until no more tool calls are requested.

## internal/tools

### `func RunBash(command string) string`
Executes a bash command with a timeout of 120 seconds. Returns combined stdout and stderr.

### `func Execute(content []anthropic.ContentBlock) []anthropic.ContentBlockParamUnion`
Processes the content returned by the LLM, triggering `RunBash` if a `ToolUseBlock` is encountered.

## internal/config

### `type Config struct`
Runtime configuration.
- `ModelID`: Model identifier.
- `APIKey`: API authentication key.
- `BaseURL`: API endpoint.
- `SystemMsg`: The system prompt defining the agent's persona.

## internal/ui
Provides utility functions for colored terminal output:
- `PrintThinking()`: Green text for model internal reasoning.
- `PrintText()`: Cyan text for model responses.
- `PrintCommand()`: Yellow text for executed bash commands.
- `PrintError()`: Red text for error messages.
