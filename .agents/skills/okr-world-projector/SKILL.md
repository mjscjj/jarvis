---
name: okr-world-projector
description: 将已启用 OKR 模块里的 O、KR、Point 和核心指标按证据投影为 Jarvis 可解析节点与跨模块关系；不复制原生层级和负责人关系，不读取周报进展，不直接访问模块数据库，不按标题相似度硬绑，也不创建 Task。
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

按明确项目代号、已有关系和稳定来源引用逐步查询：

```bash
jarvis-tools list-projects --keyword '<明确项目代号或名称>' --limit 20
jarvis-tools list-key-matters --keyword '<明确事项名>' --limit 20
jarvis-tools list-relations --source-type okr_kr --source-id '<kr_id>' --limit 100
```

标题相似不能单独构成映射。缺少稳定证据时保留未确认，不创建关系。

## 建立通用关系

只把需要跨模块查询的强关系写入通用 `entity_relation`，来源使用模块自己的稳定 ID。优先把 Point 映射到现实 KeyMatter：

```bash
jarvis-tools create-relation --payload - <<'JSON'
{
  "source_type": "okr_point",
  "source_id": "<point_id>",
  "relation_type": "maps_to",
  "target_type": "key_matter",
  "target_id": "<key_matter_id>",
  "evidence": {"source":"okr-module","basis":"<直接证据>"},
  "confidence": 1,
  "confirmed_at": "<RFC3339>"
}
JSON
```

现实 KeyMatter 或 Project 对 KR 的明确贡献使用 `advances`；不要默认建立 KR `belongs_to` Project。关系词只从七组核心语义中选择：`belongs_to/contains`、`owned_by/owns`、`participates_in/has_participant`、`depends_on/required_by`、`advances/advanced_by`、`maps_to/mapped_from`、`derived_from/produces`，正反只存一条，由读取层生成反向显示。

OKR 内部的 Objective/KR/Metric/Point 层级和 Owner 已由模块真源表达，读取时直接派生，不能再复制进 EntityRelation。不要为了投影 Owner 创建 Person；只有世界模型本身确需维护该人物时，才按稳定 open_id 解析或创建。不要把模块 ID 写入 Project/KeyMatter 专用字段，也不修改 `okr_workspace_*` 数据；周报进展不属于本 Skill。

## 更新事实

本 Skill 只建立或复核实体关系，不根据周报写 Fact/Page。周进展的证据写回由 `weekly-report-progress-sync` 先读后写、写后回读；局部动作不能自动宣告上层目标完成。

## 完成标准

- 所有新关系都能用 `list-relations` 回读；
- 重跑相同关系只更新证据，不产生重复边；
- 原生 OKR 层级和 Owner 没有被复制到 EntityRelation；
- 每项映射说明直接证据或保留“未确认”；
- 没有直接访问模块表、没有直接发消息、没有创建 Task。
