/**
 * 种子数据：Weekly Catch Up 8.25「Focus item in 26Y Q3」公会业务段落原文，
 * 状态写法按 template.ts 的枚举归一，其余内容照抄。
 */

import type { Objective } from './types'

export const PAGE_TITLE = 'Emily · OKR 协作台'

export const OBJECTIVES: Objective[] = [
  {
    id: 'o1',
    title: 'O1: B端市场',
    krs: [
      {
        id: 'kr1',
        title: 'KR1: 公会大会顺利落地，参会率和满意度达成预期',
        metricNote: '8/25 数据',
        metrics: [
          { id: 'm1', text: '核心邀约嘉宾 RSVP 确认率：0% → 51.5%（450 人），目标 80%', light: 'green' },
          { id: 'm2', text: '大会宣发物料语种：0 → 23 种，预计 8/31 前完成上线', light: 'green' },
          { id: 'm3', text: '团播 Tour 报名人数：0 → 229 人，目标 80 人', light: 'green' },
        ],
        points: [
          {
            id: 'p1',
            kind: 'strategy',
            title: '公会大会和颁奖典礼顺利落地，核心邀约嘉宾参会率 80%、整体满意度 ≥4/5',
            entries: [
              {
                id: 'e1',
                status: 'in_progress',
                text: '议题内容进入最终交付阶段，37 场演讲启动 PR/Legal Review，本周重点启动首轮线上 Dry Run 和 PPT 美化。',
                docs: [],
                images: [],
              },
              {
                id: 'e2',
                status: 'in_progress',
                text: '邀约进入催收阶段，A 组出席率 95%、会前机酒安排，B/C 组继续跟进 8/19 完成收集，同步启动补邀。',
                docs: [],
                images: [],
              },
              {
                id: 'e3',
                status: 'in_progress',
                text: '会客厅 Meet-up with LIVE 进入预约配置前收口阶段，预计覆盖走政策、虚拟人、音乐、区域等高价值交流主题；本周重点确认标题、简介、owner、房间、容量、预约规则和现场材料。',
                docs: [],
                images: [],
              },
              {
                id: 'e4',
                status: 'in_progress',
                text: '展区整体设计已完成，正在推进各展区宣传物料修改定稿；本周重点完成 BU 展台文案、设计素材、Fest Series、Group LIVE Showcase 和 Nanlite 相关物料确认。',
                docs: [],
                images: [],
              },
              {
                id: 'e5',
                status: 'in_progress',
                text: 'H5 进入终局配置阶段，覆盖议程、会客厅预约、签到、地图/FAQ、白皮书下载和问卷入口；本周重点确定问卷入口和上线前测试。',
                docs: [],
                images: [],
              },
              {
                id: 'e6',
                status: 'in_progress',
                text: '同传方案已确认采用人工远程同传 + AI 同传字幕；本周重点完成设备、同传入口、术语表、PPT/script 交付和 Dry Run 素材同步。',
                docs: [],
                images: [],
              },
              {
                id: 'e7',
                status: 'in_progress',
                text: 'Awards Gala 继续推进 Cue Sheet、颁奖流程、表演团队、奖杯奖牌、获奖嘉宾录制和演讲嘉宾证书；本周重点完成 MC Script、颁奖人组合、上屏素材和表演流程确认。',
                docs: [],
                images: [],
              },
              {
                id: 'e8',
                status: 'in_progress',
                text: '现场体验与运营方案进入细化阶段，本周重点推进签到技术支持、扫码枪、胸卡设计优化、互动展区设计、Staff Training、Move-in 行程、运营手册和展区 Journey SOP。',
                docs: [],
                images: [],
              },
            ],
          },
          {
            id: 'p2',
            kind: 'strategy',
            title: '线上宣发总曝光 ≥11M，白皮书分页面曝光 ≥1M，白皮书下载量 ≥1600',
            entries: [
              {
                id: 'e9',
                status: 'in_progress',
                text: '白皮书英文版已完成定稿并已确认打样，本周将推进英文设计最终定稿、多语言版本制作及英文版印刷。',
                docs: [
                  {
                    id: 'd1',
                    title: 'TikTok LIVE Creator Networks: Fueling the Next Wave',
                    url: 'https://bytedance.larkoffice.com/wiki/LI3lwENobiDTvHkMSkbcHOVRnEd',
                  },
                ],
                images: [],
              },
            ],
          },
          {
            id: 'p3',
            kind: 'strategy',
            title: '团播 Tour 参会人群 ≥80 人，满意度 ≥3.8/5',
            entries: [
              {
                id: 'e10',
                status: 'done',
                text: '整体方案及路线已确认。',
                docs: [
                  {
                    id: 'd2',
                    title: '【执行文档】团播游学',
                    url: 'https://bytedance.larkoffice.com/wiki/LkZuwfKhXi2ycZk03X7cvjgjnj7',
                  },
                ],
                images: [],
              },
              {
                id: 'e11',
                status: 'in_progress',
                text: '报名审核：本周收集报名单并进入国家经理审核。目前已有 229 人报名，名额已满。',
                docs: [],
                images: [],
              },
              {
                id: 'e12',
                status: 'in_progress',
                text: '工作人员：已确定，5 位泰国 AM 负责公会场地接待对接。',
                docs: [],
                images: [],
              },
            ],
          },
          {
            id: 'p4',
            kind: 'strategy',
            title: '媒体参与并报道',
            entries: [
              {
                id: 'e13',
                status: 'in_progress',
                text: '媒体方案已于上周完成初步对齐，将由梁梓统一负责邀约。',
                docs: [],
                images: [],
              },
            ],
          },
          {
            id: 'p5',
            kind: 'product',
            title: '上线公会 H5，支持参会者登录鉴权、日程查看管理、签到、分享等功能',
            entries: [
              {
                id: 'e14',
                status: 'in_progress',
                text: '线上 H5 一期核心功能即将上线：一期主流程 8/25 上线，8/26 众测。',
                docs: [
                  {
                    id: 'd3',
                    title: '[Crowdtest] TCN conference H5 2026',
                    url: 'https://bytedance.larkoffice.com/docx/UjPhdBuhdokFJqxDdOvmsUcGy0O',
                  },
                ],
                images: [],
              },
              {
                id: 'e15',
                status: 'in_progress',
                text: '线下保障计划准备中。',
                docs: [],
                images: [],
              },
              {
                id: 'e16',
                status: 'done',
                text: '完成 PRD 组内评审，合规已通过。',
                docs: [],
                images: [],
              },
            ],
          },
        ],
      },
      {
        id: 'kr2',
        title: 'KR2: 通过 SEO 和官网内容持续更新，优化官网排名和自然搜索 UV',
        metricNote: 'Q3 QTD',
        metrics: [
          {
            id: 'm4',
            text: '自然流量 UV 371,871；上周 46,947，前一周 44,700（+5.0%）',
            light: 'green',
          },
          { id: 'm5', text: '自然线索 7,588；上周 938，前一周 964（-2.7%）', light: 'yellow' },
          {
            id: 'm6',
            text: '累计自然入驻 1,253 家，线索到入驻转化率 16.51%',
            light: 'green',
          },
        ],
        points: [
          {
            id: 'p6',
            kind: 'strategy',
            title: '官网 SEO 常态发稿 15 篇，官网 UV 提升 6,750，公会自然入驻增量 60～100',
            entries: [
              {
                id: 'e17',
                status: 'in_progress',
                text: 'Q3 预计发布 15 篇官网 SEO 文章，已发布 9 篇。',
                docs: [],
                images: [],
              },
              {
                id: 'e18',
                status: 'done',
                text: 'TCN 官网品牌刷新：8 月 13 日完成并正式上线，同步增加公会大会信息露出。',
                docs: [],
                images: [],
              },
            ],
          },
          {
            id: 'p7',
            kind: 'product',
            title: '官网更新带来 UV +6,750，线索扩量',
            entries: [
              {
                id: 'e19',
                status: 'in_progress',
                text: 'US&EU 内广线索数据回传 TTP 改造：计划 8.31 上线，电话渠道因外域依赖延期至 9.10。',
                docs: [],
                images: [],
              },
              {
                id: 'e20',
                status: 'done',
                text: '官网 SEO 文章多语言标签优化 & title 优化。',
                docs: [],
                images: [],
              },
            ],
          },
        ],
      },
    ],
  },
  {
    id: 'o2',
    title: 'O2: 平台政策',
    krs: [
      {
        id: 'kr3',
        title: 'KR1: 公会目标和预算管控，目标线上化落地，区域预算可控',
        metricNote: '6 月 vs 7 月',
        metrics: [
          { id: 'm7', text: '公会营收占比 41.4% -> 41.6%', light: 'green' },
          { id: 'm8', text: '高平台价值公会数从 514 -> 521', light: 'green' },
          { id: 'm9', text: '新主播成材率 9.2% -> 9.1%', light: 'yellow' },
          { id: 'm10', text: '成材主播升保级率 68.0% -> 67.2%', light: 'red' },
          { id: 'm11', text: '流水目标采纳率 73.4%，目标 85%', light: 'yellow' },
        ],
        points: [
          {
            id: 'p8',
            kind: 'strategy',
            title: '目标线上化落地，采纳率提升；区域预算可控，红黄灯区域收敛',
            entries: [
              {
                id: 'e21',
                status: 'in_progress',
                text: '预算看板建设：X 政策分成比月中预估逻辑制定中，预计本周完成。',
                docs: [],
                images: [],
              },
              {
                id: 'e22',
                status: 'in_progress',
                text: '流水目标采纳率分析：目标 cap 调整原因占比最高，8 月达 45%；本周对齐优化方案。',
                docs: [],
                images: [],
              },
              {
                id: 'e23',
                status: 'in_progress',
                text: '流水目标采纳率看板：预计本周内交付。',
                docs: [],
                images: [],
              },
              {
                id: 'e24',
                status: 'done',
                text: '目标线上化系统上线，各区域 7、8 月公会目标均已通过新系统下发完毕。',
                docs: [],
                images: [],
              },
              {
                id: 'e25',
                status: 'done',
                text: 'Q3 预算完成定稿。',
                docs: [],
                images: [],
              },
            ],
          },
          {
            id: 'p9',
            kind: 'product',
            title: '审计功能与采纳率推送能力上线',
            entries: [
              {
                id: 'e26',
                status: 'in_progress',
                text: '审计功能测试中，9 月上线。',
                docs: [],
                images: [],
              },
              {
                id: 'e27',
                status: 'in_progress',
                text: '采纳率分析推送：9 月目标设定完成后支持在群组内推送采纳率、红绿灯分析总结。',
                docs: [],
                images: [],
              },
            ],
          },
        ],
      },
      {
        id: 'kr4',
        title: 'KR4: 三方机制优化，欧美退会三方仲裁上升',
        metricNote: '',
        metrics: [{ id: 'm12', text: '仲裁机制覆盖区域 2 个，目标 4 个', light: 'yellow' }],
        points: [
          {
            id: 'p10',
            kind: 'strategy',
            title: '欧美退会三方仲裁机制扩区落地',
            entries: [
              {
                id: 'e28',
                status: 'at_risk',
                text: '欧美法务已确认 opt-out 方案，8.26 组内评审；US 仲裁 HC 培训到位后上岗预计 9 月 21 日，人力到位时间存在不确定性。',
                docs: [
                  {
                    id: 'd4',
                    title: '【BRD】三方仲裁机制扩区及历史遗留功能优化',
                    url: 'https://bytedance.larkoffice.com/wiki/AYV3wit4vi58btkGExxchTFBnEe',
                  },
                ],
                images: [],
              },
            ],
          },
          {
            id: 'p11',
            kind: 'product',
            title: '主播入会即有 LIVE Studio 权限',
            entries: [
              {
                id: 'e29',
                status: 'delayed',
                text: '因 QA 人力问题，排期从 8 月底推到 9 月中，正在沟通协调中。',
                docs: [],
                images: [],
              },
            ],
          },
        ],
      },
    ],
  },
]
