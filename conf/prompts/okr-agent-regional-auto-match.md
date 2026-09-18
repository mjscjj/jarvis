# 区域需求与 Platform OKR 自动匹配

目标：基于 Task `source_payload.regional_alignment_snapshot` 中冻结的 Part 0 数据，重新判断每条区域需求与 Platform Plan KR 的实质承接关系，并一次性替换该区域全部 `plan_kr_ids`。Part 1 由这些关系与“是否承接 / 是否上车”状态实时投影，不生成第二份匹配数据。

## 事实范围

- 只使用冻结快照中的 `plan`、`demands`、`decisions` 和 `match_version`；它代表用户点击时的完整现场。
- `demands[].acceptance` 表示 Platform 是否承接区域需求；`decisions[].onboard` 表示当前区域是否上车 Platform KR。两者用于解释 Part 1 状态，不决定语义关系本身。
- `demands[].plan_kr_ids` 是待重算的旧关系，不是匹配证据。不得因为旧值存在而保留，也不得因为旧值为空而拒绝建立关系。
- 不调查或修改生产 Jarvis 的消息、普通会话、世界模型或私人资料，不读取冻结快照以外的业务材料。

## 判断标准

结合双方的业务结果、服务对象、范围、交付物、时间、负责人以及 KR 的完整拆解、指标和标签判断。标题相似不能单独建立关系；同一需求可以对应多个 KR，同一 KR 也可以承接多个需求。

- 高置信度：双方描述的是同一业务结果，或 Platform KR 的明确交付能直接满足该区域需求。写入正式关系。
- 中/低置信度：只有主题接近、范围不明、依赖间接或存在冲突。不要写入正式关系，在 Task 结果中列为人工复核候选并说明缺口。
- 没有实质对应时使用空数组。不得为了覆盖率强行匹配。

## 执行

1. 先读取 `okr_agent_principles` 和本 Prompt，核对 Task 中的季度、区域、全部 demand ID、全部 Plan KR ID 与 `match_version`。
2. 为每条当前需求产出且只产出一个完整结果项：`{"demand_id":"...","plan_kr_ids":["..."]}`。即使没有匹配也必须包含该需求并使用空数组。
3. 调用一次：

   `scripts/biz-okr-tools replace-regional-matches --quarter <季度> --region <区域代码> --payload -`

   stdin JSON 必须为：

   `{"expected_match_version":"<冻结版本>","items":[<覆盖全部需求的结果>]}`

4. 原子工具返回冲突时立即停止，不基于新现场自行重试；说明 Part 0 已变化，需要用户重新点击。工具返回其它错误时保留原始错误并停止，不直接写数据库或逐条调用需求编辑接口。

## 禁止修改

本动作只能替换需求的 `plan_kr_ids`。不得修改需求正文、优先级、负责人、`acceptance`、交付物、Platform KR、`onboard`、上车地区、区域 OKR、隐藏状态或其它字段。

## 完成回执

说明 Prompt key、季度和区域、冻结 `match_version`、需求与 KR 数量、原子工具回执中的变化需求数 / 新增关系数 / 移除关系数 / 未变化需求数；逐条列出最终高置信度关系的主要依据，并单列未匹配需求、中低置信度候选和覆盖缺口。不得用“Task 成功”代替写入回执。
