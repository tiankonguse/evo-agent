# evo-agent 项目探索总结 (2026-05-14)

## 📊 探索范围

- ✅ 完整目录结构 (17 个 .go 文件)
- ✅ 核心代码逻辑 (~1500 行)
- ✅ 工具注册与调度系统
- ✅ MCP 客户端实现 (三种传输协议)
- ✅ Agent 循环与压缩策略
- ✅ 配置系统与 MCP 加载机制
- ✅ 完整的执行流程分析

---

## 🎯 项目核心

### 一句话定义
**Go 编写的轻量级 AI 代理框架**，采用自注册工具模式 + 表驱动调度，支持原生工具和 MCP 远程工具混合，具有三层上下文压缩机制。

### 核心价值

1. **模块化工具系统**
   - 自注册模式: 添加工具无需修改核心代码
   - 表驱动调度: O(1) 工具查找
   - Schema 反射: 自动生成 JSON Schema

2. **无缝 MCP 集成**
   - 支持三种传输: stdio、streamableHttp、sse
   - MCP 工具与原生工具统一接口
   - 动态工具列表加载

3. **智能上下文管理**
   - 三层压缩策略适应不同场景
   - MicroCompact: 快速、无损
   - LLM Summarization: 深度、自动
   - Manual Compact: 精准、可控

4. **生产级稳定性**
   - 持久化大型输出
   - 转录保存 (审计)
   - 超时管理
   - 故障降级

---

## 🔬 代码质量观察

### 优点 ✅

1. **架构清晰**
   - 职责分离明确 (config/agent/tools/ui)
   - 接口设计一致 (mcpClient, Handler, ToolDef)
   - 流程逻辑直观

2. **扩展性强**
   - 自注册工具模式极易添加
   - MCP 配置驱动
   - 压缩参数可调

3. **容错机制**
   - 大输出持久化失败自动降级
   - MCP 连接失败继续运行
   - 缺失 .evo-agent/mcp.json 不致命

4. **性能意识**
   - Token 限制 (8000 per request)
   - 文件大小限制 (50KB read, 50KB bash output)
   - 上下文大小估计与自动压缩

### 改进空间 🔧

1. **错误处理**
   - Dispatch 返回 nil 对工具不存在, 无错误提示
   - MCP 连接失败日志仅 stderr
   - 工具超时无重试

2. **并发**
   - 单线程执行工具 (可考虑异步)
   - MCP stdio 使用 mutex (性能可接受)

3. **配置灵活性**
   - System Prompt 硬编码在 config.go
   - 压缩阈值在常数中，运行时不可调

4. **文档完整性**
   - 缺少 edit_file 和 compact 工具的详细说明
   - MCP Schema 验证缺失

---

## 🌳 工作流详解

### 启动流程 (main.go)
```
1. LoadEnv()           → 加载 .env 文件
2. Load()              → 读环境变量到 Config
3. BuildOptions()      → 构建 API 客户端选项
4. anthropic.NewClient() → 创建 API 客户端
5. InitMCP()           → 读 .evo-agent/mcp.json, 连接所有 MCP 服务
6. PrintToolList()     → 打印已加载的工具
7. agent.New()         → 创建 Agent 实例
8. agent.Run(stdin)    → 启动 REPL
```

### REPL 循环 (Run → Loop)
```
REPL: for {
  query = readline()
  history.append(UserMessage(query))
  
  Loop(state{Messages: history, ...}):
    repeat {
      autoCompact()
        → MicroCompact(保留最近3个结果)
        → if contextSize > 50000: LLM 总结
      
      resp = client.Messages.New(
        Model, System, Messages, Tools
      )
      
      history.append(resp)
      
      toolResults = Execute(resp.Content)
        → for each block:
          - TextBlock → PrintText()
          - ToolUseBlock → Dispatch()
          - ThinkingBlock → PrintThinking()
      
      if toolResults.empty():
        return false  # 循环结束
      
      history.append(ToolResult)
      manualCompact()  # 检查 "compact" 工具
    }
  
  print(最终响应)
}
```

### 工具执行流 (Dispatch → Execute)
```
Execute(resp.Content):
  for block in Content:
    if ToolUseBlock:
      output = Dispatch(block.Name, block.Input)
        if block.Name.HasPrefix("mcp__"):
          → DispatchMCP()
            1. parse: mcp__X__Y → X=server, Y=tool
            2. return mcpServers[X].callTool(Y, input)
        else:
          → registry[block.Name].Handler()
      
      output = persistLargeOutput(output)
        if len(output) > 30KB:
          → save to .evo-agent/tool-results/{id}.txt
          → return placeholder
      
      append ToolResultBlock(id, output, isError)
  
  return results
```

### 压缩触发

#### 自动压缩 (autoCompact)
```
if EstimateContextSize(messages) > 50000:
  summary = SummarizeHistory(messages)
  messages = [UserMessage("This conversation was compacted...\n\n" + summary)]
```

#### 手动压缩 (manualCompact)
```
if resp contains block{Type:"tool_use", Name:"compact"}:
  focus = block.Input["focus"]
  WriteTranscript(messages)  → .evo-agent/transcripts/
  summary = SummarizeHistory(messages)
  messages = [UserMessage("...\n\n" + summary + focus_hint)]
```

---

## 📋 文件速查表

| 文件 | 行数 | 核心职责 |
|------|------|---------|
| main.go | 50 | 入口、环境初始化、MCP 启动 |
| loop.go | 193 | 🔴 Agent 主循环、REPL、工具执行协调 |
| tool.go | 68 | 🔴 工具注册机制、统一调度接口 |
| mcp.go | 702 | 🔴 MCP 客户端、三种传输协议实现 |
| compact.go | 213 | 三层压缩、LLM 总结、文件追踪 |
| executor.go | 56 | 内容块处理、工具路由、输出持久化 |
| state.go | 19 | 循环状态、压缩状态数据结构 |
| config.go | 44 | 配置加载、环境变量处理 |
| transcripts.go | 76 | 转录保存 (JSONL 格式) |
| persist.go | 43 | 大输出持久化、故障降级 |
| bash.go | 63 | bash 工具实现 (120s 超时) |
| read_file.go | 55 | 文件读取工具 (支持行数限制) |
| write_file.go | 43 | 文件写入工具 (自动创建目录) |
| terminal.go | 34 | 终端 UI (颜色输出) |

---

## 🔑 关键设计模式

### 1. 自注册工具模式
```go
// 每个工具文件 init() 中:
func init() {
  Register(ToolDef{...})  // ← 自动注册
}

// 优势: 添加工具无需修改核心代码
```

### 2. 表驱动调度
```go
registry := map[string]ToolDef{}

Dispatch(name, input):
  if strings.HasPrefix(name, "mcp__"):
    → DispatchMCP()
  else:
    → registry[name].Handler()
```

### 3. MCP 接口抽象
```go
type mcpClient interface {
  getTools() []mcpToolSpec
  callTool(name string, args json.RawMessage) (string, error)
  stop()
}

// 三种实现: mcpProcess, mcpHTTPClient, mcpSSEClient
```

### 4. 三层压缩策略
```
MicroCompact (快) → LLMSummarization (深) → ManualCompact (精)
```

### 5. 故障转移模式
```
try persistLargeOutput()
if error:
  fallback to in-memory truncation
```

---

## 📦 配置要点

### 环境变量 (必需/可选)
```bash
MODEL_ID=claude-3-5-sonnet-20241022          # 必需
ANTHROPIC_API_KEY=sk-ant-xxxxx                # 可选
ANTHROPIC_BASE_URL=https://custom.endpoint    # 可选
```

### MCP 配置 (.evo-agent/mcp.json)
```json
{
  "mcpServers": {
    "name": {
      "type": "stdio|sse|streamableHttp",
      "disabled": false,
      "timeout": 30,
      // stdio fields
      "command": "...",
      "args": [...],
      "env": {...},
      // remote fields
      "url": "https://...",
      "headers": {...}
    }
  }
}
```

---

## 📊 性能特性

| 指标 | 值 | 说明 |
|------|-----|------|
| 工具执行超时 | 120s (bash) | 防止无限运行 |
| API 输出限制 | 8000 tokens | 单次请求限制 |
| 文件读取限制 | 50KB | 防止大文件加载 |
| 工具输出限制 | 50KB | bash/read_file 输出 |
| 输出持久化阈值 | 30KB | 超过则保存到磁盘 |
| 自动压缩触发 | 50000 chars | 上下文大小 |
| 压缩保留结果 | 3 个 | MicroCompact 保留个数 |
| 最近文件追踪 | 5 个 | 用于压缩提示 |

---

## 🚀 快速开始

```bash
cd /Users/tiankonguse-m3/project/github/AIProject/evo-agent/src

# 设置环境
export MODEL_ID=claude-3-5-sonnet-20241022
export ANTHROPIC_API_KEY=sk-ant-xxxxx

# 编译
go build -o evo-agent

# 运行
./evo-agent

# 交互
>> your task here
```

---

## 📚 生成的文档

| 文档 | 内容 | 位置 |
|------|------|------|
| **EVO_AGENT_COMPLETE_ANALYSIS.md** | 615 行完整分析 | 项目根目录 |
| **QUICK_REFERENCE.md** | 快速参考卡片 | 项目根目录 |
| **本文件** | 探索总结 | 项目根目录 |
| **notepad (Manual)** | MemPalace 手动笔记 | Claude Code |

---

## ✅ 探索完成清单

- [x] 目录结构分析
- [x] 17 个 .go 文件内容读取
- [x] 工具注册与调度机制理解
- [x] MCP 客户端三种协议分析
- [x] Agent 循环完整流程图
- [x] 上下文压缩三层机制理解
- [x] 配置加载流程分析
- [x] .evo-agent/ 配置解析
- [x] 完整文件内容提取
- [x] 代码质量评估
- [x] 扩展点识别
- [x] 性能特性分析
- [x] 多层级文档生成

---

## 🎓 关键收获

### 架构洞察
1. **自注册工具** 是模块化设计的范例
2. **表驱动调度** 提供灵活的扩展机制
3. **三层压缩** 优雅地处理无限长对话
4. **MCP 抽象** 支持多种部署拓扑

### 生产经验
1. **故障转移** 比完美错误处理更实用
2. **持久化策略** 平衡内存与磁盘 I/O
3. **配置驱动** 运行时灵活性高
4. **监控友好** (日志、工具列表打印)

### 可学习的模式
- ✅ 自注册工具模式 (直接应用)
- ✅ 接口驱动的多协议实现 (参考 MCP)
- ✅ 分层压缩策略 (上下文管理)
- ✅ 故障转移设计 (容错)

---

**报告时间**: 2026-05-14  
**项目根**: `/Users/tiankonguse-m3/project/github/AIProject/evo-agent`  
**总代码行**: ~1500 行  
**文件数**: 17 个 .go 文件  

