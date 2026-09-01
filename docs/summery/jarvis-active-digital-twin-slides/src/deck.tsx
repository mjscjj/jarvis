import { useState, type ReactNode } from "react";
import codeReviewImage from "../assets/screenshots/code-review.png";
import issueInvestigationImage from "../assets/screenshots/issue-investigation.png";
import ambientTaskImage from "../assets/screenshots/ambient-task.png";
import commentTaskImage from "../assets/screenshots/comment-task.png";
import meetingPrepImage from "../assets/screenshots/meeting-prep.png";
import approvalCaseImage from "../assets/screenshots/approval-case.png";
import oncallClarifyImage from "../assets/screenshots/oncall-clarify.jpg";
import approvalWebImage from "../assets/screenshots/approval-web.png";
import approvalChatImage from "../assets/screenshots/approval-chat.png";
import approvalDetailImage from "../assets/screenshots/approval-detail.png";

export type SceneDefinition = {
  id: string;
  title: string;
  section: string;
  beats: number;
  profile: "speaker-led" | "reading-first" | "hybrid";
  theme: "whiteboard";
  render: (beat: number) => ReactNode;
};

function Reveal({ beat, at, className = "", children }: { beat: number; at: number; className?: string; children: ReactNode }) {
  return <div className={`reveal ${beat >= at ? "show" : ""} ${className}`}>{children}</div>;
}

function SceneTitle({ kicker, title, note }: { kicker: string; title: string; note: string }) {
  return <header className="scene-title"><p>{kicker}</p><h1>{title}</h1><span>{note}</span></header>;
}

function Marker({ tone, children }: { tone: string; children: ReactNode }) {
  return <span className={`marker marker-${tone}`}>{children}</span>;
}

function CodeStamp({ children }: { children: ReactNode }) {
  return <span className="code-stamp">{children}</span>;
}

function Takeaway({ beat, at, children }: { beat: number; at: number; children: ReactNode }) {
  return <Reveal beat={beat} at={at} className="takeaway">{children}</Reveal>;
}

function OpeningScene({ beat }: { beat: number }) {
  return (
    <article className="scene opening-scene" data-audit>
      <div className="opening-copy">
        <Reveal beat={beat} at={0}><p className="opening-kicker">JARVIS / ACTIVE DIGITAL TWIN</p></Reveal>
        <Reveal beat={beat} at={0}><h1>主动式数字分身<br /><em>探索实践</em></h1></Reveal>
        <Reveal beat={beat} at={1}><p>世界模型与决策方案</p></Reveal>
        <Reveal beat={beat} at={2} className="opening-formula"><span>持续理解现实</span><i>×</i><span>自主判断</span><i>×</i><span>行动闭环</span></Reveal>
      </div>
      <div className="opening-orbit" aria-label="世界变化围绕 Jarvis 决策核心">
        <div className="orbit-line orbit-line-one" /><div className="orbit-line orbit-line-two" />
        <Reveal beat={beat} at={0} className="orbit-core"><small>JARVIS</small><strong>J</strong><span>STATE × DECISION</span></Reveal>
        {[["消息","orbit-message"],["会议","orbit-meeting"],["文档","orbit-document"],["代码","orbit-code"]].map(([label,className], index) => <Reveal beat={beat} at={index > 1 ? 1 : 0} className={`orbit-token ${className}`} key={label}>{label}</Reveal>)}
        <Reveal beat={beat} at={2} className="orbit-verdict"><b>ACTION</b><span>or</span><strong>NOTHING</strong></Reveal>
      </div>
      <Reveal beat={beat} at={2} className="opening-note">不是等人提问，而是理解现实、主动决策并把事情推进到底。</Reveal>
    </article>
  );
}

function OriginLoopScene({ beat }: { beat: number }) {
  const stops = [
    ["01", "OpenClaw 启蒙", "Mac mini 上看见 Agent 能力的上限", "OPEN SOURCE"],
    ["02", "云端多集群 Agent", "把开源 Agent loop 带进真实团队", "TEAM PRACTICE"],
    ["03", "自建 Agent Loop", "AI Coding 辅助下，不到一周搭出底座", "FROM ZERO"],
    ["04", "Jarvis", "把完整 Agent 变成主动式数字分身", "ACTIVE TWIN"],
  ];
  return (
    <article className="scene origin-scene" data-audit>
      <SceneTitle kicker="02 / ORIGIN" title="从 OpenClaw 到自建 Agent Loop" note="真正的转折不是换了一个框架，而是从零搭过一遍之后，开始重新理解 Agent。" />
      <section className="origin-track">
        <svg viewBox="0 0 1610 470" aria-hidden="true"><path d="M105 245 C340 80 500 410 775 235 S1210 90 1515 240" /></svg>
        {stops.map(([index,title,body,tag], stop) => (
          <Reveal beat={beat} at={stop} className={`origin-stop origin-stop-${stop + 1}`} key={title}>
            <span>{index}</span><div className="origin-pin" /><small>{tag}</small><h2>{title}</h2><p>{body}</p>
          </Reveal>
        ))}
      </section>
      <CodeStamp>OpenClaw / Pi → Agent Runtime → Jarvis</CodeStamp>
      <Takeaway beat={beat} at={3}>搭完整 Agent 没有想象中难；难的是让它长期代表一个人判断和行动。</Takeaway>
    </article>
  );
}

function ActiveNotChatbotScene({ beat }: { beat: number }) {
  return (
    <article className="scene active-scene" data-audit>
      <SceneTitle kicker="03 / PRODUCT DEFINITION" title="它不是一个等人提问的机器人" note="普通 Agent 从明确目标开始；Jarvis 的输入只是“世界变了”。" />
      <section className="active-comparison">
        <Reveal beat={beat} at={0} className="passive-machine">
          <Marker tone="blue">PASSIVE AGENT</Marker><div className="machine-input"><span>人给目标</span><b>“帮我完成这件事”</b></div><i>→</i><div className="machine-output"><span>执行</span><b>Result</b></div>
          <small>核心问题：怎么做完？</small>
        </Reveal>
        <Reveal beat={beat} at={1} className="active-machine">
          <Marker tone="green">JARVIS</Marker>
          <div className="world-inputs"><span>消息</span><span>会议</span><span>日程</span><span>代码</span></div><i>→</i>
          <div className="decision-gate"><small>先判断</small><b>Goal?</b><span>是否值得现在推进</span></div><i>→</i>
          <div className="decision-outputs"><b>ACTION</b><strong>NOTHING</strong></div>
          <small>核心问题：要不要做？</small>
        </Reveal>
        <Reveal beat={beat} at={2} className="input-shift-stamp">分水岭在输入端</Reveal>
      </section>
      <Takeaway beat={beat} at={2}>主动不是更频繁地执行，而是能够合法、安静地判断“不做”。</Takeaway>
    </article>
  );
}

const capabilities = [
  ["01", "上下文答疑", "先找事实，再回答问题", "context"],
  ["02", "代码 Review", "自动 AP、CC 与回读结果", "review"],
  ["03", "问题闭环", "分析 → 修复 → 提交", "repair"],
  ["04", "场内召唤", "群消息与评论直接派发", "ambient"],
  ["05", "会前准备", "日程识别并写入材料", "meeting"],
  ["06", "审批 / Oncall", "关键操作请示与事实澄清", "guard"],
] as const;

function CapabilityMapScene({ beat }: { beat: number }) {
  return (
    <article className="scene capability-scene" data-audit>
      <SceneTitle kicker="04 / CAPABILITY MAP" title="六类能力，围绕同一个人工作" note="能力表面上不同，底层都依赖同一份上下文、同一个决策器与同一条执行闭环。" />
      <section className="capability-map">
        <svg viewBox="0 0 1640 620" aria-hidden="true"><path d="M820 300 L240 120 M820 300 L805 78 M820 300 L1400 120 M820 300 L240 495 M820 300 L820 550 M820 300 L1400 495" /></svg>
        <Reveal beat={beat} at={0} className="capability-core"><span>PRINCIPAL</span><strong>Jarvis</strong><small>统一上下文 · 统一决策 · 统一执行</small></Reveal>
        {capabilities.map(([index,title,body,className], item) => <Reveal beat={beat} at={Math.min(3, Math.floor(item / 2) + 1)} className={`capability-card capability-${className}`} key={title}><span>{index}</span><h2>{title}</h2><p>{body}</p></Reveal>)}
      </section>
      <Takeaway beat={beat} at={3}>不是六个自动化脚本，而是同一个数字分身在不同协作现场里的动作。</Takeaway>
    </article>
  );
}

function CodeLoopScene({ beat }: { beat: number }) {
  return (
    <article className="scene code-loop-scene" data-audit>
      <SceneTitle kicker="05 / CODE CASE" title="从问题到代码交付，一段对话闭环" note="先把问题查清，再修改、提交、自动 Review，并把结果送回原协作现场。" />
      <section className="code-case-board">
        <Reveal beat={beat} at={0} className="evidence-shot issue-shot"><img src={issueInvestigationImage} alt="问题分析与修复过程截图" /><span>01 · INVESTIGATE</span><b>问题分析</b></Reveal>
        <div className="code-flow-track">
          {["定位上下文", "核验根因", "修改代码", "提交变更", "自动 AP / CC"].map((step,index) => <Reveal beat={beat} at={Math.min(3,index)} className="code-flow-step" key={step}><span>0{index + 1}</span><b>{step}</b></Reveal>)}
        </div>
        <Reveal beat={beat} at={2} className="evidence-shot review-shot"><img src={codeReviewImage} alt="自动代码 Review 与 AP 截图" /><span>05 · REVIEW</span><b>结果读回</b></Reveal>
        <Reveal beat={beat} at={3} className="code-case-stamp">问题不是“回答了”，而是“闭环了”</Reveal>
      </section>
      <Takeaway beat={beat} at={3}>Agent 的价值不在生成一段建议，而在让真实改动经过验证后落地。</Takeaway>
    </article>
  );
}

function AmbientFeishuScene({ beat }: { beat: number }) {
  return (
    <article className="scene ambient-scene" data-audit>
      <SceneTitle kicker="06 / FEISHU AMBIENT" title="任务不必离开协作现场" note="群聊原文、文档评论和话题上下文本身就是任务入口，不必再复制到另一个工作台。" />
      <section className="ambient-board">
        <Reveal beat={beat} at={0} className="ambient-shot ambient-chat"><img src={ambientTaskImage} alt="飞书群内无需 at 的任务处理截图" /><div><Marker tone="green">群聊</Marker><h2>无需 @ 也能识别任务</h2><p>结合群绑定、说话人和上下文判断是否与 Principal 有关。</p></div></Reveal>
        <Reveal beat={beat} at={1} className="ambient-shot ambient-comment"><img src={commentTaskImage} alt="飞书文档评论派发任务截图" /><div><Marker tone="purple">评论</Marker><h2>评论直接进入执行链</h2><p>保留原文、引用对象与协作语境，执行结果回到原位置。</p></div></Reveal>
        <Reveal beat={beat} at={2} className="ambient-route"><span>现场变化</span><i>→</i><span>统一线索</span><i>→</i><span>判断 / 执行</span><i>→</i><strong>原地反馈</strong></Reveal>
      </section>
      <Takeaway beat={beat} at={2}>真正的场内 Agent，不要求人先把工作搬运到 Agent 面前。</Takeaway>
    </article>
  );
}

function CollaborationProofScene({ beat }: { beat: number }) {
  const proofs = [
    [meetingPrepImage, "MEETING", "识别日程并准备会前材料", "proof-meeting"],
    [oncallClarifyImage, "ONCALL", "核验事实，澄清真实故障点", "proof-oncall"],
    [approvalCaseImage, "APPROVAL", "关键副作用先请求 Principal", "proof-approval"],
  ] as const;
  return (
    <article className="scene collaboration-scene" data-audit>
      <SceneTitle kicker="07 / COLLABORATION PROOF" title="会议、Oncall、审批：都走同一套判断" note="来源不同，判断对象相同：当前世界发生了什么，以及现在最合理的动作是什么。" />
      <section className="proof-fan">
        {proofs.map(([imageUrl,tag,title,className],index) => <Reveal beat={beat} at={index} className={`proof-card ${className}`} key={tag}><img src={imageUrl} alt={`${title}截图`} /><span>{tag}</span><h2>{title}</h2></Reveal>)}
        <Reveal beat={beat} at={3} className="proof-common"><small>COMMON CORE</small><strong>Context → Decision → Tools → Verify</strong></Reveal>
      </section>
      <Takeaway beat={beat} at={3}>业务案例不需要各写一条 Go 链路；来源差异进入上下文与 Skill。</Takeaway>
    </article>
  );
}

function BackstageScene({ beat }: { beat: number }) {
  const metrics = [["24", "活跃 Task"], ["07", "等待 / 审批"], ["12", "今日 Todo"], ["05", "运行中 Agent"]];
  return (
    <article className="scene backstage-scene" data-audit>
      <SceneTitle kicker="08 / BACKSTAGE" title="主动行为必须可观察、可介入、可追溯" note="后台不是另一个决策中心，它负责把 Task、Run、状态、提案与 Effects 如实呈现出来。" />
      <section className="backstage-window">
        <div className="backstage-sidebar"><b>JARVIS</b>{["总览","任务","线索","世界模型","Agent 设置"].map((item,index) => <span className={index === 1 ? "active" : ""} key={item}>{item}</span>)}</div>
        <div className="backstage-main">
          <div className="backstage-top"><h2>任务运行总览</h2><span>LIVE · LOCAL FIRST</span></div>
          <div className="backstage-metrics">{metrics.map(([value,label],index) => <Reveal beat={beat} at={index > 1 ? 1 : 0} className="backstage-metric" key={label}><strong>{value}</strong><span>{label}</span></Reveal>)}</div>
          <Reveal beat={beat} at={2} className="task-table"><div><b>#1452</b><span>准备周会材料并写回文档</span><em className="run">executing</em></div><div><b>#1451</b><span>澄清线上文件上传故障</span><em className="approval">awaiting approval</em></div><div><b>#1449</b><span>同步代码 Review 结论</span><em>done</em></div></Reveal>
          <Reveal beat={beat} at={3} className="run-trace"><span>ExecutionRun #1881</span><i /><b>调查</b><i /><b>行动</b><i /><b>验证</b><i /><strong>Effects</strong></Reveal>
        </div>
      </section>
      <CodeStamp>Task · Todo · ExecutionRun · task_event · effects</CodeStamp>
      <Takeaway beat={beat} at={3}>模型拥有判断权，但每一次状态变化与外部后果都必须留下机器可见的事实。</Takeaway>
    </article>
  );
}

function DecisionProblemScene({ beat }: { beat: number }) {
  return (
    <article className="scene decision-problem-scene" data-audit>
      <SceneTitle kicker="09 / THE HARD PROBLEM" title="主动式真正难的，是分寸" note="太主动会制造噪音，太克制又没有作用；判断质量取决于它看到的世界是否真实。" />
      <section className="decision-balance">
        <Reveal beat={beat} at={0} className="balance-side noise"><span>OVER-ACT</span><strong>噪音太大</strong><p>每条输入都制造任务、消息和打扰。</p></Reveal>
        <Reveal beat={beat} at={0} className="balance-side silent"><span>UNDER-ACT</span><strong>没有作用</strong><p>漏掉承诺、阻塞和需要推进的结果。</p></Reveal>
        <div className={`balance-beam ${beat >= 1 ? "settled" : ""}`}><i /><span /></div>
        <Reveal beat={beat} at={1} className="balance-pivot"><small>DECISION</small><b>此刻该不该做？</b></Reveal>
        <Reveal beat={beat} at={2} className="decision-equation"><span>可持续更新的世界观</span><i>+</i><span>自主决策</span><i>+</i><span>行动反馈</span><strong>= ACTIVE DIGITAL TWIN</strong></Reveal>
      </section>
      <Takeaway beat={beat} at={2}>主动式不是一个触发器问题，而是一个持续状态上的决策问题。</Takeaway>
    </article>
  );
}

function SystemCoreScene({ beat }: { beat: number }) {
  const dimensions = [
    ["01", "实体与关系", "世界里有什么"],
    ["02", "当前认知", "现在分别怎样"],
    ["03", "时间化证据", "为什么这样判断"],
    ["04", "行动状态", "还有什么未闭环"],
  ];
  return (
    <article className="scene system-core-scene" data-audit>
      <SceneTitle kicker="10 / SYSTEM CORE" title="Jarvis 内核只有两件事：状态与决策" note="世界模型维护“现在是什么”，快慢脑持续判断“该不该做点什么”。" />
      <section className="core-board">
        <Reveal beat={beat} at={0} className="world-sheet">
          <div className="sheet-heading"><Marker tone="green">STATE / 世界模型</Marker><small>CONTINUOUSLY COMPILED</small></div>
          <h2>当前工作世界</h2>
          <div className="dimension-grid">
            {dimensions.map(([index, title, note]) => <div key={title} className="dimension-row"><span>{index}</span><strong>{title}</strong><small>{note}</small></div>)}
          </div>
          <div className="sheet-query"><i />进入每一次判断前，先回答：<b>此刻真实世界是什么？</b></div>
        </Reveal>

        <Reveal beat={beat} at={1} className="state-feed">
          <span>CURRENT WORLD</span><i /><b>context</b>
        </Reveal>

        <Reveal beat={beat} at={1} className="decision-console">
          <div className="console-heading"><Marker tone="purple">DECISION / 快慢脑</Marker><small>MODEL-OWNED SEMANTICS</small></div>
          <div className="brain-card fast-brain"><span>M3 · FAST</span><strong>这件事值得处理吗？</strong><p>准入 · 观察 · 放弃</p></div>
          <div className="brain-divider"><i /><b>Todo → Task</b><i /></div>
          <div className="brain-card slow-brain"><span>M5 · SLOW</span><strong>现在最合理的动作是什么？</strong><p>调查 · 重判 · 行动 · 等待 · 停止</p></div>
        </Reveal>

        <Reveal beat={beat} at={2} className="execution-loop">
          <Marker tone="blue">ACTION LOOP</Marker>
          <div className="execution-steps">
            <span><i>01</i><b>调用工具</b><small>连接现实</small></span>
            <em>→</em>
            <span><i>02</i><b>产生 Effects</b><small>改变外部世界</small></span>
            <em>→</em>
            <span><i>03</i><b>验证结果</b><small>不是“调用成功”</small></span>
            <em>→</em>
            <span className="writeback"><i>04</i><b>校正认知</b><small>写回当前世界</small></span>
          </div>
        </Reveal>

        <svg className={`core-feedback ${beat >= 2 ? "active" : ""}`} viewBox="0 0 1660 650" aria-hidden="true">
          <path d="M1470 570 C1570 570 1605 520 1605 440 L1605 155 C1605 75 1540 35 1460 35 L280 35 C190 35 130 85 130 165" />
        </svg>

        <Reveal beat={beat} at={3} className="kernel-equation">
          <span>STATE</span><i>×</i><span>DECISION</span><i>=</i><strong>PROACTIVE AGENT</strong>
        </Reveal>
      </section>
      <CodeStamp>goal.md · internal/background · internal/factengine · internal/execute</CodeStamp>
      <Takeaway beat={beat} at={3}>主动不是定时跑更多任务，而是基于最新世界，持续做出有边界的下一步判断。</Takeaway>
    </article>
  );
}

function RagVsWorldScene({ beat }: { beat: number }) {
  return (
    <article className="scene rag-scene" data-audit>
      <SceneTitle kicker="11 / RAG ≠ WORLD MODEL" title="资料池会记住历史，世界模型要跟上现实" note="项目会上线、负责人会变化、旧判断会失效；只检索相似片段，不等于知道现在怎样。" />
      <section className="rag-compare">
        <Reveal beat={beat} at={0} className="rag-pool">
          <Marker tone="amber">TRADITIONAL RAG</Marker><h2>把资料放进池子再搜索</h2>
          <div className="fragment-pool"><span>“项目尚未上线”</span><span>“负责人是 A”</span><span>“昨日讨论过风险”</span><span>“项目已上线”</span><span>“负责人变更为 B”</span></div>
          <div className="rag-query"><small>今天的问题</small><b>?</b><span>相似片段可能彼此冲突</span></div>
        </Reveal>
        <Reveal beat={beat} at={1} className="world-compiler">
          <Marker tone="green">WORLD MODEL</Marker><h2>把变化编译成当前认知</h2>
          <div className="compiled-page"><small>Project / Jarvis</small><strong>当前已上线 · 负责人 B</strong><p>风险：用户反馈进入验证阶段</p></div>
          <div className="history-ledger"><span>Fact</span><b>发生过什么</b><i>→</i><span>PageRevision</span><b>以前怎么看</b></div>
        </Reveal>
        <Reveal beat={beat} at={2} className="rag-verdict"><span>检索</span><b>找得到相关资料</b><i>≠</i><span>建模</span><strong>知道此刻真实世界是什么</strong></Reveal>
      </section>
      <Takeaway beat={beat} at={2}>工作世界持续变化；长期记忆必须能淘汰旧结论，而不是只会追加相似片段。</Takeaway>
    </article>
  );
}

const entitySpecs = [
  ["Person", "角色 · 承诺 · 协作", "entity-person"],
  ["Project", "目标 · 进展 · 风险", "entity-project"],
  ["KeyMatter", "长期推进 · 尚未闭环", "entity-matter"],
  ["Group", "稳定协作语境", "entity-group"],
  ["Resource", "文档 · 仓库 · 系统", "entity-resource"],
];

const recordCopy = {
  Summary: ["现在怎样", "当前可工作的结论；新证据出现时允许被改写。", "update-page"],
  Fact: ["发生过什么", "带主体、时间和来源追加保存，是认知可追溯的现实证据。", "append-facts"],
  PageRevision: ["以前怎么看", "实体页改写前归档旧 Summary，记录 Jarvis 的认知历史。", "archive before write"],
} as const;

type RecordKind = keyof typeof recordCopy;

function WorldAnatomyScene({ beat }: { beat: number }) {
  const [selectedRecord, setSelectedRecord] = useState<RecordKind>("Fact");
  return (
    <article className="scene anatomy-scene" data-audit>
      <SceneTitle kicker="12 / WORLD MODEL ANATOMY" title="一张图看懂整个世界模型" note="实体回答“世界里有什么”，认知账页回答“现在怎么看”，行动状态回答“正在发生什么”。" />
      <section className="anatomy-board">
        <Reveal beat={beat} at={0} className="entity-world-panel">
          <div className="panel-heading"><Marker tone="blue">ENTITY WORLD</Marker><span>所有实体围绕 Principal 坐标系组织</span></div>
          <svg className="entity-links" viewBox="0 0 1030 490" aria-hidden="true">
            <path d="M515 245 L215 115 M515 245 L815 115 M515 245 L850 380 M515 245 L515 430 M515 245 L180 380" />
            <circle cx="515" cy="245" r="172" /><circle cx="515" cy="245" r="235" />
          </svg>
          <div className="principal-node"><span>PRINCIPAL</span><strong>我</strong><small>认知坐标原点</small></div>
          {entitySpecs.map(([title, note, className]) => <div key={title} className={`entity-node ${className}`}><span>{title}</span><strong>{title === "Project" ? "Jarvis" : note}</strong><small>{title === "Project" ? note : ""}</small></div>)}
          <Reveal beat={beat} at={1} className="relation-strip"><b>RELATIONS</b><code>[文字](type:id)</code><span>目标必须真实存在</span><i>→</i><strong>Backlinks 从 Summary 派生</strong></Reveal>
        </Reveal>

        <Reveal beat={beat} at={2} className="cognition-ledger">
          <div className="ledger-heading"><Marker tone="green">COGNITION LEDGER</Marker><span>Project「Jarvis」</span></div>
          <div className="record-tabs">
            {(Object.keys(recordCopy) as RecordKind[]).map((kind) => (
              <button
                type="button"
                key={kind}
                aria-label={`查看认知记录：${kind}`}
                data-interactive
                className={`record-tab record-${kind.toLowerCase()} ${selectedRecord === kind ? "active" : ""}`}
                onPointerDown={(event) => event.stopPropagation()}
                onClick={(event) => { event.stopPropagation(); setSelectedRecord(kind); }}
              >
                <span>{kind}</span><strong>{recordCopy[kind][0]}</strong><small>{recordCopy[kind][2]}</small>
              </button>
            ))}
          </div>
          <div className={`record-inspector inspector-${selectedRecord.toLowerCase()}`}>
            <span>SELECTED / {selectedRecord}</span>
            <p>{recordCopy[selectedRecord][1]}</p>
          </div>
          <div className="ledger-rule"><i />现实事实不被覆盖，当前认知允许修正。</div>
        </Reveal>

        <Reveal beat={beat} at={3} className="action-skeleton">
          <Marker tone="amber">ACTION STATE</Marker>
          <div className="action-node todo"><span>Todo</span><strong>发现变化</strong></div><i>→</i>
          <div className="action-node task"><span>Task</span><strong>承接目标</strong></div><i>→</i>
          <div className="action-node run"><span>ExecutionRun</span><strong>行动 · 验证 · Effects</strong></div>
          <small>结构页只定义“有哪些状态”；谁改变状态，在决策流水线里解释。</small>
        </Reveal>
      </section>
      <CodeStamp>internal/domain/models.go · internal/background/reference.go · internal/domain/progress.go</CodeStamp>
      <Takeaway beat={beat} at={3}>世界模型不是第三方知识图谱，而是围绕具体实体维护的当前认知、历史证据与行动状态。</Takeaway>
    </article>
  );
}

function FactCompileScene({ beat }: { beat: number }) {
  const compileSteps = [
    ["01", "读取增量", "Message · Todo · Task"],
    ["02", "识别实体", "Person · Project · KeyMatter"],
    ["03", "读取当前认知", "Summary + 相关 Fact"],
    ["04", "比较并裁决", "ADD · UPDATE · REJUDGE · NOTHING"],
    ["05", "写回并读回", "Fact + Summary + PageRevision"],
  ];
  return (
    <article className="scene fact-compile-scene" data-audit>
      <SceneTitle kicker="13 / FACTENGINE" title="核心方法不是检索，而是持续编译" note="外部变化不是直接堆进知识池；FactEngine 先找到受影响实体，再重算当前认知。" />
      <section className="compile-board">
        <div className="compile-line" />
        {compileSteps.map(([index,title,body],step) => <Reveal beat={beat} at={Math.min(step,4)} className={`compile-step compile-step-${step + 1}`} key={title}><span>{index}</span><h2>{title}</h2><p>{body}</p></Reveal>)}
        <Reveal beat={beat} at={3} className="nothing-branch"><span>没有新信息</span><i>→</i><strong>NOTHING</strong><small>不为“显得有产出”而写入</small></Reveal>
        <Reveal beat={beat} at={4} className="compile-guards">
          <div><Marker tone="purple">CAS</Marker><b>冲突时读回最新页，再合并重写</b></div>
          <div><Marker tone="green">CURSOR</Marker><b>只有整轮成功，独立游标才推进</b></div>
          <div><Marker tone="amber">REPLAY</Marker><b>失败材料保留，下轮继续编译</b></div>
        </Reveal>
        <svg className="compile-feedback" viewBox="0 0 1640 620" aria-hidden="true"><path d="M1515 260 C1600 260 1605 525 1450 525 H210 C70 525 65 260 120 260" /></svg>
      </section>
      <CodeStamp>internal/factengine · fact_source_cursor · updated_at CAS</CodeStamp>
      <Takeaway beat={beat} at={4}>知识维护是一轮可重放的认知编译：现实证据不丢，当前结论可以被修正。</Takeaway>
    </article>
  );
}

function ContextDisclosureScene({ beat }: { beat: number }) {
  const layers = [
    ["list-pages", "先看索引", "有哪些人、项目、事项；只给页首行与更新时间"],
    ["get-page", "再读整页", "拿到一个实体当前可工作的完整认知"],
    ["list-facts", "最后下钻", "只有需要核验原因时，按主体与日期读取证据"],
  ];
  return (
    <article className="scene context-scene" data-audit>
      <SceneTitle kicker="14 / CONTEXT DISCLOSURE" title="像人一样：先看目录，再按需详查" note="默认上下文不塞事实全文；决策者先判断需要什么，再主动下钻到相应证据。" />
      <section className="context-board">
        <div className="context-stairs">
          {layers.map(([command,title,body],index) => <Reveal beat={beat} at={index} className={`context-layer context-layer-${index + 1}`} key={command}><span>0{index + 1}</span><code>{command}</code><h2>{title}</h2><p>{body}</p></Reveal>)}
        </div>
        <Reveal beat={beat} at={3} className="context-assembly">
          <div className="snapshot-card"><span>FROZEN</span><b>Todo.context_snapshot</b><p>线索准入时的完整审计背景</p></div>
          <i>+</i>
          <div className="fresh-card"><span>FRESH</span><b>Summary / Fact / Task</b><p>执行期主动读取的实时世界</p></div>
          <strong>→ M5 判断</strong>
        </Reveal>
        <Reveal beat={beat} at={3} className="context-rule">一次组装、全程携带；需要新鲜事实时补充，不在下游重拼“等价背景”。</Reveal>
      </section>
      <CodeStamp>list-pages · get-page · list-facts · Todo.context_snapshot</CodeStamp>
      <Takeaway beat={beat} at={3}>沉淀了但取不回来等于没沉淀；上下文设计决定世界模型能否真正进入判断。</Takeaway>
    </article>
  );
}

const pipelineStages = [
  { at: 0, code: "SOURCE", title: "外部变化", detail: "消息 · 会议 · 文档", tone: "neutral", className: "pipeline-source" },
  { at: 0, code: "M2", title: "原样采集", detail: "不分类，不裁决", tone: "blue", className: "pipeline-m2" },
  { at: 1, code: "M3 / FAST", title: "是否值得处理", detail: "准入 · 观察 · 放弃", tone: "amber", className: "pipeline-m3" },
  { at: 2, code: "STATE", title: "Todo → Task", detail: "发现变化 → 固化目标", tone: "purple", className: "pipeline-state" },
  { at: 3, code: "M5 / SLOW", title: "现在该怎么做", detail: "调查 · 重判 · 决策", tone: "blue", className: "pipeline-m5" },
  { at: 3, code: "TOOLS", title: "连接真实世界", detail: "查询 · 写入 · 等待", tone: "blue", className: "pipeline-tools" },
  { at: 4, code: "VERIFY", title: "验证并写回", detail: "Effects · 认知校正", tone: "green", className: "pipeline-verify" },
] as const;

function DecisionPipelineScene({ beat }: { beat: number }) {
  return (
    <article className="scene decision-scene" data-audit>
      <SceneTitle kicker="15 / FAST & SLOW BRAIN" title="快脑决定是否做，慢脑决定怎么做" note="快脑只承担低成本准入；真正的调查、重判、行动与停止，都留给执行期的语义决策。" />
      <section className="pipeline-board">
        <div className="pipeline-lane-labels"><span>现实</span><span>机械</span><span>语义</span><span>状态</span><span>语义</span><span>能力</span><span>结果</span></div>
        <svg className="pipeline-route" viewBox="0 0 1640 360" aria-hidden="true">
          <path className={beat >= 0 ? "active" : ""} d="M120 170 H345" />
          <path className={beat >= 1 ? "active" : ""} d="M345 170 H570" />
          <path className={beat >= 2 ? "active" : ""} d="M570 170 H795" />
          <path className={beat >= 3 ? "active" : ""} d="M795 170 H1245" />
          <path className={beat >= 4 ? "active success" : ""} d="M1245 170 H1470" />
        </svg>
        <div className="pipeline-columns">
          {pipelineStages.map((stage) => <Reveal key={stage.code} beat={beat} at={stage.at} className={`pipeline-stage ${stage.className} tone-${stage.tone}`}><span>{stage.code}</span><strong>{stage.title}</strong><p>{stage.detail}</p></Reveal>)}
        </div>

        <Reveal beat={beat} at={1} className="m3-outcomes"><span>observing</span><span>discarded</span><b>证据够了就停止，不替 M5 提前干活</b></Reveal>
        <Reveal beat={beat} at={2} className="materializer-note"><span>MATERIALIZER</span><b>extracted Todo</b><i>→</i><strong>幂等固化 Task</strong></Reveal>
        <Reveal beat={beat} at={3} className="m5-actions"><span>INVESTIGATE</span><span>ACT</span><span>WAIT</span><span>ASK</span><span>APPROVE?</span><span>STOP</span></Reveal>

        <Reveal beat={beat} at={4} className="ownership-band">
          <div><span>MODEL</span><strong>判断语义与后果</strong><small>是否做 · 怎么做 · 是否审批</small></div>
          <div><span>CODE</span><strong>守住机器硬边界</strong><small>幂等 · 权限 · 状态 · 调度</small></div>
          <div><span>TOOLS</span><strong>连接并改变现实</strong><small>查询候选 → 读取细节 → 执行 → 验证</small></div>
        </Reveal>
        <Reveal beat={beat} at={4} className="pipeline-feedback"><i>↺</i><span>验证结果成为下一轮世界模型证据</span></Reveal>
      </section>
      <CodeStamp>internal/clue · internal/extract · internal/materialize · internal/execute · internal/toolcatalog</CodeStamp>
      <Takeaway beat={beat} at={4}>同一套通用流水线处理所有来源；来源差异进入上下文和 Skill，不进入 Go 专用分支。</Takeaway>
    </article>
  );
}

function ClearBrainScene({ beat }: { beat: number }) {
  const workers = [
    ["CODE", "扫描仓库与提交", "只读证据"],
    ["DOC", "展开长文档与引用", "只读证据"],
    ["TRACE", "核验日志与链路", "只读证据"],
  ];
  return (
    <article className="scene clear-brain-scene" data-audit>
      <SceneTitle kicker="16 / KEEP THE BRAIN CLEAR" title="把资料扫描分出去，把裁决留在主脑" note="长程任务会不断引入消息、文档和代码；证据整理可以并行，目标、风险和最终动作不能被稀释。" />
      <section className="brain-board">
        <Reveal beat={beat} at={0} className="main-brain"><span>MAIN AGENT</span><strong>保持目标清醒</strong><div><b>目标判断</b><b>风险判断</b><b>最终裁决</b></div></Reveal>
        {workers.map(([tag,title,note],index) => <Reveal beat={beat} at={index + 1} className={`worker-card worker-${index + 1}`} key={tag}><span>{tag}</span><h2>{title}</h2><p>{note}</p><i>→ evidence</i></Reveal>)}
        <svg viewBox="0 0 1640 610" aria-hidden="true"><path d="M320 150 C520 150 560 300 760 300 M320 305 H760 M320 460 C520 460 560 300 760 300" /><path className="decision-path" d="M930 300 H1470" /></svg>
        <Reveal beat={beat} at={3} className="effect-gate"><small>REAL-WORLD EFFECT</small><b>发消息 · 改共享文档 · 提交代码</b><strong>主 Agent 根据具体后果决定是否先审批</strong></Reveal>
        <Reveal beat={beat} at={3} className="brain-rule">调查可以拆，判断权不能拆。</Reveal>
      </section>
      <Takeaway beat={beat} at={3}>子任务负责扩大证据面；主 Agent 始终持有目标、风险与停止条件。</Takeaway>
    </article>
  );
}

function OwnershipScene({ beat }: { beat: number }) {
  const roles = [
    ["MODEL", "负责语义", "是否行动 · 如何行动 · 是否需要审批", ["Prompt", "Rules", "Context"]],
    ["CODE", "负责硬边界", "幂等 · 权限 · 调度 · 状态 · 恢复", ["Schema", "Runtime", "State"]],
    ["TOOLS", "负责连接现实", "查询候选 · 读取细节 · 写入 · 验证", ["jarvis-tools", "lark-cli", "git"]],
  ] as const;
  return (
    <article className="scene ownership-scene" data-audit>
      <SceneTitle kicker="17 / OWNERSHIP" title="提示词 + 工具，而不是为每个 Case 写代码" note="先判断语义所有权：模型决定意义，代码保证机器协议，工具把判断连接到真实世界。" />
      <section className="ownership-orbits">
        {roles.map(([tag,title,body,tokens],index) => <Reveal beat={beat} at={index} className={`owner-orbit owner-${index + 1}`} key={tag}><span>{tag}</span><h2>{title}</h2><p>{body}</p><div>{tokens.map(token => <code key={token}>{token}</code>)}</div></Reveal>)}
        <Reveal beat={beat} at={3} className="ownership-query"><small>NEW DOMAIN / NEW CASE</small><strong>新增上下文、规则或 Skill</strong><i>≠</i><span>新增 Go 专用分支</span></Reveal>
        <svg viewBox="0 0 1640 620" aria-hidden="true"><path d="M540 305 H710 M930 305 H1100" /></svg>
      </section>
      <Takeaway beat={beat} at={3}>判断属于拥有完整上下文的模型；程序只固定真正需要确定性的部分。</Takeaway>
    </article>
  );
}

function SafeActionScene({ beat }: { beat: number }) {
  return (
    <article className="scene safe-action-scene" data-audit>
      <SceneTitle kicker="18 / SAFE ACTION LOOP" title="审批不是流程阶段，而是模型随时可以选择的动作" note="风险在动作的具体后果里，不在 action_type 名字里；代码只提供停下、批准、驳回与留痕的载体。" />
      <section className="safe-action-board">
        <Reveal beat={beat} at={0} className="approval-proof approval-proof-web"><img src={approvalWebImage} alt="后台审批操作截图" /><span>WEB</span><b>页面批准 / 驳回</b></Reveal>
        <Reveal beat={beat} at={1} className="approval-proof approval-proof-chat"><img src={approvalChatImage} alt="飞书机器人审批截图" /><span>FEISHU</span><b>协作现场批准 / 驳回</b></Reveal>
        <Reveal beat={beat} at={2} className="risk-scale"><div><Marker tone="green">低干扰</Marker><b>写 Jarvis 内部账本</b><small>通常无需审批</small></div><i>≠</i><div><Marker tone="amber">外部副作用</Marker><b>代表 Principal 发言</b><small>根据具体后果先请示</small></div></Reveal>
        <Reveal beat={beat} at={3} className="effect-chain"><span>模型判断</span><i>→</i><b>awaiting_approval?</b><i>→</i><span>工具执行</span><i>→</i><span>Effects</span><i>→</i><strong>读回验证</strong><i>→</i><em>校正世界模型</em></Reveal>
        <Reveal beat={beat} at={4} className="approval-detail-shot"><img src={approvalDetailImage} alt="审批详情截图" /><span>完整提案与具体后果必须对人可见</span></Reveal>
        <Reveal beat={beat} at={4} className="effects-note"><span>EFFECTS</span><b>原样记录未知类型</b><small>留痕不是独立外部回执；关键结果仍需 read-back / verifier。</small></Reveal>
      </section>
      <Takeaway beat={beat} at={4}>模型决定要不要停下来请示；代码不替它判断风险，但必须记录真实后果。</Takeaway>
    </article>
  );
}

function GenericHorizonScene({ beat }: { beat: number }) {
  return (
    <article className="scene generic-scene" data-audit>
      <SceneTitle kicker="19 / GENERALIZE" title="内核与领域无关，差距在全局调度" note="设置一个新领域，本质是定义世界模型、快脑准入规则和慢脑执行规则；同一个 Agent 内核继续复用。" />
      <section className="generic-board">
        <Reveal beat={beat} at={0} className="domain-config"><Marker tone="amber">DOMAIN CONFIG</Marker><div><span>世界模型</span><span>快脑规则</span><span>慢脑规则</span><span>领域工具</span></div></Reveal>
        <Reveal beat={beat} at={1} className="generic-kernel"><small>REUSABLE CORE</small><strong>STATE</strong><i>×</i><strong>DECISION</strong><i>→</i><b>ACTION LOOP</b></Reveal>
        <svg viewBox="0 0 1640 620" aria-hidden="true"><path d="M380 265 H640 M1000 265 H1260" /></svg>
        <Reveal beat={beat} at={2} className="domain-outcomes"><span>Lark 协作</span><span>研发 Agent</span><span>个人工作流</span></Reveal>
        <Reveal beat={beat} at={2} className="coverage-gauge"><small>当前可处理工作</small><div><i style={{ width: "40%" }} /></div><strong>10–20%</strong><span>目标 30–40%</span></Reveal>
        <Reveal beat={beat} at={3} className="scheduler-gap"><Marker tone="red">CURRENT GAP</Marker><h2>还不会真正做资源 / 优先级决策</h2><p>更多依赖事件与定时触发；尚缺“从所有候选工作中选择最重要一件”的全局调度。</p></Reveal>
      </section>
      <Takeaway beat={beat} at={3}>下一步不是再接十个 Case，而是让系统知道有限资源此刻最该投向哪里。</Takeaway>
    </article>
  );
}

function ClosingScene({ beat }: { beat: number }) {
  return (
    <article className="scene closing-scene" data-audit>
      <div className="closing-copy">
        <Reveal beat={beat} at={0}><p>JARVIS / THE BET</p></Reveal>
        <Reveal beat={beat} at={0}><h2>主动式 Agent 最难的，<br />不是把活干完。</h2></Reveal>
        <Reveal beat={beat} at={1}><h1>而是在绝大多数时刻，<br /><em>正确地什么都不做。</em></h1></Reveal>
        <Reveal beat={beat} at={2} className="closing-open"><span>预计近期开源</span><i>·</i><span>支持安装</span><i>·</i><strong>欢迎一起讨论</strong></Reveal>
      </div>
      <div className="closing-radar"><div /><div /><div /><Reveal beat={beat} at={1}><span>NOTHING</span><small>世界仍在变化<br />系统保持安静</small></Reveal></div>
      <Reveal beat={beat} at={2} className="closing-formula">STATE × DECISION × FEEDBACK</Reveal>
    </article>
  );
}

export const deckRegistry: SceneDefinition[] = [
  { id: "opening", title: "主动式数字分身探索实践", section: "开场", beats: 3, profile: "speaker-led", theme: "whiteboard", render: (beat) => <OpeningScene beat={beat} /> },
  { id: "origin-loop", title: "从 OpenClaw 到自建 Agent Loop", section: "起点", beats: 4, profile: "hybrid", theme: "whiteboard", render: (beat) => <OriginLoopScene beat={beat} /> },
  { id: "active-not-chatbot", title: "不是等人提问的机器人", section: "定位", beats: 3, profile: "speaker-led", theme: "whiteboard", render: (beat) => <ActiveNotChatbotScene beat={beat} /> },
  { id: "capability-map", title: "六类能力", section: "能力", beats: 4, profile: "hybrid", theme: "whiteboard", render: (beat) => <CapabilityMapScene beat={beat} /> },
  { id: "code-loop", title: "问题到代码交付", section: "能力", beats: 4, profile: "hybrid", theme: "whiteboard", render: (beat) => <CodeLoopScene beat={beat} /> },
  { id: "ambient-feishu", title: "任务留在协作现场", section: "能力", beats: 3, profile: "reading-first", theme: "whiteboard", render: (beat) => <AmbientFeishuScene beat={beat} /> },
  { id: "collaboration-proof", title: "会议 Oncall 审批", section: "能力", beats: 4, profile: "hybrid", theme: "whiteboard", render: (beat) => <CollaborationProofScene beat={beat} /> },
  { id: "backstage", title: "主动行为可追溯", section: "能力", beats: 4, profile: "reading-first", theme: "whiteboard", render: (beat) => <BackstageScene beat={beat} /> },
  { id: "decision-problem", title: "主动式真正难的是分寸", section: "核心设计", beats: 3, profile: "speaker-led", theme: "whiteboard", render: (beat) => <DecisionProblemScene beat={beat} /> },
  { id: "system-core", title: "状态与决策", section: "核心设计", beats: 4, profile: "hybrid", theme: "whiteboard", render: (beat) => <SystemCoreScene beat={beat} /> },
  { id: "rag-vs-world", title: "RAG 不等于世界模型", section: "世界模型", beats: 3, profile: "hybrid", theme: "whiteboard", render: (beat) => <RagVsWorldScene beat={beat} /> },
  { id: "world-anatomy", title: "世界模型解剖", section: "世界模型", beats: 4, profile: "reading-first", theme: "whiteboard", render: (beat) => <WorldAnatomyScene beat={beat} /> },
  { id: "fact-compile", title: "FactEngine 持续编译", section: "世界模型", beats: 5, profile: "reading-first", theme: "whiteboard", render: (beat) => <FactCompileScene beat={beat} /> },
  { id: "context-disclosure", title: "渐进式上下文披露", section: "世界模型", beats: 4, profile: "hybrid", theme: "whiteboard", render: (beat) => <ContextDisclosureScene beat={beat} /> },
  { id: "decision-pipeline", title: "快慢脑流水线", section: "决策执行", beats: 5, profile: "hybrid", theme: "whiteboard", render: (beat) => <DecisionPipelineScene beat={beat} /> },
  { id: "clear-brain", title: "保持决策大脑清醒", section: "决策执行", beats: 4, profile: "hybrid", theme: "whiteboard", render: (beat) => <ClearBrainScene beat={beat} /> },
  { id: "ownership", title: "模型代码工具分工", section: "决策执行", beats: 4, profile: "reading-first", theme: "whiteboard", render: (beat) => <OwnershipScene beat={beat} /> },
  { id: "safe-action", title: "审批与行动闭环", section: "决策执行", beats: 5, profile: "reading-first", theme: "whiteboard", render: (beat) => <SafeActionScene beat={beat} /> },
  { id: "generic-horizon", title: "通用内核与当前差距", section: "展望", beats: 4, profile: "hybrid", theme: "whiteboard", render: (beat) => <GenericHorizonScene beat={beat} /> },
  { id: "closing", title: "正确地什么都不做", section: "收束", beats: 3, profile: "speaker-led", theme: "whiteboard", render: (beat) => <ClosingScene beat={beat} /> },
];
