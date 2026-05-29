# MCP Integration - Decision Checklist

**Status**: Awaiting user input on all items below  
**Created**: 2026-05-28  
**Phase**: Planning (no implementation yet)

---

## Required Decisions Before Implementation

### 1. Target MCP Servers ⚠️ REQUIRED

What MCP servers should evo-agent integrate with?

**Examples**:
- [ ] Filesystem (read/write files)
- [ ] Git (repo operations)
- [ ] Database (SQL queries)
- [ ] Web (HTTP requests)
- [ ] Custom/proprietary servers?

**Decision**: _______________________

---

### 2. Implementation Scope ⚠️ REQUIRED

How much should we implement now?

- [ ] **Phase 1 only** (basic stdio client, single server)
  - Estimated effort: 4-6 hours
  - Delivers: Basic MCP support
  - Pro: Fast, validate approach
  - Con: Limited to one server

- [ ] **Phases 1-2** (multi-server support)
  - Estimated effort: 8-10 hours
  - Delivers: Multiple servers with namespacing
  - Pro: Production-ready for multiple servers
  - Con: More upfront work

- [ ] **Phases 1-4** (full implementation)
  - Estimated effort: 14-20 hours
  - Delivers: Production-hardened MCP integration
  - Pro: Complete solution
  - Con: Longer implementation time

**Decision**: _______________________

---

### 3. Configuration Strategy ⚠️ REQUIRED

How should MCP servers be configured?

- [ ] **Hardcoded** (src/internal/tools/mcp.go)
  - Pro: Simplest
  - Con: Requires code change to modify

- [ ] **Environment variables** (MCP_SERVERS=...)
  - Pro: Flexible, CI/CD friendly
  - Con: Parsing complexity

- [ ] **Config file** (.tasks/mcp_config.yaml or .env)
  - Pro: Human-readable, version-controlled
  - Con: File loading complexity

- [ ] **Auto-discovery** (scan ~/bin, $PATH)
  - Pro: Zero configuration
  - Con: Unpredictable behavior

**Decision**: _______________________

**If Config File Selected**: Where? (.tasks/mcp_config.yaml or .env?)
**Answer**: _______________________

---

### 4. Server Lifecycle Management ⚠️ REQUIRED

Should evo-agent spawn MCP servers or connect to pre-started ones?

- [ ] **Spawn servers** (evo-agent owns processes)
  - Pro: User-friendly, automatic
  - Con: More complex code, requires binaries

- [ ] **Connect to existing** (user pre-starts servers)
  - Pro: Simpler code
  - Con: More user setup required

**Decision**: _______________________

---

### 5. Tool Namespacing ⚠️ REQUIRED

How to handle tool name collisions across servers?

- [ ] **Prefix namespacing** (mcp_fs:read_file, mcp_git:read_file)
  - Pro: No collisions
  - Con: Verbose

- [ ] **Server naming** (filesystem:read_file, git:read_file)
  - Pro: Clear origin
  - Con: User must know server names

- [ ] **Priority-based** (built-in > Server A > Server B)
  - Pro: Can override built-ins
  - Con: Complex logic, confusing behavior

**Decision**: _______________________

---

### 6. Error Handling on Startup ⚠️ REQUIRED

If an MCP server fails to connect, should evo-agent:

- [ ] **Fail-hard** (exit with error)
  - Pro: Guarantees all servers work
  - Con: Blocks on any server issue

- [ ] **Fail-soft** (warn and continue)
  - Pro: Resilient, user-friendly
  - Con: Might hide issues

- [ ] **Hybrid** (critical servers → fail-hard, optional → fail-soft)
  - Pro: Best of both
  - Con: Adds complexity

**Decision**: _______________________

---

### 7. Mid-Conversation Failure Handling ⚠️ REQUIRED

If an MCP server crashes during a tool call:

- [ ] **Retry immediately** (restart server, retry call)
  - Pro: Transparent recovery
  - Con: Might hide real issues

- [ ] **Return error** (append error as tool result, agent decides)
  - Pro: Agent has control
  - Con: Agent must handle errors

- [ ] **Abort turn** (mark failed, ask user)
  - Pro: Safe, explicit
  - Con: Breaks flow

**Decision**: _______________________

---

### 8. Testing Strategy ⚠️ OPTIONAL (can defer)

How to test MCP integration?

- [ ] **Mock servers** (unit tests with stubs)
  - Pro: Fast, isolated
  - Con: Doesn't catch real protocol issues

- [ ] **Test fixtures** (record/replay real responses)
  - Pro: Fast + realistic
  - Con: Fixtures can get stale

- [ ] **Test containers** (real servers in Docker)
  - Pro: Realistic
  - Con: Slower, requires Docker

- [ ] **Manual testing only** (no automated tests initially)
  - Pro: Fastest
  - Con: Regression risk

**Decision**: _______________________

---

### 9. Specific Priorities ⚠️ OPTIONAL

Any specific features or servers you want prioritized?

**Features you want in Phase 1**:
_______________________

**Features you want deferred to later**:
_______________________

---

## Summary of Decisions

Once all required items (⚠️) are completed, use this summary:

```
IMPLEMENTATION PARAMETERS:
- Target servers: [decision #1]
- Scope: [decision #2]
- Config: [decision #3]
- Lifecycle: [decision #4]
- Namespacing: [decision #5]
- Error on startup: [decision #6]
- Mid-call failures: [decision #7]
- Testing: [decision #8]
- Priorities: [decision #9]

Ready to proceed with code generation:
- Code files to create: mcp_client.go, mcp_tool_factory.go, mcp_config.go
- Files to modify: main.go, tool.go, config.go
- Estimated effort: [based on scope]
- Timeline: [based on scope]
```

---

## How to Provide Your Answers

Please respond with:
1. Each decision number (e.g., "1. Filesystem, Git, Database")
2. Selected option (e.g., "2. Phases 1-2" or just "1-2")
3. Specific answers where indicated (e.g., "3. Environment variables, MCP_SERVERS=...")

Example:

```
1. Filesystem and Git servers
2. Phases 1-2 (multi-server support)
3. Environment variables (MCP_SERVERS=...)
4. Spawn servers (evo-agent owns processes)
5. Server naming (filesystem:read_file)
6. Fail-soft (warn and continue)
7. Return error (agent decides)
8. Test fixtures (record/replay)
9. Priority: error handling over logging
```

Once received, implementation begins immediately.
