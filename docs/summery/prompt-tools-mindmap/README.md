# Jarvis 提示词与工具脑图

这张图用于文章「提示词+工具，而不是代码」一节，表达两个层次：

- 提示词负责理解目标、判断边界、选择工具和决定何时停止。
- 工具层只提供稳定能力与机器硬约束；Jarvis 当前稳定入口是 `jarvis-tools`、`lark-cli`、`bytedcli` 和 `git`。

工具清单核对自：

- `internal/toolcatalog/catalog.go`
- `./scripts/jarvis-tools --help`
- `lark-cli skills list`
- `bytedcli --json --all-help`

交付物：

- `jarvis-prompt-tools.mmd`：插入飞书文档的详细 Mermaid 思维脑图。
- `jarvis-prompt-tools.drawio`：FlowForge 生成的可编辑工程图。
