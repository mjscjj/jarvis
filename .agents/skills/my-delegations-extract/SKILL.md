---
name: my-delegations-extract
description: 在 M3 识别 principal 交给他人的待办，保存完整来源与背景；已有待办的新证据只产生关联核验线索。
---

# 我的交办：先产出待办

识别 principal 明确交给其他人的具体交付物。讨论、建议、泛化要求和纯粹提到某人不算交办。
负责人只能确认姓名时保留姓名，不猜 open_id；没有期限不编造期限。

首次发现独立交办时输出 `action_type=delegated_followup` 的 candidate。它落盘后的 Todo
就是交办主体，ID 长期不变，“我的交办”在 M5 运行前即可展示它。记录交办不因主动程度减少；是否现在核验服从当前 M3 工作规则及明确看护目标。普通档、活跃档尚需核验的写 extracted；安静档没有当前核验必要时先写 observing；
是否完成属于交办进展，不能用 Todo 的 extracted/materialized/observing 表示。
title 描述原始交办，target 明确为“核验这条交办的当前进展并写回待办”；payload 记录准入判断；annotation 不生成简报或执行建议，原始交办对话和关联用于 M5 核验。

## 去重与后续证据

用 `list-delegations --state all --query ...` 做围绕负责人、交付物或来源的短查询，必要时
读取匹配项和关联 Task。此查询只服务“新交办还是已有交办”，不要展开执行调查。

- 相同交付物的改派、延期和进展，仍属于原待办，不因负责人或表述变化创建第二条。
- 已有交办出现值得核验的新证据，且没有等价检查正在执行或等待时，输出普通 investigate
  candidate，在宽松 `annotation.delegation_id` 中记录原 Todo 的数字 ID。该 candidate
  是一次检查线索，不再使用 delegated_followup；payload 交代新证据和为何现在检查。
  target 保留现有去重身份；annotation.delegation_id 让 M5 能找到原记录，不证明业务判断正确。
- 重复信息、不需要检查的信息不创建新 candidate。不要因为已有一个完成的检查 Task，
  就认定对方已经交付；也不要因为一个检查失败就判断交办失败。
- 不同独立交付物拆开；共同交付同一结果可以保留多个负责人。

## 来源和背景

payload 完整保存当时的理解：谁交给谁什么事情、为什么要做、前序决定、依赖和约束、
怎样算交付、明确期限及不确定性。这些理解属于 M3 审计，annotation 只补充有出处的关联，
不要求重复一份完整背景，也没有必填的负责人业务 schema。

source_message_ids 必须包含原话以及理解“这个/上面的方案”所需的消息。mentions 中的
稳定身份原样利用。程序冻结 source、capture 和 annotation，不用摘要替代原文，
也不为补齐模板在 M3 展开深入调查。
