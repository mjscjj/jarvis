import { useMemo, useState } from 'react'
import { useBoard } from '../board'
import { buildKRHierarchy, businessCategoryOf, businessCategoryOptions, isStructuralTag, priorityOf, withSelectedBusinessCategory } from '../hierarchy'
import { tagLabel } from '../labels'
import { hasOwner, ownerOptions, splitOwnerNames } from '../people'
import type { Kr, KrOwner, KrPriority, KrTag, Objective } from '../types'
import { BusinessCategoryTabs } from './BusinessCategoryTabs'
import { FeishuPeoplePicker, FeishuPeoplePickerInput } from './FeishuPeoplePicker'
import { KrDefinitionDetails } from './Table'
import { TagEditor } from './TagEditor'

// 业务分类和优先级各自有专属控件（分类标签条、优先级下拉、每行的选择器），
// 所以通用标签筛选和标签计数只涵盖其余标签，避免同一语义两个入口。
function allTagsOf(kr: Kr): KrTag[] {
	return [...(kr.tags ?? []), ...kr.points.flatMap((point) => point.tags ?? [])].filter((tag) => !isStructuralTag(tag))
}

function priorityTone(priority: KrPriority | '') {
  if (priority === 'p0') return 'border-red-200 bg-red-50 font-semibold text-red-700'
  if (priority === 'p1') return 'border-amber-200 bg-amber-50 font-semibold text-amber-700'
  return 'border-slate-200 bg-white text-slate-500'
}

function BusinessCategoryField({ value, categories, onChange, allowEmpty = false, ariaLabel, className }: {
	value: string
	categories: string[]
	onChange: (value: string) => void
	allowEmpty?: boolean
	ariaLabel: string
	className: string
}) {
	const [creating, setCreating] = useState(categories.length === 0 && !value)
	const [draft, setDraft] = useState('')
	const options = value && !categories.includes(value) ? [value, ...categories] : categories
	const commit = () => {
		const clean = draft.trim()
		if (!clean) return
		onChange(clean)
		setDraft('')
		setCreating(false)
	}

	if (creating) {
		return <span className="inline-flex min-w-0 items-center gap-1">
			<input autoFocus value={draft} onChange={(event) => setDraft(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') commit(); if (event.key === 'Escape' && categories.length > 0) setCreating(false) }} placeholder="新业务分类" aria-label={ariaLabel} className={className} />
			<button type="button" disabled={!draft.trim()} onClick={commit} className="h-6 rounded bg-blue-600 px-1.5 text-[9px] text-white disabled:opacity-30">确定</button>
			{categories.length > 0 && <button type="button" onClick={() => setCreating(false)} className="h-6 px-1 text-[9px] text-slate-400">取消</button>}
		</span>
	}

	return <select value={value} onChange={(event) => {
		if (event.target.value === '__new__') {
			setCreating(true)
			return
		}
		onChange(event.target.value)
	}} aria-label={ariaLabel} className={className}>
		{allowEmpty ? <option value="">未标注业务</option> : <option value="" disabled>选择业务分类</option>}
		{options.map((category) => <option key={category} value={category}>{category}</option>)}
		<option value="__new__">+ 新业务分类…</option>
	</select>
}

// 上下移动只在这一页出现：填写和会议页按业务分类/优先级导航，顺序不是它们的语义。
function MoveButtons({ label, onUp, onDown }: { label: string; onUp?: () => void; onDown?: () => void }) {
	const style = 'h-5 w-5 rounded text-[10px] leading-none text-slate-400 transition-colors enabled:hover:bg-white enabled:hover:text-blue-600 disabled:opacity-25'
	return <span className="inline-flex shrink-0 items-center">
		<button type="button" disabled={!onUp} onClick={onUp} title={`上移这${label}`} aria-label={`上移这${label}`} className={style}>↑</button>
		<button type="button" disabled={!onDown} onClick={onDown} title={`下移这${label}`} aria-label={`下移这${label}`} className={style}>↓</button>
	</span>
}

function KrTagEditor({ kr, suggestions }: { kr: Kr; suggestions: KrTag[] }) {
  const { addTag, removeTag } = useBoard()
	const allTags = (kr.tags ?? []).filter((tag) => !isStructuralTag(tag))
  return <TagEditor idPrefix={`tag-options-${kr.id}`} tags={allTags} suggestions={suggestions.filter((item) => !isStructuralTag(item))} onAdd={(value, type) => addTag(kr.id, value, type)} onRemove={(type, value) => removeTag(kr.id, type, value)} />
}

function KrEditorRow({ objectiveId, kr, tagSuggestions, businessCategories, detailsOpen, onToggleDetails, onMoveUp, onMoveDown }: { objectiveId: string; kr: Kr; tagSuggestions: KrTag[]; businessCategories: string[]; detailsOpen: boolean; onToggleDetails: () => void; onMoveUp?: () => void; onMoveDown?: () => void }) {
	const { setKrTitle, setKrBusinessCategory, setKrPriority, deleteKr } = useBoard()
	const [confirmDelete, setConfirmDelete] = useState(false)
	const [deleting, setDeleting] = useState(false)
	const priority = priorityOf(kr)
	const definitionCount = kr.metrics.length + kr.points.length

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
    <div data-okr-target-kind="kr" data-okr-objective-id={objectiveId} data-okr-kr-id={kr.id} className="group/kr grid grid-cols-[minmax(0,1fr)_auto] gap-2 px-3.5 py-2 transition-colors hover:bg-slate-50/70">
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-1.5">
          <input
            value={kr.title}
            onChange={(event) => setKrTitle(objectiveId, kr.id, event.target.value)}
            placeholder="填写 KR 内容"
            aria-label="KR 内容"
            className="h-7 min-w-64 flex-[1_1_32rem] rounded-md border border-transparent bg-transparent px-1.5 text-[11px] font-medium text-slate-700 outline-none transition-colors hover:border-slate-200 hover:bg-white focus:border-blue-300 focus:bg-white focus:ring-2 focus:ring-blue-50"
				/>
					<FeishuPeoplePicker kr={kr} />
					<BusinessCategoryField value={businessCategoryOf(kr)} categories={businessCategories} onChange={(value) => setKrBusinessCategory(kr.id, value)} allowEmpty ariaLabel="业务分类" className="h-6 max-w-36 rounded-md border border-blue-200 bg-blue-50 px-2 text-[10px] text-blue-700 outline-none focus:border-blue-400" />
				<select value={priority} onChange={(event) => setKrPriority(kr.id, event.target.value as KrPriority | '')} aria-label="优先级标签" className={`h-6 rounded-md border px-2 text-[10px] outline-none focus:border-blue-400 ${priorityTone(priority)}`}>
					<option value="">未标注</option><option value="p0">Focus · P0</option><option value="p1">P1</option><option value="p2">P2</option>
				</select>
        </div>
        <div className="mt-1 flex items-start gap-2 px-1.5">
          <div className="min-w-0 flex-1"><KrTagEditor kr={kr} suggestions={tagSuggestions} /></div>
          <button
            type="button"
            aria-expanded={detailsOpen}
            onClick={onToggleDetails}
            className={`h-6 shrink-0 rounded-md border px-2 text-[10px] font-medium transition-colors ${detailsOpen ? 'border-indigo-200 bg-indigo-50 text-indigo-700' : 'border-slate-200 bg-white text-slate-500 hover:border-indigo-200 hover:text-indigo-600'}`}
          >
            指标与拆解{definitionCount > 0 ? ` · ${definitionCount}` : ''}
          </button>
        </div>
      </div>
      <div className="flex min-w-12 items-center justify-end gap-1">
        <MoveButtons label="条 KR" onUp={onMoveUp} onDown={onMoveDown} />
        {confirmDelete ? (
          <>
            <button type="button" onClick={() => void remove()} disabled={deleting} className="h-6 rounded-md bg-red-600 px-2 text-[9px] font-medium !text-white hover:bg-red-700 disabled:opacity-50">{deleting ? '删除中' : '确认'}</button>
            <button type="button" onClick={() => setConfirmDelete(false)} className="h-6 px-1 text-[9px] text-slate-400 hover:text-slate-700">取消</button>
          </>
        ) : <button type="button" onClick={() => setConfirmDelete(true)} title="删除 KR" className="h-6 rounded-md px-1.5 text-[10px] text-slate-300 transition-colors hover:bg-red-50 hover:text-red-600">删除</button>}
      </div>
		{detailsOpen && <div className="col-span-2 px-1.5 pb-1"><KrDefinitionDetails objectiveId={objectiveId} kr={kr} tagSuggestions={tagSuggestions} /></div>}
    </div>
  )
}

function NewKrRow({ objective, businessCategories, peopleOptions, onClose }: { objective: Objective; businessCategories: string[]; peopleOptions: KrOwner[]; onClose: () => void }) {
		const { createKr } = useBoard()
		const [title, setTitle] = useState('')
		const [owners, setOwners] = useState<KrOwner[]>([])
	const [businessCategory, setBusinessCategory] = useState(() => (objective.krs[0] ? businessCategoryOf(objective.krs[0]) : '') || businessCategories[0] || '')
	const [priority, setPriority] = useState<KrPriority>('p1')
	const [creating, setCreating] = useState(false)

  const submit = async () => {
		if (!title.trim() || !businessCategory.trim() || creating) return
    setCreating(true)
    try {
				await createKr(objective.id, { title: title.trim(), owners, businessCategory: businessCategory.trim(), priority })
      onClose()
    } catch {
      setCreating(false)
    }
  }

  return (
			<div className="grid gap-1.5 bg-blue-50/50 px-3.5 py-2 md:grid-cols-[minmax(18rem,1.3fr)_minmax(9rem,0.5fr)_minmax(9rem,0.5fr)_5.5rem_auto] md:items-center md:gap-2">
				<input autoFocus value={title} onChange={(event) => setTitle(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') void submit(); if (event.key === 'Escape') onClose() }} placeholder="填写新 KR 内容" className="h-8 min-w-0 rounded-md border border-blue-200 bg-white px-2.5 text-[11px] font-medium text-slate-700 outline-none focus:border-blue-400 focus:ring-2 focus:ring-blue-100" />
				<div className="flex min-h-8 items-center rounded-md border border-slate-200 bg-white px-2"><FeishuPeoplePickerInput owners={owners} options={peopleOptions} onChange={setOwners} /></div>
				<BusinessCategoryField value={businessCategory} categories={businessCategories} onChange={setBusinessCategory} ariaLabel="新 KR 业务分类" className="h-8 w-full rounded-md border border-slate-200 bg-white px-2 text-[10px] outline-none focus:border-blue-400" />
			<select value={priority} onChange={(event) => setPriority(event.target.value as KrPriority)} aria-label="新 KR 优先级标签" className={`h-8 rounded-md border px-2 text-[10px] outline-none ${priorityTone(priority)}`}><option value="p0">Focus · P0</option><option value="p1">P1</option><option value="p2">P2</option></select>
			<div className="flex justify-end gap-1">
				<button type="button" onClick={() => void submit()} disabled={!title.trim() || !businessCategory.trim() || creating} className="h-7 rounded-md bg-blue-600 px-2.5 text-[10px] font-medium !text-white hover:bg-blue-700 disabled:opacity-40">{creating ? '创建中…' : '创建'}</button>
        <button type="button" onClick={onClose} className="h-7 px-1.5 text-[10px] text-slate-400 hover:text-slate-700">取消</button>
      </div>
    </div>
  )
}

function ObjectiveEditorHeader({
	objective,
	visibleKrCount,
	totalKrCount,
	creatingKr,
	onToggleCreateKr,
	open,
	onToggleOpen,
	onMoveUp,
	onMoveDown,
}: {
	objective: Objective
	visibleKrCount: number
	totalKrCount: number
	creatingKr: boolean
	onToggleCreateKr: () => void
	open: boolean
	onToggleOpen: () => void
	onMoveUp?: () => void
	onMoveDown?: () => void
}) {
	const { updateObjective, deleteObjective } = useBoard()
	const [editing, setEditing] = useState(false)
	const [title, setTitle] = useState(objective.title)
	const [busy, setBusy] = useState(false)
	const [confirmDelete, setConfirmDelete] = useState(false)

	const cancel = () => {
		setTitle(objective.title)
		setEditing(false)
	}
	const save = async () => {
		const clean = title.trim()
		if (!clean || clean === objective.title) {
			cancel()
			return
		}
		setBusy(true)
		try {
			await updateObjective(objective.id, clean)
			setEditing(false)
		} catch {
			// BoardProvider exposes the failure through the shared sync notice.
		} finally {
			setBusy(false)
		}
	}
	const remove = async () => {
		setBusy(true)
		try {
			await deleteObjective(objective.id)
		} catch {
			// BoardProvider exposes the failure through the shared sync notice.
		} finally {
			setBusy(false)
			setConfirmDelete(false)
		}
	}

	return (
		<div data-okr-target-kind="objective" data-okr-objective-id={objective.id} className="flex min-h-9 flex-wrap items-center gap-1.5 border-y border-slate-100 bg-slate-50/80 px-3.5 py-1.5 first:border-t-0">
			{editing ? <>
				<input autoFocus value={title} onChange={(event) => setTitle(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') void save(); if (event.key === 'Escape') cancel() }} aria-label="O 标题" className="h-7 min-w-64 flex-1 rounded-md border border-slate-200 bg-white px-2 text-[11px] font-medium text-slate-700 outline-none focus:border-blue-400" />
				<button type="button" disabled={busy || !title.trim()} onClick={() => void save()} className="h-6 rounded-md bg-blue-600 px-2 text-[9px] font-medium text-white disabled:opacity-40">保存</button>
				<button type="button" disabled={busy} onClick={cancel} className="h-6 px-1 text-[9px] text-slate-400">取消</button>
			</> : <>
				<button
					type="button"
					onClick={onToggleOpen}
					title={open ? '收起这个 O 的 KR' : '展开这个 O 的 KR'}
					aria-label={open ? '收起这个 O 的 KR' : '展开这个 O 的 KR'}
					aria-expanded={open}
					className="shrink-0 rounded p-0.5 text-slate-400 hover:bg-white hover:text-slate-700"
				>
					<svg viewBox="0 0 12 12" aria-hidden className={`size-2.5 transition-transform ${open ? 'rotate-90' : ''}`}>
						<path d="M4 2.2 L8.8 6 L4 9.8 Z" fill="currentColor" />
					</svg>
				</button>
				<h3 className="min-w-0 flex-1 truncate text-[10px] font-semibold text-slate-500">{objective.title}</h3>
				<span className="text-[9px] tabular-nums text-slate-400">{visibleKrCount}{visibleKrCount !== totalKrCount ? ` / ${totalKrCount}` : ''} 条</span>
				<MoveButtons label="个 O" onUp={onMoveUp} onDown={onMoveDown} />
				<button type="button" onClick={() => { setTitle(objective.title); setEditing(true) }} className="h-5 rounded-md px-1.5 text-[9px] text-slate-500 hover:bg-white hover:text-blue-600">重命名 O</button>
				{totalKrCount === 0 && (confirmDelete ? <>
					<button type="button" disabled={busy} onClick={() => void remove()} className="h-5 rounded-md bg-red-600 px-1.5 text-[9px] font-medium text-white disabled:opacity-40">确认删除</button>
					<button type="button" disabled={busy} onClick={() => setConfirmDelete(false)} className="h-5 px-1 text-[9px] text-slate-400">取消</button>
				</> : <button type="button" onClick={() => setConfirmDelete(true)} className="h-5 rounded-md px-1.5 text-[9px] text-red-400 hover:bg-red-50 hover:text-red-600">删除空 O</button>)}
				<button type="button" onClick={onToggleCreateKr} className={`h-5 rounded-md px-1.5 text-[9px] font-medium ${creatingKr ? 'bg-blue-50 text-blue-700' : 'text-blue-600 hover:bg-blue-50'}`}>{creatingKr ? '收起新建' : '+ 新建 KR'}</button>
			</>}
		</div>
	)
}

export function ManagementView() {
  const { objectives, quarter, syncState, createObjective, swapObjectives, swapKrs } = useBoard()
  const [query, setQuery] = useState('')
  const [owner, setOwner] = useState('')
  const [priority, setPriority] = useState('')
  const [tag, setTag] = useState('')
  // undefined means every category; '' is the untagged one, which is a real
  // choice here because management is where those KRs get their category.
  const [businessCategory, setBusinessCategory] = useState<string>()
  const [creatingObjectiveId, setCreatingObjectiveId] = useState('')
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set())
  // 「指标与拆解」的展开态放在这里，工具栏的全部展开/折叠才管得到每一行。
  const [openDetails, setOpenDetails] = useState<Set<string>>(new Set())
  const [creatingObjective, setCreatingObjective] = useState(false)
  const [objectiveTitle, setObjectiveTitle] = useState('')
  const [objectiveQuarter, setObjectiveQuarter] = useState(quarter)
  const hasFilters = Boolean(query.trim() || owner || priority || tag || businessCategory !== undefined)
	const peopleOptions = useMemo(() => ownerOptions(objectives), [objectives])
	const owners = useMemo(() => peopleOptions.map((person) => person.name), [peopleOptions])
	const tags = useMemo(() => [...new Map(objectives.flatMap((objective) => objective.krs.flatMap((kr) => allTagsOf(kr).map((item) => [`${item.type}:${item.value}`, item] as const)))).entries()].map(([key, item]) => ({ key, ...item })).sort((left, right) => tagLabel(left.type, left.value).localeCompare(tagLabel(right.type, right.value))), [objectives])
	const businessCategories = useMemo(() => [...new Set(objectives.flatMap((objective) => objective.krs.map(businessCategoryOf)).filter(Boolean))].sort(), [objectives])
  const filtered = useMemo(() => objectives.map((objective) => ({
    ...objective,
		totalKrCount: objective.krs.length,
    krs: objective.krs.filter((kr) => {
      const matchesQuery = !query.trim() || `${objective.title} ${kr.title} ${kr.points.map((point) => point.title).join(' ')}`.toLowerCase().includes(query.trim().toLowerCase())
				return matchesQuery && (!owner || hasOwner(kr.ownerName, owner)) && (!priority || priorityOf(kr) === priority) && (!tag || allTagsOf(kr).some((item) => `${item.type}:${item.value}` === tag))
    }),
  })), [objectives, owner, priority, query, tag])
  // Counts read every filter except the category itself, so each tab states how
  // many rows picking it would leave. Objectives the other filters emptied
  // contribute nothing rather than an untagged bucket of zero.
  const categoryOptions = useMemo(
    () => withSelectedBusinessCategory(businessCategoryOptions(buildKRHierarchy(filtered.filter((objective) => objective.krs.length > 0))), businessCategory),
    [businessCategory, filtered],
  )
  const categoryTotal = useMemo(() => filtered.reduce((total, objective) => total + objective.krs.length, 0), [filtered])
  const groups = useMemo(() => filtered
    .map((objective) => ({ ...objective, krs: objective.krs.filter((kr) => businessCategory === undefined || businessCategoryOf(kr) === businessCategory) }))
    .filter((objective) => !hasFilters || objective.krs.length > 0), [businessCategory, filtered, hasFilters])
  const resultCount = groups.reduce((total, objective) => total + objective.krs.length, 0)
  const tagCount = objectives.reduce((total, objective) => total + objective.krs.reduce((sum, kr) => sum + allTagsOf(kr).length, 0), 0)
	const defaultQuarter = quarter || `${new Date().getFullYear()}-Q${Math.floor(new Date().getMonth() / 3) + 1}`

	const submitObjective = async () => {
		const title = objectiveTitle.trim()
		const targetQuarter = objectiveQuarter.trim()
		if (!title || !targetQuarter) return
		try {
			await createObjective({ quarter: targetQuarter, title })
			setObjectiveTitle('')
			setCreatingObjective(false)
		} catch {
			// BoardProvider exposes the failure through the shared sync notice.
		}
	}

  const toggleObjective = (id: string) => setCollapsed((previous) => {
    const next = new Set(previous)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    return next
  })
  // 新建 KR 时先展开，否则新行会落在收起的区块里看不见。
  const expand = (id: string) => setCollapsed((previous) => {
    if (!previous.has(id)) return previous
    const next = new Set(previous)
    next.delete(id)
    return next
  })
  const toggleDetails = (id: string) => setOpenDetails((previous) => {
    const next = new Set(previous)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    return next
  })
  // 和填写/会议页同一套语义：全部展开摊开到指标与拆解，折叠到 KR 只留 KR 行。
  const expandAll = () => {
    setCollapsed(new Set())
    setOpenDetails(new Set(groups.flatMap((objective) => objective.krs.map((kr) => kr.id))))
  }
  const collapseToKr = () => {
    setCollapsed(new Set())
    setOpenDetails(new Set())
  }

  const clearFilters = () => {
    setQuery('')
    setOwner('')
    setPriority('')
    setTag('')
    setBusinessCategory(undefined)
  }

  return (
    <div className="flex flex-col gap-3">
      <section className="overflow-hidden rounded-xl border border-slate-200/80 bg-white shadow-[0_1px_2px_rgba(15,23,42,0.04)]">
        <div className="border-b border-slate-100 px-3.5 py-3">
          <div className="flex flex-wrap items-center gap-2">
          <div className="mr-2">
            <div className="flex items-baseline gap-2">
              <h2 className="text-[13px] font-semibold text-slate-800">OKR 管理</h2>
              <span className="text-[9px] tabular-nums text-slate-400">{resultCount} KR · {tagCount} 标签</span>
            </div>
            <p className="mt-0.5 text-[10px] text-slate-400">标签可标在整条 KR，也可下钻到策略/产品要点；长标签完整换行展示</p>
          </div>
          <div className="ml-auto rounded-lg bg-slate-50 px-2 py-1 text-[10px] text-slate-500">
            <span className="font-medium text-slate-700">{resultCount}</span> / {objectives.reduce((total, objective) => total + objective.krs.length, 0)} 条
          </div>
			<button type="button" onClick={() => { setObjectiveQuarter(defaultQuarter); setCreatingObjective((value) => !value) }} className="h-8 rounded-lg bg-indigo-600 px-3 text-[10px] font-medium text-white hover:bg-indigo-700">+ 新建 O</button>
          </div>
		  {creatingObjective && <div className="mt-3 flex flex-wrap items-center gap-2 rounded-xl border border-indigo-100 bg-indigo-50/60 p-2">
			<input value={objectiveQuarter} onChange={(event) => setObjectiveQuarter(event.target.value)} placeholder="2026-Q3" aria-label="季度" className="h-8 w-28 rounded-lg border border-slate-200 bg-white px-2.5 text-[11px] outline-none focus:border-indigo-400" />
			<input value={objectiveTitle} onChange={(event) => setObjectiveTitle(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') void submitObjective() }} placeholder="目标名称" aria-label="目标名称" autoFocus className="h-8 min-w-64 flex-1 rounded-lg border border-slate-200 bg-white px-2.5 text-[11px] outline-none focus:border-indigo-400" />
			<button type="button" onClick={() => void submitObjective()} disabled={!objectiveTitle.trim() || !objectiveQuarter.trim() || syncState.kind === 'saving'} className="h-8 rounded-lg bg-indigo-600 px-3 text-[10px] font-medium text-white disabled:opacity-40">创建目标</button>
			<button type="button" onClick={() => setCreatingObjective(false)} className="h-8 px-2 text-[10px] text-slate-400">取消</button>
		  </div>}
          <div className="mt-3 flex flex-wrap gap-1.5 rounded-xl border border-slate-200 bg-slate-50/70 p-2">
            <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索 O / KR 内容" className="h-8 min-w-48 flex-1 rounded-lg border border-slate-200 bg-white px-3 text-[11px] outline-none transition-colors focus:border-blue-400 focus:ring-2 focus:ring-blue-50 sm:max-w-72" />
            <select value={owner} onChange={(event) => setOwner(event.target.value)} className="h-8 rounded-lg border border-slate-200 bg-white px-2.5 text-[10px] text-slate-600 outline-none focus:border-blue-400"><option value="">全部负责人</option>{owners.map((item) => <option key={item} value={item}>{item}</option>)}</select>
            <select value={priority} onChange={(event) => setPriority(event.target.value)} className="h-8 rounded-lg border border-slate-200 bg-white px-2.5 text-[10px] text-slate-600 outline-none focus:border-blue-400"><option value="">全部优先级</option><option value="p0">P0</option><option value="p1">P1</option><option value="p2">P2</option></select>
            <select value={tag} onChange={(event) => setTag(event.target.value)} title={tags.find((item) => item.key === tag)?.value ?? '全部标签'} className="h-8 max-w-80 rounded-lg border border-slate-200 bg-white px-2.5 text-[10px] text-slate-600 outline-none focus:border-blue-400"><option value="">全部标签</option>{tags.map((item) => <option key={item.key} value={item.key}>{tagLabel(item.type, item.value)}</option>)}</select>
            {hasFilters && <button type="button" onClick={clearFilters} className="h-8 rounded-lg px-2.5 text-[10px] font-medium text-slate-500 hover:bg-white hover:text-slate-800">清空筛选</button>}
            <span className="ml-auto self-center text-[10px] text-slate-400">层级</span>
            <div className="inline-flex h-8 items-center overflow-hidden rounded-lg border border-slate-200 bg-white text-[10px]">
              <button type="button" onClick={expandAll} className="h-full px-2.5 text-slate-500 hover:bg-slate-50 hover:text-slate-700">全部展开</button>
              <button type="button" onClick={collapseToKr} className="h-full border-l border-slate-200 px-2.5 text-slate-500 hover:bg-slate-50 hover:text-slate-700">折叠到 KR</button>
            </div>
          </div>
          <div className="mt-2 rounded-xl border border-slate-200 bg-gradient-to-b from-slate-50/90 to-slate-100/65 p-2.5">
            <BusinessCategoryTabs options={categoryOptions} activeValue={businessCategory} total={categoryTotal} showOverview onSelect={setBusinessCategory} />
          </div>
        </div>

        <div>
          {groups.map((objective, objectiveIndex) => (
            <section key={objective.id}>
              <ObjectiveEditorHeader
                objective={objective}
                visibleKrCount={objective.krs.length}
                totalKrCount={objective.totalKrCount}
                creatingKr={creatingObjectiveId === objective.id}
                onToggleCreateKr={() => { expand(objective.id); setCreatingObjectiveId((current) => current === objective.id ? '' : objective.id) }}
                open={!collapsed.has(objective.id)}
                onToggleOpen={() => toggleObjective(objective.id)}
                onMoveUp={objectiveIndex > 0 ? () => void swapObjectives(objective.id, groups[objectiveIndex - 1].id) : undefined}
                onMoveDown={objectiveIndex < groups.length - 1 ? () => void swapObjectives(objective.id, groups[objectiveIndex + 1].id) : undefined}
              />
              {!collapsed.has(objective.id) && <div className="divide-y divide-slate-100">
						{creatingObjectiveId === objective.id && <NewKrRow objective={objective} businessCategories={businessCategories} peopleOptions={peopleOptions} onClose={() => setCreatingObjectiveId('')} />}
						{objective.krs.map((kr, krIndex) => <KrEditorRow
							key={kr.id}
							objectiveId={objective.id}
							kr={kr}
							tagSuggestions={tags}
							businessCategories={businessCategories}
							detailsOpen={openDetails.has(kr.id)}
							onToggleDetails={() => toggleDetails(kr.id)}
							onMoveUp={krIndex > 0 ? () => void swapKrs(objective.id, kr.id, objective.krs[krIndex - 1].id) : undefined}
							onMoveDown={krIndex < objective.krs.length - 1 ? () => void swapKrs(objective.id, kr.id, objective.krs[krIndex + 1].id) : undefined}
						/>)}
              </div>}
            </section>
          ))}
          {groups.length === 0 && <div className="py-12 text-center text-[11px] text-slate-400">没有符合条件的 KR</div>}
        </div>
      </section>
    </div>
  )
}
