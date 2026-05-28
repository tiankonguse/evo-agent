# Agent.md

This file provides guidance to evo-agent when working with code in this repository.

## Project Overview
Evo-Agent is a lightweight, tool-augmented AI agent written in Go. It implements a ReAct loop using the Anthropic API to perform tasks in a local workspace, featuring a Bubble Tea TUI, a two-layer skill system, MCP client support, and a subagent system.

## Tech Stack
- **Language:** Go (1.26)
- **TUI:** Bubble Tea, Lipgloss
- **LLM:** Anthropic API
- **Tools:** MCP (stdio, sse, streamableHttp)

## Architecture Map
- `src/main.go`: Entry point (TUI/plain mode, MCP init, skill catalog, slash dispatch)
- `src/internal/agent/`: Core ReAct loop, state management, and subagent logic
- `src/internal/tools/`: Self-registering tool implementations (bash, read_file, etc.)
- `src/internal/skills/`: Skill registry and slash command dispatch
- `src/internal/tui/`: Bubble Tea TUI rendering and event handling
- `src/internal/ui/`: Terminal and event primitives
- `.evo-agent/`: Configuration, skills, commands, and persistent memory

## Development Commands
- **Build:** `make build` (outputs to `build/evo-agent`)
- **Run (TUI):** `./build/evo-agent`
- **Run (Plain):** `./build/evo-agent --plain`
- **Test:** `make test`
- **Dependencies:** `make deps` (runs `go mod tidy`)
- **Clean:** `make clean`

## Development Conventions
- **Commit Format:** `<type>: <description>` (e.g., `feat: add mcp support`)
- **Tool Registration:** Use the `init()` pattern in `src/internal/tools/`; no central registration required.
- **Branching:** `main` is protected; use feature branches via PR.

## Prohibited
- Do NOT manually register tools in a central list; use the self-registering `init()` pattern.
- Do NOT allow recursive `task` tool calls (subagents must not spawn subagent).
- Do NOT include huge raw outputs in the chat history; use `PersistLargeOutput` to save to disk.
