---
name: okr-world-projector
description: 将已启用 OKR 模块里的 O、KR、负责人和核心指标按证据投影为 Jarvis 通用实体关系；不读取周报进展，不直接访问模块数据库，不按标题相似度硬绑，也不创建 Task。
module: okr
---

# OKR 世界投影

本 Skill 是 OKR 模块与 Jarvis 世界模型之间唯一的语义桥。模块保存产品真源，Jarvis 保存通用世界实体和关系；两边不共享数据库外键。

## 读取产品真源

先运行：

```bash
scripts/okr-module-tools scope
scripts/okr-module-tools board --quarter <quarter>
```

只使用返回的 O、KR、结构化负责人和核心指标。接口失败时停止，不直接查询 `okr_workspace_*` 表。周进展由 `weekly-report-progress-sync` 负责。

## 读取世界模型

按负责人 open_id、明确项目代号、已有关系和稳定来源引用逐步查询：

```bash
jarvis-tools list-persons --keyword '<open_id 或姓名>' --limit 20
jarvis-tools list-projects --keyword '<明确项目代号或名称>' --limit 20
jarvis-tools list-key-matters --keyword '<明确事项名>' --limit 20
jarvis-tools list-relations --source-type okr_kr --source-id '<kr_id>' --limit 100
```

标题相似不能单独构成映射。缺少稳定证据时保留未确认，不创建关系。

## 建立通用关系

关系由通用 `entity_relation` 工具持久化，来源使用模块自己的稳定 ID：

```bash
jarvis-tools create-relation --payload - <<'JSON'
{
  "source_type": "okr_kr",
  "source_id": "<kr_id>",
  "relation_type": "owned_by",
  "target_type": "person",
  "target_id": "<person_id>",
  "evidence": {"source":"okr-module","owner_open_id":"<open_id>"},
  "confidence": 1,
  "confirmed_at": "<RFC3339>"
}
JSON
```

KR 到项目使用 `delivered_by`。不把模块 ID 写入 Project/KeyMatter 专用字段，不修改 `okr_workspace_*` 数据；周报进展到关键事项的证据关系不属于本 Skill。

## 更新事实

本 Skill 只建立或复核实体关系，不根据周报写 Fact/Page。周进展的证据写回由 `weekly-report-progress-sync` 先读后写、写后回读；局部动作不能自动宣告上层目标完成。

## 完成标准

- 所有新关系都能用 `list-relations` 回读；
- 重跑相同关系只更新证据，不产生重复边；
- 每项映射说明直接证据或保留“未确认”；
- 没有直接访问模块表、没有直接发消息、没有创建 Task。
