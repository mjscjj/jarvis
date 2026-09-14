import { useEffect, useMemo, useState } from 'react'
import { Select } from 'antd'
import { getWebConfig } from '../../api'
import {
  createRegionalDemand,
  deleteRegionalDemand,
  getRegionalAlignmentBoard,
  patchRegionalRecap,
  putRegionalCategoryOrder,
  putRegionalDecision,
  putRegionalRecapOrder,
  updateRegionalDemand,
} from './api'
import { CommentInteractionProvider, commentTargetElementId, scrollToCommentSource } from './commenting'
import { CommentDrawer } from './components/CommentDrawer'
import { FeishuPeoplePickerInput } from './components/FeishuPeoplePicker'
import { Images, Links } from './components/ui'
import { businessCategoryOf, priorityOf } from './hierarchy'
import { krOwners, ownerOptions } from './people'
import type {
  CommentTarget,
  Kr,
  KrOwner,
  Objective,
  PageComment,
  RegionalAlignmentBoard,
  RegionalCode,
  RegionalDemand,
  RegionalPlanDecisionItem,
} from './types'

const REGIONS: Array<{ code: RegionalCode; label: string }> = [
  { code: 'eu', label: 'EU' },
  { code: 'menat', label: 'MENAT' },
  { code: 'sea-cca', label: 'SEA&CCA' },
  { code: 'nea', label: 'NEA' },
  { code: 'ams-anz', label: 'AMS&ANZ' },
]

const CATEGORIES = ['公会业务', '运营效率', '优质主播专项', 'AI提效'] as const
type Category = typeof CATEGORIES[number]

const CATEGORY_ALIASES: Record<Category, string[]> = {
  公会业务: ['公会业务'],
  运营效率: ['运营效率'],
  优质主播专项: ['优质主播专项', '优质主播 & 内容专项', '优质主播&内容专项', '优质内容'],
  AI提效: ['AI提效', 'AI 提效'],
}

function Bilingual({ zh, en, inline = false }: { zh: string; en: string; inline?: boolean }) {
  return inline
    ? <span>{zh}<span className="ml-1 text-[10px] font-normal text-slate-400">{en}</span></span>
    : <span className="block text-xs font-medium text-slate-700"><span className="block">{zh}</span><span className="mt-0.5 block text-[10px] font-normal text-slate-400">{en}</span></span>
}

function categoryOf(kr: Kr): Category | undefined {
  const raw = businessCategoryOf(kr)
  return CATEGORIES.find((category) => CATEGORY_ALIASES[category].includes(raw))
}

function normalizedCategoryOrder(values: string[]): Category[] {
  const normalized = values.flatMap((value) => {
    const category = CATEGORIES.find((candidate) => CATEGORY_ALIASES[candidate].includes(value))
    return category ? [category] : []
  })
  return [...new Set([...normalized, ...CATEGORIES])]
}

function planIndex(objectives: Objective[]) {
  const byKr = new Map<string, { objective: Objective; kr: Kr }>()
  for (const objective of objectives) for (const kr of objective.krs) byKr.set(kr.id, { objective, kr })
  return byKr
}

function emptyDemand(sortOrder: number): Omit<RegionalDemand, 'id'> {
  return { version: 0, regionalOkr: '', item: '', requirement: '', docs: [], images: [], priority: '', regionalPocs: [], platformPocs: [], acceptance: 'tbd', planKrIds: [], deliverable: '', sortOrder }
}

function SectionTitle({ part, zh, en, action }: { part: string; zh: string; en: string; action?: React.ReactNode }) {
  return <div className="mb-5 flex flex-wrap items-end gap-3 border-b border-slate-200 pb-4">
    <div><div className="text-[10px] font-semibold uppercase tracking-[0.18em] text-indigo-500">{part}</div><h2 className="mt-1 text-lg font-semibold text-slate-900">{zh}</h2><p className="mt-1 text-xs text-slate-400">{en}</p></div>
    {action && <div className="ml-auto">{action}</div>}
  </div>
}

function CategoryTabs({ order, value, onChange, onReorder, labels = 'alignment' }: { order: Category[]; value: Category; onChange: (value: Category) => void; onReorder: (values: Category[]) => void; labels?: 'platform' | 'alignment' | 'recap' }) {
  const [dragging, setDragging] = useState<Category>()
  const label = (category: Category) => {
    if (category === '优质主播专项') return labels === 'platform' ? '优质内容' : labels === 'recap' ? '优质主播&内容专项' : '优质主播专项'
    if (category === 'AI提效' && labels === 'platform') return 'AI 提效'
    return category
  }
  return <div className="flex flex-wrap gap-1.5" aria-label="业务方向">
    {order.map((category) => <button
      key={category}
      type="button"
      draggable
      onDragStart={() => setDragging(category)}
      onDragOver={(event) => event.preventDefault()}
      onDrop={() => {
        if (!dragging || dragging === category) return
        const next = [...order]
        const from = next.indexOf(dragging)
        const to = next.indexOf(category)
        next.splice(to, 0, next.splice(from, 1)[0])
        setDragging(undefined)
        onReorder(next)
      }}
      onClick={() => onChange(category)}
      className={`cursor-grab rounded-lg border px-3 py-1.5 text-[11px] font-medium ${value === category ? 'border-indigo-300 bg-indigo-50 text-indigo-700' : 'border-slate-200 bg-white text-slate-500 hover:border-indigo-200'}`}
    >{label(category)}</button>)}
  </div>
}

function KRDetails({ kr }: { kr: Kr }) {
  return <details className="mt-2 rounded-lg border border-slate-100 bg-slate-50/60" open>
    <summary className="cursor-pointer list-none px-3 py-2 text-[11px] font-medium text-slate-700"><span className="mr-1 text-slate-300">▸</span>{kr.title}<span className="ml-2 font-normal text-slate-400">{kr.ownerName || krOwners(kr).map((owner) => owner.name).join('、') || '未填写负责人'}</span></summary>
    <div className="space-y-2 border-t border-slate-100 px-3 py-2">
      {kr.points.map((point) => <details key={point.id} open className="rounded-md bg-white px-2 py-1.5">
        <summary className="cursor-pointer text-[10px] font-medium text-slate-600">{point.kind === 'product' ? '产品具体 KR / Product KR' : '策略具体 KR / Strategy KR'}：{point.title}</summary>
        <div className="mt-1 pl-3 text-[10px] text-slate-400">POC：{(point.owners ?? []).map((owner) => owner.name).join('、') || '未填写'}</div>
        {point.entries.map((entry) => <div key={entry.id} className="mt-1 whitespace-pre-wrap pl-3 text-[10px] leading-5 text-slate-500">{entry.text}</div>)}
      </details>)}
      {kr.points.length === 0 && <div className="text-[10px] text-slate-400">暂无策略或产品具体 KR / No detailed KR</div>}
    </div>
  </details>
}

function PlanKRSelect({ value, objectives, onChange }: { value: string[]; objectives: Objective[]; onChange: (value: string[]) => void }) {
  return <Select
    mode="multiple"
    allowClear
    showSearch
    value={value}
    onChange={onChange}
    optionFilterProp="label"
    maxTagCount="responsive"
    placeholder="选择关联的 Platform KR"
    aria-label="Platform OKR"
    className="w-full"
    options={objectives.map((objective) => ({
      label: objective.title,
      options: objective.krs.map((kr) => ({ label: kr.title, value: kr.id })),
    }))}
  />
}

function DemandEditor({ initial, index, objectives, people, busy, onSave, onCancel, onDelete, onComment }: { initial: RegionalDemand | Omit<RegionalDemand, 'id'>; index?: number; objectives: Objective[]; people: KrOwner[]; busy: boolean; onSave: (value: RegionalDemand | Omit<RegionalDemand, 'id'>) => void; onCancel?: () => void; onDelete?: () => void; onComment?: () => void }) {
  const [value, setValue] = useState(initial)
  useEffect(() => setValue(initial), [initial])
  const field = (key: keyof typeof value, next: unknown) => setValue((current) => ({ ...current, [key]: next }))
  const id = 'id' in initial ? initial.id : 'new'
  const isNew = !('id' in initial)
  const inputClass = 'mt-1.5 min-h-10 w-full rounded-lg border border-slate-200 bg-white px-3 py-2 text-xs leading-5 text-slate-700 outline-none transition focus:border-indigo-400 focus:ring-2 focus:ring-indigo-100'
  const selectClass = 'mt-1.5 h-10 w-full rounded-lg border border-slate-200 bg-white px-3 text-xs text-slate-700 outline-none focus:border-indigo-400'
  const peopleClass = 'mt-1.5 flex min-h-10 items-center rounded-lg border border-slate-200 bg-white px-2.5 py-1.5'
  return <article id={commentTargetElementId({ type: 'alignment_item', id: `demand:${id}` })} className="group/entry overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-sm">
    <div className="flex flex-wrap items-center gap-3 border-b border-slate-100 px-4 py-3 sm:px-5">
      <div>
        <h3 className="text-sm font-semibold text-slate-800">{isNew ? '新增区域需求' : `区域需求 ${Number(index ?? 0) + 1}`}</h3>
        <p className="mt-0.5 text-[11px] text-slate-400">{isNew ? '先填写区域诉求，再补充平台承接信息' : value.item || value.requirement || '未命名需求'}</p>
      </div>
      <div className="ml-auto flex items-center gap-2">
        {onComment && <button type="button" onClick={onComment} className="h-8 rounded-lg border border-slate-200 px-3 text-xs text-slate-500 hover:border-indigo-200 hover:text-indigo-600">💬 评论</button>}
        {onDelete && <button type="button" disabled={busy} onClick={onDelete} className="h-8 rounded-lg border border-red-200 px-3 text-xs text-red-600 disabled:opacity-40">删除</button>}
      </div>
    </div>
    <div className="grid gap-4 p-4 sm:p-5 xl:grid-cols-[minmax(0,5fr)_minmax(0,7fr)]">
      <section className="min-w-0 rounded-xl border border-slate-200 bg-slate-50/70 p-4">
        <div className="mb-4"><h4 className="text-xs font-semibold text-slate-700">区域诉求</h4><p className="mt-0.5 text-[10px] text-slate-400">Regional input</p></div>
        <div className="grid gap-4 sm:grid-cols-2">
          <label><Bilingual zh="区域 OKR" en="Regional OKR" /><textarea value={value.regionalOkr} onChange={(event) => field('regionalOkr', event.target.value)} className={inputClass} rows={2} /></label>
          <label><Bilingual zh="事项" en="Item" /><textarea value={value.item} onChange={(event) => field('item', event.target.value)} className={inputClass} rows={2} /></label>
          <label className="sm:col-span-2"><Bilingual zh="具体需求" en="Detailed requirement" /><textarea value={value.requirement} onChange={(event) => field('requirement', event.target.value)} className={inputClass} rows={4} /><div className="mt-2 flex flex-wrap gap-2"><Links value={value.docs} onChange={(next) => field('docs', next)} /><Images value={value.images} onChange={(next) => field('images', next)} maxDisplayWidth={180} /></div></label>
        </div>
      </section>
      <section className="min-w-0 rounded-xl border border-indigo-100 bg-indigo-50/35 p-4">
        <div className="mb-4"><h4 className="text-xs font-semibold text-slate-700">平台承接</h4><p className="mt-0.5 text-[10px] text-slate-400">Platform response</p></div>
        <div className="grid gap-4 sm:grid-cols-2">
          <label><Bilingual zh="优先级" en="Priority" /><select value={value.priority} onChange={(event) => field('priority', event.target.value)} className={selectClass}><option value="">未选择</option><option value="p0">P0</option><option value="p1">P1</option><option value="p2">P2</option></select></label>
          <label><Bilingual zh="是否承接" en="Accepted" /><select value={value.acceptance} onChange={(event) => field('acceptance', event.target.value)} className={selectClass}><option value="yes">是 / Yes</option><option value="no">否 / No</option><option value="tbd">TBD</option></select></label>
          <label><Bilingual zh="区域负责人" en="Regional POC" /><div className={peopleClass}><FeishuPeoplePickerInput owners={value.regionalPocs} options={people} preferredDepartmentKeywords={['LIVE']} onChange={(next) => field('regionalPocs', next)} /></div></label>
          <label><Bilingual zh="平台负责人" en="Platform POC" /><div className={peopleClass}><FeishuPeoplePickerInput owners={value.platformPocs} options={people} preferredDepartmentKeywords={['LIVE Platform', 'Platform', 'LIVE']} onChange={(next) => field('platformPocs', next)} /></div></label>
          <label className="sm:col-span-2"><Bilingual zh="关联 Platform OKR" en="Related Platform OKR" /><div className="mt-1.5"><PlanKRSelect value={value.planKrIds} objectives={objectives} onChange={(next) => field('planKrIds', next)} /></div></label>
          <label className="sm:col-span-2"><Bilingual zh="交付物" en="Deliverable" /><textarea value={value.deliverable} onChange={(event) => field('deliverable', event.target.value)} className={inputClass} rows={2} /></label>
        </div>
      </section>
    </div>
    <div className="flex flex-wrap items-center justify-end gap-2 border-t border-slate-100 bg-slate-50/60 px-4 py-3 sm:px-5">
      {onCancel && <button type="button" disabled={busy} onClick={onCancel} className="h-9 rounded-lg border border-slate-200 bg-white px-4 text-xs font-medium text-slate-600 disabled:opacity-40">取消</button>}
      <button type="button" disabled={busy} onClick={() => onSave(value)} className="h-9 rounded-lg bg-indigo-600 px-5 text-xs font-semibold text-white shadow-sm disabled:opacity-40">{isNew ? '创建需求' : '保存修改'}</button>
    </div>
  </article>
}

function DecisionEditor({ initial, region, people, busy, onSave, onComment }: { initial: RegionalPlanDecisionItem; region: RegionalCode; people: KrOwner[]; busy: boolean; onSave: (value: RegionalPlanDecisionItem) => void; onComment: () => void }) {
  const [value, setValue] = useState(initial)
  useEffect(() => setValue(initial), [initial])
  const suggestions = region === 'menat' ? ['MENA', 'TR'] : region === 'eu' ? ['EU-A', 'EU-B'] : []
  const controlClass = 'mt-1.5 h-9 w-full rounded-lg border border-slate-200 bg-white px-2.5 text-xs text-slate-700 outline-none focus:border-indigo-400'
  return <div id={commentTargetElementId({ type: 'alignment_item', id: `plan:${value.planKrId}` })} className="mt-3 rounded-xl border border-slate-200 bg-slate-50/60 p-3">
    <div className="grid items-start gap-3 sm:grid-cols-2 xl:grid-cols-[8rem_minmax(9rem,1fr)_minmax(10rem,1fr)_minmax(14rem,1.5fr)_auto]">
      <label className="text-[10px] font-medium text-slate-500">是否上车 / Onboard<select value={value.onboard} onChange={(event) => setValue((current) => ({ ...current, onboard: event.target.value as RegionalPlanDecisionItem['onboard'] }))} className={controlClass}><option value="">未选择</option><option value="yes">是 / Yes</option><option value="no">否 / No</option></select></label>
      <label className="text-[10px] font-medium text-slate-500">上车地区 / Launch region<input list={`region-${region}-${value.planKrId}`} value={value.launchRegions.join('、')} onChange={(event) => setValue((current) => ({ ...current, launchRegions: event.target.value.split(/[、,，]/).map((item) => item.trim()).filter(Boolean) }))} placeholder="支持自由输入" className={controlClass} /><datalist id={`region-${region}-${value.planKrId}`}>{suggestions.map((item) => <option key={item}>{item}</option>)}</datalist></label>
      <label className="text-[10px] font-medium text-slate-500">区域负责人 / Regional POC<div className="mt-1.5 flex min-h-9 items-center rounded-lg border border-slate-200 bg-white px-2"><FeishuPeoplePickerInput small owners={value.regionalPocs} options={people} preferredDepartmentKeywords={['LIVE']} onChange={(next) => setValue((current) => ({ ...current, regionalPocs: next }))} /></div></label>
      <label className="text-[10px] font-medium text-slate-500">关联区域 OKR / Related regional OKR<input value={value.regionalOkr} onChange={(event) => setValue((current) => ({ ...current, regionalOkr: event.target.value }))} className={controlClass} /></label>
      <div className="flex gap-2 self-end sm:col-span-2 xl:col-span-1"><button type="button" disabled={busy} onClick={() => onSave(value)} className="h-9 rounded-lg bg-indigo-600 px-4 text-xs font-medium text-white disabled:opacity-40">保存</button><button type="button" onClick={onComment} className="h-9 rounded-lg border border-slate-200 bg-white px-3 text-xs text-slate-500">💬</button></div>
    </div>
  </div>
}

function PlatformSection({ board, category, people, busy, onDecision, onComment }: { board: RegionalAlignmentBoard; category: Category; people: KrOwner[]; busy: boolean; onDecision: (value: RegionalPlanDecisionItem) => void; onComment: (target: CommentTarget) => void }) {
  const [showHidden, setShowHidden] = useState(false)
  const decisions = new Map(board.decisions.map((item) => [item.planKrId, item]))
  const objectives = board.plan.objectives.map((objective) => ({ ...objective, krs: objective.krs.filter((kr) => categoryOf(kr) === category && (showHidden || !decisions.get(kr.id)?.hidden)) })).filter((objective) => objective.krs.length)
  return <div className="space-y-3"><label className="flex justify-end text-[10px] text-slate-400"><input type="checkbox" checked={showHidden} onChange={(event) => setShowHidden(event.target.checked)} className="mr-1" />显示已移除 Platform OKR / Show removed</label>
    {objectives.map((objective) => <details key={objective.id} open className="rounded-xl border border-slate-200 bg-white p-3 shadow-sm"><summary className="cursor-pointer list-none text-sm font-semibold text-slate-800">{objective.title}</summary>{objective.krs.map((kr) => {
      const decision = decisions.get(kr.id) ?? { planKrId: kr.id, version: 0, onboard: '', launchRegions: [], regionalPocs: [], regionalOkr: '', hidden: false } as RegionalPlanDecisionItem
      return <div key={kr.id} className={`mt-3 border-t border-slate-100 pt-2 ${decision.hidden ? 'opacity-60' : ''}`}><KRDetails kr={kr} /><DecisionEditor initial={decision} region={board.region.regionCode} people={people} busy={busy} onSave={onDecision} onComment={() => onComment({ type: 'alignment_item', id: `plan:${kr.id}`, title: kr.title })} /><button type="button" disabled={busy} onClick={() => onDecision({ ...decision, hidden: !decision.hidden })} className="mt-1 text-[9px] text-slate-400 hover:text-red-600">{decision.hidden ? '恢复到本区域视图 / Restore' : '从本区域视图移除 / Remove'}</button></div>
    })}</details>)}
    {objectives.length === 0 && <div className="rounded-xl border border-dashed border-slate-300 px-5 py-10 text-center text-xs text-slate-400">当前分类暂无 Platform OKR</div>}
  </div>
}

type MatchBucket = 'p0' | 'p12' | 'pending'

function AlignmentProjects({ board, bucket, category }: { board: RegionalAlignmentBoard; bucket: MatchBucket; category: Category }) {
  const [pendingTab, setPendingTab] = useState<'demand' | 'platform'>('demand')
  const decisions = new Map(board.decisions.map((item) => [item.planKrId, item]))
  const index = planIndex(board.plan.objectives)
  const krMatches = (kr: Kr) => {
    const decision = decisions.get(kr.id)
    if (bucket === 'pending') return decision?.onboard === 'no'
    if (decision?.onboard !== 'yes') return false
    return bucket === 'p0' ? priorityOf(kr) === 'p0' : priorityOf(kr) !== 'p0'
  }
  const demandMatches = (demand: RegionalDemand) => {
    if (bucket === 'pending') return demand.acceptance === 'no'
    if (demand.acceptance !== 'yes') return false
    const linked = demand.planKrIds.map((id) => index.get(id)?.kr).filter((kr): kr is Kr => Boolean(kr))
    const p0 = demand.priority === 'p0' && linked.length > 0 && linked.every((kr) => priorityOf(kr) === 'p0')
    return bucket === 'p0' ? p0 : !p0
  }
  const objectives = board.plan.objectives.map((objective) => ({ ...objective, krs: objective.krs.filter((kr) => categoryOf(kr) === category && krMatches(kr)) })).filter((objective) => objective.krs.length)
  const demands = board.demands.filter(demandMatches).filter((demand) => {
    if (bucket === 'pending') return true
    const linkedCategories = demand.planKrIds.flatMap((id) => { const kr = index.get(id)?.kr; return kr ? [categoryOf(kr)] : [] })
    return linkedCategories.length ? linkedCategories.includes(category) : category === CATEGORIES[0]
  })
  if (bucket === 'pending') return <div><div className="mb-3 flex gap-1">{([{ key: 'demand', label: '区域需求 / Regional requirements' }, { key: 'platform', label: 'Platform OKR' }] as const).map((item) => <button key={item.key} type="button" onClick={() => setPendingTab(item.key)} className={`rounded-lg border px-3 py-1.5 text-[10px] ${pendingTab === item.key ? 'border-amber-300 bg-amber-50 text-amber-700' : 'border-slate-200 bg-white text-slate-500'}`}>{item.label}</button>)}</div>{pendingTab === 'demand'
    ? <section className="rounded-xl border border-slate-200 bg-white p-3">{demands.length ? demands.map((demand) => <div key={demand.id} className="mt-2 rounded-lg bg-slate-50 p-2 text-[10px] leading-5"><div className="font-medium text-slate-700">{demand.requirement || demand.item || '未填写需求'}</div><div className="text-slate-400">Regional POC：{demand.regionalPocs.map((owner) => owner.name).join('、') || '未填写'}</div><div className="text-slate-400">Platform POC：{demand.platformPocs.map((owner) => owner.name).join('、') || '未填写'}</div></div>) : <p className="mt-3 text-[10px] text-slate-400">暂无不承接区域需求</p>}</section>
    : <section className="rounded-xl border border-slate-200 bg-white p-3">{objectives.length ? objectives.map((objective) => <details key={objective.id} open className="mt-2"><summary className="cursor-pointer text-[11px] font-semibold text-slate-700">{objective.title}</summary>{objective.krs.map((kr) => <KRDetails key={kr.id} kr={kr} />)}</details>) : <p className="mt-3 text-[10px] text-slate-400">暂无区域不上车的 Platform OKR</p>}</section>}</div>
  return <div className="space-y-3">{objectives.map((objective) => {
    const linkedDemands = demands.filter((demand) => demand.planKrIds.some((id) => objective.krs.some((kr) => kr.id === id)))
    return <article key={objective.id} className="rounded-xl border border-slate-200 bg-white p-4 shadow-sm"><h3 className="text-sm font-semibold text-slate-800">{objective.title}</h3><div className="mt-3 grid gap-3 lg:grid-cols-2"><div><div className="text-[10px] font-semibold text-indigo-600">Key points for the Platform team</div>{objective.krs.map((kr) => <div key={kr.id}><KRDetails kr={kr} /><div className="mt-1 text-[10px] text-slate-400">Platform POC：{kr.ownerName || krOwners(kr).map((owner) => owner.name).join('、') || '未填写'}</div></div>)}</div><div><div className="text-[10px] font-semibold text-emerald-600">Key points for the Regional team</div>{linkedDemands.map((demand) => <details key={demand.id} open className="mt-2 rounded-lg bg-emerald-50/60 p-2"><summary className="cursor-pointer text-[10px] font-medium text-slate-700">{demand.requirement || demand.item || '未填写需求'}</summary><div className="mt-1 text-[10px] text-slate-400">Regional POC：{demand.regionalPocs.map((owner) => owner.name).join('、') || '未填写'}</div></details>)}{linkedDemands.length === 0 && <p className="mt-2 text-[10px] text-slate-400">暂无关联区域需求</p>}</div></div></article>
  })}{objectives.length === 0 && demands.length === 0 && <div className="rounded-xl border border-dashed border-slate-300 px-5 py-10 text-center text-xs text-slate-400">当前条件下暂无匹配项目</div>}{demands.filter((demand) => demand.planKrIds.length === 0).map((demand) => <article key={demand.id} className="rounded-xl border border-emerald-200 bg-emerald-50/50 p-3"><div className="text-[10px] font-semibold text-emerald-700">未关联 Platform KR 的区域需求</div><div className="mt-1 text-[11px] text-slate-700">{demand.requirement || demand.item}</div></article>)}</div>
}

function RecapSection({ board, busy, onOrder, onHide }: { board: RegionalAlignmentBoard; busy: boolean; onOrder: (bucket: string, objectives: Objective[]) => void; onHide: (bucket: string, objective: Objective, hidden: boolean) => void }) {
  const [category, setCategory] = useState<'全部OKR' | Category>('全部OKR')
  const [priority, setPriority] = useState<'all' | 'p0' | 'p1' | 'p2'>('all')
  const [showHidden, setShowHidden] = useState(false)
  const [dragging, setDragging] = useState<string>()
  const bucket = `${category}:${priority}`
  const overlays = new Map(board.recapOverlays.filter((item) => item.bucketKey === bucket).map((item) => [item.objectiveId, item]))
  const filtered = board.recap.objectives.filter((objective) => objective.krs.some((kr) => (category === '全部OKR' || categoryOf(kr) === category) && (priority === 'all' || priorityOf(kr) === priority)))
  const ordered = [...filtered].sort((left, right) => (overlays.get(left.id)?.sortOrder ?? 9999) - (overlays.get(right.id)?.sortOrder ?? 9999))
  const visible = ordered.filter((objective) => showHidden || !overlays.get(objective.id)?.hidden)
  return <>
    <div className="mb-3 flex flex-wrap items-center gap-2"><div className="flex flex-wrap gap-1">{(['全部OKR', ...CATEGORIES] as const).map((item) => <button key={item} type="button" onClick={() => setCategory(item)} className={`rounded-lg border px-2.5 py-1 text-[10px] ${category === item ? 'border-indigo-300 bg-indigo-50 text-indigo-700' : 'border-slate-200 bg-white text-slate-500'}`}>{item === '优质主播专项' ? '优质主播&内容专项' : item}</button>)}</div><div className="ml-auto flex gap-1">{(['all', 'p0', 'p1', 'p2'] as const).map((item) => <button key={item} type="button" onClick={() => setPriority(item)} className={`rounded-lg border px-2.5 py-1 text-[10px] ${priority === item ? 'border-emerald-300 bg-emerald-50 text-emerald-700' : 'border-slate-200 bg-white text-slate-500'}`}>{item === 'all' ? 'Focus / P1 / P2' : item === 'p0' ? 'Focus' : item.toUpperCase()}</button>)}</div><label className="text-[10px] text-slate-400"><input type="checkbox" checked={showHidden} onChange={(event) => setShowHidden(event.target.checked)} className="mr-1" />显示已移除</label></div>
    <div className="space-y-3">{visible.map((objective) => <details key={objective.id} open draggable onDragStart={() => setDragging(objective.id)} onDragOver={(event) => event.preventDefault()} onDrop={() => { if (!dragging || dragging === objective.id) return; const next = [...ordered]; const from = next.findIndex((item) => item.id === dragging); const to = next.findIndex((item) => item.id === objective.id); next.splice(to, 0, next.splice(from, 1)[0]); setDragging(undefined); onOrder(bucket, next) }} className={`rounded-xl border p-3 ${overlays.get(objective.id)?.hidden ? 'border-dashed border-slate-300 bg-slate-50 opacity-70' : 'border-slate-200 bg-white'}`}><summary className="cursor-grab list-none text-sm font-semibold text-slate-800">⋮⋮ {objective.title}</summary>{objective.krs.filter((kr) => (category === '全部OKR' || categoryOf(kr) === category) && (priority === 'all' || priorityOf(kr) === priority)).map((kr) => <KRDetails key={kr.id} kr={kr} />)}<button type="button" disabled={busy} onClick={() => onHide(bucket, objective, !overlays.get(objective.id)?.hidden)} className="mt-2 text-[9px] text-slate-400 hover:text-red-600">{overlays.get(objective.id)?.hidden ? '恢复 / Restore' : '移除 / Remove'}</button></details>)}{visible.length === 0 && <div className="rounded-xl border border-dashed border-slate-300 px-5 py-10 text-center text-xs text-slate-400">当前分类暂无上季度 Review 内容</div>}</div>
  </>
}

export default function RegionalAlignmentApp({ initialQuarter, initialRegion, initialCommentId = '', autoMatchAccess, onScopeChange }: { initialQuarter: string; initialRegion?: string; initialCommentId?: string; autoMatchAccess: boolean; onScopeChange?: (quarter: string, region: RegionalCode) => void }) {
  const validRegion = REGIONS.some((item) => item.code === initialRegion) ? initialRegion as RegionalCode : 'eu'
  const [quarter, setQuarter] = useState(initialQuarter)
  const [quarterDraft, setQuarterDraft] = useState(initialQuarter)
  const [region, setRegion] = useState<RegionalCode>(validRegion)
  const [board, setBoard] = useState<RegionalAlignmentBoard>()
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [newDemand, setNewDemand] = useState<Omit<RegionalDemand, 'id'>>(() => emptyDemand(0))
  const [addingDemand, setAddingDemand] = useState(false)
  const [platformCategory, setPlatformCategory] = useState<Category>('公会业务')
  const [alignmentCategory, setAlignmentCategory] = useState<Category>('公会业务')
  const [matchBucket, setMatchBucket] = useState<MatchBucket>('p0')
  const [matchNotice, setMatchNotice] = useState('')
  const [shareNotice, setShareNotice] = useState('')
  const [shareLink, setShareLink] = useState('')
  const [commentsOpen, setCommentsOpen] = useState(Boolean(initialCommentId))
  const [reviewingComments, setReviewingComments] = useState(false)
  const [commentCount, setCommentCount] = useState(0)
  const [commentCounts, setCommentCounts] = useState<Record<string, number>>({})
  const [comments, setComments] = useState<PageComment[]>([])
  const [commentTarget, setCommentTarget] = useState<CommentTarget>()
  const [focusedComment, setFocusedComment] = useState<PageComment>()

  const load = async (targetQuarter = quarter, targetRegion = region) => {
    setLoading(true); setError('')
    try { const value = await getRegionalAlignmentBoard(targetQuarter, targetRegion); setBoard(value); setNewDemand(emptyDemand(value.demands.length)); setAddingDemand(false); onScopeChange?.(targetQuarter, targetRegion) }
    catch (reason) { setError(reason instanceof Error ? reason.message : '区域对齐页加载失败') }
    finally { setLoading(false) }
  }
  useEffect(() => { void load() }, [quarter, region])
  useEffect(() => { if (focusedComment) window.setTimeout(() => scrollToCommentSource(focusedComment), 80) }, [focusedComment])

  const objectives = board?.plan.objectives ?? []
  const people = useMemo(() => ownerOptions(objectives), [objectives])
  const categoryOrder = normalizedCategoryOrder(board?.region.categoryOrder ?? [])
  const saveDemand = async (value: RegionalDemand | Omit<RegionalDemand, 'id'>) => {
    if (!board) return
    setBusy(true); setError('')
    try {
      if ('id' in value) { const updated = await updateRegionalDemand(quarter, region, value); setBoard({ ...board, demands: board.demands.map((item) => item.id === updated.id ? updated : item) }) }
      else { const created = await createRegionalDemand(quarter, region, value); setBoard({ ...board, demands: [...board.demands, created] }); setNewDemand(emptyDemand(board.demands.length + 1)); setAddingDemand(false) }
    } catch (reason) { setError(reason instanceof Error ? reason.message : '保存失败') } finally { setBusy(false) }
  }
  const removeDemand = async (value: RegionalDemand) => { if (!board || !window.confirm('确认删除这条区域需求？')) return; setBusy(true); try { await deleteRegionalDemand(quarter, region, value); setBoard({ ...board, demands: board.demands.filter((item) => item.id !== value.id) }) } catch (reason) { setError(reason instanceof Error ? reason.message : '删除失败') } finally { setBusy(false) } }
  const saveDecision = async (value: RegionalPlanDecisionItem) => { if (!board) return; setBusy(true); try { const updated = await putRegionalDecision(quarter, region, value); setBoard({ ...board, decisions: [...board.decisions.filter((item) => item.planKrId !== updated.planKrId), updated] }) } catch (reason) { setError(reason instanceof Error ? reason.message : '保存失败') } finally { setBusy(false) } }
  const reorderCategories = async (next: Category[]) => { if (!board) return; setBusy(true); try { const updated = await putRegionalCategoryOrder(quarter, region, board.region.version, next); setBoard({ ...board, region: updated }) } catch (reason) { setError(reason instanceof Error ? reason.message : '分类顺序保存失败') } finally { setBusy(false) } }
  const copyShare = async () => { setShareNotice(''); setShareLink(''); try { const config = await getWebConfig(); const url = new URL(config.public_base_url.trim() || window.location.href); url.hash = `/biz-okr?${new URLSearchParams({ tab: 'regional-alignment', quarter, region })}`; const link = url.toString(); if (navigator.clipboard) { await navigator.clipboard.writeText(link); setShareNotice('区域 OKR 对齐页链接已复制 / Link copied') } else { setShareLink(link); setShareNotice('请复制下方链接 / Copy the link below') } } catch (reason) { setShareNotice(reason instanceof Error ? reason.message : '分享链接生成失败') } }
  const openComments = (target?: CommentTarget) => { setCommentTarget(target); setFocusedComment(undefined); setReviewingComments(false); setCommentsOpen(true) }

  return <>
    <header className="sticky top-0 z-40 border-b border-slate-200/80 bg-white/95 backdrop-blur">
      <div className={`mx-auto flex min-h-16 max-w-[1480px] flex-wrap items-center gap-3 px-4 py-3 sm:px-6 lg:px-8 ${commentsOpen ? 'lg:pr-[420px]' : ''}`}>
        <span className="flex size-9 items-center justify-center rounded-xl bg-indigo-600 text-sm font-semibold text-white shadow-sm">R</span>
        <div>
          <h1 className="text-base font-semibold text-slate-900">Emily · 区域 OKR 对齐</h1>
          <p className="mt-0.5 text-[10px] text-slate-400">Regional OKR Alignment</p>
        </div>
        <div className="ml-auto flex flex-wrap items-center justify-end gap-2">
          <div className="flex h-9 items-center overflow-hidden rounded-lg border border-slate-200 bg-white">
            <span className="border-r border-slate-100 px-2.5 text-[10px] font-medium text-slate-400">季度</span>
            <input value={quarterDraft} onChange={(event) => setQuarterDraft(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') setQuarter(quarterDraft.trim()) }} className="h-full w-24 px-2.5 text-xs outline-none" aria-label="季度" />
            <button type="button" onClick={() => setQuarter(quarterDraft.trim())} className="h-full border-l border-slate-100 px-3 text-xs font-medium text-slate-600 hover:bg-slate-50">切换</button>
          </div>
          <button type="button" onClick={() => void copyShare()} className="h-9 rounded-lg border border-slate-200 bg-white px-3 text-xs font-medium text-slate-600 hover:border-indigo-200 hover:text-indigo-600">分享页面</button>
          <button type="button" onClick={() => commentsOpen ? setCommentsOpen(false) : openComments()} className={`relative h-9 rounded-lg border px-3 text-xs font-medium ${commentsOpen ? 'border-indigo-200 bg-indigo-50 text-indigo-700' : 'border-slate-200 bg-white text-slate-600 hover:border-indigo-200'}`}>💬 评论{commentCount > 0 && <span className="ml-1 rounded-full bg-indigo-600 px-1.5 py-0.5 text-[9px] text-white">{commentCount}</span>}</button>
        </div>
      </div>
    </header>
    <main className={`mx-auto max-w-[1480px] space-y-6 px-4 py-5 sm:px-6 lg:px-8 ${commentsOpen ? 'lg:pr-[420px]' : ''}`}>
      {(shareNotice || error) && <div className={`rounded-xl border px-4 py-3 text-xs ${error ? 'border-red-200 bg-red-50 text-red-700' : 'border-blue-100 bg-blue-50 text-blue-700'}`}>{error || shareNotice}{shareLink && <input readOnly value={shareLink} onFocus={(event) => event.currentTarget.select()} className="mt-2 h-9 w-full rounded-lg border border-blue-200 bg-white px-3" />}</div>}
      <nav className="flex min-w-0 gap-1 overflow-x-auto rounded-xl border border-slate-200 bg-white p-1 shadow-sm">{REGIONS.map((item) => <button key={item.code} type="button" onClick={() => setRegion(item.code)} className={`min-w-24 flex-1 rounded-lg px-4 py-2.5 text-xs font-semibold transition ${region === item.code ? 'bg-indigo-600 text-white shadow-sm' : 'text-slate-500 hover:bg-slate-50 hover:text-slate-700'}`}>{item.label}</button>)}</nav>
      {loading ? <div className="rounded-2xl border border-slate-200 bg-white py-24 text-center text-sm text-slate-400">正在加载区域对齐数据… / Loading…</div> : board ? <CommentInteractionProvider value={{ enabled: true, triggerMode: 'button', selected: commentTarget, focused: focusedComment, comments, counts: commentCounts, setPendingSelection: () => undefined, select: openComments }}>
        <div className="space-y-6">
          <section className="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm sm:p-6">
            <SectionTitle
              part="Part 0 · Pre-alignment"
              zh="Regional Ops Team 高优痛点 & 核心业务诉求"
              en="Pain points & Key Focusing items With Top Priority"
              action={<button type="button" disabled={busy || addingDemand} onClick={() => setAddingDemand(true)} className="h-9 rounded-lg bg-indigo-600 px-4 text-xs font-semibold text-white shadow-sm disabled:opacity-40">+ 新增区域需求</button>}
            />
            <div className="space-y-4">
              {board.demands.map((demand, index) => <DemandEditor key={`${demand.id}:${demand.version}`} index={index} initial={demand} objectives={objectives} people={people} busy={busy} onSave={saveDemand} onDelete={() => void removeDemand(demand)} onComment={() => openComments({ type: 'alignment_item', id: `demand:${demand.id}`, title: demand.requirement || demand.item || '区域需求' })} />)}
              {board.demands.length === 0 && !addingDemand && <div className="rounded-2xl border border-dashed border-slate-300 bg-slate-50/60 px-6 py-12 text-center"><p className="text-sm font-medium text-slate-600">尚未填写区域需求</p><p className="mt-1 text-xs text-slate-400">先录入区域痛点和核心诉求，再进行平台承接对齐。</p><button type="button" onClick={() => setAddingDemand(true)} className="mt-4 h-9 rounded-lg border border-indigo-200 bg-white px-4 text-xs font-semibold text-indigo-600 hover:bg-indigo-50">新增第一条需求</button></div>}
              {addingDemand && <DemandEditor key={`new:${newDemand.sortOrder}`} initial={newDemand} objectives={objectives} people={people} busy={busy} onSave={saveDemand} onCancel={() => { setNewDemand(emptyDemand(board.demands.length)); setAddingDemand(false) }} />}
            </div>
            <div className="mt-8 border-t border-slate-200 pt-6">
              <div className="mb-4"><h3 className="text-base font-semibold text-slate-800">Platform Team 平台团队关键项目 & 重要业务解决方案</h3><p className="mt-1 text-xs text-slate-400">Key projects & important solutions</p></div>
              <CategoryTabs order={categoryOrder} value={platformCategory} onChange={setPlatformCategory} onReorder={(next) => void reorderCategories(next)} labels="platform" />
              <div className="mt-4"><PlatformSection board={board} category={platformCategory} people={people} busy={busy} onDecision={(value) => void saveDecision(value)} onComment={openComments} /></div>
            </div>
          </section>
          <section className="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm sm:p-6">
            <SectionTitle part="Part 1 · OKR Alignment" zh="OKR 对齐" en="Regional & Platform project alignment" action={autoMatchAccess ? <button type="button" disabled={loading} onClick={() => { void load().then(() => setMatchNotice(`已按最新数据匹配 · ${new Date().toLocaleTimeString()}`)) }} className="rounded-lg bg-indigo-600 px-4 py-2 text-xs font-semibold text-white disabled:opacity-40">自动匹配<br /><span className="text-[9px] font-normal opacity-80">Auto match</span></button> : undefined} />
            {matchNotice && <div className="mb-3 text-right text-[10px] text-emerald-600">{matchNotice}</div>}
            <div className="mb-4 flex flex-wrap gap-2">{([{ key: 'p0', label: 'P0 Projects' }, { key: 'p12', label: 'P1 & P2 Projects' }, { key: 'pending', label: 'Pending items' }] as const).map((item) => <button key={item.key} type="button" onClick={() => setMatchBucket(item.key)} className={`rounded-lg border px-3 py-2 text-xs font-medium ${matchBucket === item.key ? 'border-indigo-300 bg-indigo-50 text-indigo-700' : 'border-slate-200 bg-white text-slate-500'}`}>{item.label}</button>)}</div>
            <CategoryTabs order={categoryOrder} value={alignmentCategory} onChange={setAlignmentCategory} onReorder={(next) => void reorderCategories(next)} />
            <div className="mt-4"><AlignmentProjects board={board} bucket={matchBucket} category={alignmentCategory} /></div>
          </section>
          <section className="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm sm:p-6">
            <SectionTitle part="Part 2 · Recap For Last Quarter" zh="上季度复盘" en={`${board.alignment.recapQuarter} Review recap`} />
            <RecapSection board={board} busy={busy} onOrder={(bucket, next) => { void (async () => { try { const values = await putRegionalRecapOrder(quarter, region, bucket, next.map((item) => item.id)); setBoard({ ...board, recapOverlays: [...board.recapOverlays.filter((item) => item.bucketKey !== bucket), ...values] }) } catch (reason) { setError(reason instanceof Error ? reason.message : '顺序保存失败') } })() }} onHide={(bucket, objective, hidden) => { void (async () => { const current = board.recapOverlays.find((item) => item.bucketKey === bucket && item.objectiveId === objective.id); try { const updated = await patchRegionalRecap(quarter, region, bucket, objective.id, current?.version ?? 0, hidden); setBoard({ ...board, recapOverlays: [...board.recapOverlays.filter((item) => !(item.bucketKey === bucket && item.objectiveId === objective.id)), updated] }) } catch (reason) { setError(reason instanceof Error ? reason.message : '移除失败') } })() }} />
          </section>
        </div>
      </CommentInteractionProvider> : <div className="rounded-2xl border border-slate-200 bg-white py-24 text-center text-sm text-slate-400">暂无区域对齐数据</div>}
    </main>
    {board && <CommentDrawer open={commentsOpen} reviewEnabled reviewing={reviewingComments} quarter={quarter} alignmentId={board.alignment.id} alignmentRegion={region} sourceTab="regional-alignment" scopeLabel={`${REGIONS.find((item) => item.code === region)?.label} · ${quarter}`} objectives={[...board.plan.objectives, ...board.recap.objectives]} target={commentTarget} focusCommentId={initialCommentId} onStartReview={() => { setCommentTarget(undefined); setReviewingComments(true) }} onShowAll={() => { setCommentTarget(undefined); setFocusedComment(undefined); setReviewingComments(false) }} onClose={() => { setCommentsOpen(false); setCommentTarget(undefined); setFocusedComment(undefined); setReviewingComments(false) }} onFocusCommentChange={setFocusedComment} onCountChange={setCommentCount} onCountsChange={setCommentCounts} onCommentsChange={setComments} />}
  </>
}
