# Todo / Task 的渐进式上下文

> Status: current
> Authority: normative
> Last verified: 2026-09-05

本页描述当前实现。阶段职责见 [整体架构](00-overview.md)。

## 所有权

Todo 是 M3 产生和更新的高噪声线索；Task 是经过准入、需要付出成本执行的任务。两者的状态、版本和职责不合并。

- M3 判断准入，提供已验证的来源消息 ID、准入简报和开放说明，不整理材料引用图，不誊写消息正文。
- 捕获端将创建时原文和背景冻结为 `capture`。`contextsnap` 只用于捕获端的局部投影，不作为全链路共享 DTO。
- `contextpack` 保存 `source + capture + annotation`，提供直接字段投影，不维护材料字典或 refs。
- 固化器直接复制 `Todo.content` 到 `Task.source_payload`，不重新装配背景。
- M5 自己决定需要读什么、如何判断重复、如何执行；代码负责状态、幂等、工具参数和留痕。

## 冻结存储

```json
{
  "source": {
    "source_message_ids": ["om_link", "om_request"],
    "source_quote": "仔细 review 这个",
    "payload": "完整准入说明，其他来源字段同样原样保留"
  },
  "capture": {
    "captured_at": "2026-09-05T12:00:00Z",
    "messages": [
      {"message_id": "om_link", "content": "原始 MR 链接"},
      {"message_id": "om_request", "content": "仔细 review 这个"},
      {"message_id": "om_context", "content": "其他周边消息"}
    ],
    "group": {"name": "来源群"},
    "project": {"summary": "创建时的项目事实"}
  },
  "annotation": {
    "brief": "需要 review 指定 MR",
    "scene": "提交者贴出 MR，principal 随后交办",
    "background": "可以进一步了解项目目标",
    "其他判断": {}
  }
}
```

`capture.messages` 包含本轮捕获的完整会话，包括已引用直接证据；正文只存一次。`source.source_message_ids` 选择直接证据，不新增内部材料 key。新建上下文的来源 ID 必须对应冻结消息，M3 还校验真实来源、本轮新消息和引用原句。Todo 的来源索引字段投影当前修订的来源 ID，早期修订由事件快照保留。

`annotation` 是宽松 JSON；brief、scene、background 只是推荐表达，不设语义枚举、必填嵌套结构或 refs。未知字段保留。模型说明不能覆盖 source/capture，不用于 reaction 或机器来源定位。

M3 两个模型入口共用控制 schema。`annotation` 用 JSON 字符串穿过 Structured Output，在 Candidate 解码时还原为开放对象。存储里的 `source` 保留完整 Candidate，包括其原始说明；`annotation` 是阅读用的开放说明。

手工、定时和主动 Task 使用相同外壳；`source` 保存完整原始请求，可为任意 JSON。没有来源消息 ID 时，原始请求就是默认直接证据，不强制伪造消息、群或项目。

## 默认输入和按需读取

M5 初始输入包含 Task 当前状态、整体进展、追加指示，以及：

- annotation.brief 和 scene；
- source_message_ids 对应的完整冻结消息；没有消息来源时直接给原始 source；
- 会话消息数、捕获时间、可读取区块名称；
- 本任务运行历史数量和最近状态。

不自动展开完整 Candidate、周边会话、实体背景页、全局 Todo/Task 列表或历史 run 正文。相关工作由 M5 查询，历史结果和 effects 按 run 读取，包括失败尝试。同一 Session 恢复只补充当前任务状态与新增指示。

工具：

- `get-task/get-todo --id ID`：默认概览。
- `--context conversation`：完整冻结消息数组。
- `--context background`：除消息外的冻结背景。
- `--context project|group|participants|resources|其他已列出的区块`：直接读取该捕获字段。
- `--context source|annotation|full`：原始来源、全部模型说明或完整包。
- `--message-id ID`：在当前冻结快照里按原生 ID 读取单条消息，不查询当前 message 表重建原文。
- `get-todo --revision N`：跨修订读取返回冲突。
- `list-tasks/list-todos --source-message-id ID`：根据真正来源 ID 查相关工作；关键词搜索覆盖来源语义和冻结消息正文，支持分页和项目等筛选。
- `list-task-runs` 分页读取概要；`get-task-run` 展开结果、错误和 effects，`--include-prompt` 才读取 prompt。
- `get-page/list-facts` 读取实体当前状态与历史事实，不能替代冻结证据。

reaction 仅从 source_message_ids 对应的冻结消息定位；模型说明和无关周边消息不参与目标选择。来源索引只帮助召回，是否重复仍由模型判断。

Web 列表只读名称和结果预览，打开详情再读取完整 Task/Todo；上下文面板直接展示冻结字段，不解释引用图。run 列表继续分页，正文按展开读取。

## 数据边界和验证

运行时只接受当前 `source + capture + annotation` 外壳，不保留旧上下文格式的迁移和读取分支。已有数据库若仍含旧格式，需要显式重建，不在服务启动时猜测或改写历史语义。

验证覆盖：模型输出与 schema 一致、未知字段和精确数值不丢、消息正文只存一次、直接证据默认可见而背景按需读取、原生消息 ID 查询、正文搜索、annotation 不影响 reaction、Todo→Task 完整复制，以及 UI 详情和 run 分页。
