# 世界模型工作稿

`run_dir/world-model.md` 是 Agent 的调查工作稿，不是 runtime schema、审批合同或自动批量导入文件。它帮助用户和后续 Agent 看清“为什么写入这些对象、哪些没有写”。内容可以随证据和用户纠正继续编辑。

建议包含以下部分，但可以根据实际证据增删：

1. 证据窗口、实际覆盖范围和定向扩展理由。
2. Principal 身份、职责、直属上级和 Git author；高级通讯录字段不可见时写明证据来源、非阻塞边界和当前未知项。
3. 项目、关键人物、权威资料、重点事项和拟监听群。
4. 实体间重要关系和有真实时间的基线事实。
5. 证据冲突、权限缺口、低置信候选和明确未建模的内容。
6. 已写入并读回的真实对象 ID，以及尚未执行的下一步。

每个候选尽量写自然语言理由、证据文件定位和 high/medium/low 置信度。置信度只帮助 Agent 决定调查与询问深度，不是机器状态枚举。

## 何时直接应用

- fresh 实例、高置信、业务键唯一且不会覆盖用户既有数据：直接通过现有 `jarvis-tools` 写入并读回。
- 低置信但不影响其它对象的线索：保留在未知项，不为了填满世界模型写入。
- 项目归属、人物身份、群监听范围等高影响歧义：展示已有证据，请用户决定后继续。
- 发现存量对象，需要合并、覆盖、删除或大范围重建：停止并取得用户策略。

不生成 `approved-draft.json`、`approval.json` 或 hash 审批状态。用户可以随时纠正工作稿；真正的业务真源始终是 M1/M2 及其事实记录。

## 应用与恢复

按 `Principal → Project → Person → KeyMatter/ManagedResource → Group → RelationFact → baseline Fact` 的依赖顺序逐项处理。每次写入后立即使用对应 get/list/query 命令读回，并把真实 ID 和结果写到工作稿；属于整体安装时同步更新 `INSTALL_CHECKLIST.md` 的世界模型 E 区。

中断恢复时先按业务键查询当前世界模型：Person 用同 App 的 open_id，Group 用 chat_id，资料用规范化 URL/token，Project 用明确 repo/code/稳定名称，Fact 用主体、发生时间、完整事件和 `source_kind=initialization`。确认不存在才创建；有歧义就停止，不靠事务、回滚或重复追加解决。
