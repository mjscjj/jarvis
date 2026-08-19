import type { ReactNode } from "react";

export type AuditProfile = "speaker-led" | "reading-first" | "hybrid";

export type SceneDefinition = {
  id: string;
  title: string;
  section: string;
  beats: number;
  profile: AuditProfile;
  render: (beat: number) => ReactNode;
};

function R({ beat, at = 0, className = "", children }: { beat: number; at?: number; className?: string; children: ReactNode }) {
  return <div className={`reveal ${beat >= at ? "show" : ""} ${className}`}>{children}</div>;
}

function Chip({ children, tone = "teal" }: { children: ReactNode; tone?: "teal" | "cyan" | "amber" | "red" | "muted" }) {
  return <span className={`chip chip-${tone}`}>{children}</span>;
}

function Metric({ value, label, tone = "teal", note }: { value: string; label: string; tone?: string; note?: string }) {
  return (
    <div className={`metric metric-${tone}`}>
      <strong>{value}</strong>
      <span>{label}</span>
      {note && <small>{note}</small>}
    </div>
  );
}

function Opening(beat: number) {
  return (
    <div className="scene-layout opening-layout" data-audit>
      <div className="hero-copy">
        <R beat={beat}><p className="scene-kicker">JARVIS / PROACTIVE DIGITAL TWIN</p></R>
        <R beat={beat}><h1>主动式任务<br /><em>数字分身</em></h1></R>
        <R beat={beat} at={1}><p className="hero-subtitle">它不等你下单。它持续理解世界，自己决定此刻值不值得做点什么。</p></R>
      </div>
      <div className="world-orbit" aria-label="世界变化进入 Jarvis 判断核心">
        <div className="orbit orbit-outer" />
        <div className="orbit orbit-inner" />
        {[
          ["消息", "orbit-message"], ["会议", "orbit-meeting"], ["日程", "orbit-calendar"], ["代码", "orbit-code"],
        ].map(([label, className]) => <span key={label} className={`orbit-object ${className}`}>{label}</span>)}
        <div className="jarvis-core"><span>J</span><strong>判断</strong><small>该不该做 · 做什么</small></div>
        <R beat={beat} at={1} className="orbit-result"><Chip tone="teal">ACTION</Chip><Chip tone="muted">NOTHING</Chip></R>
      </div>
      <R beat={beat} at={1} className="opening-line"><span />世界变化是输入，行动只是可能的输出。</R>
    </div>
  );
}

function InputShift(beat: number) {
  return (
    <div className="scene-layout input-shift-layout" data-audit>
      <div className="scene-heading">
        <p className="scene-kicker">01 / 输入端</p>
        <h2>主动式的分水岭，<em>在输入端。</em></h2>
      </div>
      <div className="input-panels">
        <R beat={beat} className="input-panel conventional">
          <div className="input-icon">→</div><Chip tone="muted">普通 Agent</Chip>
          <h3>人给一个目标</h3>
          <p>“帮我完成这项任务。”</p>
          <div className="input-route"><span>Goal</span><i>执行</i><b>Result</b></div>
        </R>
        <R beat={beat} at={1} className="input-panel proactive">
          <div className="input-icon">⌁</div><Chip tone="teal">Jarvis</Chip>
          <h3>世界发生变化</h3>
          <p>消息、会议、日程、外部线索不断流入。</p>
          <div className="input-route"><span>World</span><i>判断</i><b>Goal?</b></div>
        </R>
      </div>
      <R beat={beat} at={2} className="takeaway-bar">别人解决“怎么做完”；Jarvis 先回答“要不要做”。</R>
    </div>
  );
}

function EvidenceFunnel(beat: number) {
  return (
    <div className="scene-layout evidence-layout" data-audit>
      <div className="scene-heading compact"><p className="scene-kicker">真实运行 / 2026-08-18</p><h2>六千条消息之后，<em>大多数没有变成工作。</em></h2></div>
      <div className="funnel-flow">
        <R beat={beat} className="funnel-stage wide"><Metric value="6,164" label="已采集消息" tone="cyan" /><small>68 个产生消息的会话</small></R>
        <R beat={beat} at={1} className="funnel-arrow">→</R>
        <R beat={beat} at={1} className="funnel-stage medium"><Metric value="1,522" label="Todo 线索" tone="amber" /><small>M3 完成准入判断</small></R>
        <R beat={beat} at={2} className="funnel-branch">
          <div className="branch-line" />
          <div className="branch-result materialized"><strong>356</strong><span>当前 materialized</span><small>23.4%</small></div>
          <div className="branch-result observing"><strong>1,166</strong><span>当前 observing</span><small>76.6%</small></div>
        </R>
      </div>
      <R beat={beat} at={2} className="evidence-note">另有主动巡视、交互、手工和定时来源；当前 Task 总数为 562。</R>
    </div>
  );
}

function HardConstraints(beat: number) {
  const twin = ["长期记忆必须是世界模型", "对外副作用必须能停下来请示"];
  const proactive = ["输入可以只是“世界变了”", "系统必须能合法地说“不做”"];
  return (
    <div className="scene-layout constraints-layout" data-audit>
      <div className="scene-heading"><p className="scene-kicker">产品定义</p><h2>两个概念，推出<em>四条硬约束。</em></h2></div>
      <div className="constraint-pillars">
        <R beat={beat} className="pillar twin-pillar"><span className="pillar-index">01</span><h3>数字分身</h3><p>它在协作现场代表 Principal，不只是一个工具。</p>{twin.map((item, index) => <R beat={beat} at={index + 1} className="constraint-row" key={item}><b>✓</b>{item}</R>)}</R>
        <R beat={beat} className="pillar proactive-pillar"><span className="pillar-index">02</span><h3>主动式</h3><p>输入不是一个订单，而是一连串外部变化。</p>{proactive.map((item, index) => <R beat={beat} at={index + 1} className="constraint-row" key={item}><b>✓</b>{item}</R>)}</R>
      </div>
    </div>
  );
}

function WorldModel(beat: number) {
  return (
    <div className="scene-layout world-layout" data-audit>
      <div className="scene-heading compact"><p className="scene-kicker">状态 / WORLD MODEL</p><h2>页答“现在是什么”，<em>事实流答“发生过什么”。</em></h2></div>
      <div className="world-dual">
        <R beat={beat} className="world-page">
          <div className="page-corner" /><Chip tone="teal">SUMMARY PAGE</Chip><h3>Agent 现在相信什么</h3>
          <div className="page-line strong" /><div className="page-line" /><div className="page-line short" />
          <div className="page-features"><span>整体读写</span><span>8,000 字符上限</span><span>CAS 防覆盖</span></div>
        </R>
        <R beat={beat} at={1} className="world-stream">
          <Chip tone="cyan">FACT STREAM</Chip><h3>世界曾经发生什么</h3>
          {["新消息进入", "Task 调查得到新证据", "旧结论被新事实推翻"].map((item, index) => <div className="stream-event" key={item}><i>{index + 1}</i><span>{item}</span><small>完整历史证据</small></div>)}
        </R>
        <R beat={beat} at={2} className="world-bridge"><span>当前状态</span><b>⇄</b><span>历史底账</span></R>
      </div>
      <R beat={beat} at={2} className="takeaway-bar">写入单位和读取单位一致，世界状态才可能持续收敛。</R>
    </div>
  );
}

function ProgressiveContext(beat: number) {
  const layers = [
    ["01", "索引", "先看有哪些人、项目、事项", "list-pages"],
    ["02", "整页", "再读一个实体的当前事实", "get-page"],
    ["03", "明细", "需要证据时按主体和日期下钻", "list-facts"],
  ];
  return (
    <div className="scene-layout progressive-layout" data-audit>
      <div className="scene-heading"><p className="scene-kicker">渐进式披露</p><h2>像人一样，<em>先看摘要，再按需详查。</em></h2></div>
      <div className="context-layers">
        {layers.map(([index, title, body, command], layer) => (
          <R beat={beat} at={layer} className={`context-layer layer-${layer + 1}`} key={title}>
            <span>{index}</span><h3>{title}</h3><p>{body}</p><code>{command}</code>
          </R>
        ))}
        <div className="context-lens"><span>J</span><small>只加载当前判断需要的证据</small></div>
      </div>
    </div>
  );
}

function Pipeline(beat: number) {
  const nodes = [
    ["M2", "采集", "存原文 · 幂等 · 唤醒", "cyan"],
    ["M3", "准入", "只判断是否值得执行", "amber"],
    ["—", "机械固化", "不调用模型", "muted"],
    ["M5", "执行", "调查 · 动作 · 验证", "teal"],
  ];
  return (
    <div className="scene-layout pipeline-layout" data-audit>
      <div className="scene-heading compact"><p className="scene-kicker">来源无关 / ONE PIPELINE</p><h2>新来源只改外围，<em>内核始终是一条链。</em></h2></div>
      <div className="pipeline-source"><span>消息</span><span>会议</span><span>日程</span><span>外部 Skill</span></div>
      <div className="pipeline-line">
        {nodes.map(([id, title, body, tone], index) => (
          <R beat={beat} at={index} className={`pipeline-node pipeline-${tone}`} key={title}>
            <small>{id}</small><h3>{title}</h3><p>{body}</p>{index < nodes.length - 1 && <div className="pipeline-connector"><i /></div>}
          </R>
        ))}
        <R beat={beat} at={2} className="observing-exit"><span>observing</span><small>保留事实，不创建 Task</small></R>
      </div>
      <R beat={beat} at={3} className="takeaway-bar">语义判断属于模型；幂等、调度、权限和状态属于代码。</R>
    </div>
  );
}

function Admission(beat: number) {
  return (
    <div className="scene-layout admission-layout" data-audit>
      <div className="admission-copy"><p className="scene-kicker">M3 / ADMISSION</p><h2>多数输入的正确答案，<br />是<em>不行动。</em></h2><R beat={beat} at={2}><p>不是丢弃：事实被保留，新的证据可以让它重新进入判断。</p></R></div>
      <div className="admission-ring">
        <div className="ring-chart"><span className="ring-value">76.6<small>%</small></span><b>observing</b></div>
        <R beat={beat} at={1} className="ring-callout action-callout"><strong>23.4%</strong><span>materialized</span></R>
        <R beat={beat} at={2} className="ring-callout quiet-callout"><strong>1,166</strong><span>当前无需行动</span></R>
      </div>
      <R beat={beat} at={1} className="admission-question">“这件事是否值得启动一次 M5？”</R>
    </div>
  );
}

function ObservingCase(beat: number) {
  return (
    <div className="scene-layout case-layout" data-audit>
      <div className="scene-heading compact"><p className="scene-kicker">真实案例 / TODO 374</p><h2>它做对的事，是判断出<em>不需要它干活。</em></h2></div>
      <div className="case-thread">
        <R beat={beat} className="thread-message"><Chip tone="cyan">NEW EVIDENCE</Chip><p>“当前工具网关还没有这种参数校验。”</p><small>群聊里的新增事实</small></R>
        <R beat={beat} at={1} className="thread-inspection"><span>调查</span><b>374</b><i>⇄</i><b>372</b><small>已有任务正在承接同一问题</small></R>
        <R beat={beat} at={2} className="thread-result"><Chip tone="muted">OBSERVING</Chip><h3>不新建重复任务</h3><p>把新增事实交给 372，避免重复推进、重复发言。</p></R>
      </div>
    </div>
  );
}

function ExecuteLoop(beat: number) {
  const steps = ["调查真实状态", "选择下一动作", "执行并读回", "重新判断是否停止"];
  return (
    <div className="scene-layout execute-layout" data-audit>
      <div className="scene-heading"><p className="scene-kicker">M5 / EXECUTION</p><h2>M5 不是计划执行器，<em>它是持续决策器。</em></h2></div>
      <div className="execute-loop">
        <div className="loop-core"><span>M5</span><small>完整上下文<br />+ 实时工具</small></div>
        {steps.map((step, index) => <R beat={beat} at={Math.min(index, 2)} className={`loop-step loop-step-${index + 1}`} key={step}><b>0{index + 1}</b><span>{step}</span></R>)}
        <svg viewBox="0 0 840 520" aria-hidden="true"><path d="M190 270 C190 90 650 90 650 270 C650 450 190 450 190 270" /></svg>
      </div>
      <R beat={beat} at={2} className="execute-outcomes"><Chip tone="teal">done</Chip><Chip tone="muted">observing</Chip><Chip tone="amber">waiting</Chip><Chip tone="red">failed</Chip></R>
    </div>
  );
}

function Approval(beat: number) {
  return (
    <div className="scene-layout approval-layout" data-audit>
      <div className="scene-heading compact"><p className="scene-kicker">APPROVAL</p><h2>风险在动作的具体内容里，<em>不在类型名里。</em></h2></div>
      <div className="risk-compare">
        <R beat={beat} className="risk-card safe"><Chip tone="teal">改代码</Chip><h3>修改一处说明文字</h3><p>可逆、范围明确、无外部承诺。</p><strong>可以直接完成</strong></R>
        <R beat={beat} at={1} className="risk-card dangerous"><Chip tone="red">发消息</Chip><h3>代表 Principal 对外承诺</h3><p>一旦发出，由本人承担关系与兑现成本。</p><strong>必须先批准</strong></R>
      </div>
      <R beat={beat} at={2} className="approval-carrier"><span>模型判断风险</span><i>→</i><b>awaiting_approval</b><i>→</i><span>人明确批准 / 驳回</span></R>
      <R beat={beat} at={2} className="approval-note">代码不判断风险，但必须记录后果。</R>
    </div>
  );
}

function Resume(beat: number) {
  const states = [
    ["waiting", "等到明确时间", "同一 Session 到期续跑"],
    ["needs_human", "只有人能补充", "回复后沿用原上下文"],
    ["awaiting_approval", "等待明确授权", "批准后进入 fresh apply run"],
  ];
  return (
    <div className="scene-layout resume-layout" data-audit>
      <div className="scene-heading"><p className="scene-kicker">LONG-RUNNING WORK</p><h2>等待不是失败，<em>恢复也不是从头来过。</em></h2></div>
      <div className="resume-track">
        <div className="resume-start"><span>executing</span><small>调查中</small></div>
        {states.map(([status, title, body], index) => <R beat={beat} at={index} className={`resume-state state-${index + 1}`} key={status}><Chip tone={index === 2 ? "amber" : "cyan"}>{status}</Chip><h3>{title}</h3><p>{body}</p></R>)}
        <div className="resume-end"><span>done</span><small>真实完成</small></div>
      </div>
    </div>
  );
}

function Patrol(beat: number) {
  return (
    <div className="scene-layout patrol-layout" data-audit>
      <div className="patrol-copy"><p className="scene-kicker">PROACTIVE HEARTBEAT</p><h2><em>NOTHING</em><br />是一个高质量结果。</h2><R beat={beat} at={2}><p>先维护未闭环事项，再看今天的新变化。没有值得推进的事，就安静结束。</p></R></div>
      <div className="patrol-radar">
        <div className="radar-circle radar-1" /><div className="radar-circle radar-2" /><div className="radar-circle radar-3" /><div className="radar-sweep" />
        {["Task", "Todo", "消息", "日程", "世界页"].map((item, index) => <R beat={beat} at={Math.min(index, 1)} className={`radar-point point-${index + 1}`} key={item}>{item}</R>)}
        <R beat={beat} at={2} className="radar-result"><span>本轮结论</span><strong>NOTHING</strong><small>系统处于健康状态</small></R>
      </div>
    </div>
  );
}

function SystemLoop(beat: number) {
  return (
    <div className="scene-layout system-loop-layout" data-audit>
      <div className="scene-heading compact"><p className="scene-kicker">THE CORE</p><h2>闭环必须<em>真的闭上。</em></h2></div>
      <div className="system-loop">
        <R beat={beat} className="system-node node-world"><small>01</small><h3>世界变化</h3><p>外部事实进入</p></R>
        <R beat={beat} at={1} className="system-node node-state"><small>02</small><h3>世界模型</h3><p>唯一长期状态</p></R>
        <R beat={beat} at={1} className="system-node node-decision"><small>03</small><h3>判断</h3><p>该不该做 · 做什么</p></R>
        <R beat={beat} at={2} className="system-node node-action"><small>04</small><h3>动作</h3><p>工具 · 审批 · 验证</p></R>
        <R beat={beat} at={2} className="system-node node-memory"><small>05</small><h3>沉淀</h3><p>结果成为新事实</p></R>
        <svg viewBox="0 0 1200 620" aria-hidden="true"><path d="M180 315 C180 70 1010 70 1010 315 C1010 550 180 550 180 315" /></svg>
        <div className="system-core">J</div>
      </div>
      <R beat={beat} at={2} className="takeaway-bar">沉淀了却取不回来，等于从未发生。</R>
    </div>
  );
}

function Proof(beat: number) {
  return (
    <div className="scene-layout proof-layout" data-audit>
      <div className="scene-heading compact"><p className="scene-kicker">RUNNING WATERLINE / 2026-08-18</p><h2>它已经持续运行；<em>还没有成熟。</em></h2></div>
      <div className="proof-grid">
        <R beat={beat}><Metric value="6,164" label="Message" tone="cyan" note="真实采集输入" /></R>
        <R beat={beat}><Metric value="1,522" label="Todo" tone="amber" note="完成准入判断" /></R>
        <R beat={beat}><Metric value="562" label="Task" tone="teal" note="五类来源" /></R>
        <R beat={beat}><Metric value="707" label="Execution Run" tone="purple" note="执行轮次" /></R>
        <R beat={beat} at={1} className="proof-status"><strong>336</strong><span>done</span></R>
        <R beat={beat} at={1} className="proof-status"><strong>145</strong><span>observing</span></R>
        <R beat={beat} at={1} className="proof-status danger"><strong>72</strong><span>failed</span></R>
        <R beat={beat} at={1} className="proof-status"><strong>9</strong><span>等批准 / 等人</span></R>
      </div>
      <R beat={beat} at={1} className="proof-caption">这些数字是水位，不是胜利宣言。失败与等待必须和完成一样可见。</R>
    </div>
  );
}

function Gaps(beat: number) {
  const gaps = [
    ["Verifier", "外部后果仍是 Agent 声明，不是独立回执"],
    ["Long-horizon control", "尚无 Goal Store / Supervisor / 独立 Verifier"],
    ["World coverage", "人物页 41/103 · 项目页 4/7 · 资料页 8/35"],
    ["Re-admission", "编辑既有飞书消息不会重新唤醒 M3"],
  ];
  return (
    <div className="scene-layout gaps-layout" data-audit>
      <div className="scene-heading"><p className="scene-kicker">BOUNDARIES</p><h2>现在还不成立的部分，<em>必须写出来。</em></h2></div>
      <div className="gap-board">
        {gaps.map(([title, body], index) => <R beat={beat} at={index > 1 ? 1 : 0} className="gap-card" key={title}><span>0{index + 1}</span><h3>{title}</h3><p>{body}</p><Chip tone="red">NOT YET</Chip></R>)}
      </div>
      <R beat={beat} at={1} className="gap-footer">可信来自诚实的边界，而不是把每个缺口都写成路线图口号。</R>
    </div>
  );
}

function Roadmap(beat: number) {
  const priorities = [
    ["状态可信", "补齐关键世界页，让新事实持续淘汰旧结论"],
    ["结果可信", "为关键外部副作用增加可核对的 receipt"],
    ["打扰可信", "只有 Principal 才能回答的问题，才进入 needs_human"],
  ];
  return (
    <div className="scene-layout roadmap-layout" data-audit>
      <div className="scene-heading compact"><p className="scene-kicker">NEXT</p><h2>下一步不是更多自动化，<em>是更可信的判断。</em></h2></div>
      <div className="roadmap-stairs">
        {priorities.map(([title, body], index) => <R beat={beat} at={index} className={`road-step step-${index + 1}`} key={title}><span>0{index + 1}</span><h3>{title}</h3><p>{body}</p></R>)}
      </div>
      <R beat={beat} at={2} className="roadmap-horizon">真实需要出现之后，再增加最小 Goal / Supervisor / Verifier。</R>
    </div>
  );
}

function Closing(beat: number) {
  return (
    <div className="scene-layout closing-layout" data-audit>
      <div className="closing-signal"><span /><span /><span /><i /></div>
      <R beat={beat}><p className="scene-kicker">JARVIS / THE BET</p></R>
      <R beat={beat}><h2>主动式 Agent 最难的，<br />不是把活干完。</h2></R>
      <R beat={beat} at={1}><h1>而是在绝大多数时刻，<br /><em>正确地什么都不做。</em></h1></R>
      <R beat={beat} at={1} className="closing-mark"><span>J</span><small>世界仍在变化。系统保持安静。</small></R>
    </div>
  );
}

export const deckRegistry: SceneDefinition[] = [
  { id: "opening", title: "主动式任务数字分身", section: "opening", beats: 2, profile: "speaker-led", render: Opening },
  { id: "input-shift", title: "分水岭在输入端", section: "position", beats: 3, profile: "speaker-led", render: InputShift },
  { id: "evidence-funnel", title: "六千条消息之后", section: "evidence", beats: 3, profile: "reading-first", render: EvidenceFunnel },
  { id: "hard-constraints", title: "两个概念，四条硬约束", section: "position", beats: 3, profile: "hybrid", render: HardConstraints },
  { id: "world-model", title: "页答现在，事实答历史", section: "architecture", beats: 3, profile: "hybrid", render: WorldModel },
  { id: "progressive-context", title: "先看摘要，再按需下钻", section: "architecture", beats: 3, profile: "speaker-led", render: ProgressiveContext },
  { id: "pipeline", title: "一条来源无关的流水线", section: "architecture", beats: 4, profile: "hybrid", render: Pipeline },
  { id: "admission", title: "多数输入的正确答案是不行动", section: "decision", beats: 3, profile: "speaker-led", render: Admission },
  { id: "observing-case", title: "Task 374 为什么没有干活", section: "evidence", beats: 3, profile: "hybrid", render: ObservingCase },
  { id: "execute-loop", title: "M5 不是计划执行器", section: "execution", beats: 3, profile: "speaker-led", render: ExecuteLoop },
  { id: "approval", title: "风险在动作内容里", section: "execution", beats: 3, profile: "hybrid", render: Approval },
  { id: "resume", title: "等待不是失败", section: "execution", beats: 3, profile: "hybrid", render: Resume },
  { id: "patrol", title: "NOTHING 是高质量结果", section: "proactive", beats: 3, profile: "speaker-led", render: Patrol },
  { id: "system-loop", title: "闭环必须真的闭上", section: "architecture", beats: 3, profile: "hybrid", render: SystemLoop },
  { id: "proof", title: "当前运行水位", section: "evidence", beats: 2, profile: "reading-first", render: Proof },
  { id: "gaps", title: "现在还不成立的部分", section: "boundary", beats: 2, profile: "reading-first", render: Gaps },
  { id: "roadmap", title: "下一步不是更多自动化", section: "roadmap", beats: 3, profile: "hybrid", render: Roadmap },
  { id: "closing", title: "正确地什么都不做", section: "closing", beats: 2, profile: "speaker-led", render: Closing },
];
