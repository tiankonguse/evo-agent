# EVO-Agent Codebase Exploration Summary

**Date**: May 18, 2026  
**Explorer**: Explore Agent (a9083426cf7709dc8)  
**Project**: `/Users/tiankonguse-m3/project/github/AIProject/evo-agent`

---

## What I Found

You have a **well-architected LLM agent framework** written in Go. It's designed to coordinate multi-turn conversations with Claude, dispatch tools (both native and MCP), manage context automatically, and inject specialized skills dynamically.

### Core Strengths

✅ **Clean separation of concerns**: Agent loop, tool dispatch, config, UI all isolated  
✅ **Extensible tool system**: Self-registering tools via registry pattern  
✅ **MCP support**: Three transports (stdio, SSE, HTTP) for flexibility  
✅ **Smart context management**: Two-tier auto-compaction (lightweight + LLM)  
✅ **Skill injection**: Dynamic instruction loading without model retraining  
✅ **Output persistence**: Large results saved to disk, not forced into context  
✅ **Stateful REPL**: Multi-turn sessions with persistent history  

---

## Architecture Quick View

```
main()
  ├─ Load config (MODEL_ID, API_KEY, BASE_URL)
  ├─ Initialize MCP servers from .evo-agent/mcp.json
  ├─ Load skills from .evo-agent/skill/**/SKILL.md
  └─ Start REPL: Agent.Run(stdin)
      └─ Loop: Read query → Agent.Loop() → Print response
          └─ Agent.Loop():
              ├─ Auto-compact if context > 50K chars
              ├─ Call LLM (claude-3-5-sonnet-20241022 recommended)
              ├─ Dispatch tool calls (native or MCP)
              └─ Loop until stop_reason == "end_turn"
```

---

## Three Documentation Files Created

I've created comprehensive guides in your project directory:

### 1. **EVO_AGENT_ARCHITECTURE.md** (13 sections, ~500 lines)
Deep dive into every component:
- Directory structure & file purposes
- Main entry point initialization
- Agent loop & message flow
- Tool system & registry pattern
- MCP integration (stdio/SSE/HTTP)
- Skills system (SKILL.md format)
- Context management & auto-compaction
- UI/output system (current ANSI colors)
- Dependencies & key files
- TUI layer opportunities

### 2. **TUI_INTEGRATION_GUIDE.md** (14 sections, ~600 lines)
Actionable TUI design guide:
- Current vs proposed architecture
- Recommended approach (Option A: minimal changes)
- Step-by-step implementation with code examples
- Integration points (input, output, state access)
- Layout mockup for 4-panel TUI
- Files to create/modify (minimal changes needed)
- Dependency options (Bubble Tea recommended)
- Data flow & real-time dashboard design
- Testing strategy & implementation phases
- Performance considerations & migration path

### 3. **QUICK_REFERENCE.md** (20 sections, ~400 lines)
Handy cheat sheet:
- Project at a glance (table)
- File organization tree
- Environment variables
- Build & run commands
- All native tools explained
- MCP & skills configuration
- State management structures
- How to extend (add tools/skills)
- Debugging tips
- Common issues & fixes
- Key concepts explained

---

## Files in Project

### Must-Read (In Order)
1. `src/main.go` (58 lines)
2. `src/internal/agent/loop.go` (193 lines)
3. `src/internal/tools/tool.go` (68 lines)
4. `src/internal/tools/executor.go` (57 lines)
5. `src/internal/tools/mcp.go` (696 lines!)
6. `src/internal/agent/compact.go` (213 lines)

### Key Packages
| Package | Purpose | Files |
|---------|---------|-------|
| `agent` | Loop & state | loop.go, state.go, compact.go, transcripts.go |
| `tools` | Registry & dispatch | tool.go, executor.go, mcp.go, + 6 native tools |
| `config` | Environment config | config.go |
| `ui` | Output rendering | terminal.go (ANSI colors only) |
| `skills` | Skill loading | registry.go |

### Size & Scope
- **Total Go files**: 22
- **Total lines**: ~3,000 (excluding vendor)
- **Main logic**: ~1,500 lines (agent loop, tools, MCP)
- **MCP implementation**: ~700 lines (very comprehensive)

---

## Key Insights

### 1. Tool Dispatch Pattern
Every tool is registered once in `init()`:
```go
func init() {
    Register(ToolDef{
        Schema: anthropic.ToolParam{...},
        Handler: func(input json.RawMessage) (string, error) {...},
    })
}
```
No central registry file needed. Dispatch routes to handler or MCP prefix.

### 2. MCP is a Client, Not a Server
You're implementing the **MCP Client** side:
- Connects to MCP servers via stdio/SSE/HTTP
- Calls their tools via JSON-RPC
- Exposes tools to Claude as `mcp__{server}__{tool}`
- Very comprehensive: 696 lines of protocol handling

### 3. Context Auto-Management
Two-tier approach:
- **MicroCompact** (cheap): Replace old tool results with placeholder
- **CompactHistory** (expensive): Call LLM to summarize entire conversation
- Threshold: 50,000 chars triggers compaction
- Recent files tracked (FIFO, max 5) for re-opening

### 4. Skills Are Loaded, Not Trained
Skills are just markdown files with YAML frontmatter:
```yaml
---
name: "My Skill"
description: "Short description"
---
Full instructions here...
```
Injected into context via `load_skill` tool or system prompt.

### 5. UI is Minimal (ANSI Only)
Current output system:
- 5 Print functions (Thinking, Text, ToolCall, Command, Error)
- ANSI color codes only
- No widgets, no panels, no interactivity
- **Perfect entry point for TUI layer**

---

## What's Missing (TUI Opportunities)

### Current Limitations
❌ No interactive input widget (just `bufio.Scanner`)
❌ No scrollable history (all output sequential)
❌ No real-time dashboard (token usage, context size)
❌ No tool result viewer (can't expand/collapse)
❌ No MCP server list display
❌ No skill catalog browser
❌ No file history sidebar
❌ No theme support

### Recommended TUI Additions (Priority Order)
1. **Interactive input** (top priority for UX)
2. **Multi-panel layout** (history, status, input)
3. **Real-time metrics** (tokens, context, compactions)
4. **Tool result viewer** (expandable tool outputs)
5. **Dashboard** (MCP servers, skills, recent files)

---

## TUI Integration Strategy

### Recommended Approach: **Minimal Changes**
- Create new `src/internal/tui/` package
- Add `--tui` flag to `main.go`
- Wrap `Agent.Run()` with TUI event loop
- **Keep all agent logic unchanged**
- Capture stdout from agent, route to TUI panels
- Parse ANSI colors to identify message types

### Why This Works
✅ Zero changes to core agent loop  
✅ Tool dispatch completely unaffected  
✅ Easy to toggle between terminal and TUI  
✅ Can test each independently  
✅ Maintains scriptability (pipe input/output)  

### Framework Choice
**Bubble Tea** (charmbracelet/bubbletea)
- Elegant model-update-view pattern
- Great for terminal apps
- Active community
- Matches Go idioms

---

## Next Steps for You

### If You Want to Build a TUI:
1. Read **TUI_INTEGRATION_GUIDE.md** (focus on "Recommended Approach")
2. Create `src/internal/tui/` package with Bubble Tea
3. Implement 4-panel layout (history, input, status, debug)
4. Add output parser to identify message types from ANSI codes
5. Route agent stdout to TUI panels
6. Test with simple queries first

### If You Want to Understand the Codebase Better:
1. Read **QUICK_REFERENCE.md** first (5 min overview)
2. Read **EVO_AGENT_ARCHITECTURE.md** section by section (30 min deep dive)
3. Build and run the agent: `cd src && go build && ./evo-agent`
4. Try the tools: `read_file`, `write_file`, `bash`, `load_skill`
5. Check the code: trace from `main()` → `loop.go` → `executor.go`

### If You Want to Extend Features:
1. **Add a tool**: Copy pattern from `src/internal/tools/bash.go`, add to init()
2. **Add a skill**: Create `.evo-agent/skill/my-skill/SKILL.md` with frontmatter
3. **Configure MCP**: Add server to `.evo-agent/mcp.json`, tools appear automatically
4. **Tweak limits**: Context limit = 50K, bash timeout = 120s, output = 50KB

---

## Dependency Graph

```
main.go
├─ config         (loads .env, MODEL_ID, API_KEY)
├─ anthropic-sdk-go (Claude API client)
├─ tools
│  ├─ tool.go           (registry)
│  ├─ executor.go       (dispatch & output)
│  ├─ mcp.go            (MCP client)
│  ├─ skill.go          (load_skill tool)
│  └─ [bash,read,write,edit,compact].go (native tools)
├─ agent
│  ├─ loop.go           (agent loop)
│  ├─ state.go          (LoopState, CompactState)
│  ├─ compact.go        (context management)
│  └─ transcripts.go    (session recording)
├─ skills
│  └─ registry.go       (load from .evo-agent/skill/)
└─ ui
   └─ terminal.go       (ANSI colors)

External packages:
├─ github.com/anthropics/anthropic-sdk-go v1.41.0
├─ github.com/invopop/jsonschema v0.13.0
└─ github.com/joho/godotenv v1.5.1
```

---

## Key Metrics

| Metric | Value |
|--------|-------|
| **Go Version** | 1.26 |
| **Module** | `evo-agent` |
| **Main logic lines** | ~1,500 |
| **Test coverage** | Minimal (one test file) |
| **External deps** | 3 major + transitive |
| **Context limit** | 50,000 chars |
| **Auto-compact threshold** | 50,000 chars |
| **Recent files tracked** | 5 (FIFO) |
| **Bash timeout** | 120 seconds |
| **Tool output limit** | 50KB (file persistence at 30KB) |
| **MCP clients supported** | 3 (stdio, SSE, HTTP) |
| **Native tools** | 6 (read, write, edit, bash, skill, compact) |

---

## Files Created for You

All in `/Users/tiankonguse-m3/project/github/AIProject/evo-agent/`:

1. **EVO_AGENT_ARCHITECTURE.md** (650 lines)
   - Comprehensive breakdown of every component
   - Code flow diagrams
   - Design patterns explained
   - TUI integration opportunities

2. **TUI_INTEGRATION_GUIDE.md** (650 lines)
   - Current architecture diagram
   - Two integration approaches (recommend Option A)
   - Step-by-step implementation guide
   - Code examples for each layer
   - Testing strategy & phases

3. **QUICK_REFERENCE.md** (400 lines)
   - One-page cheat sheet
   - Tool reference
   - Configuration examples
   - How to extend
   - Troubleshooting

4. **README_EXPLORATION.md** (this file)
   - Summary of what was found
   - Highlights & insights
   - Next steps

---

## Conclusion

EVO-Agent is a **solid, well-designed framework** for multi-turn LLM agent workflows. The codebase is clean, extensible, and ready for a TUI layer. The tool system is elegant, MCP support is comprehensive, and context management is thoughtful.

**Biggest opportunity**: The UI is currently terminal-only with ANSI colors. A TUI layer with Bubble Tea would provide:
- Real-time visibility into agent state
- Better organization of information (panels)
- Interactive capabilities (expandable results, file browser)
- Professional appearance

**Best approach**: Wrap the existing Agent.Run() with TUI event loop. No changes to core logic needed.

---

## Questions to Ask Yourself

1. **Do you want a TUI?** → Yes: Start with Phase 1 from TUI_INTEGRATION_GUIDE.md
2. **Do you want to extend features?** → See "How to extend" in QUICK_REFERENCE.md
3. **Do you want to add MCP servers?** → Create .evo-agent/mcp.json with server config
4. **Do you want to add skills?** → Create .evo-agent/skill/name/SKILL.md with frontmatter
5. **Do you want to understand it better?** → Read EVO_AGENT_ARCHITECTURE.md sections 1-8

---

## Resources Generated

📄 **EVO_AGENT_ARCHITECTURE.md** - Full architectural deep dive  
📄 **TUI_INTEGRATION_GUIDE.md** - Actionable TUI design & implementation  
📄 **QUICK_REFERENCE.md** - Handy cheat sheet & troubleshooting  
📄 **README_EXPLORATION.md** - This summary (you are here)  

Total: ~1,700 lines of documentation covering every aspect of the codebase.

---

**Happy coding!** 🚀

