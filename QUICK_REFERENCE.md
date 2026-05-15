# evo-agent 快速参考卡片

## 🎯 项目一句话概括
Go 编写的 AI 代理框架，集成 Anthropic API + 自注册工具系统 + MCP 支持。

---

## 📌 5 个核心概念

### 1️⃣ 工具注册 (Self-Registration Pattern)
```go
// 每个工具文件中:
func init() {
  Register(ToolDef{
    Schema: anthropic.ToolParam{Name: "my_tool", ...},
    Handler: func(input json.RawMessage)(string, error) { ... },
  })
}
// 无需修改 tool.go!
```

### 2️⃣ 工具调度 (Dispatch Router)
```
Dispatch(name, input)
  ├─ if name == "mcp__X__Y" → DispatchMCP()
  └─ else → registry[name].Handler()
```

### 3️⃣ Agent 循环 (ReAct Loop)
```
repeat {
  autoCompact()
  resp = api_call(messages, tools)
  messages += resp
  toolResults = Execute(resp.Content)
  if toolResults.empty() {
    return  // 循环结束
  }
  messages += ToolResult
}
```

### 4️⃣ MCP 集成 (Three Transports)
```
stdio        ← 本地进程 (JSON-RPC over stdin/stdout)
streamableHttp ← 无状态 HTTP (POST, 可选 SSE 响应)
sse          ← 持久化 SSE (GET + POST)
```

### 5️⃣ 三层压缩 (Compression Strategy)
```
MicroCompact      → 替换旧结果为占位符 (快)
LLM Summarization → 生成文本总结 (深)
Manual Compact    → 手动触发 + 保存转录 (精)
```

---

## 📂 文件导航

| 文件 | 行数 | 用途 | 关键函数 |
|------|------|------|---------|
| **main.go** | 50 | 入口、配置、MCP 启动 | `main()`, `BuildOptions()` |
| **loop.go** | 193 | 🔴 Agent 循环核心 | `Loop()`, `Run()` |
| **tool.go** | 68 | 🔴 工具注册系统 | `Register()`, `Tools()`, `Dispatch()` |
| **mcp.go** | 702 | 🔴 MCP 客户端 | `InitMCP()`, `MCPTools()`, `DispatchMCP()` |
| **compact.go** | 213 | 压缩策略 | `MicroCompact()`, `CompactHistory()`, `SummarizeHistory()` |
| **executor.go** | 56 | 工具执行 | `Execute()` |
| **state.go** | 19 | 状态定义 | `LoopState`, `CompactState` |
| **config.go** | 44 | 配置加载 | `Load()`, `LoadEnv()` |
| **transcripts.go** | 76 | 转录保存 | `WriteTranscript()` |
| **persist.go** | 43 | 输出持久化 | `persistLargeOutput()` |
| **bash.go** | 63 | bash 工具 | `runBash()` |
| **read_file.go** | 55 | read 工具 | `runReadFile()` |
| **write_file.go** | 43 | write 工具 | `runWriteFile()` |
| **terminal.go** | 34 | UI 输出 | `PrintThinking()`, `PrintText()`, ... |

---

## 🔧 配置

### 环境变量 (.env 或系统环境)
```bash
MODEL_ID=claude-3-5-sonnet-20241022          # 必需
ANTHROPIC_API_KEY=sk-ant-xxxxx                # 可选 (支持 dummy)
ANTHROPIC_BASE_URL=https://custom.endpoint    # 可选
```

### MCP 配置 (.evo_agent/mcp.json)
```json
{
  "mcpServers": {
    "server_name": {
      "type": "stdio|sse|streamableHttp",
      "command": "...",              // stdio only
      "url": "https://...",          // remote only
      "headers": {...},              // remote only
      "disabled": false,
      "timeout": 30
    }
  }
}
```

---

## ⚡ 快速操作

### 添加新工具
```bash
# 创建文件: src/internal/tools/my_tool.go
cat > src/internal/tools/my_tool.go << 'EOF'
package tools

import (
  "encoding/json"
  "github.com/anthropics/anthropic-sdk-go"
)

type MyInput struct {
  Param string `json:"param" jsonschema_description:"Description"`
}

func init() {
  Register(ToolDef{
    Schema: anthropic.ToolParam{
      Name: "my_tool",
      Description: anthropic.String("Tool description"),
      InputSchema: GenerateSchema[MyInput](),
    },
    Handler: func(input json.RawMessage) (string, error) {
      var in MyInput
      json.Unmarshal(input, &in)
      // 实现逻辑
      return "output", nil
    },
  })
}
EOF

# 重新编译
cd src && go build -o evo-agent
```

### 调整压缩参数
```go
// compact.go 顶部
const (
  CONTEXT_LIMIT       = 50000   // ← 自动压缩触发阈值
  KEEP_RECENT_RESULTS = 3       // ← 保留最近 N 个结果
)
```

### 调整工具超时
```go
// bash.go
ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
// ↑ 改为所需的超时时间

// mcp.go - MCPServerConfig.timeout()
if c.Timeout > 0 {
  return time.Duration(c.Timeout) * time.Second
}
return 30 * time.Second  // ← 默认 30s
```

---

## 📊 关键常数一览

```go
// compact.go
CONTEXT_LIMIT        = 50000        // 自动压缩触发 (chars)
KEEP_RECENT_RESULTS  = 3            // MicroCompact 保留个数
maxConversationBytes = 80000        // 总结模型输入上限

// executor.go
previewPrintLen      = 200          // 终端打印预览长度

// persist.go
persistThreshold     = 30000        // 输出持久化阈值
previewChars         = 2000         // 持久化输出预览长度

// read_file.go
maxReadBytes         = 50000        // 单次读取上限

// bash.go
timeout              = 120s         // bash 命令超时

// loop.go
MaxTokens            = 8000         // 单次请求最大输出
```

---

## 🔍 调试技巧

### 打印工具列表
```bash
./evo-agent  # 启动时会自动打印:
# [MCP] Connected to "unionplus_mcp_normal" (N tools)
# Server: unionplus_mcp_normal
#   - tool1: description
#   - tool2: description
```

### 查看压缩日志
```
[auto compact triggered: 55000 chars]
DEBUG: Generating summary...
DEBUG: Summary generated in 2.34s
[Compacted to 2145 chars, removed 12 messages]
```

### 转录文件位置
```
.evo_agent/transcripts/transcript_1715680400.jsonl
```

### 大输出持久化位置
```
.evo_agent/tool-results/{toolID}.txt
```

---

## 🎓 工作流程概览

```
┌────────────┐
│ 用户输入    │
└─────┬──────┘
      ↓
┌────────────────────────┐
│ Run(REPL)              │
│ + 历史管理             │
└─────┬──────────────────┘
      ↓
┌────────────────────────┐
│ Loop()                 │
│ + autoCompact()        │
│ + API 调用             │
│ + 工具执行循环         │
└─────┬──────────────────┘
      ↓
┌────────────────────────┐
│ Execute()              │
│ + Dispatch()           │
│ ├─ 原生工具            │
│ └─ MCP 工具            │
└─────┬──────────────────┘
      ↓
┌────────────────────────┐
│ 返回最终响应            │
└────────────────────────┘
```

---

## 🛠️ 常见问题

**Q: 如何添加新的 MCP 服务器?**
A: 在 `.evo_agent/mcp.json` 中添加配置，重启程序。

**Q: 工具何时自动压缩?**
A: 当 `EstimateContextSize(messages) > 50000` 时自动触发 LLM 总结。

**Q: 如何手动触发压缩?**
A: 模型调用 `"compact"` 工具，自动保存转录到 `.evo_agent/transcripts/`。

**Q: MCP 工具名如何映射?**
A: `mcp__{serverName}__{toolName}`，例: `mcp__unionplus_mcp_normal__some_tool`

**Q: 大型工具输出会发生什么?**
A: 超过 30KB 自动保存到 `.evo_agent/tool-results/{toolID}.txt`，返回占位符。

**Q: 如何自定义 System Prompt?**
A: 修改 `config.go` 中的 `Load()` 函数，或在程序启动前设置环境变量。

---

## 📚 文档地址

- **完整分析报告**: `EVO_AGENT_COMPLETE_ANALYSIS.md`
- **项目 README**: `README.md`
- **开发博客**: `blog/` 目录
- **API 参考**: `doc/API_REFERENCE.md`

