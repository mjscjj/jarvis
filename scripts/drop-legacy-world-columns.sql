-- 实体长期事实 summary 单页化：删除废弃列与 relation_fact 表。
-- 见 docs/design-entity-summary.md §4.2 / §4.3 / §4.4。
--
-- 执行前提：服务已停止，且数据库已备份。
-- 内容不迁移（用户已拍板），summary 从空开始由 factengine 重新积累。
-- summary 与 last_progress_at 两列由服务启动时的 AutoMigrate 负责补齐，此处不建。

PRAGMA foreign_keys = OFF;

ALTER TABLE project DROP COLUMN description;
ALTER TABLE project DROP COLUMN repos;
ALTER TABLE project DROP COLUMN tech_stack;
ALTER TABLE project DROP COLUMN key_decisions;
ALTER TABLE project DROP COLUMN timeline;
ALTER TABLE project DROP COLUMN notes;

ALTER TABLE person DROP COLUMN relation;
ALTER TABLE person DROP COLUMN comm_style;
ALTER TABLE person DROP COLUMN notes;

ALTER TABLE managed_resource DROP COLUMN description;

ALTER TABLE principal_profile DROP COLUMN background;
ALTER TABLE principal_profile DROP COLUMN preferences;

-- feishu_group.description 保留：它是 capture 同步的飞书群公告，属外部事实。
ALTER TABLE feishu_group DROP COLUMN background_note;

DROP TABLE relation_fact;

PRAGMA foreign_keys = ON;
