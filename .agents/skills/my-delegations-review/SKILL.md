---
name: my-delegations-review
description: 主动巡视启用的我的交办，按真实证据与检查时机为已有待办创建一次核验 Task，不把 Task 状态当作交付状态。
---

# 我的交办：按需安排检查

本插件开启时，未结束交办是巡视的候选来源之一，仍服从本轮巡视的注意力预算和停止条件。
用 list-delegations 读取未结束项，按页查看；需要时 get-delegation 读当前理解与原始来源。
没有检查记录表示待核验，不表示待办不存在或对方未做。

结合明确期限、已有进展、下一检查时机和新证据判断现在是否需要 M5 核验，不对每条交办
每轮都建 Task，不用固定天数代替判断。创建前用 list-delegation-tasks 查所有相关状态：
已有等价 pending/executing/waiting/needs_human 检查就复用，不重复派单；检查 done 只说明
查过一次，对方是否交付看待办 content 和证据。

确实需要检查时，用普通 create-task 创建目标明确的一次核验 Task：
- source_payload 中携带数字 delegation_id、why_now，以及原样复制 get-delegation 返回的
  source_payload 到 original_context，当前 content 到 current_progress；不让模型重写背景。
- action_type 使用 investigate，目标明确“本次核验并回写”，不是“跟进到对方完成”。
- 交办 ID 是原 Todo ID。Task 的 source.delegation_id 用于回查关联检查历史。

正文搬运使用工具 JSON 原样组装。Task 创建结果读回确认；不要在本轮继续检查刚派出的工作。
本插件只安排核验，不主动创建催办任务，不以 Task 状态代替证据关闭交办。

插件关闭后不再注入本规则、停止安排新检查；已有待办和历史保留，已派出的单次工作仍按
其目标收口。重新开启继续读取原待办，不从头建账。
