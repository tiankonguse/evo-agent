---
name: init
description: Analyze codebase and generate Agent.md
user-invocable: true
---

为当前仓库生成 Agent.md。Agent.md 会在每次 evo-agent 会话中作为上下文加载，因此必须简洁 — 只写 agent 不知道就会犯错的内容。

## Phase 1: 探索代码库

用 `read_file` 和 `bash` 读取关键文件来了解项目：

- 依赖清单：package.json, go.mod, Cargo.toml, pyproject.toml, pom.xml 等
- README.md 和 docs/ 目录
- 构建配置：Makefile, Dockerfile, CI 配置（.github/workflows/, .gitlab-ci.yml）
- 已有的 Agent.md, CLAUDE.md, .cursor/rules, .cursorrules, .github/copilot-instructions.md
- 入口文件：main.go, index.ts, main.py, cmd/ 等
- 目录树（2-3 层）
- 已有的 `.evo-agent/command/` 目录
- MCP 配置：`.evo-agent/mcp.json`

需要识别：
- 构建、测试、lint 命令（尤其是非标准的）
- 语言、框架、包管理器
- 项目结构（monorepo、多模块、还是单项目）
- 与语言默认不同的代码风格规则
- 非显而易见的坑、必需的环境变量、工作流特殊要求
- 格式化工具配置（prettier, biome, ruff, black, gofmt, rustfmt 等）

## Phase 2: 写 Agent.md

在项目根目录写 Agent.md。每一行都要过测试："删掉这行 agent 会犯错吗？" 不会就删。

**要写的内容：**
- 项目概述（是什么、解决什么问题，3-5 句话）
- 技术栈和关键依赖
- 架构地图（核心结构，关键目录及说明）
- agent 猜不到的构建/测试/lint 命令（非标准脚本、特殊参数、执行顺序）
- 与语言默认不同的代码风格规则
- 测试指南和特殊要求（如 "运行单测: go test -run TestName"）
- 开发约定（分支命名、PR 规范、commit 格式）
- 必需的环境变量或配置步骤
- 非显而易见的坑或架构决策
- 禁止事项（至少 2 条，从架构约束推导）
- 已有 AI 工具配置中的重要部分

**不要写的内容：**
- 逐文件的结构列表（agent 可以自己读）
- 语言的标准惯例（agent 已经知道）
- 泛泛的建议（"写干净的代码"、"处理好错误"）
- 详细的 API 文档或长篇参考
- 经常变化的信息
- 从清单文件就能看出的命令（如标准的 "go test"、"npm test"）

### 自检 — 写完后逐项检查，不通过就修复：

| 检查项 | 标准 | 修复方式 |
|--------|------|----------|
| 总行数 | <= 100 行 | 删除描述性段落，保留导航信息 |
| 项目概述 | 清晰定位 | 补充"是什么、解决什么问题" |
| 关键目录 | 对应 src/ 二级目录 | 补充缺失目录 |
| 常用命令 | 可直接复制执行 | 从 Makefile/package.json 提取真实命令 |
| 禁止事项 | 至少 2 条 | 从架构约束推导 |

### 规则

- 总行数 <= 100
- 命令必须真实可执行（从 Makefile/package.json 等提取）
- 不要自己编造章节如 "常见开发任务"、"开发提示"
- 只写从文件中确实读到的信息
- 如果 Agent.md 已存在：先读、再提出具体修改建议并解释原因，不要静默覆盖
- 文件开头固定为：

```
# Agent.md

This file provides guidance to evo-agent when working with code in this repository.
```

## Phase 3: 总结

1. 列出 Agent.md 的关键要点
2. 展示自检结果（每项通过/未通过）
3. 提醒用户可随时重新执行 `/init` 来更新
