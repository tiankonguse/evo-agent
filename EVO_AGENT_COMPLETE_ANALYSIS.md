# evo-agent 项目完整代码探索报告

**报告日期**: 2026-05-14  
**项目根**: `/Users/tiankonguse-m3/project/github/AIProject/evo-agent`  
**总代码行数**: ~1500 行 Go 代码 (17 个 .go 文件)

---

## 📖 核心概念速记

### 工具系统架构

```
┌─────────────────────────────────┐
│   Global Registry               │
│   registry: map[name]ToolDef    │ ← 所有工具在此注册
└─────────────────────────────────┘
        ↓ (Register 方式)
┌─────────────────────────────────┐
│ 每个工具文件 init()             │
│ bash.go, read_file.go, ...      │ ← 自动注册，无需中心编辑
└─────────────────────────────────┘
        ↓ (Tools() 合并)
┌─────────────────────────────────┐
│ tools.Tools()                   │
│ [原生工具] + [MCP工具]          │
└─────────────────────────────────┘
        ↓ (发送给模型)
┌─────────────────────────────────┐
│ claude.Messages.New()           │
│ Model: cfg.ModelID              │
│ Tools: tools.Tools()            │
└─────────────────────────────────┘
        ↓ (模型调用工具)
┌─────────────────────────────────┐
│ Execute(resp.Content)           │
│ ToolUseBlock → Dispatch()       │
└─────────────────────────────────┘
        ↓ (路由)
┌─────────────────────────────────┐
│ Dispatch(name, input)           │
├─────────────────────────────────┤
│ if name.HasPrefix("mcp__"):     │
│   → DispatchMCP()               │
│ else:                           │
│   → registry[name].Handler()    │
└─────────────────────────────────┘
```

### MCP 集成流程

```
InitMCP()
  ↓ 读取 .evo_agent/mcp.json
  ↓ for each mcpServer:
    ├─ Type="stdio" → startMCPProcess()
    │   └─ exec.Command + JSON-RPC over stdin/stdout
    ├─ Type="streamableHttp" → connectMCPStreamableHTTP()
    │   └─ POST 请求 (可选 SSE 响应)
    └─ Type="sse" → connectMCPSSE()
        └─ GET SSE 连接 + POST 请求

MCPTools()
  ↓ for each server/tool:
    ├─ prefixed = "mcp__" + serverName + "__" + toolName
    └─ return []ToolParam

DispatchMCP("mcp__X__Y", input)
  ├─ parse: X=serverName, Y=toolName
  └─ mcpServers[X].callTool(Y, input)
```

### Agent 循环核心

```
Run(REPL) {
  for {
    query = user_input
    history += UserMessage(query)
    
    Loop(state{Messages: history}) {
      repeat {
        autoCompact()              // MicroCompact + 超限检查
          ↓
        resp = api_call(messages, tools)
          ↓
        history += resp            // 追加模型响应
          ↓
        toolResults = Execute()    // 执行工具调用
          ↓
        if toolResults.empty():
          return false             // 循环结束
        else:
          history += ToolResult    // 追加结果继续
          manualCompact()          // 检查压缩请求
      }
    }
    
    print(最终响应)
  }
}
```

### 上下文压缩三层策略

```
Layer 1: MicroCompact (微压缩)
├─ 保留最近 3 个完整 tool_result
├─ 替换其他为占位符 "[Earlier tool result compacted...]"
└─ 保留最后一条 tool_result 消息的所有结果
→ 快速、无信息损失、仅节约 token

Layer 2: LLM Summarization (自动触发)
├─ 触发: EstimateContextSize > CONTEXT_LIMIT (50000 chars)
├─ 调用模型生成 1-2KB 总结
├─ 保留: 当前目标、重要发现、文件清单、剩余工作
└─ 结果: 完整历史 → 单条消息
→ 深度压缩、保留关键信息

Layer 3: Manual Compact (手动触发)
├─ 模型调用 "compact" 工具
├─ CompactHistory() 保存完整转录到 .evo_agent/transcripts/
├─ 生成总结 + 支持 focus 参数
└─ 结果: 完整历史 → 单条消息
→ 精准控制、保留历史记录
```

---

## 🔴 五大核心文件

### 1. main.go (50 行) - 程序入口

**关键流程**:
```go
main() {
  config.LoadEnv()           // 加载 .env (二级: exe目录 → cwd覆盖)
  cfg = config.Load()         // 读环境变量 → Config{ModelID, APIKey, BaseURL, SystemMsg}
  
  opts = BuildOptions(cfg)    // 构建 API 选项
  client = anthropic.NewClient(opts...)
  
  tools.InitMCP()             // ← 加载 .evo_agent/mcp.json, 连接所有 MCP 服务
  defer tools.ShutdownMCP()   // ← 清理连接
  
  tools.PrintToolList()       // ← 打印工具清单
  
  agent = agent.New(&client, cfg)
  agent.Run(os.Stdin)         // ← 启动 REPL
}
```

**BuildOptions 规则**:
```
if BaseURL != "":
  → 使用自定义端点
  → 取消设置 ANTHROPIC_AUTH_TOKEN
  → 使用 dummy 密钥
else:
  → 使用 Anthropic 官方端点
  → 使用 APIKey (或 dummy)
```

### 2. tool.go (68 行) - 工具注册与调度

**核心机制**:
```go
type ToolDef struct {
  Schema  anthropic.ToolParam       // 工具定义
  Handler func(json.RawMessage)(string, error)
}

var registry = map[string]ToolDef{}

// 自注册模式 - 在各工具的 init() 中调用
Register(ToolDef{
  Schema: anthropic.ToolParam{
    Name:        "tool_name",
    Description: "...",
    InputSchema: GenerateSchema[InputType]()  // ← 反射生成
  },
  Handler: func(input json.RawMessage)(string, error) {
    var in InputType
    json.Unmarshal(input, &in)
    return handler(in), nil
  },
})

// 统一调度
func Tools() []anthropic.ToolUnionParam {
  out := [原生工具来自registry]
  out += MCPTools()  // ← MCP工具追加
  return out
}

func Dispatch(name string, input json.RawMessage) (string, error) {
  if strings.HasPrefix(name, "mcp__"):
    return DispatchMCP(name, input)  // ← MCP 路由
  if handler, ok := registry[name]; ok:
    return handler.Handler(input)     // ← 原生工具
  return "", nil
}
```

**优势**:
- ✅ 添加工具 = 新增一个文件，在 init() 中 Register()
- ✅ Schema 自动反射，无需手写 JSON
- ✅ Dispatch 统一路由，支持原生 + MCP 混合

### 3. loop.go (193 行) - Agent 循环核心

**Loop 函数流程**:
```go
func (a *Agent) Loop(state *LoopState) bool {
  for {
    // 1. 自动压缩
    a.autoCompact(state)
    
    // 2. API 调用
    resp, err := a.client.Messages.New(context.Background(), 
      anthropic.MessageNewParams{
      Model:     cfg.ModelID,
      System:    cfg.SystemMsg,
      Messages:  state.Messages,          // 完整历史
      Tools:     tools.Tools(),           // 原生 + MCP 工具
      MaxTokens: 8000,
    })
    
    // 3. 追加响应到历史
    state.Messages = append(state.Messages, resp.ToParam())
    
    // 4. 追踪文件读取 (用于压缩时列出最近文件)
    for _, block := range resp.Content:
      if block.Type == "tool_use" && block.Name == "read_file":
        TrackRecentFile(state.CompactState, path)
    
    // 5. 执行工具
    toolResults := tools.Execute(resp.Content, state.CompactState)
    
    // 6. 如果无工具结果 → 循环结束
    if len(toolResults) == 0:
      return false
    
    // 7. 追加工具结果继续
    state.Messages = append(state.Messages, 
      anthropic.NewUserMessage(toolResults...))
    state.TurnCount++
    
    // 8. 检查手动压缩
    a.manualCompact(state, resp.Content)
  }
}

func (a *Agent) Run(r io.Reader) {  // REPL
  for {
    query = readline()
    history += UserMessage(query)
    
    state = Loop(history)  // 执行循环
    history = state.Messages
    
    print(最后的文本响应)
  }
}
```

**关键设计**:
- autoCompact: MicroCompact + 超限 LLM 总结
- manualCompact: 检测 "compact" 工具调用
- TrackRecentFile: 维护最近 5 个访问文件用于压缩

### 4. mcp.go (702 行) - MCP 客户端实现

**三种传输协议**:

#### 🟢 stdio (本地进程)
```go
startMCPProcess(name, cfg MCPServerConfig):
  1. cmd = exec.Command(cfg.Command, cfg.Args...)
  2. cmd.Env = cfg.Env
  3. stdin, stdout = pipes
  4. cmd.Start()
  
  初始化握手:
  5. call("initialize", {...})
  6. call("tools/list", {})
  
  工具调用:
  call(method, params) JSON-RPC:
    - 发送: {jsonrpc:"2.0", id:++, method, params}\n
    - 接收: 阻塞读取直到 \n
```

#### 🟣 streamableHttp (无状态 HTTP)
```go
connectMCPStreamableHTTP(name, cfg):
  1. client = &http.Client{Timeout: cfg.Timeout}
  
  初始化握手:
  2. POST(url, initialize)
  3. POST(url, tools/list)
  
  工具调用:
  call(method, params):
    - POST(url, {jsonrpc:"2.0", id, method, params})
    - 响应:
      - Content-Type: application/json → 直接解析
      - Content-Type: text/event-stream → parseSSEOnce()
```

#### 🔵 sse (持久化 SSE)
```go
connectMCPSSE(name, cfg):
  1. GET(baseURL) ← 建立 SSE 连接
  2. waitForEndpoint() ← 等待 "endpoint" 事件
  3. go readLoop() ← 后台读取 SSE 流
  
  初始化握手:
  4. POST(postURL, initialize)
  5. POST(postURL, tools/list)
  
  readLoop():
    持续读取 SSE 数据行 → 解析 JSON-RPC → 通过 chan 分发
  
  工具调用:
  call(method, params):
    1. id++
    2. ch = make(chan response, 1)
    3. pendingResp[id] = ch
    4. POST(postURL, {id, method, params})
    5. <-ch (30s 超时)
    6. readLoop 接收时触发 ch <- response
```

**工具暴露**:
```
MCPTools() → 遍历所有 MCP 服务器:
  for server in mcpServers:
    for tool in server.tools:
      prefixed = "mcp__" + serverName + "__" + toolName
      → anthropic.ToolParam{Name: prefixed, ...}

结果示例:
  "bash" (原生)
  "read_file" (原生)
  "write_file" (原生)
  "mcp__unionplus_mcp_normal__tool1" (MCP)
  "mcp__unionplus_mcp_normal__tool2" (MCP)
```

**调度**:
```
DispatchMCP("mcp__unionplus_mcp_normal__some_tool", input):
  1. rest = "unionplus_mcp_normal__some_tool"
  2. sep = strings.Index(rest, "__")
  3. serverName = rest[:sep] = "unionplus_mcp_normal"
  4. toolName = rest[sep+2:] = "some_tool"
  5. client = mcpServers[serverName]
  6. return client.callTool(toolName, input)
```

### 5. compact.go (213 行) - 上下文压缩

**三层机制**:

#### Layer 1: MicroCompact
```go
MicroCompact(messages, keepCount=3):
  1. 找最后一条包含 tool_result 的消息 (lastToolMsgIdx)
  2. 收集所有更早的 tool_result 块
  3. 如果旧结果 <= keepCount: 返回原样
  4. 否则: 替换最早的 (len(older)-keepCount) 个为占位符:
     "[Earlier tool result compacted. Re-run the tool if you need full detail.]"
  
  关键: 最后一条消息的所有结果总是保留
```

#### Layer 2: LLM Summarization
```go
SummarizeHistory(client, model, messages):
  1. 截断对话到 80000 字节
  2. 构造 prompt 要求总结:
     - 当前目标
     - 重要发现和决定
     - 修改的文件
     - 剩余工作
  3. 调用模型生成 2000 token 总结
  4. 返回文本
```

#### Layer 3: CompactHistory
```go
CompactHistory(client, model, messages, state, focus):
  1. WriteTranscript(messages)
     → .evo_agent/transcripts/transcript_{timestamp}.jsonl
  
  2. summary = SummarizeHistory(...)
  
  3. if focus != "":
       summary += "\n\nFocus to preserve next: " + focus
  
  4. if len(state.RecentFiles) > 0:
       summary += "\n\nRecent files:"
       for each file: summary += "\n- " + file
  
  5. state.HasCompacted = true
     state.LastSummary = summary
     state.CompactCount++
  
  6. return [UserMessage("This conversation was compacted...\n\n" + summary)]
     → 完整历史替换为单条消息
```

**自动触发**:
```
autoCompact(state):
  1. state.Messages = MicroCompact(state.Messages)
  
  2. contextSize = EstimateContextSize(state.Messages)
  
  3. if contextSize > CONTEXT_LIMIT (50000):
       CompactHistory(...)
```

**手动触发**:
```
manualCompact(state, content):
  for block in content:
    if block.Type == "tool_use" && block.Name == "compact":
      CompactHistory(..., focus=block.Input["focus"])
      break  // 每轮仅一个 compact 调用
```

---

## 📁 完整目录树 (17个 Go 文件)

```
src/
├── main.go                              [50 行] 入口点
├── evo-agent                            [编译产物]
├── go.mod
├── go.sum
└── internal/
    ├── agent/
    │   ├── loop.go                      [193 行] 🔴 核心循环
    │   ├── state.go                     [19 行] 状态定义
    │   ├── compact.go                   [213 行] 压缩策略
    │   └── transcripts.go               [76 行] 转录保存
    ├── config/
    │   └── config.go                    [44 行] 配置加载
    ├── tools/
    │   ├── tool.go                      [68 行] 🔴 工具注册
    │   ├── executor.go                  [56 行] 工具执行
    │   ├── mcp.go                       [702 行] 🔴 MCP 客户端
    │   ├── bash.go                      [63 行] bash 工具
    │   ├── read_file.go                 [55 行] read 工具
    │   ├── write_file.go                [43 行] write 工具
    │   ├── edit_file.go                 [? 行] edit 工具
    │   ├── compact.go                   [? 行] compact 工具
    │   └── persist.go                   [43 行] 输出持久化
    └── ui/
        └── terminal.go                  [34 行] 终端 UI

.evo_agent/
├── mcp.json                             MCP 配置
├── mcp-schema.json                      MCP Schema 参考
└── transcripts/                         压缩转录存档

总计: ~1500 行核心代码
```

---

## 🔑 关键配置常数

| 常数 | 值 | 位置 | 说明 |
|------|-----|------|------|
| `CONTEXT_LIMIT` | 50000 | compact.go | 自动压缩触发 (chars) |
| `KEEP_RECENT_RESULTS` | 3 | compact.go | MicroCompact 保留结果数 |
| `maxConversationBytes` | 80000 | compact.go | 总结模型输入最大字节 |
| `persistThreshold` | 30000 | persist.go | 工具输出持久化阈值 |
| `previewChars` | 2000 | persist.go | 持久化输出预览长度 |
| `previewPrintLen` | 200 | executor.go | 终端打印预览长度 |
| `maxReadBytes` | 50000 | read_file.go | 单次读文件最大字节 |
| `MaxTokens` | 8000 | loop.go | 单次请求最大输出 |
| `bash timeout` | 120s | bash.go | bash 命令超时 |
| `MCP timeout` | 30s | mcp.go | MCP 请求超时 |
| `Transcript DIR` | `.evo_agent/transcripts/` | transcripts.go | 转录保存目录 |
| `Tool Results DIR` | `.evo_agent/tool-results/` | persist.go | 持久化输出目录 |

---

## 🌳 执行流程总图

```
┌─────────────────────────────────────────────────────────────┐
│ USER INPUT                                                   │
└─────────────┬───────────────────────────────────────────────┘
              ↓
┌─────────────────────────────────────────────────────────────┐
│ Run(REPL)                                                    │
│ history.append(UserMessage(query))                          │
└─────────────┬───────────────────────────────────────────────┘
              ↓
┌─────────────────────────────────────────────────────────────┐
│ Loop(state)                                                  │
│ repeat {                                                     │
│   autoCompact():                                            │
│     ├─ MicroCompact(messages, 3)                          │
│     └─ if EstimateContextSize > 50000:                    │
│          CompactHistory() → LLM 总结                      │
└─────────────┬───────────────────────────────────────────────┘
              ↓
┌─────────────────────────────────────────────────────────────┐
│ client.Messages.New()                                       │
│ ├─ Model: cfg.ModelID                                      │
│ ├─ System: cfg.SystemMsg                                   │
│ ├─ Messages: state.Messages (完整历史)                    │
│ └─ Tools: tools.Tools()  ← [原生工具 + MCP工具]          │
└─────────────┬───────────────────────────────────────────────┘
              ↓
┌─────────────────────────────────────────────────────────────┐
│ Model Response (resp.Content)                              │
│ [TextBlock|ToolUseBlock|ThinkingBlock]                    │
└─────────────┬───────────────────────────────────────────────┘
              ↓
┌─────────────────────────────────────────────────────────────┐
│ history.append(resp)                                        │
│ TrackRecentFile(path)  ← if read_file tool used          │
└─────────────┬───────────────────────────────────────────────┘
              ↓
┌─────────────────────────────────────────────────────────────┐
│ Execute(resp.Content)                                       │
│ for each block:                                            │
│   ├─ TextBlock → PrintText()                             │
│   ├─ ThinkingBlock → PrintThinking()                     │
│   └─ ToolUseBlock:                                        │
│       ├─ Dispatch(name, input)                           │
│       │   ├─ if name.HasPrefix("mcp__"):                │
│       │   │   → DispatchMCP()                           │
│       │   │     ├─ parse: mcp__X__Y → X=server, Y=tool  │
│       │   │     └─ mcpServers[X].callTool(Y, input)     │
│       │   └─ else:                                      │
│       │       → registry[name].Handler()                │
│       ├─ persistLargeOutput()  ← 大输出 >30KB 保存     │
│       └─ ToolResultBlock(toolID, output, isError)      │
└─────────────┬───────────────────────────────────────────────┘
              ↓
┌─────────────────────────────────────────────────────────────┐
│ if len(toolResults) == 0:                                  │
│   return false  ← 循环结束                                 │
│ else:                                                       │
│   history.append(ToolResultMessage(...))                   │
│   TurnCount++                                              │
│   manualCompact()  ← 检查 "compact" 工具调用             │
│   continue        ← 继续循环                              │
└─────────────┬───────────────────────────────────────────────┘
              ↓
┌─────────────────────────────────────────────────────────────┐
│ Print Final Response                                        │
│ last = history[-1]                                         │
│ for text in last.Content:                                 │
│   print(text)                                             │
└─────────────────────────────────────────────────────────────┘
```

---

## 🚀 快速开始

```bash
cd /Users/tiankonguse-m3/project/github/AIProject/evo-agent/src

# 设置环境变量
export MODEL_ID=claude-3-5-sonnet-20241022
export ANTHROPIC_API_KEY=sk-ant-xxxxx

# (可选) 配置 MCP
# 编辑 .evo_agent/mcp.json 添加 MCP 服务器

# 编译
go build -o evo-agent

# 运行
./evo-agent

# REPL 交互
>> 你的任务或查询
```

---

## 📝 关键观察

### 设计优势
1. ✅ **自注册工具**: 添加工具无需修改核心代码
2. ✅ **表驱动调度**: O(1) 工具查找
3. ✅ **MCP 透明性**: MCP 工具与原生工具无缝混合
4. ✅ **三层压缩**: 适应不同压缩需求
5. ✅ **流式 MCP**: 支持 stdio/sse/streamableHttp
6. ✅ **持久化故障转移**: 大输出保存失败自动降级

### 扩展点
1. 添加新工具: 创建 `src/internal/tools/my_tool.go`，在 `init()` 中 `Register()`
2. 添加 MCP 服务器: 编辑 `.evo_agent/mcp.json` 添加配置
3. 自定义 System Prompt: 修改 `config.go` 的 `Load()` 函数
4. 调整压缩策略: 修改 `compact.go` 中的常数 (CONTEXT_LIMIT, KEEP_RECENT_RESULTS 等)

### 性能特性
- **工具执行**: 同步执行，单轮 8000 token 限制
- **上下文管理**: 三层压缩自动降级，最小化 token 消耗
- **MCP 连接**: 连接复用 (stdio/sse 持久化，streamableHttp 无状态)
- **输出处理**: 大文件自动持久化到磁盘，内存占用恒定

