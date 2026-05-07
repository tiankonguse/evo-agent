# System Architecture

## Overview
The system is designed as a **ReAct (Reason + Act)** loop. The agent doesn't just generate text; it can interact with the host operating system to gather information or perform changes.

## Components

### 1. Orchestrator (Agent)
Located in `internal/agent`, the `Agent` struct manages the conversation state.
- **\<code>LoopState\</code>**: Tracks the history of messages and the number of turns in the current session.
- **\<code>RunOneTurn\</code>**: A single cycle of sending history to the LLM and processing the response.

### 2. Tool Engine (Tools)
Located in `internal/tools`, this component bridges the LLM and the OS.
- **Bash Tool**: Defined in `Tools`, it allows the LLM to request shell command execution.
- **Executor**: The `Execute` function handles the transition from LLM tool-use blocks to actual system calls and formats the results back for the LLM.

### 3. Configuration (Config)
Located in `internal/config`, it handles environmental setup and defines the system prompt that instructs the LLM on its role as a coding agent.

### 4. User Interface (UI)
Located in `internal/ui`, it provides a formatted terminal experience using ANSI color codes to separate different types of agent output (Thinking, Tool Call, Error).

## Data Flow
1. \*\*User Input\*\* $\rightarrow$ `main.go` $\rightarrow$ `Agent.Loop()`
2. \*\*Agent\*\* $\rightarrow$ `Anthropic API` (with System Prompt + Bash Tool definition)
3. \*\*Anthropic API\*\* $\rightarrow$ `Agent` (Returns Text or ToolUse block)
4. \*\*Agent\*\* $\rightarrow$ `tools.Execute()` $\rightarrow$ `os/exec` (Run Bash)
5. \*\*Bash Output\*\* $\rightarrow$ `Agent` $\rightarrow$ `Anthropic API` (Append ToolResult)
6. \*\*Repeat\*\* until the API provides a final answer.
