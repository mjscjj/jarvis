import { useCallback, useEffect, useMemo, useRef, useState, type ClipboardEvent } from 'react'
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
  refreshRegionalAlignmentBoard,
  updateRegionalDemand,
} from './api'
import { CommentInteractionProvider, commentTargetElementId, scrollToCommentSource } from './commenting'
import { CommentDrawer, type CommentReviewMode } from './components/CommentDrawer'
import { FeishuPeoplePickerInput } from './components/FeishuPeoplePicker'
import { PeopleInline } from './components/PeopleInline'
import { Images, Links, usePastedImageUpload } from './components/ui'
import { uid } from './board'
import { businessCategoryOf, priorityOf } from './hierarchy'
import { krOwners, ownerOptions } from './people'
import {
  copyRegionalAlignmentShareLink,
  launchRegionOptions,
  matchesRegionalPriority,
  regionalAlignmentShareURL,
  regionalDecisionSignature,
  REGIONAL_PRIORITY_FILTERS,
  type RegionalPriorityFilter,
} from './regionalAlignment'
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

type RegionalDemandDraft = { key: string; value: Omit<RegionalDemand, 'id'> }

function nextDemandSortOrder(demands: Array<Pick<RegionalDemand, 'sortOrder'>>): number {
  return demands.reduce((maximum, demand) => Math.max(maximum, demand.sortOrder), -1) + 1
}

function CollapsibleBlock({ title, subtitle, level, action, children }: { title: string; subtitle?: string; level: 'primary' | 'secondary'; action?: React.ReactNode; children: React.ReactNode }) {
  const [open, setOpen] = useState(true)
  const primary = level === 'primary'
  return <div>
    <div className={`flex flex-wrap items-start gap-3 ${primary ? 'mb-5 border-b border-slate-200 pb-4' : 'mb-4'}`}>
      <button type="button" aria-expanded={open} onClick={() => setOpen((value) => !value)} className="flex min-w-0 flex-1 items-start gap-2 text-left">
        <span className={`mt-0.5 shrink-0 text-indigo-400 transition-transform ${open ? 'rotate-90' : ''}`}>›</span>
        <span><span className={`block font-semibold text-slate-900 ${primary ? 'text-lg' : 'text-base'}`}>{title}</span>{subtitle && <span className="mt-1 block text-xs font-normal text-slate-400">{subtitle}</span>}</span>
      </button>
      {action && <div className="ml-auto pt-0.5">{action}</div>}
    </div>
    {open && children}
  </div>
}

function CategoryTabs({ order, value, onChange, onReorder, labels = 'alignment' }: { order: Category[]; value?: Category; onChange: (value: Category) => void; onReorder: (values: Category[]) => void; labels?: 'platform' | 'alignment' | 'recap' }) {
  const [dragging, setDragging] = useState<Category>()
  const label = (category: Category) => {
    if (category === '公会业务') return '公会业务 / Agency'
    if (category === '运营效率') return '运营效率 / Operational Efficiency'
    if (category === '优质主播专项') return labels === 'platform' ? '优质内容 / Quality Content' : labels === 'recap' ? '优质主播&内容专项 / Premium Creators & Content' : '优质主播专项 / Premium Creators'
    return labels === 'platform' ? 'AI 提效 / AI Efficiency' : 'AI提效 / AI Efficiency'
  }
  return <div className="flex flex-wrap gap-1.5" aria-label="业务方向 / Business direction">
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

type PlatformCategory = 'all' | Category

function PlatformCategoryTabs({ order, value, onChange, onReorder }: { order: Category[]; value: PlatformCategory; onChange: (value: PlatformCategory) => void; onReorder: (values: Category[]) => void }) {
  return <div className="flex flex-wrap gap-1.5" aria-label="业务方向 / Business direction">
    <button type="button" onClick={() => onChange('all')} className={`rounded-lg border px-3 py-1.5 text-[11px] font-medium ${value === 'all' ? 'border-indigo-300 bg-indigo-50 text-indigo-700' : 'border-slate-200 bg-white text-slate-500 hover:border-indigo-200'}`}>全部业务方向 / All business directions</button>
    <CategoryTabs order={order} value={value === 'all' ? undefined : value} onChange={onChange} onReorder={onReorder} labels="platform" />
  </div>
}

function PriorityTabs({ value, onChange }: { value: RegionalPriorityFilter; onChange: (value: RegionalPriorityFilter) => void }) {
  return <div className="flex flex-wrap gap-1.5" aria-label="优先级 / Priority">
    {REGIONAL_PRIORITY_FILTERS.map((item) => <button key={item.value} type="button" onClick={() => onChange(item.value)} className={`rounded-lg border px-3 py-1.5 text-[11px] font-medium ${value === item.value ? 'border-emerald-300 bg-emerald-50 text-emerald-700' : 'border-slate-200 bg-white text-slate-500 hover:border-emerald-200'}`}>{item.label}</button>)}
  </div>
}

type TranslationMap = Record<string, string>

function OKRText({ text, translations, className = '' }: { text: string; translations: TranslationMap; className?: string }) {
  const english = translations[text]?.trim()
  return <span className={className}><span className="block whitespace-pre-wrap">{text}</span>{english && english !== text.trim() && <span className="mt-0.5 block whitespace-pre-wrap font-normal text-slate-400">{english}</span>}</span>
}

function KRDetails({ kr, translations }: { kr: Kr; translations: TranslationMap }) {
  return <details className="mt-2 rounded-lg border border-slate-100 bg-slate-50/60" open>
    <summary className="flex cursor-pointer list-none items-start gap-2 px-3 py-2 text-[11px] font-medium text-slate-700"><span className="mt-0.5 shrink-0 text-slate-300">▸</span><OKRText text={kr.title} translations={translations} className="min-w-0 flex-1" /><span className="shrink-0 pt-0.5 font-normal text-slate-400"><PeopleInline people={krOwners(kr)} compact empty="未填写负责人 / Owner not set" /></span></summary>
    <div className="space-y-2 border-t border-slate-100 px-3 py-2">
      {kr.points.map((point) => <details key={point.id} open className="rounded-md bg-white px-2 py-1.5">
        <summary className="flex cursor-pointer items-start gap-1 text-[10px] font-medium text-slate-600"><span className="shrink-0 pt-0.5">{point.kind === 'product' ? '产品具体 KR / Product KR' : '策略具体 KR / Strategy KR'}：</span><OKRText text={point.title} translations={translations} className="min-w-0 flex-1" /></summary>
        <div className="mt-1 flex items-center gap-1 pl-3 text-[10px] text-slate-400"><span>负责人 / POC：</span><PeopleInline people={point.owners ?? []} compact empty="未填写 / Not set" /></div>
        {point.entries.map((entry) => <div key={entry.id} className="mt-1 pl-3 text-[10px] leading-5 text-slate-500"><OKRText text={entry.text} translations={translations} /></div>)}
      </details>)}
      {kr.points.length === 0 && <div className="text-[10px] text-slate-400">暂无策略或产品具体 KR / No detailed KR</div>}
    </div>
  </details>
}

function PlanKRSelect({ value, objectives, translations, onChange }: { value: string[]; objectives: Objective[]; translations: TranslationMap; onChange: (value: string[]) => void }) {
  const label = (text: string) => translations[text]?.trim() && translations[text].trim() !== text.trim() ? `${text} / ${translations[text].trim()}` : text
  return <Select
    mode="multiple"
    allowClear
    showSearch
    value={value}
    onChange={onChange}
    optionFilterProp="label"
    maxTagCount="responsive"
    placeholder="选择关联的 Platform KR / Select related Platform KRs"
    aria-label="Platform OKR"
    className="w-full"
    options={objectives.map((objective) => ({
      label: label(objective.title),
      options: objective.krs.map((kr) => ({ label: label(kr.title), value: kr.id })),
    }))}
  />
}

const DEMAND_TABLE_COLUMNS = 'grid-cols-[minmax(11rem,1.05fr)_minmax(20rem,1.8fr)_8rem_minmax(11rem,1fr)_8rem_minmax(11rem,1fr)_minmax(18rem,1.7fr)_minmax(14rem,1.25fr)_8rem]'
const DEMAND_TABLE_HEADER = 'flex min-h-14 items-center border-r border-slate-200 bg-slate-100 px-3 py-2 last:border-r-0'
const DEMAND_TABLE_CELL = 'min-w-0 border-r border-t border-slate-200 bg-white last:border-r-0'

function demandSignature(value: RegionalDemand | Omit<RegionalDemand, 'id'>) {
  return JSON.stringify({ regionalOkr: value.regionalOkr, item: value.item, requirement: value.requirement, docs: value.docs, images: value.images, priority: value.priority, regionalPocs: value.regionalPocs, platformPocs: value.platformPocs, acceptance: value.acceptance, planKrIds: value.planKrIds, deliverable: value.deliverable, sortOrder: value.sortOrder })
}

function demandHasContent(value: RegionalDemand | Omit<RegionalDemand, 'id'>) {
  return Boolean(value.regionalOkr.trim() || value.item.trim() || value.requirement.trim() || value.docs.length || value.images.length || value.priority || value.regionalPocs.length || value.platformPocs.length || value.acceptance !== 'tbd' || value.planKrIds.length || value.deliverable.trim())
}

function DemandEditor({ initial, objectives, translations, people, busy, onSave, onDelete, onComment }: { initial: RegionalDemand | Omit<RegionalDemand, 'id'>; objectives: Objective[]; translations: TranslationMap; people: KrOwner[]; busy: boolean; onSave: (value: RegionalDemand | Omit<RegionalDemand, 'id'>) => Promise<RegionalDemand | undefined>; onDelete?: () => void; onComment?: () => void }) {
  const [value, setValue] = useState(initial)
  const [saving, setSaving] = useState(false)
  const paste = usePastedImageUpload(useCallback((uploaded) => setValue((current) => ({ ...current, images: [...current.images, ...uploaded] })), []))
  const lastSavedRef = useRef(demandSignature(initial))
  const attemptedRef = useRef(lastSavedRef.current)
  const incomingSignature = demandSignature(initial)
  useEffect(() => {
    const previousSaved = lastSavedRef.current
    lastSavedRef.current = incomingSignature
    attemptedRef.current = incomingSignature
    setValue((current) => demandSignature(current) === previousSaved ? initial : { ...current, version: initial.version })
  }, [incomingSignature, initial])
  const signature = demandSignature(value)
  const isNew = !('id' in initial)
  useEffect(() => {
    if (busy || saving || signature === lastSavedRef.current || signature === attemptedRef.current || (isNew && !demandHasContent(value))) return
    const timer = window.setTimeout(() => {
      const snapshot = value
      const snapshotSignature = signature
      attemptedRef.current = snapshotSignature
      setSaving(true)
      void onSave(snapshot).then((updated) => {
        if (!updated) return
        lastSavedRef.current = demandSignature(updated)
        attemptedRef.current = lastSavedRef.current
        if (!isNew) setValue((current) => demandSignature(current) === snapshotSignature ? updated : { ...current, version: updated.version })
      }).finally(() => setSaving(false))
    }, 600)
    return () => window.clearTimeout(timer)
  }, [busy, isNew, onSave, saving, signature, value])
  const field = (key: keyof typeof value, next: unknown) => setValue((current) => ({ ...current, [key]: next }))
  const id = 'id' in initial ? initial.id : 'new'
  const tableInputClass = 'min-h-10 w-full border-0 bg-transparent px-3 py-2 text-xs leading-5 text-slate-700 outline-none focus:bg-indigo-50/30'
  const tableSelectClass = 'h-10 w-full border-0 bg-transparent px-3 text-xs text-slate-700 outline-none focus:bg-indigo-50/30'
  const pasteRequirement = (event: ClipboardEvent<HTMLDivElement>) => {
    if (Array.from(event.clipboardData.files).some((file) => file.type.startsWith('image/'))) {
      paste.onPaste(event)
      return
    }
    if (!(event.target instanceof HTMLTextAreaElement)) return
    const raw = event.clipboardData.getData('text/plain').trim()
    const urls = raw.split(/\s+/).filter(Boolean)
    if (!urls.length || !urls.every((item) => /^https?:\/\/\S+$/i.test(item))) return
    event.preventDefault()
    setValue((current) => ({ ...current, docs: [...current.docs, ...urls.map((url) => ({ id: uid('d'), title: (() => { try { return new URL(url).hostname.replace(/^www\./, '') } catch { return '链接 / Link' } })(), url }))] }))
  }
  return <div id={commentTargetElementId({ type: 'alignment_item', id: `demand:${id}` })} className={`grid items-stretch ${DEMAND_TABLE_COLUMNS}`} role="row">
    <div className={DEMAND_TABLE_CELL} role="cell"><textarea aria-label="区域 OKR / Regional OKR" value={value.regionalOkr} onChange={(event) => field('regionalOkr', event.target.value)} className={tableInputClass} rows={5} /></div>
    <div className={DEMAND_TABLE_CELL} role="cell" onPaste={pasteRequirement} tabIndex={0} aria-busy={paste.uploading}><textarea aria-label="具体需求 / Detailed requirement" value={value.requirement} onChange={(event) => field('requirement', event.target.value)} placeholder="填写需求，或粘贴链接/图片 / Enter requirement or paste links/images" className={tableInputClass} rows={3} /><div className="flex flex-wrap items-start gap-2 border-t border-slate-100 px-3 py-2"><Links value={value.docs} onChange={(next) => field('docs', next)} /><Images value={value.images} onChange={(next) => field('images', next)} maxDisplayWidth={120} pasteEnabled={false} /><span className={`text-[10px] ${paste.uploadError ? 'text-red-500' : 'text-slate-400'}`}>{paste.uploading ? '图片上传中… / Uploading…' : paste.uploadError || '支持粘贴链接或图片 / Paste a link or image'}</span></div></div>
    <div className={DEMAND_TABLE_CELL} role="cell"><select aria-label="优先级 / Priority" value={value.priority} onChange={(event) => field('priority', event.target.value)} className={tableSelectClass}><option value="">未选择 / Not selected</option><option value="p0">Focus item</option><option value="p1">P1</option><option value="p2">P2</option></select></div>
    <div className={`${DEMAND_TABLE_CELL} flex min-h-28 items-start px-2 py-2`} role="cell"><FeishuPeoplePickerInput owners={value.regionalPocs} options={people} preferredDepartmentKeywords={['LIVE']} onChange={(next) => field('regionalPocs', next)} /></div>
    <div className={`${DEMAND_TABLE_CELL} !bg-sky-50`} role="cell"><select aria-label="是否承接 / Accepted" value={value.acceptance} onChange={(event) => field('acceptance', event.target.value)} className={tableSelectClass}><option value="yes">是 / Yes</option><option value="no">否 / No</option><option value="tbd">待定 / TBD</option></select></div>
    <div className={`${DEMAND_TABLE_CELL} flex min-h-28 items-start !bg-sky-50 px-2 py-2`} role="cell"><FeishuPeoplePickerInput owners={value.platformPocs} options={people} preferredDepartmentKeywords={['LIVE Platform', 'Platform', 'LIVE']} onChange={(next) => field('platformPocs', next)} /></div>
    <div className={`${DEMAND_TABLE_CELL} !bg-sky-50 px-2 py-2`} role="cell"><PlanKRSelect value={value.planKrIds} objectives={objectives} translations={translations} onChange={(next) => field('planKrIds', next)} /></div>
    <div className={DEMAND_TABLE_CELL} role="cell"><textarea aria-label="交付物 / Deliverable" value={value.deliverable} onChange={(event) => field('deliverable', event.target.value)} className={tableInputClass} rows={5} /></div>
    <div className={`${DEMAND_TABLE_CELL} flex flex-col items-center justify-center gap-2 px-2 py-2 text-[9px] text-slate-400`} role="cell">{isNew ? <span>{saving ? '保存中… / Saving…' : '新增行 / New row'}</span> : <><button type="button" onClick={onComment} className="text-indigo-600 hover:underline">评论 / Comment</button><button type="button" disabled={busy || saving} onClick={onDelete} className="text-red-600 hover:underline disabled:opacity-40">删除 / Delete</button>{saving && <span>保存中… / Saving…</span>}</>}</div>
  </div>
}

function DemandTable({ demands, drafts, objectives, translations, people, busy, onSave, onDraftSaved, onDelete, onComment }: { demands: RegionalDemand[]; drafts: RegionalDemandDraft[]; objectives: Objective[]; translations: TranslationMap; people: KrOwner[]; busy: boolean; onSave: (value: RegionalDemand | Omit<RegionalDemand, 'id'>) => Promise<RegionalDemand | undefined>; onDraftSaved: (key: string) => void; onDelete: (value: RegionalDemand) => void; onComment: (value: RegionalDemand) => void }) {
  return <div>
    <div className="mb-3 rounded-lg border border-sky-100 bg-sky-50/70 px-3 py-2 text-xs leading-5 text-slate-600">
      <p>区域 OKR、具体需求、优先级、区域负责人列由区域运营填写；是否承接、平台负责人、关联 Platform OKR 列由 Platform 团队填写。</p>
      <p className="text-[11px] text-slate-400">Regional Operations fills in Regional OKR, Detailed Requirement, Priority, and Regional POC. The Platform team fills in Accepted, Platform POC, and Related Platform OKR.</p>
    </div>
    <div className="overflow-x-auto">
      <div className="min-w-[1360px] overflow-hidden rounded-xl border border-slate-200" role="table" aria-label="区域需求表格 / Regional requirement table">
        <div className={`grid ${DEMAND_TABLE_COLUMNS}`} role="row">
          <div className={DEMAND_TABLE_HEADER} role="columnheader"><Bilingual zh="区域 OKR" en="Regional OKR" /></div><div className={DEMAND_TABLE_HEADER} role="columnheader"><Bilingual zh="具体需求" en="Detailed requirement" /></div><div className={DEMAND_TABLE_HEADER} role="columnheader"><Bilingual zh="优先级" en="Priority" /></div><div className={DEMAND_TABLE_HEADER} role="columnheader"><Bilingual zh="区域负责人" en="Regional POC" /></div><div className={`${DEMAND_TABLE_HEADER} !bg-sky-100`} role="columnheader"><Bilingual zh="是否承接" en="Accepted" /></div><div className={`${DEMAND_TABLE_HEADER} !bg-sky-100`} role="columnheader"><Bilingual zh="平台负责人" en="Platform POC" /></div><div className={`${DEMAND_TABLE_HEADER} !bg-sky-100`} role="columnheader"><Bilingual zh="关联 Platform OKR" en="Related Platform OKR" /></div><div className={DEMAND_TABLE_HEADER} role="columnheader"><Bilingual zh="交付物" en="Deliverable" /></div><div className={DEMAND_TABLE_HEADER} role="columnheader"><Bilingual zh="操作" en="Actions" /></div>
        </div>
        {demands.map((demand) => <DemandEditor key={demand.id} initial={demand} objectives={objectives} translations={translations} people={people} busy={busy} onSave={onSave} onDelete={() => onDelete(demand)} onComment={() => onComment(demand)} />)}
        {drafts.map((draft) => <DemandEditor key={draft.key} initial={draft.value} objectives={objectives} translations={translations} people={people} busy={busy} onSave={async (value) => { const saved = await onSave(value); if (saved) onDraftSaved(draft.key); return saved }} />)}
      </div>
    </div>
    <p className="mt-2 text-[10px] text-slate-400">停止输入后自动保存。/ Changes save automatically after you stop typing.</p>
  </div>
}

function DecisionEditor({ initial, region, people, onSave, onComment }: { initial: RegionalPlanDecisionItem; region: RegionalCode; people: KrOwner[]; onSave: (value: RegionalPlanDecisionItem) => Promise<RegionalPlanDecisionItem | undefined>; onComment: () => void }) {
  const [value, setValue] = useState(initial)
  const [saving, setSaving] = useState(false)
  const lastSavedRef = useRef(regionalDecisionSignature(initial))
  const attemptedRef = useRef(lastSavedRef.current)
  const incomingSignature = regionalDecisionSignature(initial)
  useEffect(() => {
    const previousSaved = lastSavedRef.current
    lastSavedRef.current = incomingSignature
    attemptedRef.current = incomingSignature
    setValue((current) => regionalDecisionSignature(current) === previousSaved ? initial : { ...current, version: initial.version })
  }, [initial.version, incomingSignature])
  const signature = regionalDecisionSignature(value)
  useEffect(() => {
    if (saving || signature === lastSavedRef.current || signature === attemptedRef.current) return
    const timer = window.setTimeout(() => {
      const snapshot = value
      const snapshotSignature = signature
      attemptedRef.current = snapshotSignature
      setSaving(true)
      void onSave(snapshot).then((updated) => {
        if (!updated) return
        lastSavedRef.current = regionalDecisionSignature(updated)
        attemptedRef.current = lastSavedRef.current
        setValue((current) => regionalDecisionSignature(current) === snapshotSignature ? updated : { ...current, version: updated.version })
      }).finally(() => setSaving(false))
    }, 600)
    return () => window.clearTimeout(timer)
  }, [onSave, saving, signature, value])
  const change = <K extends keyof RegionalPlanDecisionItem>(key: K, next: RegionalPlanDecisionItem[K]) => setValue((current) => ({ ...current, [key]: next }))
  const suggestions = launchRegionOptions(region)
  const controlClass = 'mt-1.5 h-9 w-full rounded-lg border border-slate-200 bg-white px-2.5 text-xs text-slate-700 outline-none focus:border-indigo-400'
  const fieldClass = 'flex min-w-0 flex-col justify-end text-[10px] font-medium text-slate-600'
  return <div id={commentTargetElementId({ type: 'alignment_item', id: `plan:${value.planKrId}` })} className="mt-3 rounded-xl border border-amber-200 bg-amber-50 p-3 shadow-[inset_4px_0_0_#f59e0b]">
    <div className="grid items-end gap-3 sm:grid-cols-2 xl:grid-cols-[8rem_minmax(12rem,1.25fr)_minmax(11rem,1fr)_minmax(14rem,1.5fr)_auto]">
      <label className={`${fieldClass} font-semibold text-amber-900`}>是否上车 / Onboard<select value={value.onboard} onChange={(event) => change('onboard', event.target.value as RegionalPlanDecisionItem['onboard'])} className={controlClass}><option value="">未选择 / Not selected</option><option value="yes">是 / Yes</option><option value="no">否 / No</option></select></label>
      <label className={fieldClass}>上车地区 / Launch regions<Select mode="tags" allowClear value={value.launchRegions} onChange={(next) => change('launchRegions', next)} tokenSeparators={[',', '，', '、']} options={suggestions.map((item) => ({ label: item, value: item }))} placeholder="多选或自行输入 / Select or enter" aria-label="上车地区 / Launch regions" className="mt-1.5 w-full" style={{ minHeight: 36 }} maxTagCount="responsive" /></label>
      <label className={fieldClass}>区域负责人 / Regional POC<div className="mt-1.5 flex h-9 items-center rounded-lg border border-slate-200 bg-white px-2"><FeishuPeoplePickerInput small owners={value.regionalPocs} options={people} preferredDepartmentKeywords={['LIVE']} onChange={(next) => change('regionalPocs', next)} /></div></label>
      <label className={fieldClass}>关联区域 OKR / Related regional OKR<input value={value.regionalOkr} onChange={(event) => change('regionalOkr', event.target.value)} className={controlClass} /></label>
      <div className="flex h-9 items-center justify-end self-end sm:col-span-2 xl:col-span-1"><button type="button" onClick={onComment} aria-label="评论 / Comment" className="h-9 rounded-lg border border-amber-200 bg-white px-3 text-xs text-slate-500 hover:text-indigo-600">💬 评论 / Comment</button></div>
    </div>
  </div>
}

function PlatformSection({ board, category, priority, people, busy, onDecision, onComment }: { board: RegionalAlignmentBoard; category: PlatformCategory; priority: RegionalPriorityFilter; people: KrOwner[]; busy: boolean; onDecision: (value: RegionalPlanDecisionItem) => Promise<RegionalPlanDecisionItem | undefined>; onComment: (target: CommentTarget) => void }) {
  const [showHidden, setShowHidden] = useState(false)
  const decisions = new Map(board.decisions.map((item) => [item.planKrId, item]))
  const objectives = board.plan.objectives.map((objective) => ({ ...objective, krs: objective.krs.filter((kr) => (category === 'all' || categoryOf(kr) === category) && matchesRegionalPriority(kr, priority) && (showHidden || !decisions.get(kr.id)?.hidden)) })).filter((objective) => objective.krs.length)
  return <div className="space-y-3"><label className="flex justify-end text-[10px] text-slate-400"><input type="checkbox" checked={showHidden} onChange={(event) => setShowHidden(event.target.checked)} className="mr-1" />显示已移除 Platform OKR / Show removed Platform OKRs</label>
    {objectives.map((objective) => <details key={objective.id} open className="rounded-xl border border-slate-200 bg-white p-3 shadow-sm"><summary className="flex cursor-pointer list-none items-start text-sm font-semibold text-slate-800"><OKRText text={objective.title} translations={board.translations} className="min-w-0 flex-1" /></summary>{objective.krs.map((kr) => {
      const decision = decisions.get(kr.id) ?? { planKrId: kr.id, version: 0, onboard: '', launchRegions: [], regionalPocs: [], regionalOkr: '', hidden: false } as RegionalPlanDecisionItem
      return <div key={kr.id} className={`mt-3 border-t border-slate-100 pt-2 ${decision.hidden ? 'opacity-60' : ''}`}><KRDetails kr={kr} translations={board.translations} /><DecisionEditor initial={decision} region={board.region.regionCode} people={people} onSave={onDecision} onComment={() => onComment({ type: 'alignment_item', id: `plan:${kr.id}`, title: kr.title })} /><button type="button" disabled={busy} onClick={() => void onDecision({ ...decision, hidden: !decision.hidden })} className="mt-1 text-[9px] text-slate-400 hover:text-red-600">{decision.hidden ? '恢复到本区域视图 / Restore to this region' : '从本区域视图移除 / Remove from this region'}</button></div>
    })}</details>)}
    {objectives.length === 0 && <div className="rounded-xl border border-dashed border-slate-300 px-5 py-10 text-center text-xs text-slate-400">当前分类和优先级暂无 Platform OKR / No Platform OKRs for this category and priority</div>}
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
    ? <section className="rounded-xl border border-slate-200 bg-white p-3">{demands.length ? demands.map((demand) => <div key={demand.id} className="mt-2 rounded-lg bg-slate-50 p-2 text-[10px] leading-5"><div className="font-medium text-slate-700">{demand.requirement || demand.item || '未填写需求 / Requirement not set'}</div><div className="flex items-center gap-1 text-slate-400"><span>区域负责人 / Regional POC：</span><PeopleInline people={demand.regionalPocs} compact empty="未填写 / Not set" /></div><div className="flex items-center gap-1 text-slate-400"><span>平台负责人 / Platform POC：</span><PeopleInline people={demand.platformPocs} compact empty="未填写 / Not set" /></div></div>) : <p className="mt-3 text-[10px] text-slate-400">暂无不承接区域需求 / No declined regional requirements</p>}</section>
    : <section className="rounded-xl border border-slate-200 bg-white p-3">{objectives.length ? objectives.map((objective) => <details key={objective.id} open className="mt-2"><summary className="flex cursor-pointer items-start text-[11px] font-semibold text-slate-700"><OKRText text={objective.title} translations={board.translations} className="min-w-0 flex-1" /></summary>{objective.krs.map((kr) => <KRDetails key={kr.id} kr={kr} translations={board.translations} />)}</details>) : <p className="mt-3 text-[10px] text-slate-400">暂无区域不上车的 Platform OKR / No Platform OKRs declined by the region</p>}</section>}</div>
  return <div className="space-y-3">{objectives.map((objective) => {
    const linkedDemands = demands.filter((demand) => demand.planKrIds.some((id) => objective.krs.some((kr) => kr.id === id)))
    return <article key={objective.id} className="rounded-xl border border-slate-200 bg-white p-4 shadow-sm"><h3 className="text-sm font-semibold text-slate-800"><OKRText text={objective.title} translations={board.translations} /></h3><div className="mt-3 grid gap-3 lg:grid-cols-2"><div><div className="text-[10px] font-semibold text-indigo-600">平台团队重点 / Key points for the Platform team</div>{objective.krs.map((kr) => <div key={kr.id}><KRDetails kr={kr} translations={board.translations} /><div className="mt-1 flex items-center gap-1 text-[10px] text-slate-400"><span>平台负责人 / Platform POC：</span><PeopleInline people={krOwners(kr)} compact empty="未填写 / Not set" /></div></div>)}</div><div><div className="text-[10px] font-semibold text-emerald-600">区域团队重点 / Key points for the Regional team</div>{linkedDemands.map((demand) => <details key={demand.id} open className="mt-2 rounded-lg bg-emerald-50/60 p-2"><summary className="cursor-pointer text-[10px] font-medium text-slate-700">{demand.requirement || demand.item || '未填写需求 / Requirement not set'}</summary><div className="mt-1 flex items-center gap-1 text-[10px] text-slate-400"><span>区域负责人 / Regional POC：</span><PeopleInline people={demand.regionalPocs} compact empty="未填写 / Not set" /></div></details>)}{linkedDemands.length === 0 && <p className="mt-2 text-[10px] text-slate-400">暂无关联区域需求 / No linked regional requirements</p>}</div></div></article>
  })}{objectives.length === 0 && demands.length === 0 && <div className="rounded-xl border border-dashed border-slate-300 px-5 py-10 text-center text-xs text-slate-400">当前条件下暂无匹配项目 / No matching projects</div>}{demands.filter((demand) => demand.planKrIds.length === 0).map((demand) => <article key={demand.id} className="rounded-xl border border-emerald-200 bg-emerald-50/50 p-3"><div className="text-[10px] font-semibold text-emerald-700">未关联 Platform KR 的区域需求 / Regional requirement without a linked Platform KR</div><div className="mt-1 text-[11px] text-slate-700">{demand.requirement || demand.item}</div></article>)}</div>
}

function RecapSection({ board, busy, onOrder, onHide }: { board: RegionalAlignmentBoard; busy: boolean; onOrder: (bucket: string, objectives: Objective[]) => void; onHide: (bucket: string, objective: Objective, hidden: boolean) => void }) {
  const [category, setCategory] = useState<'全部OKR' | Category>('全部OKR')
  const [priority, setPriority] = useState<RegionalPriorityFilter>('all')
  const [showHidden, setShowHidden] = useState(false)
  const [dragging, setDragging] = useState<string>()
  const bucket = `${category}:${priority}`
  const overlays = new Map(board.recapOverlays.filter((item) => item.bucketKey === bucket).map((item) => [item.objectiveId, item]))
  const filtered = board.recap.objectives.filter((objective) => objective.krs.some((kr) => (category === '全部OKR' || categoryOf(kr) === category) && matchesRegionalPriority(kr, priority)))
  const ordered = [...filtered].sort((left, right) => (overlays.get(left.id)?.sortOrder ?? 9999) - (overlays.get(right.id)?.sortOrder ?? 9999))
  const visible = ordered.filter((objective) => showHidden || !overlays.get(objective.id)?.hidden)
  const categoryLabel = (item: '全部OKR' | Category) => item === '全部OKR' ? '全部 OKR / All OKRs' : item === '公会业务' ? '公会业务 / Agency' : item === '运营效率' ? '运营效率 / Operational Efficiency' : item === '优质主播专项' ? '优质主播&内容专项 / Premium Creators & Content' : 'AI提效 / AI Efficiency'
  return <>
    <div className="mb-3 flex flex-wrap items-center gap-2"><div className="flex flex-wrap gap-1">{(['全部OKR', ...CATEGORIES] as const).map((item) => <button key={item} type="button" onClick={() => setCategory(item)} className={`rounded-lg border px-2.5 py-1 text-[10px] ${category === item ? 'border-indigo-300 bg-indigo-50 text-indigo-700' : 'border-slate-200 bg-white text-slate-500'}`}>{categoryLabel(item)}</button>)}</div><label className="ml-auto text-[10px] text-slate-400"><input type="checkbox" checked={showHidden} onChange={(event) => setShowHidden(event.target.checked)} className="mr-1" />显示已移除 / Show removed</label></div>
    <div className="mb-4 border-t border-slate-100 pt-3"><PriorityTabs value={priority} onChange={setPriority} /></div>
    <div className="space-y-3">{visible.map((objective) => <details key={objective.id} open draggable onDragStart={() => setDragging(objective.id)} onDragOver={(event) => event.preventDefault()} onDrop={() => { if (!dragging || dragging === objective.id) return; const next = [...ordered]; const from = next.findIndex((item) => item.id === dragging); const to = next.findIndex((item) => item.id === objective.id); next.splice(to, 0, next.splice(from, 1)[0]); setDragging(undefined); onOrder(bucket, next) }} className={`rounded-xl border p-3 ${overlays.get(objective.id)?.hidden ? 'border-dashed border-slate-300 bg-slate-50 opacity-70' : 'border-slate-200 bg-white'}`}><summary className="flex cursor-grab list-none items-start gap-2 text-sm font-semibold text-slate-800"><span className="shrink-0 pt-0.5">⋮⋮</span><OKRText text={objective.title} translations={board.translations} className="min-w-0 flex-1" /></summary>{objective.krs.filter((kr) => (category === '全部OKR' || categoryOf(kr) === category) && matchesRegionalPriority(kr, priority)).map((kr) => <KRDetails key={kr.id} kr={kr} translations={board.translations} />)}<button type="button" disabled={busy} onClick={() => onHide(bucket, objective, !overlays.get(objective.id)?.hidden)} className="mt-2 text-[9px] text-slate-400 hover:text-red-600">{overlays.get(objective.id)?.hidden ? '恢复 / Restore' : '移除 / Remove'}</button></details>)}{visible.length === 0 && <div className="rounded-xl border border-dashed border-slate-300 px-5 py-10 text-center text-xs text-slate-400">当前分类和优先级暂无上季度 Review 内容 / No Review content for this category and priority</div>}</div>
  </>
}

export default function RegionalAlignmentApp({ initialQuarter, initialRegion, initialCommentId = '', autoMatchAccess, onScopeChange }: { initialQuarter: string; initialRegion?: string; initialCommentId?: string; autoMatchAccess: boolean; onScopeChange?: (quarter: string, region: RegionalCode) => void }) {
  const validRegion = REGIONS.some((item) => item.code === initialRegion) ? initialRegion as RegionalCode : 'eu'
  const [quarter, setQuarter] = useState(initialQuarter)
  const [quarterDraft, setQuarterDraft] = useState(initialQuarter)
  const [region, setRegion] = useState<RegionalCode>(validRegion)
  const [board, setBoard] = useState<RegionalAlignmentBoard>()
  const [demandDrafts, setDemandDrafts] = useState<RegionalDemandDraft[]>([])
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [platformCategory, setPlatformCategory] = useState<PlatformCategory>('all')
  const [platformPriority, setPlatformPriority] = useState<RegionalPriorityFilter>('all')
  const [alignmentCategory, setAlignmentCategory] = useState<Category>('公会业务')
  const [matchBucket, setMatchBucket] = useState<MatchBucket>('p0')
  const [matchNotice, setMatchNotice] = useState('')
  const [refreshing, setRefreshing] = useState<'' | 'platform' | 'recap'>('')
  const [refreshNotice, setRefreshNotice] = useState('')
  const [savedNotice, setSavedNotice] = useState('')
  const savedNoticeTimer = useRef<number | undefined>(undefined)
  const [shareNotice, setShareNotice] = useState('')
  const [shareLink, setShareLink] = useState('')
  const [commentsOpen, setCommentsOpen] = useState(Boolean(initialCommentId))
  const [commentReviewMode, setCommentReviewMode] = useState<CommentReviewMode>()
  const [commentCount, setCommentCount] = useState(0)
  const [commentCounts, setCommentCounts] = useState<Record<string, number>>({})
  const [comments, setComments] = useState<PageComment[]>([])
  const [commentTarget, setCommentTarget] = useState<CommentTarget>()
  const [focusedComment, setFocusedComment] = useState<PageComment>()

  const load = async (targetQuarter = quarter, targetRegion = region) => {
    setLoading(true); setError('')
    try { const value = await getRegionalAlignmentBoard(targetQuarter, targetRegion); setBoard(value); onScopeChange?.(targetQuarter, targetRegion) }
    catch (reason) { setError(reason instanceof Error ? reason.message : '区域对齐页加载失败 / Failed to load regional alignment') }
    finally { setLoading(false) }
  }
  useEffect(() => { void load() }, [quarter, region])
  useEffect(() => { setDemandDrafts([]) }, [quarter, region])
  useEffect(() => { if (focusedComment) window.setTimeout(() => scrollToCommentSource(focusedComment), 80) }, [focusedComment])
  useEffect(() => {
    const refreshSourceBoards = () => {
      if (document.visibilityState !== 'visible' || document.activeElement?.matches('input, textarea, select, [contenteditable="true"]')) return
      void getRegionalAlignmentBoard(quarter, region).then((value) => setBoard(value)).catch((reason) => setError(reason instanceof Error ? reason.message : '刷新失败 / Refresh failed'))
    }
    window.addEventListener('focus', refreshSourceBoards)
    document.addEventListener('visibilitychange', refreshSourceBoards)
    return () => { window.removeEventListener('focus', refreshSourceBoards); document.removeEventListener('visibilitychange', refreshSourceBoards) }
  }, [quarter, region])
  useEffect(() => () => { if (savedNoticeTimer.current) window.clearTimeout(savedNoticeTimer.current) }, [])

  const flashSaved = useCallback(() => {
    if (savedNoticeTimer.current) window.clearTimeout(savedNoticeTimer.current)
    setSavedNotice('已保存 / Saved')
    savedNoticeTimer.current = window.setTimeout(() => setSavedNotice(''), 3000)
  }, [])

  const objectives = board?.plan.objectives ?? []
  const people = useMemo(() => ownerOptions(objectives), [objectives])
  const categoryOrder = normalizedCategoryOrder(board?.region.categoryOrder ?? [])
  const addDemandDraft = useCallback(() => {
    if (!board) return
    setDemandDrafts((current) => [...current, {
      key: uid('regional-demand-draft'),
      value: emptyDemand(nextDemandSortOrder([...board.demands, ...current.map((draft) => draft.value)])),
    }])
  }, [board])
  const saveDemand = useCallback(async (value: RegionalDemand | Omit<RegionalDemand, 'id'>): Promise<RegionalDemand | undefined> => {
    setBusy(true); setError('')
    try {
      if ('id' in value) {
        const updated = await updateRegionalDemand(quarter, region, value)
        setBoard((current) => current ? { ...current, demands: current.demands.map((item) => item.id === updated.id ? updated : item) } : current)
        flashSaved()
        return updated
      }
      const created = await createRegionalDemand(quarter, region, value)
      setBoard((current) => current ? { ...current, demands: [...current.demands, created] } : current)
      flashSaved()
      return created
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '保存失败 / Save failed')
      return undefined
    } finally {
      setBusy(false)
    }
  }, [flashSaved, quarter, region])
  const removeDemand = async (value: RegionalDemand) => { if (!window.confirm('确认删除这条区域需求？ / Delete this regional requirement?')) return; setBusy(true); try { await deleteRegionalDemand(quarter, region, value); setBoard((current) => current ? { ...current, demands: current.demands.filter((item) => item.id !== value.id) } : current) } catch (reason) { setError(reason instanceof Error ? reason.message : '删除失败 / Delete failed') } finally { setBusy(false) } }
  const saveDecision = useCallback(async (value: RegionalPlanDecisionItem): Promise<RegionalPlanDecisionItem | undefined> => {
    setError('')
    try {
      const updated = await putRegionalDecision(quarter, region, value)
      setBoard((current) => current ? { ...current, decisions: [...current.decisions.filter((item) => item.planKrId !== updated.planKrId), updated] } : current)
      flashSaved()
      return updated
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '保存失败 / Save failed')
      return undefined
    }
  }, [flashSaved, quarter, region])
  const reorderCategories = async (next: Category[]) => { if (!board) return; setBusy(true); try { const updated = await putRegionalCategoryOrder(quarter, region, board.region.version, next); setBoard({ ...board, region: updated }) } catch (reason) { setError(reason instanceof Error ? reason.message : '分类顺序保存失败 / Failed to save category order') } finally { setBusy(false) } }
  const refreshLiveSources = useCallback(async (target: 'platform' | 'recap') => {
    setRefreshing(target); setRefreshNotice(''); setError('')
    try {
      const updated = await refreshRegionalAlignmentBoard(quarter, region)
      setBoard(updated)
      setRefreshNotice(target === 'platform' ? 'Platform Team 已读取最新 Biz OKR Plan 并更新英文 / Latest Plan and English translations loaded' : '上季度复盘已读取最新 Review 进展并更新英文 / Latest Review progress and English translations loaded')
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '刷新失败 / Refresh failed')
    } finally {
      setRefreshing('')
    }
  }, [quarter, region])
  const copyShare = async () => {
    // Sharing is independent from any earlier load/save failure. Do not let a
    // stale workspace error replace the share result while showing its link.
    setError('')
    setShareNotice('')
    setShareLink('')
    let link: string
    try {
      const config = await getWebConfig()
      link = regionalAlignmentShareURL(window.location.href, config.public_base_url, quarter, region)
    } catch (reason) {
      setShareNotice(reason instanceof Error ? `分享链接生成失败：${reason.message}` : '分享链接生成失败 / Failed to create share link')
      return
    }

    // The link is useful output in its own right. Keep it visible whether the
    // browser grants clipboard access or the automatic copy falls back.
    setShareLink(link)
    try {
      const copied = await copyRegionalAlignmentShareLink(link, navigator.clipboard)
      if (!copied) {
        setShareNotice('当前页面无法自动复制，请复制下方链接 / Copy the link below')
        return
      }
      setShareNotice('区域 OKR 对齐页链接已复制 / Link copied')
    } catch (reason) {
      setShareNotice(reason instanceof Error ? `自动复制失败：${reason.message}；请复制下方链接` : '自动复制失败，请复制下方链接 / Copy the link below')
    }
  }
  const openComments = (target?: CommentTarget) => { setCommentTarget(target); setFocusedComment(undefined); setCommentReviewMode(undefined); setCommentsOpen(true) }

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
            <span className="border-r border-slate-100 px-2.5 text-[10px] font-medium text-slate-400">季度 / Quarter</span>
            <input value={quarterDraft} onChange={(event) => setQuarterDraft(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') setQuarter(quarterDraft.trim()) }} className="h-full w-24 px-2.5 text-xs outline-none" aria-label="季度 / Quarter" />
            <button type="button" onClick={() => setQuarter(quarterDraft.trim())} className="h-full border-l border-slate-100 px-3 text-xs font-medium text-slate-600 hover:bg-slate-50">切换 / Go</button>
          </div>
          <button type="button" onClick={() => void copyShare()} className="h-9 rounded-lg border border-slate-200 bg-white px-3 text-xs font-medium text-slate-600 hover:border-indigo-200 hover:text-indigo-600">分享页面 / Share</button>
          <button type="button" onClick={() => commentsOpen ? setCommentsOpen(false) : openComments()} className={`relative h-9 rounded-lg border px-3 text-xs font-medium ${commentsOpen ? 'border-indigo-200 bg-indigo-50 text-indigo-700' : 'border-slate-200 bg-white text-slate-600 hover:border-indigo-200'}`}>💬 评论 / Comments{commentCount > 0 && <span className="ml-1 rounded-full bg-indigo-600 px-1.5 py-0.5 text-[9px] text-white">{commentCount}</span>}</button>
        </div>
      </div>
    </header>
    <main className={`mx-auto max-w-[1480px] space-y-6 px-4 py-5 sm:px-6 lg:px-8 ${commentsOpen ? 'lg:pr-[420px]' : ''}`}>
      {savedNotice && <div role="status" className="rounded-xl border border-emerald-200 bg-emerald-50 px-4 py-3 text-xs font-medium text-emerald-700 shadow-sm">✓ {savedNotice}</div>}
      {refreshNotice && <div role="status" className="rounded-xl border border-blue-200 bg-blue-50 px-4 py-3 text-xs font-medium text-blue-700 shadow-sm">↻ {refreshNotice}</div>}
      {error && <div role="alert" className="rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-xs text-red-700">{error}</div>}
      {(shareNotice || shareLink) && <div role="status" className="rounded-xl border border-blue-100 bg-blue-50 px-4 py-3 text-xs text-blue-700">{shareNotice}{shareLink && <input aria-label="分享链接 / Share link" readOnly value={shareLink} onFocus={(event) => event.currentTarget.select()} onClick={(event) => event.currentTarget.select()} className="mt-2 h-9 w-full rounded-lg border border-blue-200 bg-white px-3" />}</div>}
      <nav className="flex min-w-0 gap-1 overflow-x-auto rounded-xl border border-slate-200 bg-white p-1 shadow-sm">{REGIONS.map((item) => <button key={item.code} type="button" onClick={() => setRegion(item.code)} className={`min-w-24 flex-1 rounded-lg px-4 py-2.5 text-xs font-semibold transition ${region === item.code ? 'bg-indigo-600 text-white shadow-sm' : 'text-slate-500 hover:bg-slate-50 hover:text-slate-700'}`}>{item.label}</button>)}</nav>
      {loading ? <div className="rounded-2xl border border-slate-200 bg-white py-24 text-center text-sm text-slate-400">正在加载区域对齐数据… / Loading…</div> : board ? <CommentInteractionProvider value={{ enabled: true, triggerMode: 'button', selected: commentTarget, focused: focusedComment, comments, counts: commentCounts, setPendingSelection: () => undefined, select: openComments }}>
        <div className="space-y-6">
          <section className="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm sm:p-6">
            <CollapsibleBlock title="Part 0 · Pre-alignment" level="primary">
              <div className="space-y-8">
                <CollapsibleBlock title="Regional Ops Team 高优痛点&核心需求" subtitle="High-priority pain points & core requirements" level="secondary" action={<button type="button" onClick={addDemandDraft} className="h-9 rounded-lg bg-indigo-600 px-4 text-xs font-semibold text-white hover:bg-indigo-700">新增需求 / Add requirement</button>}>
                  <DemandTable demands={board.demands} drafts={demandDrafts} objectives={objectives} translations={board.translations} people={people} busy={busy} onSave={saveDemand} onDraftSaved={(key) => setDemandDrafts((current) => current.filter((draft) => draft.key !== key))} onDelete={(demand) => void removeDemand(demand)} onComment={(demand) => openComments({ type: 'alignment_item', id: `demand:${demand.id}`, title: demand.requirement || demand.item || '区域需求 / Regional requirement' })} />
                </CollapsibleBlock>
                <div className="border-t border-slate-200 pt-6">
                  <CollapsibleBlock title="Platform Team 平台团队关键项目 & 重要业务解决方案" subtitle="Key projects & important solutions" level="secondary" action={<button type="button" disabled={Boolean(refreshing)} onClick={() => void refreshLiveSources('platform')} className="h-9 rounded-lg border border-indigo-200 bg-white px-4 text-xs font-semibold text-indigo-600 hover:bg-indigo-50 disabled:opacity-40">{refreshing === 'platform' ? '刷新并翻译中… / Refreshing…' : '刷新 / Refresh'}</button>}>
                    <PlatformCategoryTabs order={categoryOrder} value={platformCategory} onChange={setPlatformCategory} onReorder={(next) => void reorderCategories(next)} />
                    <div className="mt-3 border-t border-slate-100 pt-3"><PriorityTabs value={platformPriority} onChange={setPlatformPriority} /></div>
                    <div className="mt-4"><PlatformSection board={board} category={platformCategory} priority={platformPriority} people={people} busy={busy} onDecision={saveDecision} onComment={openComments} /></div>
                  </CollapsibleBlock>
                </div>
              </div>
            </CollapsibleBlock>
          </section>
          <section className="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm sm:p-6">
            <CollapsibleBlock title="Part 1 · OKR Alignment" subtitle="OKR 对齐 · Regional & Platform project alignment" level="primary" action={autoMatchAccess ? <button type="button" disabled={loading} onClick={() => { void load().then(() => setMatchNotice(`已按最新数据匹配 / Matched with latest data · ${new Date().toLocaleTimeString()}`)) }} className="rounded-lg bg-indigo-600 px-4 py-2 text-xs font-semibold text-white disabled:opacity-40">自动匹配<br /><span className="text-[9px] font-normal opacity-80">Auto match</span></button> : undefined}>
              {matchNotice && <div className="mb-3 text-right text-[10px] text-emerald-600">{matchNotice}</div>}
              <div className="mb-4 flex flex-wrap gap-2">{([{ key: 'p0', label: '焦点项目 / Focus-item Projects' }, { key: 'p12', label: 'P1 & P2 项目 / P1 & P2 Projects' }, { key: 'pending', label: '待定事项 / Pending items' }] as const).map((item) => <button key={item.key} type="button" onClick={() => setMatchBucket(item.key)} className={`rounded-lg border px-3 py-2 text-xs font-medium ${matchBucket === item.key ? 'border-indigo-300 bg-indigo-50 text-indigo-700' : 'border-slate-200 bg-white text-slate-500'}`}>{item.label}</button>)}</div>
              <CategoryTabs order={categoryOrder} value={alignmentCategory} onChange={setAlignmentCategory} onReorder={(next) => void reorderCategories(next)} />
              <div className="mt-4"><AlignmentProjects board={board} bucket={matchBucket} category={alignmentCategory} /></div>
            </CollapsibleBlock>
          </section>
          <section className="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm sm:p-6">
            <CollapsibleBlock title="Part 2 · Recap For Last Quarter" subtitle={`上季度复盘 · ${board.alignment.recapQuarter} Review recap`} level="primary" action={<button type="button" disabled={Boolean(refreshing)} onClick={() => void refreshLiveSources('recap')} className="h-9 rounded-lg border border-indigo-200 bg-white px-4 text-xs font-semibold text-indigo-600 hover:bg-indigo-50 disabled:opacity-40">{refreshing === 'recap' ? '刷新并翻译中… / Refreshing…' : '刷新 / Refresh'}</button>}>
              <RecapSection board={board} busy={busy} onOrder={(bucket, next) => { void (async () => { try { const values = await putRegionalRecapOrder(quarter, region, bucket, next.map((item) => item.id)); setBoard({ ...board, recapOverlays: [...board.recapOverlays.filter((item) => item.bucketKey !== bucket), ...values] }) } catch (reason) { setError(reason instanceof Error ? reason.message : '顺序保存失败 / Failed to save order') } })() }} onHide={(bucket, objective, hidden) => { void (async () => { const current = board.recapOverlays.find((item) => item.bucketKey === bucket && item.objectiveId === objective.id); try { const updated = await patchRegionalRecap(quarter, region, bucket, objective.id, current?.version ?? 0, hidden); setBoard({ ...board, recapOverlays: [...board.recapOverlays.filter((item) => !(item.bucketKey === bucket && item.objectiveId === objective.id)), updated] }) } catch (reason) { setError(reason instanceof Error ? reason.message : '移除失败 / Remove failed') } })() }} />
            </CollapsibleBlock>
          </section>
        </div>
      </CommentInteractionProvider> : <div className="rounded-2xl border border-slate-200 bg-white py-24 text-center text-sm text-slate-400">暂无区域对齐数据 / No regional alignment data</div>}
    </main>
    {board && <CommentDrawer open={commentsOpen} reviewEnabled reviewMode={commentReviewMode} quarter={quarter} alignmentId={board.alignment.id} alignmentRegion={region} sourceTab="regional-alignment" scopeLabel={`${REGIONS.find((item) => item.code === region)?.label} · ${quarter}`} objectives={[...board.plan.objectives, ...board.recap.objectives]} target={commentTarget} focusCommentId={initialCommentId} onStartReview={(mode) => { setCommentTarget(undefined); setCommentReviewMode(mode) }} onShowAll={() => { setCommentTarget(undefined); setFocusedComment(undefined); setCommentReviewMode(undefined) }} onClose={() => { setCommentsOpen(false); setCommentTarget(undefined); setFocusedComment(undefined); setCommentReviewMode(undefined) }} onFocusCommentChange={setFocusedComment} onCountChange={setCommentCount} onCountsChange={setCommentCounts} onCommentsChange={setComments} />}
  </>
}
