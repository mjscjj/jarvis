
# PRD Review｜Rena 主播人群包拆分｜9月10日 Arena 组内结论


## 结论

方向可继续推进；在对接和验收前，优先统一发送前状态复核、拆包发起方、重复拆包的子包归属，以及本期容量承诺。这些是当前文档中的流程歧义与交付边界，尚未验证线上实现存在相同问题。本报告是补充评审建议，不改变会议已记录的“通过”结论。

本次仅评审这一篇。已核对原 PRD revision 1062、规则图片、流程图、可见评论与内嵌表，并补读量级扩容依赖 revision 29。原 PRD 版本与旧报告记录一致；本轮是补充核验和修正评审，不声称需求正文发生了变化。


## 来源

原 PRD：[[cohort]提供主播人群包拆包能力给rena侧使用](https://bytedance.larkoffice.com/wiki/RUjYw8XT7iCjNHk5BVCckEPTn2c)；实际文档 ID：SgBbdKf8boTfZixwApPm75ILygf，revision 1062。PRD 修改人：王亚南。关联需求：[7372109150](https://meego.larkoffice.com/tiktok_live/story/detail/7372109150)。

会议来源：公会/内部效率/数据平台组内，[张家铭发布的 Arena 组内评审结论](https://applink.feishu.cn/client/thread/open?open_chat_id=oc_43d3b4c42308cc09705a09768650734d&open_thread_id=omt_19c7d40625cf0765)，消息 ID om_x100b6511823db0a10c9dcb8cba8a841，发布时间 2026-09-10 07:31 UTC。正文标注“2026/09/10”，截图分组标注“2026/09/09”；两处日期不一致，因此本报告按“9 月 10 日发布的结论”识别来源，不把截图分组日期当成已核实的开会日期。截图共 5 个需求，本篇为第 4 项，状态“通过”。

必要依赖：[[cohort]千万量级圈人引擎查数切换](https://bytedance.larkoffice.com/docx/D3nhdazV1ourDhx6WkhmDiigyEj)，revision 29。证据读取：2026-09-11 08:46–08:49 UTC。


## 产品理解

目标是让运营在 Rena 配置活动圈人目标后，按主播所处阶段发送不同文案，减少人工拆包。母包 cohort_0 仍按 T+1 离线产出；DP 旁路逐批查询 Campaign 活动状态，异步生成子包并落 Hive；Message 分别读取各子包发送。本次明确为纯后台需求，查询引擎扩容由另一 PRD 承接。

| 分支 | 按优先级命中的子包 | 已明确的兜底 |
| --- | --- | --- |
| 报名型 | S4 已报名 → S3 已访问未报名 → S1 未访问且近 30 天登录 | 未访问且近 30 天未登录：不触达 |
| 非报名型 | S6 已开播 → S5 已访问未开播 → S1 未访问且近 30 天登录 | 未访问且近 30 天未登录：不触达 |

判断依据：[报名型规则图](https://bytedance.larkoffice.com/docx/SgBbdKf8boTfZixwApPm75ILygf#WoM6ds8Wcowch6xCO3UmiNGpyBh)、[非报名型规则图](https://bytedance.larkoffice.com/docx/SgBbdKf8boTfZixwApPm75ILygf#R9FZdQKVRopA1fxusmUmYF5Wy0b)、[深层优先、首次命中规则](https://bytedance.larkoffice.com/docx/SgBbdKf8boTfZixwApPm75ILygf#OyrgdcZgioTpYDxRfEGm9AnoyTh)。在所列状态均为有效 0/1 值的前提下，两条分支已有优先级和沉默用户兜底；旧报告把这部分泛化成“没有未命中处理”不准确，本轮已修正。


## 主要发现


### 优先澄清：小时级拆包后，发送前由谁复核状态

触发场景：主播在拆包时未报名，被分入 S3；等待子包生成或发送排队期间完成报名。若 Message 仍直接读取旧子包，会继续发送催报名文案。

原文依据：[环节 2](https://bytedance.larkoffice.com/docx/SgBbdKf8boTfZixwApPm75ILygf#CLPgdcBTZoU5k3xPm8Fmf9kLynh)明确“异步，小时级”；[时序图](https://bytedance.larkoffice.com/docx/SgBbdKf8boTfZixwApPm75ILygf#LxqZdwgcLo1CRgxZemlmJ14kydA)只画出拆包时查询状态、随后 Message 整包读取发送。但[拆包时机](https://bytedance.larkoffice.com/docx/SgBbdKf8boTfZixwApPm75ILygf#MbfNdbgyuonfv9xDSOCmDoXayvh)要求状态在“拆包 / 发送触发时刻实时准出”，[新旧逻辑图](https://bytedance.larkoffice.com/docx/SgBbdKf8boTfZixwApPm75ILygf#AguidAvIOoQOdsxPk3LmQm1kydb)也写了“拆包+发送均实时过滤”。发送时复核的执行方和失败处理未在流程中闭合。

影响与建议：这会影响“避免已报名仍收到催报名”的核心目标。由 DP、Campaign、Message 对齐：是否已有发送前过滤能力、谁负责调用、允许使用多旧的状态、超过时限或查询失败时如何处理。若现有发送底层已支持，补引用和接入点即可；若只接受拆包时快照，应收窄业务承诺，并由业务确认可接受的错发窗口。

验收建议：构造“拆包后报名、发送前开播、状态查询失败”三类样本，验证最终发送结果符合约定，而不只检查子包生成成功。


### 已确认的文档冲突：拆包发起方向写反

触发场景：母包交付后，DP 与 Campaign 按各自读到的说明实现任务入口。

原文依据：[环节 2 第一步](https://bytedance.larkoffice.com/docx/SgBbdKf8boTfZixwApPm75ILygf#QqEEdOrSroxqNDxNXj7mgbrQyEe)写“DP向发起方请求 cohort_0 后置拆包任务”；同一节[时序图](https://bytedance.larkoffice.com/docx/SgBbdKf8boTfZixwApPm75ILygf#LxqZdwgcLo1CRgxZemlmJ14kydA)却是 Reach/Campaign 发起方 → DP 后置拆包链路。[环节 1](https://bytedance.larkoffice.com/docx/SgBbdKf8boTfZixwApPm75ILygf#HdBJd17H3oVcTIx4w5imrPN8yqc)又明确本期发起方为 Campaign。

影响与建议：可能导致双方等待对方触发，或重复发起。建议 PM 王亚南与 DP/Campaign 对接人确认一个唯一入口，并同步修正文句和时序图；同时说明异步完成由谁通知、谁启动 Message 分包发送。接口序列化和错误码可留给技术设计，PRD 先明确产品流程责任。


### 对接缺口：同一母包重复拆分，缺少本次结果的选择规则

触发场景：同一 cohort_0 在上午、下午各拆一次，或首次只生成部分子包后重试；运营需要发送最新一轮结果。

原文依据：[方案评论](https://bytedance.larkoffice.com/docx/SgBbdKf8boTfZixwApPm75ILygf#ZhwGdH6toox1Wzx2nDrmIgiYyId)已说明母子包是一对多，“通过母包ID查询关联映射表，即可拉取所有对应的子包ID列表”；[子包生成](https://bytedance.larkoffice.com/docx/SgBbdKf8boTfZixwApPm75ILygf#KqGjdAHxTo59uXx3W2lme0u3yTf)与[分包发送](https://bytedance.larkoffice.com/docx/SgBbdKf8boTfZixwApPm75ILygf#O0HwdKjqQoM786xGNuMmgw3Eyse)没有进一步说明两次拆分的结果如何隔离，或子包不齐时能否启动发送。

影响与建议：只有母包到子包的总映射，下游可能混用新旧子包，同一 UID 也可能跨次重复触达。补一条可执行约定：本期是否允许同母包多次拆分；允许时，用拆分批次或等价标识绑定活动、规则和这一轮完整结果，重复请求返回同一轮结果，部分失败时不把不完整集合标作可发送。若本期只支持一次拆分，明确限制和再次申请时的行为即可。

验收建议：对同母包两轮拆分、同请求重试、一个子包生成失败三种情况，检查 Message 只消费预期轮次，重试不额外触发同一发送任务。


### 交付边界：图示的 1 亿量级不能直接作为本期承诺

触发场景：Rena 依据拆包 PRD 的“100 万～1 亿”链路图，配置超千万母包或承诺本期支持亿级活动。

原文依据：[量级说明](https://bytedance.larkoffice.com/docx/SgBbdKf8boTfZixwApPm75ILygf#Z44vdtKZIonMW1xsLpVmna09yrr)和[链路图](https://bytedance.larkoffice.com/docx/SgBbdKf8boTfZixwApPm75ILygf#AguidAvIOoQOdsxPk3LmQm1kydb)引用 100 万～1 亿。已补读的[扩容 PRD revision 29](https://bytedance.larkoffice.com/docx/D3nhdazV1ourDhx6WkhmDiigyEj)则明确短期 100 万～1000 万，1 亿属中长期；评论中研发确认的是千万级技术验证，并指出灵活查询上传到 cohort 仍有 1000 万限制。技术验证也不能直接证明已上线。

影响与建议：查询、母包生成、活动实时状态查询、拆包和发送是不同环节，上游扩大查询量级不能自动证明全链路支持同等规模。将本期承诺写成经联合验证的量级，亿级标为后续目标；确认母包上限、Campaign 查询吞吐、拆包完成时间及发送窗口。超过上限时应在任务受理阶段给出明确结果，避免异步等待后才发现无法完成。


## 待决策事项

以下为需要业务/对接方选择的边界，不据此认定线上已有缺陷：

| 事项 | 已知依据与待定选择 | 建议确认方 |
| --- | --- | --- |
| 未知状态与不触达人群 | 规则图已有沉默用户不触达，但没有明确字段缺失/接口失败是否等同于 0，也未说明“不触达”是否产出子包 ID。建议区分业务不触达与数据未知，约定排除、重查或暂停的条件，并让统计可核对母包人数。 | 王亚南、Campaign、DP |
| 一期是否提供自助配置 | 正文明确纯后台、常规对接由研发配置上线；通用化图又写“每个项目自助配置（零开发）”。建议本期按研发配置或既有入口接入明确交付，自助 UI 如需建设另行确定范围。 | 王亚南、Rena、DP |
| 活动状态与派生子包分别保存什么 | 可见评论确认活动侧数据“仅查询，不存储”；正文同时描述子包落 Hive。这两者可同时成立，不能直接认定自相矛盾。需确认活动原始字段、子包成员和发送结果分别保存哪些内容、多久可用，以及所用地域链路。内嵌影响评估未提供本需求填写结果。 | DP、Campaign 与相应数据责任人 |

建议由 PM 汇总上述 4 项发现和 3 项决策，与实际对接人逐条确认后补回 PRD。报告没有向这些人员发消息，也没有代为修改原需求。


### 建议加入本期验收的最小案例集

| 案例 | 预期结果 |
| --- | --- |
| registered=1，同时 page_visited=0 | 深层优先，仅进入报名型 S4，不重复进入 S1 |
| 非报名型 gone_live=1，同时 page_visited=0 | 仅进入 S6，不重复进入 S1 |
| page_visited=0、login_30d=0，且未命中深层条件 | 不触达；归属/排除计数按已确认规则记录 |
| registered 缺失或状态接口失败 | 按明确的数据未知策略处理，不静默当作未报名 |
| S3 成员在发送前完成报名 | 按已确认的发送前过滤/快照政策处理，并可复核结果 |
| 同母包两轮拆分、重试、子包部分失败 | 只消费指定完整轮次，重复请求不重复启动发送 |
| 超过本期已验证量级 | 受理时明确拒绝或给出已约定的分批方案，不默认承诺 1 亿 |

这些是建议验收案例，本轮没有调用真实拆包或发送接口执行测试。成功指标除拆包成功率、准确率外，建议单独观察错发率、未知状态占比和端到端耗时；阈值由业务与研发约定，本报告不虚构数值。


## 未覆盖范围

已读：原 PRD 全文和普通表格；全部 8 张内嵌工作表的返回 used range（含依赖、TTP/PDPO、安全策略、客户端及结果模板，未返回截断标记）；4 张核心图片（字段、报名型、非报名型、通用化）；1 张整体链路画板预览；4 段 Mermaid 源图；4 条可见评论线程；扩容 PRD 正文和可见评论。

未验证：Meego 实时排期和实际部署、Campaign/Message 既有接口实现、活动指标库实际接入情况、历史版本差异、真实吞吐与端到端错发率。未展开与纯后台拆包流程无直接关系的 OG 示例图片、通用法务/埋点模板参考链接；不据此认定权限或合规缺陷。

本次复用已有报告链接完成补充评审；修正了旧稿对字段和沉默用户兜底的遗漏，细化了“仅查询、不存储”的解释，并补入扩容依赖。原 PRD 保持只读；评审建议尚未被产品/研发确认或实施。
