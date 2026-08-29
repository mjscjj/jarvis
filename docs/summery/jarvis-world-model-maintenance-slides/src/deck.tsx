import { useState, type ReactNode } from "react";

export type AuditProfile = "speaker-led" | "reading-first" | "hybrid";
export type PreviewTheme = "whiteboard";

export type SceneDefinition = {
  id: string;
  title: string;
  section: string;
  beats: number;
  profile: AuditProfile;
  theme: PreviewTheme;
  render: (beat: number) => ReactNode;
};

function Reveal({ beat, at, className = "", children }: { beat: number; at: number; className?: string; children: ReactNode }) {
  return <div className={`reveal ${beat >= at ? "show" : ""} ${className}`}>{children}</div>;
}

function SceneTitle({ kicker, title, note, compact = false }: { kicker: string; title: string; note?: string; compact?: boolean }) {
  return (
    <header className={`scene-title ${compact ? "compact" : ""}`}>
      <p>{kicker}</p>
      <h1>{title}</h1>
      {note && <span>{note}</span>}
    </header>
  );
}

function Marker({ tone = "blue", children }: { tone?: "blue" | "green" | "amber" | "purple" | "red" | "gray"; children: ReactNode }) {
  return <span className={`marker marker-${tone}`}>{children}</span>;
}

function Takeaway({ beat, at, children }: { beat: number; at: number; children: ReactNode }) {
  return <Reveal beat={beat} at={at} className="takeaway"><span>{children}</span></Reveal>;
}

function CodeStamp({ children }: { children: ReactNode }) {
  return <span className="code-stamp">{children}</span>;
}

function Arrow({ active = true, vertical = false }: { active?: boolean; vertical?: boolean }) {
  return <span className={`diagram-arrow ${vertical ? "vertical" : ""} ${active ? "active" : ""}`} aria-hidden="true"><i /></span>;
}

function OpeningScene({ beat }: { beat: number }) {
  return (
    <article className="scene opening-scene" data-audit>
      <div className="opening-copy">
        <Reveal beat={beat} at={0}><Marker>JARVIS / WORLD MODEL</Marker></Reveal>
        <Reveal beat={beat} at={0}><h1>Jarvis 如何建立并维护<br />自己的世界模型</h1></Reveal>
        <Reveal beat={beat} at={1} className="opening-thesis">
          <p>它不是一座等待检索的记忆仓库，</p>
          <strong>而是一份持续编译的当前工作世界。</strong>
        </Reveal>
      </div>
      <Reveal beat={beat} at={0} className="world-orbit opening-world">
        <div className="orbit-ring ring-a" />
        <div className="orbit-ring ring-b" />
        <div className="world-center"><span>Principal</span><strong>我</strong><small>坐标原点</small></div>
        {[
          ["Person", "人", "orbit-person"],
          ["Project", "项目", "orbit-project"],
          ["KeyMatter", "关键事", "orbit-matter"],
          ["Group", "群", "orbit-group"],
          ["Resource", "资源", "orbit-resource"],
        ].map(([type, label, className], index) => (
          <div key={type} className={`orbit-node ${className} ${beat >= (index < 2 ? 0 : 1) ? "show" : ""}`}><small>{type}</small><b>{label}</b></div>
        ))}
        <div className={`orbit-action ${beat >= 2 ? "show" : ""}`}><span>Todo</span><i>→</i><span>Task</span><i>→</i><span>Run</span></div>
      </Reveal>
      <Takeaway beat={beat} at={2}>世界模型 = 实体与关系 + 当前认知 + 时间化证据 + 行动状态</Takeaway>
    </article>
  );
}

function RagBreaksScene({ beat }: { beat: number }) {
  const cards = ["群聊消息", "文档片段", "任务结果", "会议记录", "代码变更"];
  return (
    <article className="scene rag-scene" data-audit>
      <SceneTitle kicker="01 / WHY" title="工作世界一直在变，记忆池却只会继续堆积" note="普通 RAG 能找到材料，但不能自动维护一个正确、当前、可追溯的现实。" />
      <section className="rag-stage">
        <Reveal beat={beat} at={0} className="rag-pile">
          <Marker tone="gray">RAG POOL</Marker>
          {cards.map((card, index) => <div key={card} className="rag-paper" style={{ transform: `rotate(${(index - 2) * 2.4}deg) translate(${index * 4}px, ${index * -5}px)` }}><span>{card}</span><small>fragment_{index + 1}</small></div>)}
        </Reveal>
        <Reveal beat={beat} at={1} className="rag-problem problem-a"><b>01</b><strong>没有结构</strong><span>同一个人、项目和群被拆散在材料里</span></Reveal>
        <Reveal beat={beat} at={2} className="rag-problem problem-b"><b>02</b><strong>不会更新</strong><span>新进展只会新增，不会纠正旧结论</span></Reveal>
        <Reveal beat={beat} at={3} className="rag-problem problem-c"><b>03</b><strong>无法忘记</strong><span>过期判断继续参与下一轮决策</span></Reveal>
        <Reveal beat={beat} at={3} className="rag-current-world">
          <Marker tone="green">CURRENT WORLD</Marker>
          <h2>需要维护的不是材料，<br />而是“现在应该如何理解世界”。</h2>
          <div><span>旧结论</span><i>重新判断</i><strong>当前结论</strong></div>
        </Reveal>
      </section>
      <Takeaway beat={beat} at={3}>检索回答“哪份材料相关”；世界模型回答“此刻什么是真的”。</Takeaway>
    </article>
  );
}

function FormulaScene({ beat }: { beat: number }) {
  const terms = [
    { title: "实体与关系", note: "世界里有什么", tone: "blue", at: 0 },
    { title: "当前认知", note: "现在是什么", tone: "green", at: 0 },
    { title: "时间化证据", note: "发生过什么", tone: "amber", at: 1 },
    { title: "行动状态", note: "还有什么未闭环", tone: "purple", at: 2 },
  ] as const;
  return (
    <article className="scene formula-scene" data-audit>
      <SceneTitle kicker="02 / DEFINITION" title="世界模型不是一张表，而是四类语义共同成立" />
      <section className="formula-board">
        {terms.map((term, index) => (
          <div className="formula-slot" key={term.title}>
            <Reveal beat={beat} at={term.at} className={`formula-term term-${term.tone}`}>
              <span>0{index + 1}</span><strong>{term.title}</strong><small>{term.note}</small>
            </Reveal>
            {index < terms.length - 1 && <Reveal beat={beat} at={term.at} className="formula-plus">+</Reveal>}
          </div>
        ))}
        <Reveal beat={beat} at={3} className="formula-equals">=</Reveal>
        <Reveal beat={beat} at={3} className="formula-result"><span>JARVIS</span><strong>当前工作世界</strong><small>下一轮判断的现实底座</small></Reveal>
      </section>
      <Reveal beat={beat} at={3} className="formula-caption"><span>世界观</span>则是 Principal 围绕当前目标，对这份世界模型形成的一次动态投影。</Reveal>
      <Takeaway beat={beat} at={3}>结构告诉 Jarvis“看谁”；证据和认知告诉它“现在该怎么判断”。</Takeaway>
    </article>
  );
}

function TwoWorldsScene({ beat }: { beat: number }) {
  return (
    <article className="scene two-worlds-scene" data-audit>
      <SceneTitle kicker="03 / TWO SUBJECTS" title="长期实体与实时行动，是两个不同的建模主体" note="把它们混成一张大表，会同时丢掉长期认知和执行过程。" />
      <section className="two-worlds-board">
        <Reveal beat={beat} at={0} className="long-world-panel">
          <Marker tone="blue">PERSISTENT ENTITY WORLD</Marker>
          <h2>长期实体世界</h2><p>相对稳定的工作坐标</p>
          <div className="entity-mini-map"><strong>Principal</strong>{["Person", "Project", "KeyMatter", "Group", "Resource"].map((x) => <span key={x}>{x}</span>)}</div>
          <small>每个实体是一页可持续修正的当前认知</small>
        </Reveal>
        <Reveal beat={beat} at={1} className="worlds-divider"><span>同一现实</span><i /></Reveal>
        <Reveal beat={beat} at={2} className="action-world-panel">
          <Marker tone="purple">ACTIVE ACTION STATE</Marker>
          <h2>实时行动状态</h2><p>一次变化如何走向闭环</p>
          <div className="action-mini-track"><span>Todo<small>值得关注</small></span><Arrow /><span>Task<small>承接目标</small></span><Arrow /><span>Run<small>执行到了哪里</small></span></div>
          <small>它们不是长期实体，却共同定义“尚未闭环的现实”</small>
        </Reveal>
        <Reveal beat={beat} at={3} className="worlds-bridge"><span>实体提供坐标</span><b>行动改变世界</b><span>结果反向更新认知</span></Reveal>
      </section>
      <Takeaway beat={beat} at={3}>世界模型同时回答：世界里有什么，以及正在发生什么。</Takeaway>
    </article>
  );
}

type EntitySpec = { id: string; label: string; purpose: string; example: string; className: string };
const entitySpecs: EntitySpec[] = [
  { id: "person", label: "Person / 人", purpose: "角色、职责、承诺与协作关系", example: "王磊（示例人物）", className: "atlas-person" },
  { id: "project", label: "Project / 项目", purpose: "目标、进展、风险与仓库", example: "Jarvis", className: "atlas-project" },
  { id: "matter", label: "KeyMatter / 关键事", purpose: "长期推进且尚未闭环的议题", example: "讲清实体层与行动层", className: "atlas-matter" },
  { id: "group", label: "Group / 群", purpose: "协作场与稳定群上下文", example: "Jarvis 项目群", className: "atlas-group" },
  { id: "resource", label: "Resource / 资源", purpose: "文档、仓库、系统入口", example: "《主动式数字分身》", className: "atlas-resource" },
];

function EntityAtlasScene({ beat }: { beat: number }) {
  const [selected, setSelected] = useState("project");
  const current = entitySpecs.find((item) => item.id === selected)!;
  return (
    <article className="scene atlas-scene" data-audit>
      <SceneTitle kicker="04 / ENTITY MODEL" title="六类长期实体，围绕 Principal 形成工作坐标" note="实体不是固定字段模板，而是一页关于该对象的自然语言当前认知。" compact />
      <section className="atlas-board">
        <Reveal beat={beat} at={0} className="atlas-principal"><span>PRINCIPAL</span><strong>我</strong><small>责任 · 价值 · 边界</small></Reveal>
        <div className="atlas-rings" aria-hidden="true"><i /><i /></div>
        {entitySpecs.map((entity, index) => (
          <button
            key={entity.id}
            type="button"
            className={`atlas-node ${entity.className} ${selected === entity.id ? "active" : ""} ${beat >= Math.min(3, Math.floor(index / 2) + 1) ? "show" : ""}`}
            aria-label={`查看实体：${entity.label}`}
            data-interactive
            onPointerDown={(event) => event.stopPropagation()}
            onClick={(event) => { event.stopPropagation(); setSelected(entity.id); }}
          ><span>{entity.label.split(" / ")[0]}</span><strong>{entity.label.split(" / ")[1]}</strong><small>{entity.example}</small></button>
        ))}
        <Reveal beat={beat} at={3} className="atlas-detail">
          <Marker tone="green">当前选中</Marker><h2>{current.label}</h2><p>{current.purpose}</p><small>示例：{current.example}</small>
        </Reveal>
      </section>
      <CodeStamp>internal/domain/models.go</CodeStamp>
      <Takeaway beat={beat} at={3}>Principal 是坐标原点，不是普通实体节点；其它实体围绕它形成可理解的工作上下文。</Takeaway>
    </article>
  );
}

function ConcreteExampleScene({ beat }: { beat: number }) {
  const cards = [
    ["Principal", "我", "长期目标：把 Jarvis 做成主动式数字分身", "card-principal"],
    ["Person", "王磊（示例）", "角色：协作者；关系：参与 Jarvis", "card-person"],
    ["Project", "Jarvis", "目标：主动发现并完成有价值的工作", "card-project"],
    ["KeyMatter", "讲清实体层与行动层", "状态：正在完善世界模型示例", "card-matter"],
    ["Group", "Jarvis 项目群", "用途：方案讨论、评审与反馈", "card-group"],
    ["Resource", "《主动式数字分身》", "类型：飞书 Wiki；归属：Jarvis", "card-resource"],
  ];
  return (
    <article className="scene example-scene" data-audit>
      <SceneTitle kicker="05 / CONCRETE EXAMPLE" title="同一个 Jarvis 语境里，每个实体实际写什么" note="卡片展示的是 Summary 示例，不是固定表单字段。" compact />
      <section className="example-grid">
        {cards.map(([type, name, content, className], index) => (
          <Reveal key={type} beat={beat} at={index < 3 ? 0 : 1} className={`example-card ${className}`}>
            <span>{type}</span><h3>{name}</h3><p>{content}</p>
          </Reveal>
        ))}
        <Reveal beat={beat} at={2} className="relationship-strip"><b>关系示例</b><span>Principal 主导 Project</span><i>·</i><span>Person 协作 Project</span><i>·</i><span>KeyMatter 属于 Project</span><i>·</i><span>Resource 承载项目资料</span></Reveal>
        <Reveal beat={beat} at={3} className="example-action-ribbon"><Marker tone="amber">正在发生</Marker><span>Todo：发现图例缺少实例</span><Arrow /><span>Task：补齐世界模型例子</span><Arrow /><span>Run：生成并验收 Slides</span></Reveal>
      </section>
      <Takeaway beat={beat} at={3}>抽象实体只有落到具体内容、归属与关系，才能成为 Agent 的判断坐标。</Takeaway>
    </article>
  );
}

function RelationsScene({ beat }: { beat: number }) {
  return (
    <article className="scene relations-scene" data-audit>
      <SceneTitle kicker="06 / RELATIONS" title="关系不是另一张图谱表，而是实体页中的可验证引用" note="可读的 Markdown 链接同时承担语义表达、目标校验和反向关系推导。" compact />
      <section className="relations-board">
        <Reveal beat={beat} at={0} className="summary-code-card">
          <Marker tone="green">PROJECT SUMMARY</Marker>
          <h2>Project「Jarvis」</h2>
          <pre><span>负责人：</span>[我](principal:1){"\n"}<span>协作者：</span>[王磊](person:12){"\n"}<span>关键议题：</span>[实体与行动模型](key_matter:7){"\n"}<span>资料：</span>[主动式数字分身](resource:31)</pre>
        </Reveal>
        <Reveal beat={beat} at={1} className="relation-parser"><span>① PARSE</span><strong>[文字](type:id)</strong><small>从 Summary 提取实体 URI</small></Reveal>
        <Reveal beat={beat} at={2} className="relation-validator"><span>② VALIDATE</span><strong>目标必须真实存在</strong><small>缺失引用直接报错，不静默吞掉</small></Reveal>
        <Reveal beat={beat} at={3} className="backlink-board"><Marker tone="blue">BACKLINKS</Marker><h3>谁引用了这个实体？</h3><div><span>Person / 王磊</span><i>→</i><b>Project / Jarvis</b></div><div><span>KeyMatter / 实体与行动模型</span><i>→</i><b>Project / Jarvis</b></div><small>扫描实体 Summary 派生，不维护第二份关系真源</small></Reveal>
        <Reveal beat={beat} at={3} className="no-relation-table"><span>×</span><b>没有独立 RelationFact 表</b></Reveal>
      </section>
      <CodeStamp>internal/background/reference.go</CodeStamp>
      <Takeaway beat={beat} at={3}>一份可读 Summary，就是关系语义的唯一真源。</Takeaway>
    </article>
  );
}

type RecordKind = "Summary" | "Fact" | "PageRevision";
function RecordCard({ kind, title, body, selected, onSelect }: { kind: RecordKind; title: string; body: string; selected: boolean; onSelect: () => void }) {
  return (
    <button type="button" className={`record-card record-${kind.toLowerCase()} ${selected ? "active" : ""}`} aria-label={`查看记录：${kind}`} data-interactive onPointerDown={(event) => event.stopPropagation()} onClick={(event) => { event.stopPropagation(); onSelect(); }}>
      <span>{kind}</span><strong>{title}</strong><small>{body}</small>
    </button>
  );
}

function CognitionRecordsScene({ beat }: { beat: number }) {
  const [selected, setSelected] = useState<RecordKind>("Summary");
  const details: Record<RecordKind, string> = {
    Summary: "下一轮判断首先读取：Project「Jarvis」当前正在完善世界模型的解释与维护机制。",
    Fact: "2026-08-25：完成三版 Slides 视觉预览，并通过 Chromium / WebKit 检查。",
    PageRevision: "旧版曾把运行时维护画成专用同步管道；新判断已按当前代码纠正。",
  };
  return (
    <article className="scene records-scene" data-audit>
      <SceneTitle kicker="07 / COGNITIVE RECORDS" title="同一个实体挂三种记录：现在、事实与认知历史" note="它们不是三份摘要，而是三种不同的时间语义。" compact />
      <section className="records-board">
        <Reveal beat={beat} at={0} className="records-entity"><span>PROJECT</span><strong>Jarvis</strong><small>一个长期实体页</small></Reveal>
        <div className="record-links" aria-hidden="true"><i /><i /><i /></div>
        <Reveal beat={beat} at={0} className="record-slot slot-summary"><RecordCard kind="Summary" title="现在是什么" body="随可靠新证据整体修正" selected={selected === "Summary"} onSelect={() => setSelected("Summary")} /></Reveal>
        <Reveal beat={beat} at={1} className="record-slot slot-fact"><RecordCard kind="Fact" title="发生过什么" body="追加式证据，带时间与来源" selected={selected === "Fact"} onSelect={() => setSelected("Fact")} /></Reveal>
        <Reveal beat={beat} at={2} className="record-slot slot-revision"><RecordCard kind="PageRevision" title="Jarvis 曾如何理解" body="旧 Summary 的完整认知审计" selected={selected === "PageRevision"} onSelect={() => setSelected("PageRevision")} /></Reveal>
        <Reveal beat={beat} at={3} className="record-detail"><Marker tone={selected === "Fact" ? "amber" : selected === "Summary" ? "green" : "purple"}>具体示例</Marker><p>{details[selected]}</p></Reveal>
      </section>
      <CodeStamp>internal/domain/progress.go</CodeStamp>
      <Takeaway beat={beat} at={3}>Fact 记录世界怎么变；PageRevision 记录 Jarvis 的理解怎么变。</Takeaway>
    </article>
  );
}

function ActionModelScene({ beat }: { beat: number }) {
  return (
    <article className="scene action-scene" data-audit>
      <SceneTitle kicker="08 / ACTION MODEL" title="行动状态把“变化”一路承接到“执行结果”" note="Todo、Task、ExecutionRun 是独立运行记录，不是实体 Summary 的字段。" compact />
      <section className="action-track">
        <Reveal beat={beat} at={0} className="action-card todo-card"><span>TODO</span><strong>发现了什么值得关注</strong><p>“世界模型图例缺少具体实例”</p><small>变化线索</small></Reveal>
        <Arrow active={beat >= 1} />
        <Reveal beat={beat} at={1} className="action-card task-card"><span>TASK</span><strong>正在承接什么目标</strong><p>“结合代码做一套可讲解 Slides”</p><small>完整目标 + 冻结背景</small></Reveal>
        <Arrow active={beat >= 2} />
        <Reveal beat={beat} at={2} className="action-card run-card"><span>EXECUTION RUN</span><strong>单次执行到了哪里</strong><p>读取文档 → 对照代码 → 生成 → 验证</p><small>过程、结果与 effects</small></Reveal>
        <Reveal beat={beat} at={3} className="action-world-effect"><Marker tone="green">世界已改变</Marker><span>结果成为 TaskEvent</span><Arrow /><span>下一轮维护证据</span></Reveal>
        <Reveal beat={beat} at={3} className="action-entity-links"><span>Project「Jarvis」</span><i>·</i><span>KeyMatter「世界模型 Slides」</span><i>·</i><span>Resource「目标 Wiki」</span></Reveal>
      </section>
      <Takeaway beat={beat} at={3}>实体提供长期坐标；行动记录尚未闭环的现实，并把执行结果重新送回世界。</Takeaway>
    </article>
  );
}

function EvidenceSourcesScene({ beat }: { beat: number }) {
  const lanes = [
    { name: "Message", note: "相关且允许进入记忆的群消息", tone: "blue" },
    { name: "TodoEvent", note: "线索准入、观察与状态变化", tone: "amber" },
    { name: "TaskEvent", note: "目标变化、执行结果与 effects", tone: "purple" },
  ];
  return (
    <article className="scene evidence-scene" data-audit>
      <SceneTitle kicker="09 / SOURCE PROJECTION" title="三类变化材料，被机械投影成同一种 SourceUnit" note="来源可以不同，但下游只接收一套自然语言证据协议。" compact />
      <section className="evidence-lanes">
        {lanes.map((lane, index) => (
          <Reveal beat={beat} at={index === 0 ? 0 : 1} className={`evidence-lane lane-${lane.tone}`} key={lane.name}><span>0{index + 1}</span><strong>{lane.name}</strong><p>{lane.note}</p><i /></Reveal>
        ))}
        <Reveal beat={beat} at={2} className="source-unit-envelope"><Marker tone="green">SHARED PROTOCOL</Marker><h2>SourceUnit</h2><div><span>source / key / occurred_at</span><span>known subject hints</span><strong>完整自然语言 body</strong></div><small>一条材料不拆、不按字符截断</small></Reveal>
        <Reveal beat={beat} at={3} className="maintenance-session"><span>ONE COMPLETE SESSION</span><strong>World Maintenance Agent</strong><p>程序只投影材料；什么算新事实、是否需要写入，由模型判断。</p></Reveal>
      </section>
      <CodeStamp>internal/factengine/source.go · store.go</CodeStamp>
      <Takeaway beat={beat} at={3}>来源差异停在投影层；世界认知维护始终只有一套语义协议。</Takeaway>
    </article>
  );
}

function CompileLoopScene({ beat }: { beat: number }) {
  const steps = [
    ["01", "列索引", "list-pages", "loop-list"],
    ["02", "读整页", "get-page", "loop-read"],
    ["03", "对照证据", "重新判断", "loop-compare"],
    ["04", "写当前认知", "update-page", "loop-write"],
    ["05", "追加事实", "append-facts", "loop-facts"],
    ["06", "读回验证", "get-page", "loop-verify"],
  ];
  return (
    <article className="scene compile-scene" data-audit>
      <SceneTitle kicker="10 / COMPILE LOOP" title="核心方法是编译：先读当前世界，再决定它是否需要改变" note="FactEngine 不是摘要器，也不是任务执行器；它维护的是实体的当前长期认知。" compact />
      <section className="compile-loop">
        <svg viewBox="0 0 940 610" aria-hidden="true"><path className={beat >= 1 ? "active" : ""} d="M470 75 C700 75 840 215 840 305 C840 470 690 545 470 545 C245 545 100 460 100 305 C100 160 245 75 470 75 Z" /></svg>
        <Reveal beat={beat} at={0} className="compile-core"><span>FACT ENGINE</span><strong>认知维护 Agent</strong><small>一轮处理一个完整世界维护会话</small></Reveal>
        {steps.map(([number, title, command, className], index) => (
          <div key={number} className={`compile-step ${className} ${beat >= Math.min(3, Math.floor(index / 2) + 1) ? "show" : ""}`}><span>{number}</span><strong>{title}</strong><small>{command}</small></div>
        ))}
        <Reveal beat={beat} at={3} className="compile-nothing"><b>NOTHING</b><span>证据没有带来新认知时，不为了“有产出”而写入。</span></Reveal>
        <Reveal beat={beat} at={4} className="compile-success"><Marker tone="green">SESSION SUCCESS</Marker><strong>写入完成 + 读回完成</strong><span>之后才推进各来源游标</span></Reveal>
      </section>
      <CodeStamp>conf/prompts/fact-extract-system-prompt.md</CodeStamp>
      <Takeaway beat={beat} at={4}>编译不是把新材料拼上去，而是在完整旧认知上重新形成当前结论。</Takeaway>
    </article>
  );
}

function SemanticOutcomesScene({ beat }: { beat: number }) {
  const outcomes = [
    { cls: "outcome-nothing", mark: "—", title: "NOTHING", question: "没有新增认知", action: "不写入，保持当前状态", at: 0 },
    { cls: "outcome-fact", mark: "+", title: "新事实", question: "现实发生了新变化", action: "追加 Fact；Summary 可不变", at: 1 },
    { cls: "outcome-summary", mark: "↗", title: "认知前进", question: "当前理解需要补充", action: "追加 Fact，并重写 Summary", at: 2 },
    { cls: "outcome-revision", mark: "↺", title: "推翻旧认知", question: "旧判断已不成立", action: "改写 Summary；旧页留作 PageRevision", at: 3 },
  ];
  return (
    <article className="scene outcomes-scene" data-audit>
      <SceneTitle kicker="11 / SEMANTIC ROUTING" title="同一条新证据，可能产生四种完全不同的结果" note="分支由维护 Agent 根据上下文判断，不由 Go 枚举原因码。" compact />
      <section className="outcomes-grid">
        {outcomes.map((item, index) => (
          <Reveal beat={beat} at={item.at} className={`semantic-outcome ${item.cls}`} key={item.title}><span className="outcome-number">0{index + 1}</span><i>{item.mark}</i><h2>{item.title}</h2><strong>{item.question}</strong><p>{item.action}</p></Reveal>
        ))}
        <Reveal beat={beat} at={3} className="outcomes-axis"><span>世界没变</span><i>证据改变认知的程度</i><span>旧认知被纠正</span></Reveal>
      </section>
      <Takeaway beat={beat} at={3}>语义判断保持开放；程序只保证事实追加、页面版本和写入边界。</Takeaway>
    </article>
  );
}

function ReliabilityScene({ beat }: { beat: number }) {
  const guards = [
    ["独立游标", "Message / TodoEvent / TaskEvent 各自推进", "guard-cursor", 0],
    ["完整证据", "超预算时减小行数，不截断 SourceUnit", "guard-unit", 1],
    ["成功后推进", "维护 Agent 失败时所有材料留待重放", "guard-success", 2],
    ["CAS 写页", "IfUnchangedSince 冲突后重读、重判", "guard-cas", 3],
    ["读回验证", "确认真实当前页，不以工具返回成功代替", "guard-readback", 4],
  ];
  return (
    <article className="scene reliability-scene" data-audit>
      <SceneTitle kicker="12 / RELIABILITY" title="五个硬边界，让认知可以重放、并发和纠错" note="程序不替模型判断语义，但必须保证材料不会被吞、旧页不会被乱覆盖。" compact />
      <section className="reliability-board">
        <Reveal beat={beat} at={0} className="replay-shield"><span>FAIL</span><strong>REPLAY</strong><small>失败不是跳过</small></Reveal>
        {guards.map(([title, note, className, at], index) => (
          <Reveal beat={beat} at={Number(at)} className={`guard-card ${className}`} key={String(title)}><span>0{index + 1}</span><strong>{title}</strong><p>{note}</p></Reveal>
        ))}
        <Reveal beat={beat} at={4} className="reliability-rule"><span>事实批量写失败</span><i>→</i><strong>整轮停止</strong><i>→</i><span>不降级为单条偷偷续写</span></Reveal>
      </section>
      <CodeStamp>internal/factengine/worker.go · internal/background/page.go</CodeStamp>
      <Takeaway beat={beat} at={4}>模型负责理解世界；代码负责让一次失败永远不会伪装成已经维护完成。</Takeaway>
    </article>
  );
}

function MaintainersScene({ beat }: { beat: number }) {
  return (
    <article className="scene maintainers-scene" data-audit>
      <SceneTitle kicker="13 / OWNERSHIP" title="FactEngine 是自动主维护者；执行 Agent 只在拿到可靠新认知时补充" note="这是一套共同的世界模型工具，不是两条互相同步的专用数据管道。" compact />
      <section className="maintainers-board">
        <Reveal beat={beat} at={0} className="maintainer-primary"><Marker tone="green">PRIMARY</Marker><span>定时触发</span><h2>FactEngine</h2><strong>持续消费增量证据</strong><ul><li>读实体页</li><li>比较变化</li><li>更新 Summary / Fact</li><li>不创建或执行 Task</li></ul></Reveal>
        <Reveal beat={beat} at={1} className="maintainer-boundary"><span>同一世界模型</span><i>不是镜像同步</i></Reveal>
        <Reveal beat={beat} at={2} className="maintainer-aux"><Marker tone="purple">AUXILIARY</Marker><span>执行 / 主动巡检</span><h2>M5 · Proactive</h2><strong>辅助补写者：只维护已查证的新事实</strong><ul><li>先完成真实目标</li><li>必要时修正实体页</li><li>外部动作仍必须成为 Task</li><li>不替 FactEngine 扫描全世界</li></ul></Reveal>
        <Reveal beat={beat} at={3} className="shared-tool-lane"><span>list-pages</span><span>get-page</span><span>list-facts</span><span>update-page</span><strong>一套通用工具边界</strong></Reveal>
      </section>
      <Takeaway beat={beat} at={3}>所有权按语义划分：FactEngine 维护认知，执行 Agent 完成目标并记录它真正改变了什么。</Takeaway>
    </article>
  );
}

function ConsumptionScene({ beat }: { beat: number }) {
  return (
    <article className="scene consumption-scene" data-audit>
      <SceneTitle kicker="14 / PROGRESSIVE DISCLOSURE" title="世界模型不会一次塞满上下文，而是按判断深度逐层展开" note="先给当前结论，需要追溯时再读完整页面、反向链接与历史 Fact。" compact />
      <section className="consumption-board">
        <Reveal beat={beat} at={0} className="consumer-m3"><Marker tone="blue">M3 / ADMISSION</Marker><h2>冻结一次上下文快照</h2><p>Principal、选中 Project、群上下文、其它项目简报与资源提示</p><small>Todo.context_snapshot → Task.background</small></Reveal>
        <Reveal beat={beat} at={1} className="consumer-m5"><Marker tone="purple">M5 / EXECUTION</Marker><h2>读取新鲜行动状态</h2><p>近期 Task brief + 未闭环 Todo brief，排除当前任务自身</p><small>current world 在执行时补充</small></Reveal>
        <Reveal beat={beat} at={2} className="disclosure-lens">
          <div className="lens-level level-index"><span>LEVEL 1</span><strong>list-pages</strong><small>实体索引与当前一句话</small></div>
          <div className="lens-level level-page"><span>LEVEL 2</span><strong>get-page</strong><small>完整 Summary + links</small></div>
          <div className="lens-level level-facts"><span>LEVEL 3</span><strong>list-facts</strong><small>按主体与时间追溯证据</small></div>
        </Reveal>
        <Reveal beat={beat} at={3} className="consumption-result"><span>少量默认上下文</span><Arrow /><strong>围绕当前目标主动下钻</strong><Arrow /><span>得到足够判断证据后停止</span></Reveal>
      </section>
      <CodeStamp>internal/contextsnap · internal/execute/currentworld.go · internal/toolcatalog</CodeStamp>
      <Takeaway beat={beat} at={3}>存储保持完整，进入上下文时渐进披露；省 token 不能以丢失认知边界为代价。</Takeaway>
    </article>
  );
}

function ClosingScene({ beat }: { beat: number }) {
  return (
    <article className="scene closing-scene" data-audit>
      <div className="closing-copy">
        <Reveal beat={beat} at={0}><Marker tone="green">THE OPERATING PRINCIPLE</Marker></Reveal>
        <Reveal beat={beat} at={0}><h1>Jarvis 不是在<br /><span>检索记忆</span></h1></Reveal>
        <Reveal beat={beat} at={1}><h2>而是在持续编译<br /><em>下一次决策所需的现实</em></h2></Reveal>
      </div>
      <Reveal beat={beat} at={0} className="closing-loop">
        <div className="closing-core"><span>NOW</span><strong>当前世界</strong></div>
        {[
          ["证据", "closing-evidence"],
          ["认知", "closing-cognition"],
          ["判断", "closing-decision"],
          ["行动", "closing-action"],
        ].map(([label, className], index) => <div key={label} className={`closing-node ${className} ${beat >= (index < 2 ? 0 : 1) ? "show" : ""}`}>{label}</div>)}
        <svg viewBox="0 0 650 650" aria-hidden="true"><path d="M325 55 C495 55 595 175 595 325 C595 485 480 595 325 595 C165 595 55 480 55 325 C55 165 175 55 325 55 Z" /></svg>
      </Reveal>
      <Takeaway beat={beat} at={2}>事实持续累积，认知持续修正，行动持续改变世界。</Takeaway>
    </article>
  );
}

export const deckRegistry: SceneDefinition[] = [
  { id: "opening", title: "Jarvis 世界模型", section: "开场", beats: 3, profile: "speaker-led", theme: "whiteboard", render: (beat) => <OpeningScene beat={beat} /> },
  { id: "rag-breaks", title: "为什么普通 RAG 不够", section: "问题", beats: 4, profile: "hybrid", theme: "whiteboard", render: (beat) => <RagBreaksScene beat={beat} /> },
  { id: "world-formula", title: "世界模型的定义", section: "定义", beats: 4, profile: "speaker-led", theme: "whiteboard", render: (beat) => <FormulaScene beat={beat} /> },
  { id: "two-worlds", title: "长期实体与实时行动", section: "模型", beats: 4, profile: "hybrid", theme: "whiteboard", render: (beat) => <TwoWorldsScene beat={beat} /> },
  { id: "entity-atlas", title: "六类长期实体", section: "模型", beats: 4, profile: "hybrid", theme: "whiteboard", render: (beat) => <EntityAtlasScene beat={beat} /> },
  { id: "concrete-example", title: "一个完整 Jarvis 实例", section: "模型", beats: 4, profile: "reading-first", theme: "whiteboard", render: (beat) => <ConcreteExampleScene beat={beat} /> },
  { id: "relations", title: "关系即可验证引用", section: "模型", beats: 4, profile: "hybrid", theme: "whiteboard", render: (beat) => <RelationsScene beat={beat} /> },
  { id: "cognition-records", title: "Summary、Fact 与 PageRevision", section: "模型", beats: 4, profile: "hybrid", theme: "whiteboard", render: (beat) => <CognitionRecordsScene beat={beat} /> },
  { id: "action-model", title: "Todo 到 ExecutionRun", section: "模型", beats: 4, profile: "hybrid", theme: "whiteboard", render: (beat) => <ActionModelScene beat={beat} /> },
  { id: "evidence-sources", title: "维护材料从哪里来", section: "维护", beats: 4, profile: "hybrid", theme: "whiteboard", render: (beat) => <EvidenceSourcesScene beat={beat} /> },
  { id: "compile-loop", title: "世界模型编译循环", section: "维护", beats: 5, profile: "speaker-led", theme: "whiteboard", render: (beat) => <CompileLoopScene beat={beat} /> },
  { id: "semantic-outcomes", title: "四种语义结果", section: "维护", beats: 4, profile: "hybrid", theme: "whiteboard", render: (beat) => <SemanticOutcomesScene beat={beat} /> },
  { id: "reliability", title: "维护的工程硬边界", section: "维护", beats: 5, profile: "reading-first", theme: "whiteboard", render: (beat) => <ReliabilityScene beat={beat} /> },
  { id: "maintainers", title: "谁负责维护世界模型", section: "维护", beats: 4, profile: "hybrid", theme: "whiteboard", render: (beat) => <MaintainersScene beat={beat} /> },
  { id: "consumption", title: "世界模型如何被使用", section: "使用", beats: 4, profile: "hybrid", theme: "whiteboard", render: (beat) => <ConsumptionScene beat={beat} /> },
  { id: "closing", title: "持续编译现实", section: "收束", beats: 3, profile: "speaker-led", theme: "whiteboard", render: (beat) => <ClosingScene beat={beat} /> },
];
