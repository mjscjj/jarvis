import { useMemo, useState } from 'react'
import { useBoard } from '../board'
import { tagLabel } from '../labels'
import { hasOwner, splitOwnerNames } from '../people'
import type { Kr, KrTag } from '../types'
import { FeishuPeoplePicker } from './FeishuPeoplePicker'
import { QuarterlyOKRDraft } from './QuarterlyOKRDraft'

type Tool = 'quarterly'

const TAG_TYPES = [
  { value: 'custom', label: '自定义' },
  { value: 'management_focus', label: '管理重点' },
  { value: 'biweekly', label: '双周会' },
  { value: 'platform_report', label: '中台周报' },
  { value: 'region', label: '区域' },
]

const TOOLS: Array<{ value: Tool; mark: string; label: string; description: string }> = [
	{ value: 'quarterly', mark: '季', label: '季度材料', description: '按 O / KR 汇总' },
]

function tagTone(type: string) {
  switch (type) {
    case 'management_focus': return 'bg-amber-50 text-amber-700 ring-amber-100'
    case 'biweekly': return 'bg-violet-50 text-violet-700 ring-violet-100'
    case 'platform_report': return 'bg-indigo-50 text-indigo-700 ring-indigo-100'
    case 'region': return 'bg-sky-50 text-sky-700 ring-sky-100'
    case 'topic': return 'bg-slate-100 text-slate-600 ring-slate-200'
    default: return 'bg-emerald-50 text-emerald-700 ring-emerald-100'
  }
}

function TagChip({ tag, onRemove }: { tag: KrTag; onRemove: () => void }) {
  return (
    <span className={`group/tag inline-flex h-5 items-center gap-1 rounded-md px-1.5 text-[10px] ring-1 ring-inset ${tagTone(tag.type)}`}>
      <span className="max-w-28 truncate">{tagLabel(tag.type, tag.value)}</span>
      <button type="button" onClick={onRemove} title="移除标签" className="-mr-0.5 text-current opacity-0 transition-opacity group-hover/tag:opacity-50 hover:!opacity-100">×</button>
    </span>
  )
}

function priorityTone(priority?: Kr['priority']) {
  if (priority === 'p0') return 'border-red-200 bg-red-50 font-semibold text-red-700'
  if (priority === 'p1') return 'border-amber-200 bg-amber-50 font-semibold text-amber-700'
  return 'border-slate-200 bg-white text-slate-500'
}

function TagEditor({ kr, suggestions }: { kr: Kr; suggestions: KrTag[] }) {
  const { addTag, removeTag } = useBoard()
  const [editing, setEditing] = useState(false)
  const [expanded, setExpanded] = useState(false)
  const [type, setType] = useState('custom')
  const [value, setValue] = useState('')
  const allTags = kr.tags ?? []
  const values = [...new Set(suggestions.filter((item) => item.type === type).map((item) => item.value))].sort()
  const visibleTags = expanded ? allTags : allTags.slice(0, 4)
  const hiddenCount = allTags.length - visibleTags.length

  const submit = () => {
    if (!value.trim()) return
    addTag(kr.id, value, type)
    setValue('')
    setEditing(false)
  }

  return (
    <div className="min-w-0">
      <div className="flex min-h-6 flex-wrap items-center gap-1">
        {visibleTags.map((tag) => <TagChip key={`${tag.type}:${tag.value}`} tag={tag} onRemove={() => removeTag(kr.id, tag.type, tag.value)} />)}
        {hiddenCount > 0 && <button type="button" onClick={() => setExpanded(true)} className="h-5 rounded-md bg-slate-50 px-1.5 text-[10px] text-slate-400 hover:bg-slate-100 hover:text-slate-600">+{hiddenCount}</button>}
        {expanded && allTags.length > 4 && <button type="button" onClick={() => setExpanded(false)} className="h-5 px-1 text-[10px] text-slate-400 hover:text-slate-600">收起</button>}
        {!editing && <button type="button" onClick={() => setEditing(true)} className="h-6 rounded-md border border-dashed border-slate-300 px-2 text-[10px] font-medium text-slate-500 hover:border-blue-300 hover:bg-blue-50 hover:text-blue-600">+ 添加标签</button>}
      </div>
      {editing && (
        <div className="mt-1.5 flex max-w-md items-center gap-1 rounded-md bg-slate-50 p-1">
          <select value={type} onChange={(event) => setType(event.target.value)} className="h-7 rounded border border-slate-200 bg-white px-1.5 text-[10px] text-slate-600 outline-none focus:border-blue-400">
            {TAG_TYPES.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
          </select>
          <input list={`tag-options-${kr.id}-${type}`} autoFocus value={value} onChange={(event) => setValue(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') submit(); if (event.key === 'Escape') setEditing(false) }} placeholder={type === 'region' ? '如 sea' : '标签值'} className="h-7 min-w-0 flex-1 rounded border border-slate-200 bg-white px-2 text-[10px] outline-none focus:border-blue-400" />
          <datalist id={`tag-options-${kr.id}-${type}`}>{values.map((item) => <option key={item} value={item} />)}</datalist>
          <button type="button" onClick={submit} disabled={!value.trim()} className="h-7 rounded bg-slate-800 px-2 text-[10px] font-medium text-white hover:bg-slate-700 disabled:opacity-30">添加</button>
          <button type="button" onClick={() => setEditing(false)} className="h-7 px-1.5 text-[10px] text-slate-400 hover:text-slate-600">取消</button>
        </div>
      )}
    </div>
  )
}

function KrEditorRow({ objectiveId, kr, peopleOptions, tagSuggestions }: { objectiveId: string; kr: Kr; peopleOptions: string[]; tagSuggestions: KrTag[] }) {
  const { setKrTitle, setKrPriority, deleteKr } = useBoard()
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [deleting, setDeleting] = useState(false)

  const remove = async () => {
    setDeleting(true)
    try {
      await deleteKr(kr.id)
    } catch {
      setDeleting(false)
      setConfirmDelete(false)
    }
  }

  return (
    <div className="group/kr grid grid-cols-[minmax(0,1fr)_auto] gap-2 px-3.5 py-2 transition-colors hover:bg-slate-50/70">
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-1.5">
          <input
            value={kr.title}
            onChange={(event) => setKrTitle(objectiveId, kr.id, event.target.value)}
            placeholder="填写 KR 内容"
            aria-label="KR 内容"
            className="h-7 min-w-64 flex-[1_1_32rem] rounded-md border border-transparent bg-transparent px-1.5 text-[11px] font-medium text-slate-700 outline-none transition-colors hover:border-slate-200 hover:bg-white focus:border-blue-300 focus:bg-white focus:ring-2 focus:ring-blue-50"
          />
          <FeishuPeoplePicker kr={kr} options={peopleOptions} />
          <select value={kr.priority ?? 'p1'} onChange={(event) => setKrPriority(kr.id, event.target.value as NonNullable<Kr['priority']>)} aria-label="优先级" className={`h-6 rounded-md border px-2 text-[10px] outline-none focus:border-blue-400 ${priorityTone(kr.priority)}`}>
            <option value="p0">P0</option><option value="p1">P1</option><option value="p2">P2</option>
          </select>
        </div>
        <div className="mt-1 px-1.5">
          <TagEditor kr={kr} suggestions={tagSuggestions} />
        </div>
      </div>
      <div className="flex min-w-12 items-center justify-end gap-1">
        {confirmDelete ? (
          <>
            <button type="button" onClick={() => void remove()} disabled={deleting} className="h-6 rounded-md bg-red-600 px-2 text-[9px] font-medium !text-white hover:bg-red-700 disabled:opacity-50">{deleting ? '删除中' : '确认'}</button>
            <button type="button" onClick={() => setConfirmDelete(false)} className="h-6 px-1 text-[9px] text-slate-400 hover:text-slate-700">取消</button>
          </>
        ) : <button type="button" onClick={() => setConfirmDelete(true)} title="删除 KR" className="h-6 rounded-md px-1.5 text-[10px] text-slate-300 transition-colors hover:bg-red-50 hover:text-red-600">删除</button>}
      </div>
    </div>
  )
}

function NewKrRow({ objectiveId, onClose }: { objectiveId: string; onClose: () => void }) {
  const { createKr } = useBoard()
  const [title, setTitle] = useState('')
  const [ownerName, setOwnerName] = useState('')
  const [priority, setPriority] = useState<NonNullable<Kr['priority']>>('p1')
  const [creating, setCreating] = useState(false)

  const submit = async () => {
    if (!title.trim() || creating) return
    setCreating(true)
    try {
      await createKr(objectiveId, { title: title.trim(), ownerName: ownerName.trim(), priority })
      onClose()
    } catch {
      setCreating(false)
    }
  }

  return (
    <div className="grid gap-1.5 bg-blue-50/50 px-3.5 py-2 md:grid-cols-[minmax(18rem,1.3fr)_minmax(10rem,0.55fr)_4.25rem_minmax(15rem,0.9fr)_auto] md:items-center md:gap-2">
      <input autoFocus value={title} onChange={(event) => setTitle(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') void submit(); if (event.key === 'Escape') onClose() }} placeholder="填写新 KR 内容" className="h-8 min-w-0 rounded-md border border-blue-200 bg-white px-2.5 text-[11px] font-medium text-slate-700 outline-none focus:border-blue-400 focus:ring-2 focus:ring-blue-100" />
      <input value={ownerName} onChange={(event) => setOwnerName(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') void submit() }} placeholder="负责人，可用、分隔多人" className="h-8 rounded-md border border-slate-200 bg-white px-2 text-[10px] outline-none focus:border-blue-400" />
      <select value={priority} onChange={(event) => setPriority(event.target.value as NonNullable<Kr['priority']>)} aria-label="新 KR 优先级" className={`h-7 rounded-md border px-2 text-[10px] outline-none ${priorityTone(priority)}`}><option value="p0">P0</option><option value="p1">P1</option><option value="p2">P2</option></select>
      <span className="text-[10px] text-slate-400">创建后可继续分配人员、优先级与标签；周进展在周报模块填写</span>
      <div className="flex justify-end gap-1">
        <button type="button" onClick={() => void submit()} disabled={!title.trim() || creating} className="h-7 rounded-md bg-blue-600 px-2.5 text-[10px] font-medium !text-white hover:bg-blue-700 disabled:opacity-40">{creating ? '创建中…' : '创建'}</button>
        <button type="button" onClick={onClose} className="h-7 px-1.5 text-[10px] text-slate-400 hover:text-slate-700">取消</button>
      </div>
    </div>
  )
}

export function ManagementView({ weeklyEnabled, onOpenPoint }: { weeklyEnabled: boolean; onOpenPoint: (pointId: string) => void }) {
  const { objectives, quarter } = useBoard()
  const [tool, setTool] = useState<Tool>()
  const [materialsOpen, setMaterialsOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [owner, setOwner] = useState('')
  const [priority, setPriority] = useState('')
  const [tag, setTag] = useState('')
  const [creatingObjectiveId, setCreatingObjectiveId] = useState('')
  const hasFilters = Boolean(query.trim() || owner || priority || tag)
  const owners = useMemo(() => [...new Set(objectives.flatMap((objective) => objective.krs.flatMap((kr) => splitOwnerNames(kr.ownerName))))].sort(), [objectives])
  const tags = useMemo(() => [...new Map(objectives.flatMap((objective) => objective.krs.flatMap((kr) => (kr.tags ?? []).map((item) => [`${item.type}:${item.value}`, item] as const)))).entries()].map(([key, item]) => ({ key, ...item })).sort((left, right) => tagLabel(left.type, left.value).localeCompare(tagLabel(right.type, right.value))), [objectives])
  const groups = useMemo(() => objectives.map((objective) => ({
    ...objective,
    krs: objective.krs.filter((kr) => {
      const matchesQuery = !query.trim() || `${objective.title} ${kr.title}`.toLowerCase().includes(query.trim().toLowerCase())
      return matchesQuery && (!owner || hasOwner(kr.ownerName, owner)) && (!priority || (kr.priority ?? 'p1') === priority) && (!tag || (kr.tags ?? []).some((item) => `${item.type}:${item.value}` === tag))
    }),
  })).filter((objective) => !hasFilters || objective.krs.length > 0), [hasFilters, objectives, owner, priority, query, tag])
  const resultCount = groups.reduce((total, objective) => total + objective.krs.length, 0)
  const tagCount = objectives.reduce((total, objective) => total + objective.krs.reduce((sum, kr) => sum + (kr.tags?.length ?? 0), 0), 0)

  const toggleTool = (next: Tool) => {
    setMaterialsOpen(true)
    setTool((current) => current === next ? undefined : next)
  }
  const clearFilters = () => {
    setQuery('')
    setOwner('')
    setPriority('')
    setTag('')
  }

  return (
    <div className="flex flex-col gap-3">
      {weeklyEnabled && <section className="order-2 overflow-hidden rounded-xl border border-slate-200/80 bg-white shadow-[0_1px_2px_rgba(15,23,42,0.04)]">
        <button type="button" onClick={() => { setMaterialsOpen((current) => !current); if (materialsOpen) setTool(undefined) }} className={`flex w-full items-center gap-3 px-3.5 py-2.5 text-left hover:bg-slate-50 ${materialsOpen ? 'border-b border-slate-100' : ''}`}>
          <div>
			<h2 className="text-xs font-semibold text-slate-800">OKR 材料</h2>
			<p className="mt-0.5 text-[10px] text-slate-400">按季度汇总 O / KR · 需要时再展开</p>
          </div>
          <span className={`ml-auto text-xs text-slate-400 transition-transform ${materialsOpen ? 'rotate-180' : ''}`}>⌄</span>
        </button>
		{materialsOpen && <div className="grid grid-cols-1 gap-px bg-slate-100 sm:max-w-xs">
          {TOOLS.map((item) => (
            <button key={item.value} type="button" onClick={() => toggleTool(item.value)} className={`group flex min-w-0 items-center gap-2 bg-white px-3 py-2.5 text-left transition-colors hover:bg-slate-50 ${tool === item.value ? 'relative z-10 bg-blue-50/70 ring-1 ring-inset ring-blue-200' : ''}`}>
              <span className={`flex size-7 shrink-0 items-center justify-center rounded-lg text-[10px] font-semibold ${tool === item.value ? 'bg-blue-600 text-white' : 'bg-slate-100 text-slate-500 group-hover:bg-slate-200'}`}>{item.mark}</span>
              <span className="min-w-0">
                <span className={`block truncate text-[11px] font-medium ${tool === item.value ? 'text-blue-700' : 'text-slate-700'}`}>{item.label}</span>
                <span className="block truncate text-[9px] text-slate-400">{item.description}</span>
              </span>
            </button>
          ))}
        </div>}
        {materialsOpen && tool && (
          <div className="border-t border-slate-100 p-3">
			{tool === 'quarterly' && <QuarterlyOKRDraft quarter={quarter} onClose={() => setTool(undefined)} onOpenPoint={onOpenPoint} />}
          </div>
        )}
      </section>}

      <section className="order-1 overflow-hidden rounded-xl border border-slate-200/80 bg-white shadow-[0_1px_2px_rgba(15,23,42,0.04)]">
        <div className="border-b border-slate-100 px-3.5 py-3">
          <div className="flex flex-wrap items-center gap-2">
          <div className="mr-2">
            <div className="flex items-baseline gap-2">
              <h2 className="text-[13px] font-semibold text-slate-800">OKR 管理</h2>
              <span className="text-[9px] tabular-nums text-slate-400">{resultCount} KR · {tagCount} 标签</span>
            </div>
            <p className="mt-0.5 text-[10px] text-slate-400">维护稳定的 KR 定义、多人负责人、优先级与标签；周进展独立填写</p>
          </div>
          <div className="ml-auto rounded-lg bg-slate-50 px-2 py-1 text-[10px] text-slate-500">
            <span className="font-medium text-slate-700">{resultCount}</span> / {objectives.reduce((total, objective) => total + objective.krs.length, 0)} 条
          </div>
          </div>
          <div className="mt-3 flex flex-wrap gap-1.5 rounded-xl border border-slate-200 bg-slate-50/70 p-2">
            <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索 O / KR 内容" className="h-8 min-w-48 flex-1 rounded-lg border border-slate-200 bg-white px-3 text-[11px] outline-none transition-colors focus:border-blue-400 focus:ring-2 focus:ring-blue-50 sm:max-w-72" />
            <select value={owner} onChange={(event) => setOwner(event.target.value)} className="h-8 rounded-lg border border-slate-200 bg-white px-2.5 text-[10px] text-slate-600 outline-none focus:border-blue-400"><option value="">全部负责人</option>{owners.map((item) => <option key={item} value={item}>{item}</option>)}</select>
            <select value={priority} onChange={(event) => setPriority(event.target.value)} className="h-8 rounded-lg border border-slate-200 bg-white px-2.5 text-[10px] text-slate-600 outline-none focus:border-blue-400"><option value="">全部优先级</option><option value="p0">P0</option><option value="p1">P1</option><option value="p2">P2</option></select>
            <select value={tag} onChange={(event) => setTag(event.target.value)} className="h-8 max-w-48 rounded-lg border border-slate-200 bg-white px-2.5 text-[10px] text-slate-600 outline-none focus:border-blue-400"><option value="">全部标签</option>{tags.map((item) => <option key={item.key} value={item.key}>{tagLabel(item.type, item.value)}</option>)}</select>
            {hasFilters && <button type="button" onClick={clearFilters} className="h-8 rounded-lg px-2.5 text-[10px] font-medium text-slate-500 hover:bg-white hover:text-slate-800">清空筛选</button>}
          </div>
        </div>

        <div>
          {groups.map((objective) => (
            <section key={objective.id}>
              <div className="flex items-center border-y border-slate-100 bg-slate-50/80 px-3.5 py-1.5 first:border-t-0">
                <h3 className="min-w-0 truncate text-[10px] font-semibold text-slate-500">{objective.title}</h3>
                <span className="ml-auto text-[9px] tabular-nums text-slate-400">{objective.krs.length} 条</span>
                <button type="button" onClick={() => setCreatingObjectiveId((current) => current === objective.id ? '' : objective.id)} className="ml-2 h-5 rounded-md px-1.5 text-[9px] font-medium text-blue-600 hover:bg-blue-50">+ 新建 KR</button>
              </div>
              <div className="divide-y divide-slate-100">
                {creatingObjectiveId === objective.id && <NewKrRow objectiveId={objective.id} onClose={() => setCreatingObjectiveId('')} />}
                {objective.krs.map((kr) => <KrEditorRow key={kr.id} objectiveId={objective.id} kr={kr} peopleOptions={owners} tagSuggestions={tags} />)}
              </div>
            </section>
          ))}
          {groups.length === 0 && <div className="py-12 text-center text-[11px] text-slate-400">没有符合条件的 KR</div>}
        </div>
      </section>
    </div>
  )
}
