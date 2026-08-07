# `draft.json` 契约

使用一个稳定小外壳承载完整自然语言，不复制数据库 schema，也不让程序自动批量 apply。

```json
{
  "version": 1,
  "run_id": "20260807-153000",
  "profile": "cli_example",
  "evidence_window": {
    "timezone": "Asia/Shanghai",
    "from": "2026-08-01T00:00:00+08:00",
    "until": "2026-08-08T00:00:00+08:00"
  },
  "items": [
    {
      "ref": "principal",
      "target_type": "principal",
      "operation": "upsert",
      "payload": {"name": "示例用户"},
      "rationale": "通讯录与登录身份一致",
      "evidence_refs": ["evidence/auth-status.json#identities.user"],
      "confidence": "high"
    }
  ],
  "unknowns": [],
  "warnings": []
}
```

## 外壳字段

- `version` 固定为 `1`。
- `ref` 在本草案内唯一，供应用日志和逻辑引用使用。
- `target_type` 只用于选择现有入口：`principal`、`project`、`person`、`key_matter`、`resource`、`group`。
- `operation` 首次初始化只使用 `upsert`（principal）或 `create`/`update`（其它对象）。
- `payload` 是对应现有 `jarvis-tools` 命令接受的完整 JSON；不要增加 onboarding 专用字段。
- `rationale`、`evidence_refs`、`confidence` 是审阅信息，不传给业务 API。
- `confidence` 使用 `high`、`medium`、`low`。这是草案展示语义，不是 runtime 状态枚举。

## 逻辑引用

草案可用 `project_ref`、`person_ref` 表达尚未生成的关联 ID，但调用业务 API 前必须删除逻辑字段并替换为前一步返回的真实 `project_id`/`person_id`。不要把 `$ref` 原样发给 API。

## 业务 payload 要点

- `principal`：`name` 必填；可含 `department`、`title`、`background`、`preferences`、`leader_open_id`、`leader_name`。open_id 不在 payload 中。
- `project`：`name`、`role`、`status`、`priority` 必填；role 为 `owner|participant`，status 为 `planning|active|paused|archived|done`，priority 为 1 到 5；其余语义 JSON 保持宽松。
- `person`：`open_id`、`name`、`role`、`priority_weight` 必填；role 为 `leader|key|colleague|other`，weight 为 0 到 1。
- `key_matter`：`title` 必填；可含自然语言 `status`、`summary`、解析后的 `project_id`、`due_at`。
- `resource`：`title`、`resource_type` 必填；type 为 `doc|link|repo|note|other`，可关联解析后的 person/project。
- `group`：必须先从 `get-group` 得到真实数据库 `id`；payload 只含 `background_note`、`project_id`、`related_group`、`pinned`、`include_in_memory`、`is_key_group`。

## 应用顺序

`principal -> projects -> persons -> key_matters/resources -> groups`。每项成功后立刻读回并写 `applied.ndjson`，这样失败后可从第一个未完成 ref 继续，不需要事务或回滚。
