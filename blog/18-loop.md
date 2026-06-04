---
layout: post  
title: 让 Agent 定个闹钟：定时任务  
description: 要让 Agent 在指定时间或者按周期自动醒过来干活，还得给它一个会看时钟的调度器。  
keywords: agent,cron,scheduler,timer,golang  
tags: [agent, cron, scheduler, timer, golang]  
categories: [程序人生]  
updateDate: 2026-06-04 21:00:00  
published: true  
source: "https://mp.weixin.qq.com/s/wpBoRmGp3Rz_qfhVwJqZlQ"
---




## 零、背景


前十七篇文章分别讲了 Agent 的 [Loop](https://mp.weixin.qq.com/s/dkdrwVlwe3IkH2hzSzy53A)、[工具](https://mp.weixin.qq.com/s/xyX4_CF5cveezEDuzFT13g)、[上下文记忆](https://mp.weixin.qq.com/s/lguRAdxFoN22rqPyx3BIzw)、[上下文压缩](https://mp.weixin.qq.com/s/YRS29wRckEmFgNb0eJrxrQ)、[MCP](https://mp.weixin.qq.com/s/rCnGif8Ee7JhRI86-RoNWA)、[Skill](https://mp.weixin.qq.com/s/X2ie0aQ2vMtddAQrkbOG5g)、[TUI](https://mp.weixin.qq.com/s/fBNFZvOOpwCPT7yysh5YkQ)、[任务规划](https://mp.weixin.qq.com/s/UIlEXIuQdacowdrIg1nrDQ)、[子代理](https://mp.weixin.qq.com/s/LfgDcv27vjlmLZ9NfvQ9LA)、[命令](https://mp.weixin.qq.com/s/M1jxdA4BysQkaN7p4hwneQ)、[跨会话记忆](https://mp.weixin.qq.com/s/wEQwMadb84ixfVXteNfESA)、[Agent.md](https://mp.weixin.qq.com/s/82KmXRTsiDrhB-RZFg5sXw)、[系统提示词](https://mp.weixin.qq.com/s/15mxhcDs1oWBwguF_IIZDg)、[任务持久化](https://mp.weixin.qq.com/s/86urMkNycEkI38KCoS0mxg)、[会话持久化](https://mp.weixin.qq.com/s/zyVNi0JXBlbO-z3KtZEFcA)、[goal 命令](https://mp.weixin.qq.com/s/DfDFsIhLZJp1NiXz9dp7ug) 和 [后台任务](https://mp.weixin.qq.com/s/1fII8BYVinsUuOBnE7lMmA)。  


这一篇聊一个让 Agent「自己定闹钟」的小机制——**定时任务（Scheduled Tasks / Cron）**。  


![截图](https://res2026.tiankonguse.com/images/2026/06/04/001.png)


## 一、后台任务解决不了的问题


上一篇讲后台任务的时候，已经把「跑得慢的命令不要阻塞主循环」这件事解决了。  
但 Agent 还有一类需求，后台任务也帮不上忙。  


比如说「五分钟之后再帮我看一眼 CI 跑得怎么样」、「每小时把生产日志拉一份摘要」、「周一早上九点提醒我交周报」。  


这类需求的共同特点是——**触发时机不在「现在」，而在「未来某个时间点」**。  


后台任务的语义是「我现在就让你跑，你跑你的，我干我的」。  
它解决的是并发，不是延迟。  
你要它五分钟后再启动，它做不到——它一被调用，goroutine 就立刻起来了。  



![截图](https://res2026.tiankonguse.com/images/2026/06/04/002.png)


```mermaid
sequenceDiagram
    participant User
    participant Agent
    participant Bg as 后台任务
    participant Time as 时间

    User->>Agent: 五分钟后看一下 CI
    Agent->>Bg: bg_run("等五分钟然后看 CI")？
    Note over Bg: 后台任务不接受「延迟启动」
    Note over Agent: 用 sleep 300 阻塞当前轮？<br/>那就回到了同步 bash 的老路
    Note over Time: 5 分钟过去
    Note over Agent: 用户没输入，Agent 也不会自己醒
```


## 二、定时任务的核心：cron 表达式 + 后台 ticker


解法看起来也不复杂。  
**给 Agent 加一个会看时钟的后台调度器**，并且把「调度任务」变成一个工具，让模型可以在合适的时机自己创建。  


evo-agent 这里直接复用了运维工程师都熟悉的 cron 表达式——五个字段：分、时、日、月、星期。  
为什么是 cron 而不是发明一个新格式？  
因为 cron 是几十年沉淀下来的、跨工具通用的、模型在训练语料里见过几百万次的格式。  
**模型熟悉的 DSL，就是最不容易出错的 DSL**。  


工具长这样：`cron_create` 创建一个定时任务，`cron_list` 列出所有任务，`cron_delete` 取消一个任务。  
另外还有一个后台 goroutine，每秒醒一次，拿当前时间去和所有任务的 cron 表达式做匹配。  


![截图](https://res2026.tiankonguse.com/images/2026/06/04/003.png)


```mermaid
sequenceDiagram
    participant Agent
    participant CronCreate as cron_create
    participant Sched as CronScheduler
    participant Ticker as 后台 ticker

    Agent->>CronCreate: cron_create("*/5 * * * *", "看 CI")
    CronCreate->>Sched: 解析 + 加进 tasks 表
    CronCreate-->>Agent: 立即返回 task id
    Agent->>Agent: 继续做别的事

    loop 每秒一次
        Ticker->>Sched: tickAt(now)
        Sched->>Sched: 遍历 tasks，匹配 cron
    end

    Note over Sched: 命中匹配
    Sched->>Sched: prompt 入 notifQ
```


## 三、把结果送回 Agent：和后台任务共用一条管道


调度器醒了、命中了，下一个问题就是——**怎么把这个事件送回 Agent？**  


这个问题在上一篇讲后台任务的时候已经回答过一次了。  
Agent 的 Loop 每一轮都会调一次 LLM，最自然的注入点就在每一轮的开头。  


定时任务直接复用了这套管道。  
任务命中以后，把它的 prompt 包成一条通知放进 `notifQ`。  
Agent Loop 在每轮开头先从队列中取消息，所有的消息包装成作为一条合成的 user 消息塞进 `messages` 里。  


![截图](https://res2026.tiankonguse.com/images/2026/06/04/004.png)


```mermaid
flowchart LR
    A["ticker 命中<br/>matchCron(f, now)"] --> B["写入 notifQ"]
    B -.->|"等下一轮"| C["Agent Loop<br/>开始新一轮"]
    C --> D["DrainNotifications"]
    D --> E["FormatCronNotifications<br/>合成 scheduled-task 消息"]
    E --> F["LLM 看到 prompt<br/>当作新的用户请求"]
```


这里有一个非常重要的设计细节——**调度器只负责「在合适的时机唤醒任务」，不负责「立刻执行」**。  
任务唤醒后，prompt 排进队列，等 Agent 下一次进入 Loop 时才会处理。  


如果用户正在打字，Agent 的 Loop 没有跑，那么任务就会在队列里等着。  
直到下一次 Agent 被唤醒（用户输入、其他事件触发），才会一并被处理。  
 

## 四、持久化：要不要跨进程活下来


定时任务比后台任务多了一个考虑：**要不要在进程重启之后继续生效？**  


比如「每天九点跑一次报表」这种需求，肯定要跨进程持久化的——不然你今晚关了 Agent，明天就不会自己醒了。  
但「五分钟之后提醒我看 CI」这种就完全没必要落盘——会话退出了，提醒也就跟着没了，符合直觉。  


evo-agent 把这个选择权交给了模型，给 `cron_create` 加了一个参数。  
默认只活在内存里，进程退出就消失。  
当用户明确说「以后每天都」、「永久生效」、「设置一个长期任务」的时候，模型才会把这条任务写到磁盘。  


落盘的位置就在会话目录下面，跟其他持久化数据放一起。  


![截图](https://res2026.tiankonguse.com/images/2026/06/04/005.png)


```mermaid
graph LR
    A["cron_create 调用"] --> B{"durable?"}
    B -->|"false"| C["仅内存 map<br/>tasks[id] = task"]
    B -->|"true"| D["内存 + 磁盘<br/>tasks.json"]

    E["进程重启<br/>--resume"] --> F["loadDurable<br/>读 tasks.json"]
    F --> G["仅恢复 durable=true<br/>session-only 任务永久消失"]

    style C fill:#1c2128,color:#8b949e
    style D fill:#162118,color:#3fb950
```


## 五、一次性 vs 周期性：给「跑一次」和「跑一辈子」分别上保险


cron 表达式天然就支持周期性触发——`*/5 * * * *` 就是每五分钟一次。  
但用户的需求里有一大类是「就跑一次」——「明天早上九点提醒我」、「半小时后看一下进度」。  


这类需求其实也能用 cron 来表达。  
「明天早上九点」可以写成 `0 9 <明天的 dom> <明天的 month> *`——把「日」和「月」字段都钉死，这条 cron 就只在那一天那一刻匹配一次。  


问题是，如果不做特殊处理，明年同月同日的同一时刻，这条 cron 还会再匹配一次。  


evo-agent 给一次性任务多加了两道保险。  
第一道是不可重复标记 `durable` ——任务唤醒之后立刻从表里删除，下次就匹配不到了。  
第二道是下次运行时间 `FireBy` 字段——创建任务的时候计算出第一次匹配的时间戳，写在任务上。  
后台 ticker 每次扫的时候会先看 `now > FireBy + 2 分钟` 没有，**如果错过了窗口，就直接删除而不触发**。  


另外，周期性任务也有一道保险——**自动过期 7 天**。  
你设了一个「每五分钟」的任务，七天后它会最后触发一次然后被删掉。  



为什么要这个限制？  
因为模型很容易在帮用户解决一个临时问题的时候顺手挂一个长期 cron，但用户可能根本不想要它跑一辈子。  
七天的上限相当于一个保险丝，跑得久了自动断开，避免幽灵任务在系统里默默运行。  


![截图](https://res2026.tiankonguse.com/images/2026/06/04/006.png)


```mermaid
graph TD
    A["task 每秒被检查"] --> B{"recurring?"}
    B -->|"true"| C{"创建距今 > 7 天?"}
    C -->|"是"| D1["删除任务<br/>不再触发"]
    C -->|"否"| E{"匹配当前时间?"}
    E -->|"是"| F1["入 notifQ<br/>保留 task"]
    E -->|"否"| G["跳过"]

    B -->|"false"| H{"now > FireBy+2min?"}
    H -->|"是"| D2["错过窗口<br/>直接删除"]
    H -->|"否"| I{"匹配当前时间?"}
    I -->|"是"| F2["入 notifQ<br/>立即删除任务"]
    I -->|"否"| G

    style D1 fill:#2d1b00,color:#e3b341
    style D2 fill:#2d1b00,color:#e3b341
    style F1 fill:#162118,color:#3fb950
    style F2 fill:#162118,color:#3fb950
```


## 六、再封装一层：/loop 命令


`cron_create` 已经够用了，但有时我们想主动增加一个定时任务。  
**这时候就需要 `/loop` 命令了**。  


`/loop` 是 evo-agent 的一个内置 builtin command，专门解决「周期性跑一段命令」这一种场景。  
用法非常直接，比如 `/loop 5m /git-commit`、`/loop 30m 看一下部署状态`、`/loop check the deploy every 20m`，都能一句话搞定。  


如果只写 `/loop check the deploy`，不带间隔，默认就按每 10 分钟一次跑。  


![截图](https://res2026.tiankonguse.com/images/2026/06/04/007.png)


```mermaid
flowchart LR
    A["用户输入<br/>/loop 5m /git-commit"] --> B["builtin command<br/>解析参数"]
    B --> C["间隔 5m → */5 * * * *"]
    B --> D["prompt = /git-commit"]
    C --> E["cron_create"]
    D --> E
    E --> F["返回任务 id<br/>立即执行一次"]

    style A fill:#0d2137,color:#58a6ff
    style E fill:#162118,color:#3fb950
```


## 七、最后


从第二篇的同步 bash，到第九篇的子代理，再到第十七篇的后台任务，再到这一篇的定时任务——evo-agent 在「让 Agent 一次干更多事」这条路上又走了一步。  


bash 解决「现在动手」——Agent 现在就要执行一条命令。  
子代理解决「分身探索」——Agent 派一个独立上下文去探索复杂问题。  
后台任务解决「时间不浪费」——长命令不阻塞主循环。  
**定时任务解决「自动醒来」——Agent 不需要用户输入也能在合适的时机被唤醒**。  


![截图](https://res2026.tiankonguse.com/images/2026/06/04/007.png)


```mermaid
graph TD
    A["Agent 时间维度上的能力"] --> B["现在<br/>bash"]
    A --> C1["独立分身\nsubagent"]
    A --> C["现在并发<br/>bg_run"]
    A --> D["未来定时<br/>cron_create"]

    B --> E["秒级同步执行"]
    C1 --> F1["复杂探索隔离上下文"]
    C --> F["分钟级异步执行"]
    D --> G["按时钟自主醒来"]

    style B fill:#1c2128,color:#8b949e
    style C fill:#0d2137,color:#58a6ff
    style C1 fill:#8d3637,color:#83a9f7
    style D fill:#162118,color:#3fb950
```


有意思的是，定时任务这套机制里几乎没有什么「AI」的成分。  
cron 表达式是七十年代就有的格式，goroutine + ticker 是标准的并发原语，事件入队 + 主循环统一处理是几十年沉淀的消息驱动套路。  


**Agent 工程师做的事，其实是把这些经过几十年沉淀的系统编程套路，重新组合成一个适合 LLM 推理节奏的运行环境。**  
模型本身不会变得更聪明，但围绕它的 harness，可以让一个聪明的模型做更多的事。  


时钟、定时器、事件队列——这些工程师从大学就开始用的工具，被重新包装成 Agent 可以用自然语言调用的能力，于是 Agent 就拥有了「时间感」。  
这种「把老工具适配到新主体」的工程能力，可能比模型本身的进化更值得关注。  


《完》  


-EOF-  


本文公众号：天空的代码世界  
个人微信号：tiankonguse  
公众号 ID：tiankonguse-code  
