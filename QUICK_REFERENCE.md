# EVO-Agent Quick Reference

## Project at a Glance

| Aspect | Details |
|--------|---------|
| **Language** | Go 1.26 |
| **Module** | `evo-agent` |
| **Main Entry** | `src/main.go` |
| **Agent Loop** | `src/internal/agent/loop.go` |
| **Tool System** | `src/internal/tools/` (registry pattern) |
| **MCP Support** | stdio, SSE, streamableHttp transports |
| **Skills** | Loaded from `.evo-agent/skill/**/SKILL.md` |
| **Context Limit** | 50,000 chars (auto-compact) |
| **Current UI** | Terminal with ANSI colors only |

---

## Key Components

### 1. Agent Loop Flow
```
User Query → LLM Call → Tool Dispatch → Tool Results → Loop Until End
```

### 2. Tool Registry
```go
// Pattern: All tools register in init()
func init() {
    Register(ToolDef{
        Schema: ToolParam{...},
        Handler: func(input json.RawMessage) (string, error) {...},
    })
}
```

### 3. MCP Tools
- Prefixed as `mcp__{server}__{tool}`
- Connected via `.evo-agent/mcp.json`
- Support 3 transports: stdio, SSE, HTTP

### 4. Skills System
- Location: `.evo-agent/skill/{name}/SKILL.md`
- Format: YAML frontmatter + markdown body
- Access: `load_skill` tool or catalog in system prompt

### 5. Context Management
- **MicroCompact**: Local compression (no LLM cost)
- **CompactHistory**: LLM summarization when needed
- **RecentFiles**: Track last 5 files for re-opening

---

## File Organization

```
src/
├── main.go                    ← Start here
├── internal/
│   ├── agent/
│   │   ├── loop.go           ← Agent loop logic
│   │   ├── state.go          ← LoopState, CompactState
│   │   ├── compact.go        ← Context management
│   │   └── transcripts.go    ← Session recording
│   ├── tools/
│   │   ├── tool.go           ← Registry interface
│   │   ├── executor.go       ← Tool execution
│   │   ├── mcp.go            ← MCP client
│   │   └── [bash,read,write,edit,skill,compact].go ← Native tools
│   ├── config/
│   │   └── config.go         ← Environment config
│   ├── ui/
│   │   └── terminal.go       ← ANSI color output
│   └── skills/
│       └── registry.go       ← Skill loader
└── go.mod
```

---

## Environment Variables

| Variable | Required | Example |
|----------|----------|---------|
| `MODEL_ID` | Yes | `claude-3-5-sonnet-20241022` |
| `ANTHROPIC_API_KEY` | Yes | `sk-ant-...` |
| `ANTHROPIC_BASE_URL` | No | `https://custom-api.example.com` |

Load from: `.env` file or environment

---

## Building & Running

### Build
```bash
cd src
go build -o evo-agent
```

### Run Interactive
```bash
./evo-agent
>> Your query here
```

### Run Scripted
```bash
echo "list all .go files" | ./evo-agent
```

### With Config
```bash
export MODEL_ID=claude-3-5-sonnet-20241022
export ANTHROPIC_API_KEY=sk-ant-...
./evo-agent
```

---

## Native Tools

### read_file
```
Input: {path: "file.go", limit: 100}
Output: File contents (max 50KB)
```

### write_file
```
Input: {path: "new.go", content: "..."}
Output: Confirmation
```

### edit_file
```
Input: {path: "file.go", old_str: "foo", new_str: "bar"}
Output: Confirmation
```

### bash
```
Input: {command: "ls -la"}
Output: Command output (max 50KB, 120s timeout)
```

### load_skill
```
Input: {name: "my-skill"}
Output: <skill>body</skill>
```

### compact
```
Input: {focus: "optional focus hint"}
Output: Triggers LLM-based history compression
```

---

## MCP Configuration

### File: `.evo-agent/mcp.json`

```json
{
  "mcpServers": {
    "filesystem": {
      "type": "stdio",
      "command": "node",
      "args": ["mcp-filesystem/index.js"]
    },
    "git": {
      "type": "sse",
      "url": "https://mcp-git.example.com/sse"
    }
  }
}
```

### Tool Access
```
mcp__filesystem__read  // from filesystem server
mcp__git__log          // from git server
```

---

## Skills Configuration

### File: `.evo-agent/skill/my-skill/SKILL.md`

```yaml
---
name: "Code Review"
description: "Review Go code for quality issues"
---

# Code Review Skill

## Instructions
When asked to review code:
1. Check for nil pointer dereferences
2. Verify error handling
3. Look for race conditions
...
```

### Loading
- Automatic on startup: `skills.Init()`
- Listed in system prompt: `skills.Catalog()`
- Accessible via tool: `load_skill("Code Review")`

---

## State Management

### LoopState
```go
state.Messages           // []anthropic.MessageParam - conversation history
state.TurnCount          // int - number of tool calls
state.CompactState       // *CompactState - compression state
state.TransitionReason   // string - why we moved to next turn
```

### CompactState
```go
state.HasCompacted       // bool - was anything compressed?
state.LastSummary        // string - generated summary
state.RecentFiles        // []string - last 5 files (FIFO)
state.CompactCount       // int - # of compressions done
```

---

## Output System

### Current (terminal.go)
```go
ui.PrintThinking(text)   // Green
ui.PrintText(text)       // Cyan
ui.PrintToolCall(name)   // Blue
ui.PrintCommand(cmd)     // Yellow
ui.PrintError(msg)       // Red
```

### ANSI Codes
```
ColorReset   = "\033[0m"
ColorGreen   = "\033[32m"
ColorCyan    = "\033[36m"
ColorBlue    = "\033[34m"
ColorYellow  = "\033[33m"
ColorMagenta = "\033[35m"
ColorRed     = "\033[31m"
```

---

## Extending the System

### Add a Native Tool

**File**: `src/internal/tools/my_tool.go`

```go
package tools

import (
    "encoding/json"
    "github.com/anthropics/anthropic-sdk-go"
)

type MyInput struct {
    Param1 string `json:"param1" jsonschema_description:"Description"`
}

func init() {
    Register(ToolDef{
        Schema: anthropic.ToolParam{
            Name: "my_tool",
            Description: anthropic.String("What it does"),
            InputSchema: GenerateSchema[MyInput](),
        },
        Handler: func(input json.RawMessage) (string, error) {
            var in MyInput
            json.Unmarshal(input, &in)
            // Your implementation
            return result, nil
        },
    })
}
```

Then rebuild: `go build -o evo-agent`

### Add a Skill

**Dir**: `.evo-agent/skill/my-skill/`

**File**: `SKILL.md`

```yaml
---
name: "My Skill"
description: "Short description"
---

# Full skill documentation
Instructions, examples, constraints...
```

Then restart agent.

---

## Debugging

### Print Debug Info
```bash
# Existing debug output
./evo-agent
>> query

# Output shows:
# - Token count
# - Model used
# - stop_reason
# - Tool calls
# - Compaction events
```

### Check MCP Connections
```bash
# Errors logged at startup if MCP fails to connect
# Check .evo-agent/mcp.json syntax
```

### View Tool Results
```bash
# Large outputs saved to .evo-agent/tool-results/{id}.txt
# Check if tool output seems truncated
```

### Context Size
```bash
# Printed in debug messages when auto-compact triggered
# [auto compact triggered: 52345 chars]
```

---

## Performance Tips

1. **Keep skills focused**: Don't load unnecessary skills
2. **Use bash judiciously**: 120s timeout, 50KB output limit
3. **Monitor context size**: Auto-compact at 50K chars
4. **Batch tool calls**: Let agent make multiple calls per turn
5. **Persist large outputs**: >30KB automatically saved to disk

---

## Common Issues

| Issue | Cause | Fix |
|-------|-------|-----|
| Model not found | MODEL_ID not set | Export `MODEL_ID` env var |
| Auth failed | Invalid API key | Check `ANTHROPIC_API_KEY` |
| MCP not connecting | Bad config | Verify `.evo-agent/mcp.json` |
| Skills not loading | Wrong path | Check `.evo-agent/skill/` exists |
| Timeout errors | Tool taking too long | Bash tools have 120s limit |
| Context too large | Too much history | Manual `compact()` call |

---

## Testing Workflow

### Unit Test
```bash
cd src
go test ./internal/...
```

### Integration Test
```bash
echo "read main.go" | ./evo-agent
```

### MCP Test
```bash
# Check mcp.json is valid JSON
jq . .evo-agent/mcp.json
```

### Skill Test
```bash
# Check skill format
ls -la .evo-agent/skill/*/SKILL.md
```

---

## Key Concepts

**Registry Pattern**: Tools self-register via `init()` → no central list needed

**Handler Interface**: All tools implement `func(json.RawMessage) (string, error)`

**MCP Prefix**: `mcp__` prefix routes to MCP dispatcher, not native handler

**Graceful Degradation**: Missing configs (MCP, skills) silently ignored

**Output Persistence**: Large outputs (>30KB) saved to disk, not forced into context

**Auto-Compaction**: Two-tier: micro-compact (lightweight), then LLM summarize (expensive)

**Multi-turn**: Single REPL, persistent history across queries

---

## Architecture Decisions

✅ **Tool registry**: Self-registering tools are more maintainable
✅ **MCP support**: Three transports cover most use cases
✅ **Skill injection**: Dynamic context loading without model retraining
✅ **Auto-compaction**: Prevents context bloat automatically
✅ **Output persistence**: Keeps UI responsive even with large outputs
✅ **Modular UI**: Easy to swap terminal for TUI later

---

## TUI Layer Opportunities

- [ ] Replace `bufio.Scanner` with interactive input widget
- [ ] Add panels for thinking/text/tools/results
- [ ] Real-time status dashboard (tokens, context size, compactions)
- [ ] Tool result viewer (expandable, searchable)
- [ ] MCP server list display
- [ ] Skill catalog browser
- [ ] File history sidebar
- [ ] Theme support (dark/light)

**Recommended framework**: Bubble Tea (charmbracelet/bubbletea)

**Integration approach**: Minimal changes - wrap Agent.Run(), capture stdout

---

## Documentation Files

- **EVO_AGENT_ARCHITECTURE.md**: Detailed architecture guide
- **TUI_INTEGRATION_GUIDE.md**: TUI layer design & implementation
- **QUICK_REFERENCE.md**: This file
- **go.mod**: All dependencies listed

