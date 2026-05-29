# Evo-Agent Tool System Analysis

Complete documentation of the evo-agent Go project's tool architecture, patterns, and implementation details.

## 📚 Documentation Files

Three complementary documents are provided, each serving a different purpose:

### 1. **TOOL_QUICK_REFERENCE.txt** (27 KB) - START HERE
Visual reference card with ASCII diagrams and quick lookups.

**Contents:**
- 5 core patterns overview
- Tool registration pattern with code examples
- Agent loop flow diagram
- Singleton state managers reference
- Initialization sequence diagram
- Error handling table
- Thread safety reference
- Key files & locations
- "Adding a new tool" example

**Best for:** Quick lookups during development, visual learners, reference during implementation

---

### 2. **TOOL_PATTERNS.md** (9.3 KB) - QUICK LEARNING
Concise reference with code examples and practical patterns.

**Contents:**
- The 5 core patterns (expanded)
- Schema generation pattern
- Agent loop integration flow
- Thread safety table
- Initialization sequence with code
- Adding a new tool (step-by-step)
- Common patterns (stateless, stateful, validation)
- Key files table
- Error handling scenarios
- Performance considerations
- Architecture summary diagram

**Best for:** Getting up to speed quickly, understanding patterns, implementing new tools

---

### 3. **evo-agent-tool-patterns-analysis.md** (30 KB) - DEEP DIVE
Comprehensive analysis with extensive code examples and detailed explanations.

**Contents:**
- Executive summary of all 5 patterns
- Tool registry pattern (structure, registration, API retrieval, dispatch, schema generation)
- Task tool: subagent delegation (callback registration, input schema, handler, characteristics)
- Todo tool: session planning (state manager, update with validation, reminders, rendering, TUI integration)
- Plan tool: persistent task graph (disk layout, task record, bidirectional dependencies, operations)
- Tool execution flow (main dispatcher, output handling, result collection)
- Agent loop integration (full flow, tool results as user message)
- Schema generation pattern (input struct, automatic generation, handler pattern)
- Avoiding recursive tool calls (ToolsExcept function)
- Initialization sequence (startup order with explanations)
- Key insights (state management table, error handling, performance)
- Registration examples (minimal, with state, with singleton manager)
- Summary table of all concepts

**Best for:** Understanding the complete system, deep technical knowledge, documentation, teaching

---

## 🎯 How to Use These Documents

### If you have 5 minutes:
Read **TOOL_QUICK_REFERENCE.txt** - Get the visual overview and key patterns.

### If you have 15 minutes:
Read **TOOL_PATTERNS.md** - Understand each pattern with code examples and practical applications.

### If you have 30+ minutes:
Read **evo-agent-tool-patterns-analysis.md** - Full technical deep dive with architecture details and comprehensive examples.

### If you're implementing:
1. Start with **TOOL_QUICK_REFERENCE.txt** for the pattern overview
2. Reference **TOOL_PATTERNS.md** for the specific pattern you need
3. Consult **evo-agent-tool-patterns-analysis.md** for edge cases and detailed behavior

### If you're debugging:
1. Use **TOOL_QUICK_REFERENCE.txt** to find the relevant section (error handling, thread safety, etc.)
2. Cross-reference with **TOOL_PATTERNS.md** for code examples
3. Go to **evo-agent-tool-patterns-analysis.md** for detailed explanations

---

## 📖 The 5 Core Patterns at a Glance

| # | Pattern | File | Purpose | Key Insight |
|---|---------|------|---------|------------|
| **1** | **Tool Registry** | tool.go | Auto-discover + dispatch tools | Each tool calls `Register()` in `init()` |
| **2** | **Task Tool** | task.go | Spawn subagents with fresh context | Callback injection avoids import cycles |
| **3** | **Todo Manager** | todo.go | Session plan with stale detection | Singleton with round-tracking for reminders |
| **4** | **Plan Manager** | plan.go | Persistent task graph on disk | Bidirectional dependencies + status tracking |
| **5** | **Executor** | executor.go | Run tool calls + collect results | Single function processes all response blocks |

---

## 🏗️ Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                    AGENT LOOP (loop.go)                         │
│  autoCompact → LLM Call → Execute Tools → Inject Results → Loop │
└─────────────────────────────────────────────────────────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        │                     │                     │
        v                     v                     v
   TOOLS.DISPATCH()     TOOL RESULTS           STATE MANAGERS
        │                (executor.go)              │
        ├─→ [tool.go]                     ┌────────┼────────┐
        │   Registry                      │        │        │
        ├─→ [task.go]              GlobalTodo GlobalPlan GlobalMemory
        │   task dispatch                (memory) (disk)   (SQLite)
        ├─→ [todo.go]
        │   session plan    ┌──────────────────────┐
        ├─→ [plan.go]       │   INITIALIZATION     │
        │   tasks           │   (main.go)          │
        ├─→ [bash.go]       │                      │
        │   execution   Config → MCP → Memory
        └─→ [read_file.go]     → Plan → Skills
            ... (10+ more)     → Agent.New()
```

---

## 🔧 Common Tasks

### Adding a New Tool
See **TOOL_PATTERNS.md** section "Adding a New Tool" or **evo-agent-tool-patterns-analysis.md** section "11. Registration Examples".

Quick version:
1. Create `src/internal/tools/mytool.go`
2. Define input struct with `jsonschema_description` tags
3. Call `Register()` in `init()` function
4. Implement handler that unmarshals input and processes it

### Understanding Tool Dispatch
See **TOOL_QUICK_REFERENCE.txt** sections:
- "THE 5 CORE PATTERNS" → Pattern 1 (Registry)
- "TOOL REGISTRATION PATTERN"

### Setting Up Persistent State
See **TOOL_PATTERNS.md** or **evo-agent-tool-patterns-analysis.md** section "4. Plan Tool: Persistent Task Graph".

### Handling Tool Errors
See **TOOL_QUICK_REFERENCE.txt** section "ERROR HANDLING" or **evo-agent-tool-patterns-analysis.md** section "10. Key Insights" → Error Handling.

### Understanding the Agent Loop
See **TOOL_QUICK_REFERENCE.txt** section "AGENT LOOP FLOW" or **evo-agent-tool-patterns-analysis.md** section "6. Agent Loop Integration".

---

## 📋 File Structure

```
evo-agent/
├── src/
│   ├── main.go                         # Startup sequence
│   ├── internal/
│   │   ├── agent/
│   │   │   └── loop.go                 # Agent loop + subagent spawning
│   │   └── tools/
│   │       ├── tool.go                 # ← CORE: Registry + dispatch
│   │       ├── task.go                 # Subagent delegation
│   │       ├── todo.go                 # Session planning
│   │       ├── plan.go                 # Persistent task graph
│   │       ├── executor.go             # Tool execution
│   │       ├── memory.go               # Memory management
│   │       ├── bash.go                 # Bash execution
│   │       ├── read_file.go            # File reading
│   │       ├── write_file.go           # File writing
│   │       └── ... (10+ more tools)
│
├── TOOL_PATTERNS.md                    # ← Quick learning guide
├── TOOL_QUICK_REFERENCE.txt            # ← Visual reference card
└── README_TOOL_ANALYSIS.md             # ← This file

# Generated documentation (in parent directory):
├── /Users/tiankonguse-m3/
│   └── evo-agent-tool-patterns-analysis.md  # ← Deep dive (30KB)
```

---

## 🔑 Key Concepts

### Tool Registry Pattern
- **What:** Central registry for all tools
- **How:** Each tool calls `Register()` in its `init()` function
- **Why:** Decoupled, modular, no central file needed
- **File:** `tool.go` (core) + all `*_tool.go` files (implementations)

### Schema Generation
- **What:** Auto-generate Anthropic tool schemas from Go structs
- **How:** Use `GenerateSchema[T]()` with struct tags
- **Why:** Type-safe, prevents schema/handler mismatch
- **Pattern:** Struct tags → `jsonschema_description:"..."` → `GenerateSchema[T]()`

### Singleton Pattern
- **What:** Process-wide state managers for tools
- **Examples:** `GlobalTodo`, `GlobalPlan`, `GlobalMemory`
- **Thread-Safe:** All use `sync.RWMutex`
- **Lifecycle:** Initialized in `main()`, used throughout session

### Callback Injection
- **What:** Register callbacks at runtime to avoid import cycles
- **Example:** `subagentRunner` callback in `task.go`
- **Why:** Breaks circular dependency between `agent` and `tools` packages
- **Pattern:** `var fn func(...); func Register(f func(...)) { fn = f }`

### Tool Results as User Messages
- **What:** Tool results become the next user message in conversation
- **Why:** Keeps Claude in the loop; it sees its tool results as new input
- **Pattern:** `Messages = append(Messages, NewUserMessage(toolResults...))`

### Stale Detection
- **What:** Reminders when agent hasn't updated its plan
- **How:** Round counter; reminder after N turns without plan update
- **Where:** Injected into tool results by agent loop
- **File:** `todo.go` (GlobalTodo singleton)

---

## 💡 Best Practices

### When Adding a New Tool
1. ✅ Keep handler logic concise
2. ✅ Use struct tags for all input fields with descriptions
3. ✅ Validate input in handler; return error if invalid
4. ✅ Return concise text summaries (use `PersistLargeOutput()` for large results)
5. ❌ Don't import `agent` package (causes cycles)

### Error Handling
1. ✅ Return `(msg, error)` for user input errors
2. ✅ Catch errors in `Execute()` - they set the error flag
3. ✅ Return `("", nil)` if tool not found (silent)
4. ✅ Return `("Error: ...", nil)` for dependency issues (visible to model)

### Thread Safety
1. ✅ Use `sync.RWMutex` for shared state
2. ✅ Lock entire operation (don't lock/unlock mid-operation)
3. ✅ Use `defer` for lock cleanup
4. ✅ Pass immutable data between goroutines

### Performance
1. ✅ Keep tool handlers fast (non-blocking)
2. ✅ Use `PersistLargeOutput()` for big results
3. ✅ Micro-compact is cheap, full compact is expensive
4. ✅ Batch reads/writes to disk when possible

---

## 🚀 Getting Started

### Step 1: Understand the Patterns
Read **TOOL_PATTERNS.md** (15 min) or **TOOL_QUICK_REFERENCE.txt** (10 min)

### Step 2: Review Existing Tools
Open `src/internal/tools/` and read `task.go`, `todo.go`, `plan.go` side-by-side with the documentation

### Step 3: Trace the Agent Loop
Follow the flow in `src/internal/agent/loop.go` using the "Agent Loop Flow" section of the documentation

### Step 4: Implement
Use "Adding a New Tool" section as a template for your first tool

### Step 5: Debug
Use error handling and thread safety sections when issues arise

---

## 📞 Quick Reference

### Main Entry Point
**File:** `src/main.go`
**Purpose:** Startup sequence, initialization order

### Agent Loop
**File:** `src/internal/agent/loop.go`
**Purpose:** Agentic loop, tool dispatch, result injection

### Tool Registry
**File:** `src/internal/tools/tool.go`
**Purpose:** Central registry, dispatch, schema generation

### Tool Implementations
**Directory:** `src/internal/tools/`
**Examples:** `task.go`, `todo.go`, `plan.go`, `bash.go`, `read_file.go`

### State Management
**Singletons:** 
- `GlobalTodo` in `todo.go` (session plan)
- `GlobalPlan` in `plan.go` (persistent tasks)
- `GlobalMemory` in `memory.go` (memories)

---

## 📝 Notes

- **Go Version:** Requires generics (Go 1.18+)
- **Schema Generation:** Uses `jsonschema.Reflector` from invopop/jsonschema
- **Anthropic SDK:** Uses `anthropic-sdk-go` for API calls
- **Context Window:** ~200K tokens (context limit for auto-compaction)

---

## 🎓 Learning Path

1. **5 min:** Skim **TOOL_QUICK_REFERENCE.txt** for patterns overview
2. **10 min:** Read "Adding a New Tool" section
3. **15 min:** Read **TOOL_PATTERNS.md** for detailed patterns
4. **20 min:** Review `src/internal/tools/task.go` and `todo.go` side-by-side with docs
5. **30 min:** Trace agent loop in `src/internal/agent/loop.go`
6. **1+ hour:** Deep dive into **evo-agent-tool-patterns-analysis.md** for full understanding

---

## 📄 File Manifest

| File | Size | Purpose | Read Time |
|------|------|---------|-----------|
| TOOL_QUICK_REFERENCE.txt | 27 KB | Visual reference with diagrams | 10 min |
| TOOL_PATTERNS.md | 9.3 KB | Quick learning guide | 15 min |
| evo-agent-tool-patterns-analysis.md | 30 KB | Complete technical deep dive | 45 min |
| README_TOOL_ANALYSIS.md | (this file) | Navigation and index | 5 min |

**Total Documentation:** 66+ KB, 1382+ lines, 14+ patterns, 30+ code examples

---

**Generated:** 2026-05-28  
**Project:** evo-agent Go LLM agent framework  
**Analysis Scope:** tool.go, task.go, todo.go, plan.go, executor.go, loop.go, main.go

