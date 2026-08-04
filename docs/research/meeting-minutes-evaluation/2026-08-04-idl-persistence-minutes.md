# IDL 持久化与工具泛化调用稳定性改造技术方案 — 会议纪要

## 1. Metadata

- **Title**: IDL 持久化与工具泛化调用稳定性改造技术方案
- **Date (YYYY-MM-DD)**: 2026-08-04
- **Start Time (UTC)**: 2026-08-04T03:00:46Z（北京时间 11:00:46）
- **End Time (UTC) or Duration**: 2026-08-04T03:40:45Z（约 40 分钟）
- **Organizer**: 李倩影
- **Location / Virtual Link**: 飞书视频会议，会议号 `863 664 704`；[打开会议](https://applink.larkoffice.com/client/vctab/open?source=chat&action=detail&meetingId=7669996759813148276)
- **Minutes Author**: Codex，按 `github/awesome-copilot/meeting-minutes` 参考 Skill 从原始 Transcript 独立生成
- **Distribution List**: `TBD`；会议和 Transcript 未指定纪要分发范围

## 2. Attendance

- **Present**: 李倩影（主持人）、周文华、王帮宇、段宏达、李欣星、唐建科、储节节、陈浩轩、吴春霖、袁小轩、陈炜超。参会姓名来自 VC 参会快照与通讯录；个人角色未由会议接口提供，均隶属“国际直播-平台-公会”。王帮宇的参会记录仅约 41 秒。
- **Regrets / Absent**: `Unknown`；会议数据未提供受邀但缺席名单
- **Notetaker / Recorder**: Codex；原始材料为飞书妙记 Transcript

## 3. Agenda

以下议程由 Transcript 的实际讨论顺序归纳，不是会前议程原文：

- 当前泛化调用链路与稳定性问题
- IDL 快照库、三级读取链路和预加载方案
- MySQL 与 MongoDB 存储选型
- 刷新、缓存、超时、告警等稳定性治理
- feature branch 删除后的 fail-fast 与 fallback 边界
- IDL 刷新或版本冻结策略

## 4. Summary

会议认可为泛化调用增加持久化 IDL 快照的必要性，并确定当前阶段使用 MySQL，而不引入 MongoDB。评审没有直接通过原方案的全部语义：最大的待修正点是不能用旧快照掩盖已删除的 feature branch，非长期有效分支应及时失效并 fast-fail；持久化 fallback 主要用于 master/有效版本在 Overpass 抖动、限流或 IDL 无法重新编译时维持可用。李倩影需要补齐分支生命周期、刷新/版本策略、缓存失效和告警设计后再继续推进。

## 5. Decisions Made

- **Decision 1**: IDL 快照当前使用 MySQL 新表存储，不引入 MongoDB。
  - Who decided / approved: 储节节提出确认，唐建科和段宏达明确同意，会议无反对意见
  - Rationale: 当前访问模式是按 `PSM + branch` 精确查询、整存整取 IDL info，数据量小，不需要按内部字段查询或局部更新；MySQL 可复用现有连接、DAO、监控和运维体系。
  - Effective date: 2026-08-04

- **Decision 2**: 泛化调用需要持久化快照/存储，不能只依赖进程内存和 Overpass 实时可用性。
  - Who decided / approved: 唐建科、段宏达等评审人形成共识
  - Rationale: Overpass 本身不保证线上稳定性，上游 IDL 可能因依赖或编译问题暂时不可用，同时调用还受限流影响；已有可用 IDL 应能支撑 master/有效版本继续运行。
  - Effective date: 2026-08-04

- **Decision 3**: 已删除的 feature branch 不能继续依赖旧快照静默运行；确认分支不存在后应让对应调用 fast-fail，并使相关缓存/快照失效或标记不可用。
  - Who decided / approved: 唐建科提出明确评审要求，李倩影接受需要重新细化方案
  - Rationale: feature branch 合入 master 后可能已经发生接口变化，继续使用旧 IDL 会掩盖错误配置，并可能造成难以定位的参数或协议不一致。
  - Effective date: 修订后的设计生效时；具体失效机制尚未确定

- **Decision 4**: feature branch 原则上只应用于 PPE 等临时环境，真实生产环境不应发布依赖短期 branch 的工具版本。
  - Who decided / approved: 唐建科提出作为设计约束，会议未提出反对
  - Rationale: feature branch 会在合入后删除，不能作为生产环境的长期稳定依赖。
  - Effective date: `TBD`；需要在修订设计中明确由哪个发布环节保证

## 6. Action Items

- **[A1] Action**: 修订 feature branch 生命周期与失效流程
  - **Owner**: 李倩影
  - **Due**: `TBD`；会议未给截止时间
  - **Acceptance Criteria**: 设计明确生产/PPE 分支约束、分支不存在的检测点、fast-fail 行为、内存缓存与 DB 快照的删除或不可用状态，以及不会继续使用过期 branch IDL 的验证方法。
  - **Linked artifacts / tickets**: 《IDL 持久化与工具泛化调用稳定性改造方案》

- **[A2] Action**: 梳理并选择 IDL 更新策略
  - **Owner**: 李倩影
  - **Due**: `TBD`；会议未给截止时间
  - **Acceptance Criteria**: 对比“持续刷新最新 IDL”和“固定已验证/弱版本 IDL”，覆盖兼容性变更、工具新版本发布、PPE 合入 master、手动刷新与回滚语义，并给出最终选择。
  - **Linked artifacts / tickets**: 同上

- **[A3] Action**: 补齐刷新和多实例一致性设计
  - **Owner**: 李倩影
  - **Due**: `TBD`；会议未给截止时间
  - **Acceptance Criteria**: 明确定时刷新基准、最坏陈旧窗口、活跃 key 来源、历史 key 清理、多实例刷新错峰或统一时刻、手动刷新入口及相应指标和告警。
  - **Linked artifacts / tickets**: 同上

- **[A4] Action**: 把稳定性治理项纳入修订方案
  - **Owner**: 李倩影
  - **Due**: `TBD`；会议未给截止时间
  - **Acceptance Criteria**: 覆盖同 key 单飞刷新、分类失败缓存与退避、内存缓存容量/LRU、分阶段超时和日志、后台 goroutine 统一管理与 panic 防护、Overpass 限流指标。
  - **Linked artifacts / tickets**: 同上

## 7. Notes by Agenda Item

- **Agenda Item 1**: 当前问题与改造目标
  - Key points:
    - 当前根据工具配置中的 `PSM + branch` 从 Overpass 拉取 IDL，构建 generic client 后发起 RPC。（00:06:16）
    - 目前只有进程内懒加载缓存；branch 删除或 Overpass 抖动时，IDL 拉取失败会导致整个工具调用失败。（00:06:44）
    - 目标包括：持久化可用 IDL、降低新实例首次调用延迟、治理刷新并发和故障期重复回源。（00:07:50）

- **Agenda Item 2**: 快照库和读取链路
  - Key points:
    - 初始方案将链路改为“内存 client → IDL 快照库 → Overpass”，库 miss 时回源并 write-through。（00:08:31–00:09:18）
    - 刷新失败不能覆盖已有可用副本；启动时从当前有效工具版本枚举 `PSM + branch` 并预热，单项失败不阻塞服务启动。（00:09:18、00:21:17）
  - Open issues / questions:
    - “已有可用副本继续运行”只适合哪些 branch/版本，是后续评审争议的核心。

- **Agenda Item 3**: 存储选型
  - Key points:
    - MySQL 方案以 `PSM + branch` 为唯一 key，把完整 IDL info 序列化到 `LONGTEXT`，复用现有基础设施。（00:13:43）
    - MongoDB 更适合嵌套文档和内部字段索引，但当前没有实际查询需求，新增接入和运维成本较高。（00:15:02–00:16:34）
    - 会议最终确认先用 MySQL；当前数据本质上接近小规模 KV。（00:37:26–00:37:48）

- **Agenda Item 4**: 稳定性治理
  - Key points:
    - 同一 key 需要单飞刷新，故障期需要分类失败缓存与退避，避免热点 key 并发打 Overpass。（00:22:11）
    - 内存缓存当前只增不淘汰；generic RPC timeout 为 180 秒，需增加容量/LRU、分阶段超时与日志，并治理后台 goroutine/panic。（00:23:12）

- **Agenda Item 5**: branch 删除后的正确语义
  - Key points:
    - 唐建科指出：被删除的 feature branch 代表该临时版本已经结束，不应继续从 DB 快照运行；否则 master 后续变化可能造成协议不一致。（00:24:03）
    - 评审要求 feature branch 不得发布到真实生产环境；branch 不存在是配置/生命周期错误，应 fast-fail，不属于需要缓存兜底的系统故障。（00:26:27–00:27:39）
    - 对 master/长期有效版本，如果上游 IDL 依赖损坏导致无法编译，应使用已验证快照维持调用并报警。（00:27:39）
    - 定时刷新还需解决多实例下的最坏发现窗口；示例中的 10 分钟轮询可能形成接近 20 分钟的陈旧窗口。（00:30:33）

- **Agenda Item 6**: IDL 版本与刷新策略
  - Key points:
    - 段宏达提出线上是否应固定使用工具版本通过时的 IDL，而不是不断刷新最新 IDL，避免上游不兼容变更。（00:31:28）
    - 讨论倾向把 IDL 变化与工具新版本或 PPE→master 发布绑定，但未形成最终方案。（00:33:51–00:34:36）
  - Open issues / questions:
    - 是自动跟随最新 master，还是保存工具版本对应的已验证 IDL，需在修订方案中做出选择。

## 8. Parking Lot / Unresolved Items

- **Item**: 最新 IDL 自动刷新与已验证版本冻结的取舍
  - Why parked / next step: 会上识别到兼容性风险，但未完成方案比较；由李倩影补充后再评审。
  - Suggested owner or next meeting to resolve: 李倩影；下一次方案复审

- **Item**: 多实例刷新调度和陈旧窗口
  - Why parked / next step: 未确定统一绝对时间、间隔轮询、事件监听或其他机制。
  - Suggested owner or next meeting to resolve: 李倩影；下一次方案复审

- **Item**: 手动刷新入口和变更事件监听
  - Why parked / next step: 只确认需要运维能力，没有确定 API、权限和触发协议。
  - Suggested owner or next meeting to resolve: `TBD`

- **Item**: 快照能力是否进一步泛化到工具网关
  - Why parked / next step: 储节节提出后续可向网关抽象，但本次没有扩展范围。
  - Suggested owner or next meeting to resolve: 后续网关设计讨论

## 9. Risks / Blockers

- **Risk 1**: 旧 branch 快照掩盖 branch 已删除，可能产生协议漂移和难定位的错误；影响在线调用正确性。Mitigation owner: 李倩影（方案修订），最终执行负责人 `TBD`。
- **Risk 2**: 多实例按各自周期刷新会扩大过期 branch 的存活窗口；影响失效及时性。Mitigation owner: `TBD`。
- **Risk 3**: 自动刷新到上游最新 IDL 可能引入不兼容变更；影响 generic client 构建和请求字段匹配。Mitigation owner: `TBD`。
- **Risk 4**: Overpass 抖动、限流或 IDL 编译失败仍可能打断无持久化副本的新 key；影响首次调用。Mitigation owner: `TBD`。
- **Risk 5**: 当前内存缓存不淘汰、超时长达 180 秒，后台并发刷新和 panic 防护也不完整；影响内存、延迟和故障隔离。Mitigation owner: 李倩影（纳入修订方案），最终执行负责人 `TBD`。

## 10. Next Meeting / Follow-up

- Proposed date/time: `TBD`；会议未约定
- Objectives for next meeting:
  - 复审 feature branch fast-fail、发布约束和快照失效流程
  - 确认 IDL 刷新或版本冻结策略
  - 确认刷新调度、告警、缓存淘汰和分阶段超时
  - 在上述边界明确后决定方案是否可以进入实现

## 11. Attachments / References

- Agenda document: 《IDL 持久化与工具泛化调用稳定性改造方案》；会议搜索结果只提供标题，未返回可持久引用的文档 URL
- Slides: `None identified`
- Transcript / Recording: 飞书妙记 token `obmy8e2lhpev983uq2n29w2l`；本次通过 `lark-cli minutes +detail --transcript` 读取，原始 Transcript 未复制进仓库
- Related tickets: `None identified`
- Reference Skill: [meeting-minutes reference](../external-skills/meeting-minutes-reference/SKILL.md)

## 12. Version & Change Log

- **Version**: 1.0
- **Last updated**: 2026-08-04T14:35:07Z
- **Changes**: 首版；基于原始 Transcript、VC 参会快照与通讯录生成，未使用飞书 AI Summary
