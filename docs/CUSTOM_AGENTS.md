# 自定义 subAgent (Custom Subagents)

evo-agent 支持用 Markdown + YAML frontmatter 定义专项 subagent，让模型针对不同任务调用不同"专家"。设计参考 Claude Code 官方 `.claude/agents/`，但放在项目下的 `.evo-agent/agents/`。

## 是什么

普通 `task` 工具调用时，subagent 继承父 agent 的完整 system prompt（编码规则、记忆、计划、Agent.md……），并使用与父相同的模型。这适合"通用助手帮我看下这段代码"。

**自定义 subagent** 反过来：

- **完全替换** 父 agent 的 system prompt，只保留你写在 markdown 正文里的指令 + 一段最小环境信息（CWD、git、平台、当前日期、模型）。
- 可指定 **专属模型**（例如让简单任务跑廉价模型）。
- 可指定 **轮次上限**。

适合场景：code-reviewer、test-runner、专门 explore 的只读 agent、风格检查、特定领域的查询助手等。

## 文件位置

```
<project-root>/.evo-agent/agents/<agent-name>.md
```

每个 `.md` 文件就是一个 agent 定义。文件名（去掉 `.md`）默认是 agent 名，frontmatter 的 `name:` 可以覆盖。

## 文件格式

```markdown
---
name: code-reviewer
description: Review code for security, style, and correctness issues
model: inherit
max_turns: 20
---

You are a senior software engineer performing a focused code review.

# Your job
1. Read the files mentioned in the user's request.
2. Identify bugs, security risks, error handling gaps...
...
```

### Frontmatter 字段

| 字段 | 必备 | 默认 | 说明 |
|---|---|---|---|
| `name` | 否 | 文件名去 `.md` | 唯一标识；调用时用 `subagent_type: "<name>"` 选择 |
| `description` | 是 | `"No description"` | 一行说明，注入到主 agent system prompt 让模型知道何时调用 |
| `model` | 否 | 继承父 | Anthropic 模型 id；写 `inherit` 或留空 = 用父 agent 的模型 |
| `max_turns` | 否 | `30` | LLM 轮次上限；正整数 |

### 正文（Body）

`---` 之后到文件结尾的所有内容就是该 agent 的 system prompt。**完全自由格式**，会原样发给 LLM。

不要重复编码守则 / 记忆 / 计划这些通用上下文 —— 自定义 agent 的设计目标就是让 prompt 简洁、专一。需要项目上下文（构建命令、目录结构等）时，可以在正文里直接写或在 prompt 里指示 agent 用 `read_file` 自行加载。

## 调用方式

模型通过 `task` 工具调用：

```json
{
  "tool": "task",
  "input": {
    "description": "Review auth module",
    "subagent_type": "code-reviewer",
    "prompt": "Review src/internal/auth/*.go. Focus on session token handling. Return findings as a Markdown report."
  }
}
```

**`subagent_type` 省略 = 走通用 subagent**（继承父 prompt，向后兼容）。

模型在主 agent 的 system prompt 里能看到所有可用的自定义 agent，自动决定何时调用哪一个。例如：

```
Available custom agents (invoke via the task tool with subagent_type):
- code-reviewer: Review code for security, style, and correctness issues
- test-runner: Run tests and report failures
```

## 与通用 subagent 的差异速查

| 维度 | 通用 subagent (无 `subagent_type`) | 自定义 subagent (`subagent_type: "<name>"`) |
|---|---|---|
| System prompt | 父完整 prompt + 简单 addendum | **完全替换** 为 agent 正文 + 最小环境 |
| Model | 父的 `MODEL_ID` | frontmatter `model:` 优先，空则父 |
| Max turns | 30 | frontmatter `max_turns:` 优先，0 则 30 |
| Tools | `ToolsExcept("task")` | 同上（本期未实现工具白/黑名单） |
| 可见上下文 | Agent.md / skills / memories / plans / team / goal 全可见 | 只看到正文 + 环境（CWD/git/平台/日期/模型） |
| 持久化 | `.evo-agent/sessions/<id>/subagent/<file>.jsonl` | 同样写 sidechain，文件名用 agent 名 |

## 启动加载

启动时打印类似：

```
[Agents] Loaded 3 agent(s)
```

加载从 `.evo-agent/agents/*.md` 读取，扁平目录（不递归）。frontmatter 损坏或缺字段会回退（用文件名作 name、`"No description"` 作描述），不阻止启动。

## `/agents` 命令

REPL / TUI 都支持 `/agents` 系列纯客户端命令（不会触发 LLM 轮次）：

| 命令 | 作用 |
|---|---|
| `/agents` 或 `/agents list` | 列出所有已加载的自定义 agent（name、description、model、max_turns） |
| `/agents show <name>` | 显示某个 agent 的完整定义：frontmatter + 系统提示正文 |
| `/agents reload` | 重新扫描 `.evo-agent/agents/` 目录（编辑文件后无需重启 evo-agent） |

示例输出：

```
> /agents
Custom agents (2 loaded):
  - code-reviewer    [model=inherit, max_turns=20]
      Review code for security, style, and correctness issues
  - explore          [model=inherit, max_turns=30]
      Read-only codebase explorer

Usage: /agents show <name> | /agents reload
Invoke from the model via: task({subagent_type: "<name>", prompt: "..."})
```

`/agents` 也会出现在 TUI 的 `/` 自动补全下拉里（与 `/tools`、`/goal` 等并列）。

## 工程提示

- 把 `description` 写得**像一句使用说明**：模型按它判断何时调用，太模糊或太通用会让模型找不到合适入口。
- 想给某 agent 加 project 上下文，**直接复制粘贴到正文**比让 agent 跑 `read_file` 探索更省 token、更稳定。
- 自定义 agent 不会自动加载 skills，需要的话在正文里写明 "Use load_skill <name> first"，agent 会自己去调。
- 主 agent 调用 `task` 时只会看到 agent 的 `description`（不是正文），所以正文随便写多少都不会污染主上下文。

## 不在本期范围

- agent 维度的 `tools` / `disallowedTools` 白/黑名单
- agent 专属 MCP servers / hooks / permissionMode
- 用户级 (`~/.evo-agent/agents/`) 与项目级合并

这些都可以增量补齐 —— frontmatter 加新字段不会破坏现有 agent。

## Fork subagent

除了通用 / 自定义 subagent，`task` 工具还支持 **fork** —— 子进程**完整继承**父 agent 的 system prompt 和会话历史，然后接收一条 directive（指令）作为收尾。

调用：

```json
{
  "tool": "task",
  "input": {
    "description": "ship audit",
    "fork": true,
    "prompt": "Audit what's left before this branch can ship. Check uncommitted changes, commits ahead of main, missing tests. Return a punch list under 200 words."
  }
}
```

`fork: true` 与 `subagent_type` **互斥**（同时设置会报错）。

### 何时用 fork

- **调研类问题**（"项目里有几处用了 deprecated API？"）—— fork 跑一遍 grep / read，给你结论；中间 grep 输出的 50 屏不会污染父 context。
- **多步实现工作**（"按这个 plan 落地 feature X"）—— fork 在父已有的代码语境里直接干活。

### 与通用 / 自定义 subagent 的差异

| 维度 | 通用 subagent | 自定义 subagent | **fork** |
|---|---|---|---|
| System prompt | 父 + addendum | agent 自己的 prompt | **父完整 prompt** |
| Message history | 仅 user prompt | 仅 user prompt | **父完整 history** |
| Model | 父 | frontmatter 优先 | 父 |
| Max turns | 30 | frontmatter 优先（默认 30） | **60** |
| Tools | `ToolsExcept("task")` | 同 | 同（防止递归 fork） |
| 适用场景 | 通用委托 | 专家领域 | 减负 / 并行调研 |

### Fork 内部约定

子进程被注入一段 **fork-boilerplate** 提示，明确告诉它：

- 你是 forked worker，不是 main agent
- 不要再 fork（递归 fork 会被工具表 + boilerplate 双重拦住）
- 输出格式以 `Scope:` 开头，包含 `Result:` / `Key files:` / `Files changed:` / `Issues:`
- 字数 < 500，简洁结构化

父在 fork 返回后只看到这个结构化报告，不会看到 fork 内部的 read/grep/edit 噪音。

### 实现细节

- `RunForkSubagent`（`src/internal/agent/fork.go`）继承 `a.prompt.Build()` 与 `a.cfg.ModelID`。
- 父 message history 在转交前先过 `FilterIncompleteToolCalls`（`src/internal/agent/filter.go`）—— task 工具是在多工具一回合的中间被调用的，父 assistant 消息里那条调用 task 的 `tool_use` 块此时还没 tool_result 配对，直接发给 API 会 400。filter 把这种"孤儿"assistant 消息整条剔掉。
- 复用 `runSubagentLoop`，所以持久化 / UI / 工具分发完全与通用、自定义 subagent 一致。

## 示例：最小可用 agent

`.evo-agent/agents/hello.md`：

```markdown
---
name: hello
description: Greets back politely. Use when the user just wants a hello.
model: inherit
max_turns: 5
---

You are a polite greeter. Reply with a one-sentence friendly greeting and stop. Do not use any tools.
```

调用：`task({subagent_type: "hello", description: "greet", prompt: "say hi"})`
