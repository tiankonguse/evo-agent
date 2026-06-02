---
layout: post  
title: 让 Agent 不会停下来：goal 命令  
description: Agent 总是跑两步就停下来等你说"继续"，原因藏在 Loop 的实现机制上。/goal 命令给它一个完成条件，让它自己一轮一轮跑下去，跑到目标达成为止。  
keywords: agent,goal,loop,evaluator,autonomy,golang  
tags: [agent, goal, loop, evaluator, golang]  
categories: [程序人生]  
updateDate: 2026-06-01 21:00:00  
published: true  
source: "https://mp.weixin.qq.com/s/DfDFsIhLZJp1NiXz9dp7ug"
---


## 零、背景


前十五篇文章分别讲了 Agent 的 [Loop](https://mp.weixin.qq.com/s/dkdrwVlwe3IkH2hzSzy53A)、[工具](https://mp.weixin.qq.com/s/xyX4_CF5cveezEDuzFT13g)、[上下文记忆](https://mp.weixin.qq.com/s/lguRAdxFoN22rqPyx3BIzw)、[上下文压缩](https://mp.weixin.qq.com/s/YRS29wRckEmFgNb0eJrxrQ)、[MCP](https://mp.weixin.qq.com/s/rCnGif8Ee7JhRI86-RoNWA)、[Skill](https://mp.weixin.qq.com/s/X2ie0aQ2vMtddAQrkbOG5g)、[TUI](https://mp.weixin.qq.com/s/fBNFZvOOpwCPT7yysh5YkQ)、[任务规划](https://mp.weixin.qq.com/s/UIlEXIuQdacowdrIg1nrDQ)、[子代理](https://mp.weixin.qq.com/s/LfgDcv27vjlmLZ9NfvQ9LA)、[命令](https://mp.weixin.qq.com/s/M1jxdA4BysQkaN7p4hwneQ)、[跨会话记忆](https://mp.weixin.qq.com/s/wEQwMadb84ixfVXteNfESA)、[Agent.md](https://mp.weixin.qq.com/s/82KmXRTsiDrhB-RZFg5sXw)、[系统提示词](https://mp.weixin.qq.com/s/15mxhcDs1oWBwguF_IIZDg)、[任务持久化](https://mp.weixin.qq.com/s/86urMkNycEkI38KCoS0mxg) 和 [会话持久化](https://mp.weixin.qq.com/s/zyVNi0JXBlbO-z3KtZEFcA)。  


这篇聊聊一个让 Agent 「持续自己干活」的小命令——**/goal**。  


![截图](https://res2026.tiankonguse.com/images/2026/06/02/001.png)


## 一、Agent 为什么"干两步就停"


第一篇文章讲过，Agent 的 Loop 长这样：模型回一段话，看里面有没有 `tool_use` 块，有就执行工具，把结果塞回去再问下一轮；没有，循环结束。  


![截图](https://res2026.tiankonguse.com/images/2026/06/02/002.png)


```mermaid
sequenceDiagram
    participant User
    participant AgentLoop as Agent Loop
    participant LLM
    participant Tool as 工具

    User->>AgentLoop: 输入查询
    AgentLoop->>LLM: messages
    LLM-->>AgentLoop: 回复（含 tool_use）

    loop 只要还有 tool_use
        AgentLoop->>Tool: 执行工具
        Tool-->>AgentLoop: tool_result
        AgentLoop->>LLM: messages + tool_result
        LLM-->>AgentLoop: 下一轮回复
    end

    Note over AgentLoop: 回复里没有 tool_use
    AgentLoop-->>User: 退出，等下一次输入
```


这套逻辑对"小问答"很合理——你问一句"列一下当前目录有啥"，Agent 调一次 `bash`、看一眼结果、回你一段总结，任务结束，控制权回到用户手上。  


但碰到大任务就体验特别差了。  


假设你说："把 `src/api/` 下所有 REST 调用迁移到 GraphQL，跑通所有测试"。Agent 改了两个文件、跑了一次测试、看到一堆失败、回你一段："我修改了 user.go 和 auth.go，目前 4 个测试还在失败，需要继续修改吗？"——然后**停下来等你回复**。  


你只能再敲一句"继续"。然后又是几步、又是问"还要继续吗"。  


根因藏在 Loop 最底层的那行判断里：  


```go
// 简化伪代码
for {
    resp := llm.Call(messages)
    if !hasToolUse(resp) {
        return  // ← 这里就停了
    }
    runTools(resp)
}
```


**只要 LLM 决定不调工具，仅仅返回文本时，Loop 就退出。**  
这个机制本身没错——它防止 Agent 陷入死循环、占着控制权不还。  
但它也让"自主推进长任务"这件事变得不可能。  


## 二、/goal 的核心思路


/goal 的解法非常直接——**在 Loop 退出之前再加一个判断：用户的目标达成了吗？**  


判断本身分两段做。先扫一眼内存 todo 和磁盘 plan 里还有没有未完成项——只要还有，直接判定为没干完；只有结构化任务都已清零，才舍得调一次小模型读对话历史做语义判断。**结构化校验 + 自然语言评估**串联起来，才算真的完工。  


把轻的检查放前面是有道理的——查内存切片和扫磁盘目录都是几微秒的事，调 LLM 评估器要花几百毫秒外加 token 钱。  

**用便宜的判断挡掉大多数情况，把昂贵的判断留给最后一公里**。  


![截图](https://res2026.tiankonguse.com/images/2026/06/02/003.png)


```mermaid
flowchart LR
    A["LLM 回复"] --> B{"有 tool_use?"}
    B -->|"是"| C["执行工具"]
    C --> A
    B -->|"否"| D{"有 active goal?"}
    D -->|"无"| E["Loop 结束"]
    D -->|"有"| I{"todo / plan\n全部 completed?"}
    I -->|"还有未完成"| H["合成 reminder\n继续 Loop"]
    I -->|"全部完成"| F{"评估器：达成了?"}
    F -->|"未达成"| H
    F -->|"达成"| G["清除 goal\nLoop 结束"]
    H --> A

    style B fill:#0d2137,color:#58a6ff
    style D fill:#162118,color:#3fb950
    style I fill:#1c2128,color:#8b949e
    style F fill:#2d1b00,color:#e3b341
```


## 三、评估器：让小模型当裁判


真正有意思的是"判断目标是否达成"这一步。  


最直接的做法是让正在干活的那个模型自己判断："我做完了没？"——但这等于运动员自己当裁判。  
模型有强烈的"完成欲"，会倾向把"差不多"说成"完成了"，把"测试还差几个"模糊成"主要测试都过了"。  
让它自己评判自己的工作，可信度堪忧。  


evo-agent 的方案是**把评估这件事交给一个独立的小模型**。  


![截图](https://res2026.tiankonguse.com/images/2026/06/02/004.png)


```mermaid
graph LR
    A["主模型\n（写代码、跑测试）"] -->|"产出对话"| B["对话历史"]
    B --> C["评估器小模型\n（独立 LLM 调用）"]
    C -->|"yes/no + 原因"| D["Loop 控制"]

    style A fill:#0d2137,color:#58a6ff
    style C fill:#162118,color:#3fb950
    style D fill:#2d1b00,color:#e3b341
```


评估器的输入是两段话——目标本身、最近 N 轮的对话摘要。它的输出是 Yes/No，以及理由 reason。  



如果它说"达成了"，理由必须具体——"测试输出中显示 X passing, 0 failing"。  
如果说"没达成"，理由也要具体——"还有 3 个 unit test 失败"。  
这条理由会被用作下一轮 LLM 的指导，告诉它"差什么"。  


## 四、最后


从第八篇的内存 todo、第十四篇的磁盘 Session Plan，到这一篇的 /goal 完成条件——evo-agent 在三个不同尺度上回答了同一个问题："Agent 怎么自己往前走？"  


todo 解决"短任务里不迷路"——记住自己在做哪一步。  
Session Plan 解决"长任务跨会话推进"——记住整个项目的骨架。  
**/goal 解决"轮次之间不掉链子"——记住自己应该跑到哪里才算完。**  


三层机制叠在一起，Agent 才真正从"对话助手"长成了"自主代理"。  


```mermaid
graph TD
    A["Agent 的'自主性'"] --> B["单轮内\nTool Use"]
    A --> C["短任务内\nMemory Todo"]
    A --> D["大任务内\nSession Plan"]
    A --> E["跨轮次驱动\n/goal"]

    B --> F["回答一个具体问题"]
    C --> G["完成几分钟的小工作"]
    D --> H["跨小时/天推进项目"]
    E --> I["自己跑到目标达成"]

    style E fill:#2d1b00,color:#e3b341
    style I fill:#162118,color:#3fb950
```


《完》  


-EOF-  


本文公众号：天空的代码世界  
个人微信号：tiankonguse  
公众号 ID：tiankonguse-code  
