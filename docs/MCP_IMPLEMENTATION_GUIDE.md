# MCP Integration - Implementation Guide

**Status**: Ready for Implementation (Awaiting User Decisions)  
**Created**: 2026-05-28  
**Version**: Phase Planning & Preparation Complete

---

## Quick Start

### Prerequisites
Before implementation can begin, user must provide answers to 7 required decisions in `MCP_DECISIONS.md`:

1. Target MCP Servers (which servers to integrate?)
2. Implementation Scope (Phase 1 only, 1-2, or 1-4?)
3. Configuration Strategy (env vars, config file, hardcoded?)
4. Server Lifecycle (spawn or connect to existing?)
5. Tool Namespacing (prefix, naming, or priority-based?)
6. Error Handling on Startup (fail-hard, fail-soft, or hybrid?)
7. Mid-Conversation Failures (retry, error result, or abort?)

**See**: `/MCP_DECISIONS.md` for decision checklist

---

## Architecture Overview

### Existing Infrastructure (Already Available)

```
src/main.go
  ├─ InitMCP() / ShutdownMCP()      [Already exists]
  ├─ tools.Init()                   [Already exists]
  └─ agent.New()                    [Already exists, registers subagent callback]

src/internal/tools/
  ├─ tool.go                        [Registry pattern: Register(), Dispatch(), Tools()]
  ├─ executor.go                    [Execute() routes tool calls]
  ├─ todo.go                        [GlobalTodo singleton with state management]
  ├─ plan.go                        [GlobalPlan for persistent tasks]
  └─ task.go                        [Subagent callback pattern]

src/internal/agent/
  └─ loop.go                        [Loop() orchestrates agent conversation]
```

### New Components (To Be Created in Phase 1)

```
src/internal/tools/
  ├─ mcp_client.go                  [New] Core MCP stdio transport
  ├─ mcp_tool_factory.go            [New] Tool discovery & wrapping
  └─ mcp_config.go                  [New] Configuration parsing

src/internal/config/
  └─ mcp.go                         [New] MCP config struct
```

---

## Phase 1: Basic MCP stdio Client - Implementation Roadmap

### File 1: `src/internal/tools/mcp_client.go` (~350 lines)

**Responsibilities**:
- StdioTransport: manage subprocess lifecycle
- JSON-RPC 2.0 message protocol handling
- Tool discovery (call `tools/list` on startup)
- Tool result handling

**Key Types**:
```go
type MCPClient struct {
    name       string
    cmd        *exec.Cmd
    stdin      io.WriteCloser
    stdout     io.ReadCloser
    messages   chan jsonrpc.Message  // request/response routing
    tools      []Tool                 // discovered tools
    mu         sync.RWMutex
}

type jsonrpcMessage struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      int             `json:"id,omitempty"`
    Method  string          `json:"method,omitempty"`
    Params  json.RawMessage `json:"params,omitempty"`
    Result  json.RawMessage `json:"result,omitempty"`
    Error   *struct{
        Code    int    `json:"code"`
        Message string `json:"message"`
    } `json:"error,omitempty"`
}
```

**Key Methods**:
- `NewMCPClient(name, command string) (*MCPClient, error)` - Create & connect
- `(c *MCPClient) DiscoverTools() error` - Call tools/list
- `(c *MCPClient) CallTool(name string, args json.RawMessage) (string, error)` - Execute tool
- `(c *MCPClient) Close() error` - Cleanup
- `(c *MCPClient) Health() error` - Check if alive

**Integration Point**: Called from `mcp_tool_factory.go` during startup

---

### File 2: `src/internal/tools/mcp_tool_factory.go` (~250 lines)

**Responsibilities**:
- Discover MCP clients from config
- Spawn/connect to MCP servers
- Wrap MCP tools as Anthropic tools
- Register wrapped tools in the tool registry

**Key Types**:
```go
type MCPToolFactory struct {
    clients map[string]*MCPClient  // name -> client
    tools   map[string]MCPToolDef  // tool_name -> (server_name, tool_name)
    mu      sync.RWMutex
}

type MCPToolDef struct {
    ServerName string
    ToolName   string
    Tool       *MCPClient
}
```

**Key Functions**:
- `InitMCPTools(cfg *config.Config) error` - Called from main.go
- `(f *MCPToolFactory) RegisterTools()` - Register with tool registry
- `(f *MCPToolFactory) DispatchMCP(toolName string, input json.RawMessage) (string, error)` - Route to server
- `(f *MCPToolFactory) Shutdown()` - Cleanup all clients

**Integration Points**:
- Called from `main.go` after `tools.InitMCP()`
- Hooks into `tools.Dispatch()` for routing
- Uses `tools.Register()` for each discovered tool

---

### File 3: `src/internal/config/mcp.go` (~100 lines)

**Responsibilities**:
- Parse MCP server configuration
- Define MCP config struct

**Key Types**:
```go
type MCPConfig struct {
    Servers []MCPServer `json:"servers" yaml:"servers"`
}

type MCPServer struct {
    Name       string `json:"name" yaml:"name"`
    Command    string `json:"command" yaml:"command"`
    Args       []string `json:"args" yaml:"args"`
    Env        map[string]string `json:"env" yaml:"env"`
    Critical   bool `json:"critical" yaml:"critical"`  // fail-hard if true
}
```

**Key Functions**:
- `LoadMCPConfig(source string) (*MCPConfig, error)` - Load from env/file
- Methods for validation

---

### Modifications to Existing Files

#### 1. `src/main.go`

**After line 59** (`tools.InitMCP()`), add:
```go
// Initialize MCP servers
if err := tools.InitMCPTools(cfg); err != nil {
    if /* critical error */ {
        fmt.Fprintln(os.Stderr, "Error: MCP initialization failed:", err)
        os.Exit(1)
    }
    // Non-critical: log warning
    fmt.Fprintf(os.Stderr, "Warning: MCP initialization failed: %v\n", err)
}
```

**In defer section (line 60)**, modify:
```go
defer tools.ShutdownMCP()      // existing
defer tools.ShutdownMCPTools() // add new
```

---

#### 2. `src/internal/tools/tool.go`

**In `Dispatch()` function**, add routing:
```go
func Dispatch(name string, input json.RawMessage) (string, error) {
    if def, ok := registry[name]; ok {
        return def.Handler(input)
    }
    
    // NEW: Route to MCP if tool not found locally
    if result, err := dispatchMCP(name, input); err == nil {
        return result, nil
    }
    
    return "", fmt.Errorf("unknown tool: %s", name)
}

// Add helper function
func dispatchMCP(name string, input json.RawMessage) (string, error) {
    return GlobalMCPFactory.DispatchMCP(name, input)
}
```

**Add global**:
```go
var GlobalMCPFactory *MCPToolFactory
```

---

#### 3. `src/internal/config/config.go`

**Add field to Config struct**:
```go
type Config struct {
    // ... existing fields ...
    MCP *MCPConfig `json:"mcp" yaml:"mcp"`
}
```

**In LoadEnv()**, add:
```go
// Load MCP servers from environment
if mcp := os.Getenv("MCP_SERVERS"); mcp != "" {
    cfg.MCP = parseMCPServersEnv(mcp)
}
```

---

## Implementation Steps (In Order)

### Step 1: Create Config Structure
1. Create `src/internal/config/mcp.go`
2. Define MCPServer and MCPConfig structs
3. Add parsing function from env vars

**Validation**:
```bash
go build ./src/internal/config
```

### Step 2: Create MCP Client
1. Create `src/internal/tools/mcp_client.go`
2. Implement StdioTransport
3. Implement JSON-RPC protocol handler
4. Implement tool discovery

**Validation**:
```bash
go test ./src/internal/tools -run TestMCPClient
# (will need mock MCP server for testing)
```

### Step 3: Create Tool Factory
1. Create `src/internal/tools/mcp_tool_factory.go`
2. Implement tool discovery loop
3. Implement tool wrapping (MCP → Anthropic format)
4. Implement dispatch routing

**Validation**:
```bash
go test ./src/internal/tools -run TestMCPFactory
```

### Step 4: Integrate with Tool Registry
1. Modify `src/internal/tools/tool.go` Dispatch()
2. Add GlobalMCPFactory variable
3. Route unknown tools to MCP

**Validation**:
```bash
go build ./src/internal/tools
```

### Step 5: Integrate with Main
1. Modify `src/main.go` to call InitMCPTools()
2. Add shutdown hook
3. Handle initialization errors per configured strategy

**Validation**:
```bash
go build ./src
```

### Step 6: Update Config Loading
1. Modify `src/internal/config/config.go`
2. Add MCP field to Config struct
3. Add env var parsing

**Validation**:
```bash
go build ./src/internal/config
```

### Step 7: Full Integration Test
1. Build complete binary
2. Test with mock MCP server (or manual test)
3. Verify tool discovery and execution

**Validation**:
```bash
make build
# Test manually with running MCP server
```

---

## Testing Strategy (Phase 1)

### Unit Tests
- MCP client protocol parsing
- Tool factory wrapping logic
- Error handling edge cases

### Integration Tests
- Subprocess lifecycle (spawn, shutdown)
- Full tool discovery flow
- Tool execution end-to-end

### Manual Tests
- Run with real MCP server (e.g., https://github.com/modelcontextprotocol/servers)
- Verify tools appear in agent
- Execute tool calls and verify results

---

## Error Handling Strategy

### Startup Errors
**By Configuration** (set in MCP_DECISIONS.md):
- Fail-hard: Exit if any critical server fails
- Fail-soft: Warn and continue if server fails
- Hybrid: Critical servers → fail-hard, optional → fail-soft

**Implementation**:
```go
if err := tools.InitMCPTools(cfg); err != nil {
    if cfg.MCP.FailHard {
        return err  // Exit
    }
    // Log warning, continue
}
```

### Runtime Errors
**By Configuration** (set in MCP_DECISIONS.md):
- Return error: Tool result with error message
- Retry: Attempt restart and retry
- Abort: Mark tool as failed, skip

**Implementation** (in MCPClient.CallTool):
```go
result, err := callRemoteTool(...)
if err != nil {
    switch cfg.MCP.OnToolError {
    case "return":
        return fmt.Sprintf("Error: %v", err), nil
    case "retry":
        // Attempt restart and retry
    case "abort":
        // Return error to agent
    }
}
```

---

## Files Modified/Created Summary

| File | Type | Purpose |
|------|------|---------|
| `src/internal/tools/mcp_client.go` | NEW | Core MCP transport |
| `src/internal/tools/mcp_tool_factory.go` | NEW | Tool discovery & registry |
| `src/internal/config/mcp.go` | NEW | Configuration parsing |
| `src/main.go` | MOD | Initialize & shutdown MCP |
| `src/internal/tools/tool.go` | MOD | Route to MCP dispatch |
| `src/internal/config/config.go` | MOD | Add MCP config field |

**Total new code**: ~700 lines  
**Total modified code**: ~30 lines  
**Integration complexity**: Medium  
**Risk level**: Low (isolated to new files, minimal existing code changes)

---

## Next Steps

1. **User provides decisions** via MCP_DECISIONS.md
2. **Implementation begins** with file creation in order above
3. **Testing** at each step
4. **Integration** with existing codebase
5. **Manual verification** with real MCP server
6. **Documentation** of added tools and usage patterns

---

## Related Documents

- `MCP_DECISIONS.md` - Decision checklist (REQUIRED before implementation)
- `MCP_INTEGRATION_STRATEGY.md` - High-level strategy and architecture
- `.omc/wiki/mcp-integration-strategy-decision-framework.md` - Full decision framework

