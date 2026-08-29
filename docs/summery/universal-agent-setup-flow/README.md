# 通用 Agent 设置流程

用于飞书文章末尾的总结图：

`消息 / 事件 → 快脑 → 慢脑 → 执行`

图中将领域差异单独画成两份配置：

- 快脑加载领域准入与过滤规则。
- 慢脑加载领域调查与决策规则。
- 执行层复用通用工具，不按业务领域重写执行链路。

文件：

- `universal-agent-setup-flow.mmd`：飞书画板使用的 Mermaid 源文件。
- `universal-agent-setup-flow.drawio`：FlowForge 可编辑源文件。
- `insert.xml`：追加到飞书文档末尾的 XML 内容。
