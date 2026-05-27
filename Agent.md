# Agent.md

## Project Overview

Evo-Agent is a lightweight, tool-augmented AI agent written in Go. It implements a ReAct (Reason + Act) loop using the Anthropic API to perform tasks in a local workspace. It features a Bubble Tea TUI, a two-layer skill system for on-demand knowledge, MCP client support for external tools, and a subagent system for complex task delegation.

**Tech Stack:** Go, Bubble Tea (TUI), Anthropic API, MCP
**Repository:** `github.com/tiankonguse-m3/evo-agent`

## Architecture Map

The agent uses a modular, self-registering architecture to minimize system prompt overhead. The core loop manages state, context compaction, and tool execution.

Key directories:
- `src/internal/agent/` — Core ReAct loop, state, and subagent logic.
- `src/internal/tools/` — Self-registering tool implementations.
- `src/internal/skills/` — Skill registry and slash command dispatch.
- `src/internal/tui/` — Bubble Tea TUI rendering and event handling.
- `src/internal/config/` — Environment and configuration loading.
- `docs/` — Architectural and API reference documentation.
- `blog/` — Detailed design posts on internal mechanisms.

## Development Conventions

**Branch strategy:** `main` protected, feature branches via PR.
**Commit format:** `<type>: <description>` (e.g., `feat: add mcp support`)

**Prohibited:**
- Do not manually register tools in a central list; use the `init()` pattern in `src/internal/tools/`.
- Do not include huge raw outputs in the chat history; use `PersistLargeOutput` to save to disk.
- Do not allow recursive `task` tool calls (subagents must not spawn subagents).

## Common Commands

    make build           # Build the agent binary
    make test            # Run all tests
    make deps            # Update/add dependencies
    ./build/evo-agent    # Run in TUI mode
    ./build/evo-agent --plain  # Run in Plain REPL mode
