# evo-agent
A step-by-step journey to implement an AI agent in Go.

Evo-Agent is a lightweight, tool-augmented AI agent written in Go. It leverages the Anthropic API to perform tasks in a local workspace by executing shell commands.

## Features
- **Bash Integration**: Can execute arbitrary bash commands to read, write, and manipulate the local filesystem.
- **Multi-turn Reasoning**: Supports a loop of thought $\rightarrow$ action $\rightarrow$ observation.
- **Colored CLI**: Clear terminal output distinguishing between thinking, tool calls, and final responses.

## Configuration
The agent is configured via environment variables (or a `.env` file):
- `MODEL_ID`: The Anthropic model to use (e.g., `claude-3-5-sonnet-20240620`).
- `ANTHROPIC_API_KEY`: Your API key.
- `ANTHROPIC_BASE_URL`: (Optional) Custom API endpoint.

## Usage
1. Set the environment variables.
2. Run the agent: `go run main.go`.
3. Enter your requests in the prompt. Type `q` or `exit` to quit.