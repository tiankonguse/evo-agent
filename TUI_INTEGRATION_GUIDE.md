# TUI Layer Integration Guide for EVO-Agent

## Overview
This guide describes how to design and integrate a Terminal User Interface (TUI) layer on top of the existing evo-agent codebase without breaking changes.

---

## Current Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    main.go                              │
│  - Load config, initialize MCP, skills, client          │
│  - Start Agent.Run(os.Stdin)                            │
└─────────────────────────────────────────────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────┐
│              Agent.Run() REPL Loop                       │
│  - bufio.Scanner reads from os.Stdin                    │
│  - Calls Agent.Loop(state)                              │
│  - Prints final response to stdout                      │
└─────────────────────────────────────────────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────┐
│           Agent.Loop() - Multi-turn Loop                │
│  - autoCompact()                                         │
│  - client.Messages.New() [LLM API call]                 │
│  - tools.Execute() [Tool dispatch]                      │
│  - Loop until stop_reason != "tool_use"                 │
└─────────────────────────────────────────────────────────┘
                          ↓
          ┌─────────────────────────────────┐
          │    tools.Execute()              │
          │  - Print output via ui package  │
          │  - Dispatch to handlers         │
          │  - Return tool results          │
          └─────────────────────────────────┘
```

---

## Proposed TUI Architecture

### Option A: Minimal - Replace I/O Layer Only (Recommended)

```
┌────────────────────────────────────────────────────────────┐
│                     main.go                                │
│  (unchanged - loads config, init MCP/skills)               │
└────────────────────────────────────────────────────────────┘
                          ↓
┌────────────────────────────────────────────────────────────┐
│              TUI Event Loop (NEW)                           │
│  - Create Bubble Tea App or tcell screen                   │
│  - Manage layout: Input | History | Status                 │
│  - Route stdin to TUI input handler                        │
│  - Route Agent output to TUI panels                        │
└────────────────────────────────────────────────────────────┘
                          ↓
              Agent.Run() [UNCHANGED]
                (internally calls Agent.Loop)
                          ↓
┌────────────────────────────────────────────────────────────┐
│        Output Redirection (NEW)                            │
│  - Hijack stdout/stderr writers                           │
│  - Route to TUI panels instead of terminal               │
│  - Preserve ANSI color codes for styling                 │
└────────────────────────────────────────────────────────────┘
```

### Option B: Full Extraction - Separate I/O Interface (More Invasive)

```
Create io.Writer interface in ui package
  ├─ implements Terminal (stdout)
  ├─ implements TUIPanel (Bubble Tea)
  └─ implements NullWriter (testing)

Modify Agent.Loop to accept io.Writer
Modify tools.Execute to use io.Writer
Modify all Print* functions to use io.Writer
```

---

## Recommended Approach: Option A (Minimal Changes)

### Step 1: Create TUI Package

**File**: `src/internal/tui/tui.go`

```go
package tui

import (
    "io"
    "github.com/charmbracelet/bubbles/textinput"
    tea "github.com/charmbracelet/bubbletea"
)

type Model struct {
    // Panel state
    history     []Message
    currentCmd  string
    tools       []ToolStatus
    
    // Input widget
    input       textinput.Model
    
    // Agent connection
    agentInput  io.WriteCloser
    agentOutput io.Reader
}

type Message struct {
    Role      string    // "user", "assistant", "thinking", "tool"
    Content   string
    Timestamp time.Time
}

// Run() starts the TUI event loop
func (m *Model) Run() error {
    p := tea.NewProgram(m)
    return p.Run()
}

// Render() implements tea.Model
func (m *Model) View() string {
    return m.renderLayout()
}

// Update() handles events
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        switch msg.String() {
        case "ctrl+c":
            return m, tea.Quit
        case "enter":
            return m, m.submitCommand()
        }
    }
    return m, nil
}
```

### Step 2: Modify main.go Minimally

```go
// In main.go, add flag for --tui
var useTUI = flag.Bool("tui", false, "Use TUI mode")

func main() {
    // ... existing init code ...
    
    a := agent.New(&client, cfg)
    
    if *useTUI {
        tuiApp := tui.New(a)
        tuiApp.Run()
    } else {
        a.Run(os.Stdin)  // existing behavior
    }
}
```

### Step 3: Intercept Output in TUI Model

```go
// In tui.go: Create output capture
func (m *Model) captureAgentOutput(ctx context.Context) {
    reader := m.agentOutput
    scanner := bufio.NewScanner(reader)
    
    for scanner.Scan() {
        line := scanner.Text()
        
        // Parse ANSI colors from terminal.go
        msg := parseMessageType(line)
        m.history = append(m.history, msg)
        
        // Queue UI update
        m.send(MessageReceived{Message: msg})
    }
}

func parseMessageType(line string) Message {
    // Detect output type from ANSI codes
    if strings.Contains(line, ColorGreen) {
        return Message{Role: "thinking", Content: line}
    } else if strings.Contains(line, ColorCyan) {
        return Message{Role: "assistant", Content: line}
    } else if strings.Contains(line, ColorBlue) {
        return Message{Role: "tool", Content: line}
    }
    return Message{Role: "debug", Content: line}
}
```

### Step 4: Layout Design

```
┌───────────────────────────────────────────────────────────┐
│ EVO-AGENT TUI                               [Status Bar]  │
├───────────────────────────────────────────────────────────┤
│                                                           │
│  [HISTORY PANEL - scrollable]                            │
│                                                           │
│  🟢 THINKING: Analyzing the problem...                  │
│  💬 ASSISTANT: I'll help you with...                    │
│  🔧 TOOL: bash (find . -type f -name "*.go")            │
│  📝 RESULT: Found 5 Go files                            │
│                                                           │
│                                                           │
├───────────────────────────────────────────────────────────┤
│ >> [INPUT FIELD - active cursor here]                    │
├───────────────────────────────────────────────────────────┤
│ 📊 Status: Running | Turn: 3 | Context: 12450 / 50000   │
│ 💾 Compacted: 0 | Files: main.go, loop.go, tool.go     │
│ 🔌 MCP: 3 servers | Skills: 5 loaded                    │
└───────────────────────────────────────────────────────────┘
```

---

## Integration Points

### 1. Input Capture

**Current**:
```go
// In agent/loop.go
scanner := bufio.NewScanner(r)
scanner.Scan()
query := scanner.Text()
```

**TUI Version**:
```go
// In tui/tui.go
func (m *Model) submitCommand() tea.Cmd {
    query := m.input.Value()
    m.input.SetValue("")  // Clear input
    
    // Send to agent
    go func() {
        m.agentChannel <- AgentInput{Query: query}
    }()
    return nil
}
```

### 2. Output Capture

**Current** (in `executor.go`):
```go
ui.PrintText(v.Text)
ui.PrintToolCall(v.Name)
```

**TUI Version** (with minimal changes):
```go
// Keep existing ui.Print* calls
// They write to captured stdout
// TUI scanner reads from pipe and updates panels
```

### 3. State Access

**Available in Agent.Loop**:
```go
state.CompactState.CompactCount    // Display in status bar
state.TurnCount                     // Display in status bar
state.Messages                      // Show in history panel
state.CompactState.RecentFiles      // Show in status bar
```

---

## Files to Create/Modify

### New Files
- `src/internal/tui/tui.go` - Main TUI app (Bubble Tea or tcell)
- `src/internal/tui/layout.go` - Panel rendering
- `src/internal/tui/parser.go` - Parse terminal output
- `src/internal/tui/commands.go` - Commands from UI

### Modified Files
- `src/main.go` - Add `--tui` flag, conditionally start TUI
- `src/go.mod` - Add TUI framework dependency

### Unchanged
- `src/internal/agent/loop.go` - Core logic untouched
- `src/internal/tools/*` - All tools unchanged
- `src/internal/config/*` - Config untouched
- `src/internal/ui/terminal.go` - Keep ANSI colors

---

## Dependencies to Add

### Option 1: Bubble Tea (Recommended)
```bash
go get github.com/charmbracelet/bubbletea
go get github.com/charmbracelet/bubbles
```

Pros:
- Declarative model-view-update pattern
- Great for terminal apps
- Active community

Cons:
- More opinionated

### Option 2: tcell
```bash
go get github.com/gdamore/tcell/v2
```

Pros:
- Lower level, more control
- Lightweight

Cons:
- More boilerplate for UI

### Option 3: Fyne (Native)
```bash
go get fyne.io/fyne/v2
```

Pros:
- Native look on each platform
- Built-in widgets

Cons:
- Overkill for terminal
- Requires native libraries

---

## Data Flow Example

### User Input → Agent → Output → TUI Display

```
1. User types in TUI input field
   ↓
2. TUI captures KeyMsg in Update()
   ↓
3. On Enter, submit command to agent channel
   ↓
4. Agent.Run() reads from channel (instead of stdin)
   ↓
5. Agent.Loop() executes, prints to captured stdout
   ↓
6. TUI captures output from stdout reader
   ↓
7. Parser identifies message type from ANSI codes
   ↓
8. Message appended to history panel
   ↓
9. TUI View() re-renders with new message
   ↓
10. User sees thinking → tool call → result → response
```

---

## Real-Time Status Dashboard

### Token Usage
```go
// In executor.go, after LLM call
fmt.Printf("Tokens: %d input, %d output\n", resp.Usage.InputTokens, resp.Usage.OutputTokens)

// TUI parser detects and shows in status bar
m.status.InputTokens = 1234
m.status.OutputTokens = 567
```

### Context Size
```go
// Already in compact.go
contextSize := EstimateContextSize(state.Messages)

// TUI can read from state
m.status.ContextSize = contextSize
m.status.ContextLimit = CONTEXT_LIMIT
```

### Tool Execution Timeline
```go
// In executor.go
start := time.Now()
output, err := Dispatch(v.Name, inputBytes)
duration := time.Since(start)

// TUI displays
🔧 tool_name (123ms)
  Input: {"path": "file.go"}
  Output: [12 lines, 2.4KB]
  Status: ✓
```

---

## Testing Strategy

### Unit Tests
- Parse ANSI codes correctly
- Layout renders without crashing
- Channel communication works

### Integration Tests
- Full loop: input → agent → output → TUI
- Tool results display correctly
- Multiple turns work

### Manual Testing
```bash
# Terminal mode (existing)
cd src && go build -o evo-agent
./evo-agent < queries.txt

# TUI mode (new)
./evo-agent --tui

# Both in dev
make dev-terminal
make dev-tui
```

---

## Implementation Phases

### Phase 1: Basic TUI Shell (Week 1)
- [ ] Create tui package with Bubble Tea setup
- [ ] Implement text input widget
- [ ] Display history as read-only panel
- [ ] Add --tui flag to main

### Phase 2: Agent Integration (Week 1-2)
- [ ] Connect agent I/O to TUI
- [ ] Capture and parse agent output
- [ ] Display messages with color coding
- [ ] Handle multi-turn conversations

### Phase 3: Rich Dashboard (Week 2)
- [ ] Add status bar with real-time metrics
- [ ] Show token usage from API responses
- [ ] Display context size and compaction status
- [ ] Show MCP server list and skill catalog

### Phase 4: Advanced Features (Week 3+)
- [ ] Tool result viewer (expandable)
- [ ] File browser integration
- [ ] History search/filter
- [ ] Keyboard shortcuts for common commands
- [ ] Theme support (dark/light)

---

## Key Design Principles

1. **Non-Breaking**: Existing terminal mode works unchanged
2. **Minimal Changes**: Don't refactor core agent logic
3. **Composable**: TUI is a wrapper, not a replacement
4. **Observable**: Real-time visibility into agent state
5. **Responsive**: UI updates don't block agent execution
6. **Scriptable**: Can still pipe input/output for automation

---

## Example: Adding a New Panel

```go
// src/internal/tui/panels/tools.go
package panels

type ToolsPanel struct {
    tools []ToolStatus
}

func (p *ToolsPanel) Render(width, height int) string {
    var output strings.Builder
    output.WriteString(lipgloss.NewStyle().
        Border(lipgloss.RoundedBorder()).
        Render("Tools"))
    
    for _, tool := range p.tools {
        output.WriteString(fmt.Sprintf(
            "🔧 %s (%dms)\n",
            tool.Name,
            tool.Duration.Milliseconds(),
        ))
    }
    return output.String()
}

// In main TUI model
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case ToolExecuted:
        m.toolsPanel.tools = append(m.toolsPanel.tools, msg.Tool)
        return m, nil
    }
    return m, nil
}
```

---

## Debugging & Logging

### Enable verbose mode
```bash
./evo-agent --tui --verbose

# Logs go to .evo_agent/tui.log
tail -f .evo_agent/tui.log
```

### Inspect agent state
- Status bar shows current turn, context size
- History panel shows all messages
- Debug panel (hidden, toggle with `?`) shows raw JSON

---

## Performance Considerations

- **Async I/O**: Agent loop in goroutine, TUI in main thread
- **Debouncing**: Don't re-render on every character
- **Pagination**: Only render visible history lines
- **Memory**: Limit history to last 10,000 messages
- **Profiling**: Use pprof to check goroutine leaks

---

## Migration Path for Users

```
Phase 1: Available as optional --tui flag
Phase 2: Default to TUI if stdin is a terminal
Phase 3: Terminal-only mode requires --no-tui

# Users can still script
echo "query" | ./evo-agent --no-tui > output.txt
```

