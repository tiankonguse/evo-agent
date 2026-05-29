# MCP Integration Planning - Complete Summary

**Created**: 2026-05-28  
**Session**: Exploration Phase Complete | Planning Phase Complete | Ready for Implementation  
**Status**: ⏸️ AWAITING USER DECISIONS

---

## What Has Been Completed

### Phase 1: Exploration (Previous Session)
✅ **Analyzed evo-agent tool system in depth**:
- Tool registration pattern (self-registering init() functions)
- Tool dispatch architecture (centralized Dispatch() entry point)
- Agent loop integration (tools.Execute() → append results as user message)
- Existing tools: task (subagent), todo (session plan), plan (persistent tasks)
- State management patterns (GlobalTodo, GlobalPlan singletons with sync.RWMutex)
- Thread safety mechanisms (sync.RWMutex, proper locking)
- Schema generation (automatic from Go structs via jsonschema reflection)
- Error handling precedents (task.go shows pattern for error returns)
- Import cycle avoidance (callback injection pattern in task.go)

### Phase 2: Planning (This Session)
✅ **Comprehensive MCP Integration Plan Created**:

**Documents Created**:
1. **MCP_DECISIONS.md** (Decision Checklist)
   - 7 required decisions clearly defined with pro/con analysis
   - 2 optional decisions for future phases
   - Template format for easy user response

2. **MCP_IMPLEMENTATION_GUIDE.md** (Technical Implementation Roadmap)
   - Detailed file-by-file breakdown of Phase 1
   - Code structure examples for each new file
   - Integration points clearly documented
   - 7-step implementation process with validation checks
   - Error handling strategy tied to user decisions
   - Testing approach recommendations

3. **MCP Integration Strategy & Decision Framework** (Wiki)
   - Executive summary
   - 4-phase implementation roadmap
   - Decision points with analysis
   - Implementation readiness checklist
   - Success criteria

4. **TOOL_QUICK_REFERENCE.txt** (Previously created)
   - ASCII quick reference card for tool patterns
   - Visual guide to registry, dispatch, execution flow

5. **TOOL_PATTERNS.md** (Previously created)
   - Learning guide for tool system patterns
   - Code examples and explanations

6. **evo-agent-tool-patterns-analysis.md** (Previously created)
   - Deep technical analysis of tool system
   - 30+ code examples
   - Performance considerations

---

## Key Planning Decisions Made (Pre-Decision)

### Architecture Decisions
- ✅ MCP integration will use existing tool registry pattern (Register/Dispatch)
- ✅ New files: mcp_client.go (transport), mcp_tool_factory.go (discovery), mcp_config.go (config)
- ✅ Modified files: main.go (init/shutdown), tool.go (dispatch routing), config.go (config struct)
- ✅ Integration complexity: Medium (isolated new files, minimal changes to existing code)
- ✅ Risk level: Low (new code in separate files, proven patterns from existing tools)

### Configuration Approach
- ✅ Three viable options provided (env vars, hardcoded, config file)
- ✅ User to decide based on deployment model

### Error Handling Philosophy
- ✅ Three strategies provided (fail-hard, fail-soft, hybrid)
- ✅ User to decide based on reliability requirements

### Testing Strategy
- ✅ Multiple approaches outlined (mock, fixtures, containers, manual)
- ✅ User to decide based on testing requirements

---

## What's Ready for Implementation

### Code Patterns Available ✅
- Tool registry: `tools.Register(ToolDef{...})`
- Handler signature: `func(json.RawMessage) (string, error)`
- Tool discovery: `tools.Tools()` returns all registered tools
- Tool execution: `tools.Execute()` dispatches to handlers
- Schema generation: `GenerateSchema[T]()`
- Callback injection: `RegisterSubagentRunner()` pattern proven

### Architecture Validated ✅
- Singleton patterns (GlobalTodo, GlobalPlan precedent)
- Thread safety (sync.RWMutex established pattern)
- Error handling (task.go shows precedent)
- Import cycle avoidance (callback injection proven)
- Context handling (agent loop message flow understood)

### Documentation Complete ✅
- 1300+ lines of tool system documentation
- 30+ code examples provided
- Decision framework with pro/con analysis
- Implementation guide with file-by-file breakdown
- Testing strategies outlined
- Error handling patterns defined

---

## Required User Decisions (Before Implementation Can Begin)

### Decision #1: Target MCP Servers ⚠️ REQUIRED
**Question**: Which MCP servers should evo-agent integrate with?
**Examples**: Filesystem, Git, Database, Web, Custom?
**Status**: Awaiting answer

### Decision #2: Implementation Scope ⚠️ REQUIRED
**Question**: How much to implement now?
**Options**: Phase 1 only (4-6h) | Phases 1-2 (8-10h) | Phases 1-4 (14-20h)
**Status**: Awaiting answer

### Decision #3: Configuration Strategy ⚠️ REQUIRED
**Question**: How should MCP servers be discovered?
**Options**: Hardcoded | Environment variables | Config file | Auto-discovery
**Status**: Awaiting answer

### Decision #4: Server Lifecycle ⚠️ REQUIRED
**Question**: Should evo-agent spawn servers or connect to existing ones?
**Options**: Spawn (evo-agent owns) | Connect (user pre-starts)
**Status**: Awaiting answer

### Decision #5: Tool Namespacing ⚠️ REQUIRED
**Question**: How to handle tool name collisions?
**Options**: Prefix (mcp_fs:read_file) | Server naming (filesystem:read_file) | Priority-based
**Status**: Awaiting answer

### Decision #6: Error on Startup ⚠️ REQUIRED
**Question**: How to handle MCP server startup failures?
**Options**: Fail-hard (exit) | Fail-soft (warn & continue) | Hybrid (by criticality)
**Status**: Awaiting answer

### Decision #7: Mid-Call Failures ⚠️ REQUIRED
**Question**: If MCP server crashes during a tool call?
**Options**: Retry | Return error (agent decides) | Abort
**Status**: Awaiting answer

### Decision #8: Testing Strategy ⚠️ OPTIONAL
**Question**: How to test MCP integration?
**Options**: Mock servers | Test fixtures | Containers | Manual only
**Status**: Can defer, but helps prioritization

### Decision #9: Specific Priorities ⚠️ OPTIONAL
**Question**: Any specific features or servers to prioritize?
**Status**: Can defer

---

## How to Proceed

### Step 1: User Provides Decisions
Copy `MCP_DECISIONS.md` format and provide answers to 7 required decisions (and optionally 2 optional ones):

```
1. Filesystem and Git servers
2. Phases 1-2 (multi-server support)
3. Environment variables (MCP_SERVERS=...)
4. Spawn servers (evo-agent owns processes)
5. Server naming (filesystem:read_file)
6. Fail-soft (warn and continue)
7. Return error (agent decides)
8. Test fixtures (record/replay)
9. Priority: error handling robustness
```

### Step 2: Implementation Begins
Once decisions received:
1. Create config structure (mcp_config.go)
2. Create MCP client (mcp_client.go)
3. Create tool factory (mcp_tool_factory.go)
4. Integrate with tool registry
5. Integrate with main.go
6. Test end-to-end

**Estimated timeline**: 4-20 hours depending on scope

### Step 3: Testing & Validation
1. Unit tests for each new module
2. Integration tests with mock/real servers
3. Manual verification with real MCP server
4. Documentation of usage patterns

### Step 4: Documentation
1. Update README with MCP server setup instructions
2. Add examples of using MCP tools
3. Document new configuration options
4. Add troubleshooting guide

---

## File Organization

### New Documentation Files (Ready Now)
```
/evo-agent/
├── MCP_DECISIONS.md              ← Decision checklist (FILL THIS OUT)
├── MCP_IMPLEMENTATION_GUIDE.md   ← Technical implementation roadmap
├── PLANNING_SUMMARY.md           ← This file
├── TOOL_QUICK_REFERENCE.txt      ← Quick reference (created earlier)
├── TOOL_PATTERNS.md              ← Learning guide (created earlier)
└── .omc/wiki/
    ├── mcp-integration-strategy-decision-framework.md
    ├── evo-agent-tool-patterns-analysis.md (created earlier)
    └── [other existing wiki pages]
```

### Files to Create During Implementation
```
/src/
├── internal/
│   ├── tools/
│   │   ├── mcp_client.go        ← NEW (350 lines)
│   │   ├── mcp_tool_factory.go  ← NEW (250 lines)
│   │   └── [existing tools]
│   ├── config/
│   │   ├── mcp.go               ← NEW (100 lines)
│   │   └── [existing config]
│   └── agent/
│       └── [existing agent]
└── main.go                      ← MODIFIED (add MCP init/shutdown)
```

---

## Success Criteria for Completion

✅ **Planning Phase Complete When**:
- User provides answers to all 7 required decisions
- Decisions are consistent and feasible
- Implementation roadmap approved

✅ **Implementation Phase Complete When**:
- Phase 1 (or chosen phases) fully implemented
- All new files created and integrated
- Tests passing
- Manual verification with real MCP server successful
- Documentation updated

✅ **Full Integration Complete When**:
- All 4 phases implemented (if chosen)
- Production-hardened with logging/metrics
- Comprehensive documentation
- Example usage patterns demonstrated

---

## Timeline Estimates

| Phase | Effort | Status |
|-------|--------|--------|
| Exploration | 3-4 hours | ✅ COMPLETE |
| Planning | 2-3 hours | ✅ COMPLETE |
| Phase 1 Implementation | 4-6 hours | ⏸️ Awaiting decisions |
| Phase 2 Implementation | 3-4 hours | ⏸️ Optional |
| Phase 3 Implementation | 4-6 hours | ⏸️ Optional |
| Phase 4 Implementation | 3-4 hours | ⏸️ Optional |
| **Total (Phase 1 only)** | **9-13 hours** | ⏸️ Awaiting decisions |
| **Total (All phases)** | **17-25 hours** | ⏸️ Awaiting decisions |

---

## Key Resources

### Required for User
- MCP_DECISIONS.md (fill out and provide)

### For Developer/Implementer
- MCP_IMPLEMENTATION_GUIDE.md (step-by-step implementation)
- TOOL_PATTERNS.md (understand existing tool system)
- MCP Integration Strategy & Decision Framework (architecture reference)

### For Testing
- MCP specification: https://spec.modelcontextprotocol.io/
- Example MCP servers: https://github.com/modelcontextprotocol/servers

---

## Questions?

**About the planning approach**: See MCP_INTEGRATION_STRATEGY.md  
**About the implementation roadmap**: See MCP_IMPLEMENTATION_GUIDE.md  
**About the existing tool patterns**: See TOOL_PATTERNS.md  
**About the decision framework**: See MCP_DECISIONS.md

---

## Current Status

🔴 **BLOCKED**: Awaiting user input on 7 required decisions  
📋 **ACTION REQUIRED**: Fill out MCP_DECISIONS.md and provide answers  
✅ **READY**: All planning and preparation complete, can implement immediately once decisions provided

