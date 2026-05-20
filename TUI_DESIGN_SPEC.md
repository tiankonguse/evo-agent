# TUI Design Specification for EVO-Agent

**Author**: Exploration Agent  
**Date**: May 18, 2026  
**Purpose**: Reference specification for implementing a Terminal User Interface layer

---

## EXECUTIVE SUMMARY

The evo-agent codebase has **clear separation of concerns** and **predictable event flow**, making it ideal for TUI integration. No breaking changes are required. A TUI can intercept stdout and route events to Bubble Tea panels.

---

## ARCHITECTURE DIAGRAM: MESSAGE FLOW

```
┌─────────────────────────────────────────────────────────────┐
│                      USER INPUT                             │
│  "List all files in project"                                │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│                   Agent.Run(os.Stdin)                       │
│  - Read user input line                                     │
│  - Append to message history                               │
│  - Call Agent.Loop(state)                                  │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│                    Agent.Loop(state)                        │
│  TURN 1: User query → Model → [bash tool call]             │
│  TURN 2: Tool result → Model → [read_file tool call]       │
│  TURN 3: Tool result → Model → [no more tools]             │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│           tools.Execute() [TOOL DISPATCH]                  │
│                                                              │
│  ThinkingBlock  → ui.PrintThinking()  → GREEN OUTPUT       │
│  TextBlock      → ui.PrintText()      → CYAN OUTPUT        │
│  ToolUseBlock   → ui.PrintToolCall()  → BLUE OUTPUT        │
│                → ui.PrintCommand()   → YELLOW OUTPUT       │
│                → tools.Dispatch()    → Execute handler     │
│                → ui.PrintError()     → RED OUTPUT (if fail)│
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│              STDOUT/STDERR (ANSI colored)                   │
│  THINKING: Analyzing the project structure...              │
│  TEXT: I'll list the files in the project...               │
│  DEBUG: Tool called: bash                                  │
│  $ ls -la                                                  │
│  (file listing output)                                     │
└─────────────────────────────────────────────────────────────┘
```

---

## RECOMMENDED TUI LAYOUT (80x30 terminal)

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ EVO-AGENT TUI                                                                │
├──────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ╔════════════════════════════════════════╦═══════════════════════════════╗ │
│  ║                                        ║                               ║ │
│  ║   MESSAGE HISTORY (scrollable)         ║   STATUS PANEL               ║ │
│  ║   ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━  ║   ━━━━━━━━━━━━━━━━━━━━━━━  ║ │
│  ║                                        ║                               ║ │
│  ║   USER: List all files                 ║   Model: claude-3-5-sonnet   ║ │
│  ║                                        ║   Turn: 3 / 5                ║ │
│  ║   THINKING:                            ║   Status: Processing         ║ │
│  ║   Analyzing project structure...       ║   ⏱  Turn elapsed: 2.3s      ║ │
│  ║                                        ║                               ║ │
│  ║   TEXT:                                ║   Tokens:                    ║ │
│  ║   I'll help list the files...          ║   ├─ Input: 2,345           ║ │
│  ║                                        ║   └─ Output: 1,234          ║ │
│  ║   TOOL: bash                           ║                               ║ │
│  ║   $ ls -la                             ║   Context:                   ║ │
│  ║   total 42                             ║   ├─ Size: 45 KB            ║ │
│  ║   drwxr-xr-x 5 user staff    160       ║   ├─ Messages: 6            ║ │
│  ║   -rw-r--r-- 1 user staff    1234      ║   └─ Recent files: 2        ║ │
│  ║   (output truncated - full in file)    ║                               ║ │
│  ║                                        ║   Current Tool:              ║ │
│  ║   [scroll up ↑] [scroll down ↓]        ║   └─ bash (running)          ║ │
│  ║                                        ║                               ║ │
│  ╚════════════════════════════════════════╩═══════════════════════════════╝ │
├──────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  INPUT: >> [What would you like to know about the project?__________]      │
│                                                                              │
├──────────────────────────────────────────────────────────────────────────────┤
│  Ready | Cmd+C: quit | ↑↓: history | Enter: submit                         │
└──────────────────────────────────────────────────────────────────────────────┘
```

---

## EVENT STREAM SPECIFICATION

### Event Types

```go
type TUIEvent struct {
    Type        EventType         // thinking|text|tool_call|tool_result|error|debug|compact
    Timestamp   time.Time
    Content     string
    ToolName    string            // For tool_call/result
    ToolID      string            // For tool_result
    TurnCount   int               // Current turn
    TokensIn    int               // Input tokens
    TokensOut   int               // Output tokens
    ContextSize int               // Estimated context bytes
    IsError     bool              // For error events
}

type EventType string

const (
    EventThinking    EventType = "thinking"
    EventText        EventType = "text"
    EventToolCall    EventType = "tool_call"
    EventToolResult  EventType = "tool_result"
    EventError       EventType = "error"
    EventDebug       EventType = "debug"
    EventCompact     EventType = "compact"
)
```

### Event Emission Points

| Code Location | Event | Trigger |
|--------------|-------|---------|
| `tools.Execute()` ThinkingBlock | `EventThinking` | Model generates thinking |
| `tools.Execute()` TextBlock | `EventText` | Model generates text |
| `tools.Execute()` ToolUseBlock | `EventToolCall` | Model calls tool |
| `tools.Execute()` ToolResult | `EventToolResult` | Tool execution complete |
| `tools.Execute()` error | `EventError` | Tool fails |
| `Agent.Loop()` print statement | `EventDebug` | Token count, model info |
| `Agent.autoCompact()` | `EventCompact` | Compaction triggered |

### Example Event Sequence

```
[12:34:56.123] EventDebug "Model used: claude-3-5-sonnet, Tokens input: 2345, output: 1234, stop_reason: tool_use"
[12:34:56.234] EventThinking "Let me analyze the request..."
[12:34:56.345] EventText "I'll check the project structure..."
[12:34:56.456] EventToolCall "bash" "$ ls -la /Users/tiankonguse-m3/project"
[12:34:57.100] EventToolResult "bash" (file listing output)
[12:34:57.200] EventCompact "Context size: 45000 bytes"
```

---

## INTERCEPTION STRATEGY: STDOUT CAPTURE

### Current Output Path

```
tools.Execute()
  → ui.PrintThinking("...")  [fmt.Printf to stdout]
  → ui.PrintText("...")       [fmt.Printf to stdout]
  → fmt.Println(output)       [tool result to stdout]
```

### Proposed TUI Interception

```
tools.Execute()
  → [INTERCEPT] fmt.Printf/Println
  → [EMIT] TUIEvent to channel
  → [RENDER] Bubble Tea updates panel

Alternative: Modify ui/terminal.go to emit events instead of printing
(requires ~5 lines of change)
```

### Option A: Global Event Channel (No Code Changes)

```go
// In main.go or new tui/channel.go
var eventChan chan TUIEvent

func init() {
    eventChan = make(chan TUIEvent, 100)  // Buffered
}

// In ui/terminal.go - change PrintThinking() to:
func PrintThinking(text string) {
    if eventChan != nil {
        eventChan <- TUIEvent{
            Type: EventThinking,
            Content: text,
            Timestamp: time.Now(),
        }
    }
    // Keep stdout printing for non-TUI mode
    fmt.Printf("%sTHINKING: %s%s\n", ColorGreen, text, ColorReset)
}
```

### Option B: Dependency Injection (Cleaner)

```go
// Modify Agent to accept EventEmitter interface
type EventEmitter interface {
    EmitThinking(text string)
    EmitText(text string)
    EmitToolCall(name string, input json.RawMessage)
    // ... more methods
}

// Create implementations:
// - TerminalEmitter (current behavior)
// - TUIEmitter (sends events)
// - NullEmitter (testing)
```

---

## INPUT REDIRECTION STRATEGY

### Current Input Path

```
main.go: a.Run(os.Stdin)
  → bufio.Scanner(os.Stdin)
  → Loop: scanner.Scan() waits for user input
```

### Proposed TUI Input

```
TUIModel.Update(tea.KeyMsg)
  → buffer user input in Model.input field
  → tea.KeyMsg("enter") triggers submit
  → Write to Model.agentInput (pipe or channel)
  → Agent.Run reads from this pipe
```

### Implementation Option: Pipe Pair

```go
// In main.go with --tui flag:
pipeRead, pipeWrite := io.Pipe()

// Start TUI in goroutine
go func() {
    tui := NewTUIModel(pipeWrite)
    program := tea.NewProgram(tui)
    if err := program.Run(); err != nil {
        log.Fatal(err)
    }
}()

// Pass pipe to Agent.Run
a.Run(pipeRead)

// In TUI model:
func (m *Model) submitCommand() tea.Cmd {
    cmd := m.input.Value()
    fmt.Fprintln(m.pipe, cmd)  // Write to pipe
    m.input.Reset()
    return nil
}
```

---

## DATA FLOW CHART: TURN EXECUTION

```
┌──────────────────────┐
│   USER SUBMITS       │
│   "run tests"        │
└──────┬───────────────┘
       │
       ├─→ TUIModel.submitCommand()
       │   └─→ Write to pipeWriter
       │
       ├─→ Agent.Run() reads from pipeReader
       │   └─→ Append to history
       │
       ├─→ Agent.Loop()
       │   └─→ client.Messages.New() [API call]
       │       └─→ Model responds with tool_use
       │
       ├─→ tools.Execute()
       │   ├─→ emit EventThinking
       │   ├─→ emit EventToolCall("bash", ...)
       │   ├─→ tools.Dispatch("bash", ...)
       │   │   └─→ runBash() executes
       │   └─→ emit EventToolResult("bash", output)
       │
       ├─→ EventChannel broadcasts
       │   └─→ TUIModel receives and renders
       │
       └─→ [Loop back for next tool call or end]
           │
           └─→ Display final assistant response
```

---

## REQUIRED TUI COMPONENTS (Bubble Tea)

### 1. Main Model

```go
type Model struct {
    // UI state
    history    []Message          // Displayed messages
    status     StatusInfo
    currentCmd string             // User's input
    scrollPos  int                // History scroll position
    
    // Input
    input      textinput.Model    // Text input widget
    
    // Agent connection
    pipeWrite  io.WriteCloser     // Write user input to agent
    eventChan  <-chan TUIEvent    // Receive agent events
}

type Message struct {
    Type      string    // "user", "thinking", "text", "tool"
    Content   string
    ToolName  string    // For tool messages
    Timestamp time.Time
    IsError   bool
}

type StatusInfo struct {
    Model       string
    Turn        int
    Elapsed     time.Duration
    TokensIn    int
    TokensOut   int
    ContextSize int
    CurrentTool string
}
```

### 2. Key Methods

```go
// Bubble Tea interface implementation
func (m *Model) Init() tea.Cmd {
    return tea.Batch(
        textinput.Blink,
        m.listenForEvents(),  // Background event listener
    )
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        return m.handleKeyPress(msg)
    case TUIEvent:
        return m.handleEvent(msg)
    case tea.WindowSizeMsg:
        return m.handleResize(msg)
    }
    return m, nil
}

func (m *Model) View() string {
    return m.renderLayout()
}

func (m *Model) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
    switch msg.String() {
    case "ctrl+c":
        return m, tea.Quit
    case "enter":
        return m, m.submitCommand()
    case "up":
        // Command history
        return m, nil
    case "down":
        // Command history
        return m, nil
    }
    var cmd tea.Cmd
    m.input, cmd = m.input.Update(msg)
    return m, cmd
}

func (m *Model) handleEvent(event TUIEvent) (tea.Model, tea.Cmd) {
    switch event.Type {
    case EventThinking:
        m.history = append(m.history, Message{
            Type:      "thinking",
            Content:   event.Content,
            Timestamp: event.Timestamp,
        })
    case EventToolCall:
        m.status.CurrentTool = event.ToolName
    case EventDebug:
        if strings.Contains(event.Content, "Tokens") {
            m.parseTokenInfo(event.Content)
        }
    }
    return m, nil
}

func (m *Model) listenForEvents() tea.Cmd {
    return func() tea.Msg {
        return <-m.eventChan  // Non-blocking event listener
    }
}

func (m *Model) submitCommand() tea.Cmd {
    cmd := m.input.Value()
    go func() {
        fmt.Fprintln(m.pipeWrite, cmd)
    }()
    m.input.SetValue("")
    return m.listenForEvents()  // Resume listening
}

func (m *Model) renderLayout() string {
    // Build 3-panel layout:
    // 1. Message history (left, 2/3 width)
    // 2. Status panel (right, 1/3 width)
    // 3. Input box (bottom)
    return lipgloss.JoinHorizontal(
        lipgloss.Top,
        m.renderHistory(),
        m.renderStatus(),
    ) + "\n" + m.renderInput() + "\n" + m.renderFooter()
}
```

### 3. Rendering Functions

```go
func (m *Model) renderHistory() string {
    // Scrollable viewport of messages
    // Color-code by type:
    // - thinking: green
    // - text: cyan
    // - tool: blue
    // - error: red
    // - tool_result: yellow
}

func (m *Model) renderStatus() string {
    // Right panel with metrics
    // - Current model
    // - Turn count
    // - Token usage (input/output)
    // - Context size
    // - Current tool
    // - Timer
}

func (m *Model) renderInput() string {
    return lipgloss.NewStyle().
        Border(lipgloss.RoundedBorder()).
        Render(">> " + m.input.View())
}

func (m *Model) renderFooter() string {
    return "Ready | Ctrl+C: quit | ↑↓: history | Enter: submit"
}
```

---

## IMPLEMENTATION CHECKLIST

### Phase 1: Infrastructure (1-2 hours)

- [ ] Create `src/internal/tui/` package
- [ ] Define `TUIEvent` type and event channel
- [ ] Create Bubble Tea `Model` struct skeleton
- [ ] Implement pipe-based input (pipeRead/pipeWrite)
- [ ] Add `--tui` flag to main.go
- [ ] Test basic TUI startup and shutdown

### Phase 2: Output Routing (2-3 hours)

- [ ] Modify `ui/terminal.go` to emit events (Option A/B)
- [ ] Connect event channel to TUI model
- [ ] Implement event listener in Bubble Tea
- [ ] Verify events flow to TUI panels

### Phase 3: Panel Rendering (2-3 hours)

- [ ] Implement history panel with scrolling
- [ ] Implement status panel with metrics
- [ ] Implement input panel with text input
- [ ] Implement footer with help text
- [ ] Color-code message types

### Phase 4: Polish (1-2 hours)

- [ ] Add command history (up/down arrows)
- [ ] Add real-time token/context display
- [ ] Add progress indicators for long-running tools
- [ ] Add error highlighting
- [ ] Performance optimization

---

## KEY INSIGHTS FOR IMPLEMENTATION

### 1. Non-Breaking Integration
- Existing code doesn't need to change for basic TUI
- Can start with stdout capture + Bubble Tea overlay
- Gradually refactor to emit events

### 2. Event Buffering
- Use buffered channel (capacity 100) to avoid blocking
- Agent loop runs independently in background
- TUI renders events as they arrive

### 3. Concurrent I/O
- Use goroutines for agent execution
- TUI runs in main goroutine (Bubble Tea requirement)
- Synchronize via channels

### 4. State Management
- Keep LoopState in agent loop
- Expose minimal state to TUI (read-only views)
- Use channels for updates (not shared memory)

### 5. Message History
- Store in memory (already done by Agent.Run)
- Optionally persist to file (already done)
- TUI displays last N messages (scrollable)

---

## TESTING STRATEGY

### Unit Tests
```go
// Test event emission
// Test message parsing
// Test layout rendering
```

### Integration Tests
```go
// Test Agent + TUI together
// Test input/output flow
// Test event propagation
```

### Manual Testing
```bash
# Run with TUI
./evo-agent --tui

# Run without TUI (baseline)
./evo-agent
```

---

## FUTURE ENHANCEMENTS

1. **Tool Visualization**: Show tool execution timeline
2. **Context Browser**: Interactive view of current context
3. **Transcript Viewer**: Browse previous sessions
4. **Skill Manager**: Load/manage skills from UI
5. **MCP Manager**: Connect/disconnect MCP servers
6. **Theme Switching**: Dark/light mode
7. **Keyboard Shortcuts**: Command palette
8. **Export**: Save session to PDF/Markdown
9. **Search**: Search message history
10. **Settings Panel**: Configure agent behavior

---

## CONCLUSION

The TUI design is **straightforward and low-risk**:
- Clear event boundaries
- Minimal code changes required
- No architectural rework needed
- Bubble Tea framework is well-suited
- Can be implemented incrementally

**Estimated effort**: 8-12 hours for full implementation

