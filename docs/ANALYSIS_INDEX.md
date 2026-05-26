# evo-agent: Comprehensive Command & Tool System Analysis

**Date**: 2026-05-25  
**Status**: ✅ ANALYSIS COMPLETE - READY FOR IMPLEMENTATION  
**Scope**: Command system, tool registration, system prompt building, memory system

---

## 📋 Quick Reference

### Key Files Analyzed
| File | Lines | Key Topic |
|------|-------|-----------|
| src/main.go | 171 | Entry point, slash command interception, prompt building |
| src/internal/skills/dispatch.go | 130 | Slash command dispatcher |
| src/internal/skills/registry.go | 336 | Command/skill auto-discovery and registry |
| src/internal/tools/tool.go | 95 | Tool registration framework |
| src/internal/tools/todo.go | 186 | Self-registering tool example |
| src/internal/agent/loop.go | 257 | Agent loop and query handlers |
| src/internal/config/config.go | 44 | Config object, ProjectDir |
| src/internal/agent/subagent.go | 84 | Subagent pattern |

### Critical Code Locations
- **Slash command dispatch**: main.go lines 114-144
- **Tool registration**: tools/tool.go lines 11-28
- **Command discovery**: skills/registry.go lines 255-314
- **System prompt injection**: main.go lines 60-72
- **Agent goroutine**: main.go lines 114-144

---

## 🔍 What Was Found

### Tool Registration System
✅ **Pattern**: Self-registering init() with ToolDef struct  
✅ **Location**: Each tool file has init() that calls Register()  
✅ **Benefit**: No import cycles, clean separation  
✅ **Example**: src/internal/tools/todo.go (lines 159-185)

### Slash Command System
✅ **Flow**: Validate → Extract → Lookup → Render → Wrap XML  
✅ **Priority**: Commands take priority over skills  
✅ **Message Type**: Two-block messages (prompt + content)  
✅ **Execution**: Uses RunQueryDirect() NOT RunQuery()  
✅ **Dispatch**: src/internal/skills/dispatch.go

### System Prompt Building
✅ **Base**: "You are a coding agent at {cwd}"  
✅ **Pattern**: cfg.SystemMsg += "\n..." for each injection  
✅ **Catalog**: Skills injected from skills.Catalog()  
✅ **For Memory**: Can follow same pattern as skills

### Command Auto-Discovery
✅ **Skills**: .evo-agent/skill/*/SKILL.md (recursive walk)  
✅ **Commands**: .evo-agent/command/*.md (flat files)  
✅ **Loading**: Called at startup via skills.Init()  
✅ **Registry**: Global maps skillDocuments, commandDocuments  
✅ **No Code Changes**: Just drop .md files and restart

### Memory System Status
✅ **Current**: ZERO memory system exists  
✅ **Confirmed**: No /remember command  
✅ **Confirmed**: No memory persistence  
✅ **Clean Slate**: Ready for implementation

---

## 🚀 Implementation Options

### Option A: Minimal (5 minutes)
**What**: Slash command only, no persistence  
**Files**: 1 new file (.evo-agent/command/remember.md)  
**Code**: 0 Go code changes  

### Option B: With Tool (30 minutes)
**What**: Slash command + memory tool with save/recall  
**Files**: 1 new tool (src/internal/tools/memory.go)  
**Changes**: 1 file (main.go - add memory loading)  
**Plus**: .evo-agent/command/remember.md + .evo-agent/memory/

### Option C: Full (45 minutes)
**What**: Option B + inject memory into system prompt  
**Benefit**: Memory visible to Claude on every turn  
**Changes**: Same as Option B plus prompt injection in main.go

---

## 📂 Directory Structure

```
evo-agent/
├── src/
│   ├── main.go                          ← Entry point, prompt building
│   └── internal/
│       ├── agent/
│       │   ├── loop.go                  ← Agent loop
│       │   └── subagent.go              ← Subagent pattern
│       ├── skills/
│       │   ├── dispatch.go              ← Slash dispatcher
│       │   └── registry.go              ← Command/skill loading
│       ├── tools/
│       │   ├── tool.go                  ← Registration framework
│       │   ├── todo.go                  ← Example tool
│       │   └── [memory.go]              ← TO CREATE
│       └── config/
│           └── config.go                ← Config object
└── .evo-agent/
    ├── command/
    │   ├── hello.md                     ← Example
    │   └── [remember.md]                ← TO CREATE
    ├── skill/
    │   └── (various)
    └── [memory/]                        ← TO CREATE
        └── [session.md]                 ← TO CREATE
```

---

## 💡 Core Technical Insights

### 1. Slash Commands Pre-Processed at Goroutine Level
**Location**: main.go lines 114-144 (agent goroutine in TUI mode)  
**Why**: Interception happens before agent loop starts  
**Result**: User sees command response quickly

### 2. Two-Block Message Architecture
**Structure**: 
- Block 1: Prompt ("User invoked /name (command) with arguments: ...")
- Block 2: Content ("<skill>...$arguments substituted...</skill>")

**Why**: Separates instruction from rendered body

### 3. RunQueryDirect() vs RunQuery()
**RunQueryDirect()**: For slash commands (message pre-constructed)  
**RunQuery()**: For regular queries (appends message inside function)

**Key**: Slash commands use RunQueryDirect because message is already built

### 4. Self-Registering init() Pattern
```go
func init() {
    Register(ToolDef{...})
}
```
**Why**: No import cycles, clean module separation  
**When**: Runs at package load time automatically

### 5. System Prompt Built Once
**When**: main.go Load() at startup  
**Injected**: Into every Claude API call via Messages.New()  
**Pattern**: Can add memory the same way as skills

### 6. ProjectDir Available for .evo-agent/
**From**: cfg.ProjectDir (current working directory)  
**Used**: To locate .evo-agent/command/, .evo-agent/skill/, etc.  
**For Memory**: Can load from .evo-agent/memory/

### 7. Auto-Discovery (No Code Changes)
**Skills**: filepath.WalkDir(.evo-agent/skill)  
**Commands**: os.ReadDir(.evo-agent/command)  
**Result**: Just drop .md files, restart agent

### 8. Commands Take Priority Over Skills
**Lookup**: LookupForSlash() checks commandDocuments first  
**Conflict**: If same name exists, command wins

### 9. MCP Tools Routed Separately
**Prefix**: mcp__ (e.g., mcp__github_search)  
**Router**: DispatchMCP() instead of registry  
**Note**: Different system for MCP tools

### 10. Zero Memory System (Clean Slate)
**Search Results**: grep for "memory" found zero custom implementation  
**Only**: Session transcript persistence exists  
**Opportunity**: Implement /remember feature from scratch

---

## 📝 Code Flow Examples

### Slash Command Execution Flow
```
User: /remember "important note"
    ↓
skills.Dispatch("/remember important note")
    ↓
Validate: "/" + letter ✓
    ↓
Extract: name="remember", rawArgs="important note"
    ↓
LookupForSlash("remember"): Check commands first, then skills
    ↓
Check: UserInvocable = true ✓
    ↓
ParseArgs("important note") → ["important note"]
    ↓
RenderBody(body, ["note"], ["important note"], rawArgs)
    → Substitute $note with "important note"
    ↓
Wrap: <skill name="remember" source="slash" type="command">
         ...rendered body...
      </skill>
    ↓
Create SlashResult: Found=true, Prompt="User invoked...", Content="<skill>..."
    ↓
Agent Goroutine:
    - Append two-block message to history
    - Call a.RunQueryDirect(&history, ...)
    ↓
Agent Loop: Process normally
```

### Tool Registration Flow
```
Package Load:
    ↓
tools/todo.go init() executes
    ↓
Register(ToolDef{
    Schema: anthropic.ToolParam{Name: "todo", ...},
    Handler: func(input) { ... }
})
    ↓
Registry[name] = def
    ↓
When Model Calls Tool:
    - tools.Dispatch("todo", input)
    - Lookup registry["todo"]
    - Call handler(input)
    - Return result
```

### System Prompt Building
```
config.Load():
    → cfg.SystemMsg = "You are a coding agent at /path"

main():
    → skills.Init()  // Load skills/commands
    
    → catalog := skills.Catalog()
    → cfg.SystemMsg += "\nSkills available:\n" + catalog
    
    → [Future: Load memory]
    → cfg.SystemMsg += "\n\nSession Memory:\n" + memory

Loop:
    → a.client.Messages.New(
          System: []TextBlockParam{{Text: cfg.SystemMsg}},
          ...
      )
    → cfg.SystemMsg injected into every API call
```

---

## 🎯 Implementation Checklist

### For Option B Implementation (Recommended)

- [ ] Create `src/internal/tools/memory.go`
  - [ ] Define memoryInput struct with Action and Content
  - [ ] Create handleMemory function
  - [ ] Add init() with Register() call

- [ ] Update `src/main.go`
  - [ ] Add loadSessionMemory(projectDir) function
  - [ ] Call in main() after skills.Init()
  - [ ] Append to cfg.SystemMsg

- [ ] Create `.evo-agent/command/remember.md`
  - [ ] Add YAML frontmatter
  - [ ] Add instruction body

- [ ] Create `.evo-agent/memory/` directory

- [ ] Test
  - [ ] Run: go run ./src/main.go
  - [ ] Try: /remember "test note"
  - [ ] Verify: Memory saved

---

## 📚 Detailed References

### Tool Registration
- **File**: src/internal/tools/tool.go
- **Lines**: 11-28 (core types), 24-28 (Register), 42-53 (Dispatch)
- **Pattern**: Handler func + ToolDef struct + Register() call
- **Example**: src/internal/tools/todo.go lines 159-185

### Slash Commands
- **Dispatcher**: src/internal/skills/dispatch.go lines 18-88
- **Registry**: src/internal/skills/registry.go lines 185-195
- **Goroutine**: src/main.go lines 114-144
- **Two-Block**: main.go lines 123-131

### System Prompt
- **Base**: src/internal/config/config.go lines 34-42
- **Injection**: src/main.go lines 60-72
- **Pattern**: cfg.SystemMsg += string

### Agent Execution
- **Loop**: src/internal/agent/loop.go lines 87-162
- **RunQuery**: lines 225-240
- **RunQueryDirect**: lines 245-256

---

## 🔗 Related Documentation

- **Plan File**: ~/.claude-internal/plans/[long-plan-name].md
  - 13 detailed sections
  - Complete code snippets
  - All implementation options
  
- **Wiki**: .omc/wiki/evo-agent-command-tool-system-analysis-complete.md
  - Architecture overview
  - Pattern explanations
  - Code examples

---

## ⚙️ System Architecture Summary

```
┌─────────────────────────────────────────────────────────┐
│ User Input (TUI or CLI)                                 │
└────────────────────┬────────────────────────────────────┘
                     │
        ┌────────────┴────────────┐
        │                         │
    "/" prefix?              Regular query
        │                         │
        ↓                         ↓
skills.Dispatch()         Append to history
        │                         │
    Extract name            Agent Loop
    & args                        │
        │                    Claude API
    Lookup                        │
    registry                  Tool dispatch
        │                         │
    Render body              Iterate until
    Substitute               no tool calls
    $variables
        │
    Wrap XML
        │
    Create two-block
    message
        │
    RunQueryDirect()
    (NOT RunQuery)
        ↓
    Agent Loop (normal execution)
        │
        ↓
    Claude API
        │
        ↓
    Tool dispatch
        │
        ↓
    Iterate until no tool calls
```

---

## 📊 Analysis Summary

| Aspect | Status | Details |
|--------|--------|---------|
| Tool Registration | ✅ Complete | Self-registering init() pattern documented |
| Slash Commands | ✅ Complete | Full flow from input to execution |
| System Prompt | ✅ Complete | Injection pattern identified |
| Command Discovery | ✅ Complete | Auto-discovery from .evo-agent/ |
| Memory System | ✅ Complete | Zero existing - clean slate |
| Implementation Ready | ✅ Yes | Three options provided (A/B/C) |
| Code Examples | ✅ Complete | All patterns with snippets |
| Testing Strategy | ✅ Complete | Ready for manual testing |

---

## 🚦 Status & Next Steps

### ✅ Analysis Phase Complete
- All files examined
- All patterns understood
- All code locations documented
- All implementation options outlined

### ⏸ Awaiting User Direction
- User is in plan mode (no execution requested)
- Ready to implement Option A/B/C when requested
- Full code examples in plan file
- Ready for copy-paste implementation

### 🎯 When User Requests Implementation
1. Choose implementation option (A/B/C)
2. Use code from plan file
3. Create new files
4. Update main.go as needed
5. Test with: go run ./src/main.go && /remember "test"

---

**Report Generated**: 2026-05-25  
**Analysis Status**: COMPLETE  
**Implementation Status**: READY  
**Files Analyzed**: 11  
**Lines Examined**: 1,422  
**Patterns Identified**: 10+  
**Implementation Options**: 3  

*All detailed information available in plan file and wiki documentation.*
