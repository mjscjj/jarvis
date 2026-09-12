# 插件扩展与解耦

> Status: proposal
> Authority: non-normative
> Last reviewed: 2026-09-11

当前插件行为见 [插件系统](../modules/07-plugins.md)。本提案只记录尚未实现的解耦方向。

## 当前耦合

- Manifest 由 Go 代码内置注册；
- 授权 provider 与页面表单需要手工接线；
- 插件导航和详情仍包含内置展示逻辑；
- collector 的 schedule 由通用 ScheduledTask 承载，但配置表单不是声明式。

## 候选改造

1. 从仓库内固定目录加载声明文件，启动时严格校验；
2. 用有限字段类型描述插件设置页，不允许声明任意命令；
3. 授权适配器按 provider 注册，Manifest 只引用稳定 provider key；
4. 导航、状态和运行摘要由通用插件投影生成；
5. collector 继续使用 `ScheduledTask + Skill + /api/clues`，capability 继续只贡献阶段 Skill。

## 不变边界

- 插件不拥有第二套 M2→M3→M5；
- 外部语义保留宽松 JSON，不为每个来源建 Go DTO；
- 启停不是权限边界，不删除历史 Todo、Task 和执行记录；
- Manifest 不允许执行任意 shell；
- 只有真实新增插件成本证明当前注册方式不可接受时才实施。
