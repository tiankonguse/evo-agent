# `read_file` 工具——参考 Claude FileReadTool 的重构

> 路径：`src/internal/tools/read_file.go`
> 测试：`src/internal/tools/read_file_test.go`
> 参考实现：`refs/FileReadTool/{FileReadTool,limits,prompt,UI,imageProcessor}.ts`

## 背景

旧版 `read_file` 只暴露 `path` + `limit` 两个字段，输出按字节裁断，缺少行号、缺少越界保护、缺少幂等去重——和 Claude Code 官方 `FileReadTool` 在生产里沉淀下来的行为差距较大。

这次重构对齐了 Claude FileReadTool 的核心契约，同时去掉对 evo-agent 当前架构没意义的部分（image / PDF / notebook、增长ent flag、analytics 等）。

## 输入 schema

```jsonc
{
  "file_path": "/abs/path/to/file.go",  // 必填；相对路径会按 cwd 解析
  "offset":    1,                        // 1-indexed 起始行；默认 1（从头读）
  "limit":     2000                      // 最多行数；默认 2000
}
```

> 字段从 `path` → `file_path` 是因为官方 schema 用 `file_path`，
> 改名让 prompt 工程跨工具风格统一。`loop.go` 在解析 `tool_use.input` 时
> 仍然兼容旧 transcript 中的 `path` 字段，保证 `--resume` 不会失败。

## 输出格式

`cat -n` 风格，每行 `%6d\t<line>\n`：

```
     1	package tools
     2	
     3	import (
     4		"fmt"
```

行内容超过 2000 字符会按 rune 截断并补 `…`，避免长行单独把整次工具结果撑爆。

## 行为

| 场景 | 结果 |
| --- | --- |
| 文件不存在 | `read_file: File does not exist. Current working directory: …. Did you mean <similar>?`（同前缀兄弟文件） |
| 是目录 | `read_file: "<path>" is a directory — use the bash tool with `ls` instead` |
| 二进制扩展名（zip/exe/so/png/pdf/woff…） | 直接拒绝，不读文件内容 |
| 阻塞设备（`/dev/zero`、`/dev/random`、`/dev/stdin`、`/proc/<pid>/fd/0` …） | 直接拒绝 |
| 空文件 | `<system-reminder>Warning: the file exists but the contents are empty.</system-reminder>` |
| `offset` 超过文件总行数 | `<system-reminder>Warning: the file exists but is shorter than the provided offset (N). The file has M lines.</system-reminder>` |
| 同 `(file_path, offset, limit)` 再次读取，且 mtime 未变 | 返回 `File unchanged since last read. …` 占位字符串，不重发文件字节 |
| Windows CRLF | 去掉每行末尾的 `\r` 再渲染 |

## 去重和失效

```
read_file → 写入 readState[abs] = {mtime, offset, limit}
edit_file / write_file → InvalidateReadState(abs) 移除条目
```

`edit_file` 和 `write_file` 改动文件后会主动清掉对应路径的去重缓存，
保证后续 `read_file` 一定看到最新内容，而不是命中陈旧的 "file unchanged" 占位。

去重在测试 `TestReadFile_DedupSameRangeUnchangedMTime`、
`TestReadFile_DedupInvalidatedAfterEdit`、`TestReadFile_DedupInvalidatedAfterWrite` 中分别验证。

## 资源上限

| 常量 | 值 | 含义 |
| --- | --- | --- |
| `defaultMaxLines` | 2000 | 未指定 `limit` 时的默认行数（与 Claude `MAX_LINES_TO_READ` 一致） |
| `maxFileSizeBytes` | 256 KB | 未指定 `limit` 时的字节上限（避免误读巨文件） |
| `readFileMaxOutputBytes` | 50 000 | 渲染后字符串的硬上限——超过部分追加 `... (output truncated at byte cap)` |
| `truncatedLineLen` | 2000 | 单行最大渲染字符（按 rune 算） |

下游的 `PersistLargeOutput`（`persist.go`）会在结果整体仍然过大时把内容落到
`.evo-agent/tool-results/<id>.txt`，并把 2 000 字符的预览给模型——这一层
对 `read_file` 透明。

## 仍未对齐的点（故意省略）

| 官方功能 | 是否实现 | 说明 |
| --- | --- | --- |
| 图像（png/jpg/gif/webp） | ❌ | evo-agent 当前 loop 是纯文本，未来引入 sharp/媒体管线时再加 |
| PDF（含 `pages` 参数和 page 提取） | ❌ | 同上，需要 poppler / pdf-image |
| Jupyter notebook（`.ipynb`） | ❌ | 后续可单独加 `read_notebook` |
| GrowthBook flag、analytics | ❌ | 项目无此基础设施 |
| `<previous-conversation-summary>` 风险标注 | ❌ | 已经由 prompt builder 在系统提示层处理 |

## 测试矩阵

`internal/tools/read_file_test.go` 共 14 个用例，覆盖：

- `cat -n` 基础格式
- `offset` + `limit` 窗口
- 空文件 / 越界提示
- 文件不存在的相似文件名建议
- 拒绝目录
- 拒绝二进制扩展
- 拒绝 `/dev/zero`（在该设备存在的环境下）
- 同范围 mtime 未变的去重命中
- `edit_file` / `write_file` 之后去重失效
- CRLF 行尾归一化
- 默认 2000 行硬上限
- 相对路径相对 cwd 解析

运行：

```bash
cd src && go test ./internal/tools -run TestReadFile -v
```
