import { useCallback, useEffect, useMemo, useState } from 'react'
import { useBoard } from '../board'
import { buildAllBusinessNavigation, buildKRHierarchy, businessCategoryOf, businessCategoryOptions, commonObjectiveBusinessCategory, filterObjectivesByHierarchy, isStructuralTag, objectivesForBusiness, priorityOf, withCompletePriorityNavigation, withSelectedBusinessCategory } from '../hierarchy'
import { tagLabel } from '../labels'
import { krHasAnyOwner, krOwnerCounts, krOwnerOptions, krOwners, ownerIdentityKey, ownerOptions } from '../people'
import type { Kr, KrOwner, KrPriority, KrTag, Objective } from '../types'
import { BusinessCategoryTabs } from './BusinessCategoryTabs'
import { FeishuPeoplePicker, FeishuPeoplePickerInput } from './FeishuPeoplePicker'
import { HierarchyNav } from './HierarchyNav'
import { OwnerFilterPicker } from './OwnerFilterPicker'
import { PersonAvatar } from './PersonAvatar'
import { KrDefinitionDetails } from './Table'
import { TagEditor } from './TagEditor'
import { CommentSurfaceHint, CommentTargetButton, commentTargetElementId, scrollToCommentSource, useCommentInteraction, useCommentSurface } from '../commenting'
import { findCommentTargetLocation } from '../comments'
import { mergeVisibleObjectiveOrder } from '../ordering'
import { MoveButtons, Text } from './ui'
import { PreviewReviewButton, PreviewReviewPanel } from '../aiReviewContext'

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

function BusinessCategoryField({ value, categories, onChange, allowEmpty = false, emptyLabel = '选择业务分类', ariaLabel, className }: {
	value: string
	categories: string[]
	onChange: (value: string) => void
	allowEmpty?: boolean
	emptyLabel?: string
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
		{allowEmpty ? <option value="">未标注业务</option> : <option value="" disabled>{emptyLabel}</option>}
		{options.map((category) => <option key={category} value={category}>{category}</option>)}
		<option value="__new__">+ 新业务分类…</option>
	</select>
}

function KrTagEditor({ kr, suggestions, readOnly }: { kr: Kr; suggestions: KrTag[]; readOnly: boolean }) {
  const { addTag, removeTag } = useBoard()
	const allTags = (kr.tags ?? []).filter((tag) => !isStructuralTag(tag))
	return <TagEditor idPrefix={`tag-options-${kr.id}`} tags={allTags} suggestions={suggestions.filter((item) => !isStructuralTag(item))} onAdd={(value, type) => addTag(kr.id, value, type)} onRemove={(type, value) => removeTag(kr.id, type, value)} readOnly={readOnly} />
}

function KrEditorRow({ compactPresentation = false, objectiveId, kr, tagSuggestions, businessCategories, detailsOpen, cardHierarchy, compactEmptyPointGroups, onToggleDetails, onMoveUp, onMoveDown, showTags, showStructuralFields, deleteWarning, readOnly, reviewEnabled }: { compactPresentation?: boolean; objectiveId: string; kr: Kr; tagSuggestions: KrTag[]; businessCategories: string[]; detailsOpen: boolean; cardHierarchy: boolean; compactEmptyPointGroups: boolean; onToggleDetails: () => void; onMoveUp?: () => void; onMoveDown?: () => void; showTags: boolean; showStructuralFields: boolean; deleteWarning: string; readOnly: boolean; reviewEnabled: boolean }) {
	const { setKrTitle, setKrBusinessCategory, setKrPriority, deleteKr } = useBoard()
	const [confirmDelete, setConfirmDelete] = useState(false)
	const [deleting, setDeleting] = useState(false)
	const priority = priorityOf(kr)
	const definitionCount = kr.metrics.length + kr.points.length
	const commentTarget = { type: 'kr' as const, id: kr.id, title: kr.title }
	const commentSurface = useCommentSurface(commentTarget)
	const reviewTarget = { kind: 'kr' as const, objectiveId, krId: kr.id, title: kr.title }

  const remove = async () => {
    setDeleting(true)
    try {
      await deleteKr(kr.id)
    } catch {
      setDeleting(false)
      setConfirmDelete(false)
    }
  }

  const ownerControl = (readOnly ? <span className="flex flex-wrap justify-end gap-1">{krOwners(kr).map((owner) => <span key={`${owner.email}:${owner.name}`} title={owner.name} aria-label={owner.name} className={`inline-flex min-h-5 max-w-full items-center gap-1 rounded-full border border-slate-200 bg-slate-50 px-1.5 py-0.5 text-slate-500 ${compactPresentation ? '!min-h-4 !gap-1 !border-0 !bg-transparent !px-0.5 !py-0 text-[8px] leading-3' : 'text-[10px] leading-3.5'}`}>{compactPresentation && <PersonAvatar name={owner.name} email={owner.email} size="size-3 text-[7px]" />}<span className="min-w-0 break-words">{owner.name}</span></span>)}</span> : <FeishuPeoplePicker kr={kr} compact={!cardHierarchy} small={compactPresentation} />)

  return (
    <article id={commentTargetElementId(commentTarget)} onClick={compactPresentation ? undefined : commentSurface.onClick} className={`group/kr group/commentable grid grid-cols-[minmax(0,1fr)_auto] gap-2 transition-[background-color,box-shadow] ${cardHierarchy ? `overflow-hidden rounded-xl border border-slate-200 bg-white px-3.5 ${compactPresentation ? 'py-1.5' : 'py-2.5'} shadow-[0_2px_8px_rgba(31,35,40,0.035)]` : 'px-3.5 py-2'} ${!compactPresentation && commentSurface.enabled ? 'cursor-pointer hover:bg-indigo-50/70' : cardHierarchy ? '' : 'hover:bg-slate-50/70'} ${!compactPresentation && (commentSurface.selected || commentSurface.focused) ? 'bg-indigo-50/80 ring-2 ring-inset ring-indigo-500' : ''}`}>
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-1.5">
			{cardHierarchy && <button type="button" onClick={onToggleDetails} title={detailsOpen ? '折叠 KR' : '展开 KR'} aria-label={detailsOpen ? '折叠 KR' : '展开 KR'} aria-expanded={detailsOpen} className="shrink-0 rounded p-0.5 text-slate-400 hover:bg-slate-100 hover:text-slate-700"><svg viewBox="0 0 12 12" aria-hidden className={`size-3 transition-transform ${detailsOpen ? 'rotate-90' : ''}`}><path d="M4 2.2 L8.8 6 L4 9.8 Z" fill="currentColor" /></svg></button>}
          {compactPresentation ? <Text value={kr.title} onChange={(value) => setKrTitle(objectiveId, kr.id, value)} placeholder="填写 KR 内容" ariaLabel="KR 内容" readOnly={readOnly} fit className="[field-sizing:content] !min-w-0 text-[14px] font-semibold !leading-5 text-slate-900" /> : (
          <input
            value={kr.title}
			readOnly={readOnly}
            onChange={(event) => setKrTitle(objectiveId, kr.id, event.target.value)}
            placeholder="填写 KR 内容"
            aria-label="KR 内容"
			className={`min-w-64 flex-[1_1_32rem] rounded-md border border-transparent bg-transparent px-1.5 outline-none transition-colors ${readOnly ? 'cursor-default' : 'hover:border-slate-200 hover:bg-white focus:border-blue-300 focus:bg-white focus:ring-2 focus:ring-blue-50'} ${cardHierarchy ? 'min-h-7 text-[14px] font-semibold leading-5 text-slate-900' : 'h-7 text-[11px] font-medium text-slate-700'}`}
				/>)}
          {compactPresentation && ownerControl}
          {compactPresentation && (readOnly ? <span className={`rounded-md border px-2 py-1 text-[10px] ${priorityTone(priority)}`}>{priority ? (priority === 'p0' ? 'Focus · P0' : priority.toUpperCase()) : '未标注'}</span> : <select value={priority} onChange={(event) => setKrPriority(kr.id, event.target.value as KrPriority | '')} aria-label="优先级标签" className={`h-6 rounded-md border px-2 text-[10px] outline-none focus:border-blue-400 ${priorityTone(priority)}`}><option value="">未标注</option><option value="p0">Focus · P0</option><option value="p1">P1</option><option value="p2">P2</option></select>)}

					{!compactPresentation && <CommentTargetButton target={{ type: 'kr', id: kr.id, title: kr.title }} />}
					{showStructuralFields && !cardHierarchy && (readOnly ? <span className="rounded-md border border-blue-200 bg-blue-50 px-2 py-1 text-[10px] text-blue-700">{businessCategoryOf(kr) || '未标注业务'}</span> : <BusinessCategoryField value={businessCategoryOf(kr)} categories={businessCategories} onChange={(value) => setKrBusinessCategory(kr.id, value)} allowEmpty ariaLabel="业务分类" className="h-6 max-w-36 rounded-md border border-blue-200 bg-blue-50 px-2 text-[10px] text-blue-700 outline-none focus:border-blue-400" />)}
				{showStructuralFields && !cardHierarchy && (readOnly ? <span className={`rounded-md border px-2 py-1 text-[10px] ${priorityTone(priority)}`}>{priority ? (priority === 'p0' ? 'Focus · P0' : priority.toUpperCase()) : '未标注'}</span> : <select value={priority} onChange={(event) => setKrPriority(kr.id, event.target.value as KrPriority | '')} aria-label="优先级标签" className={`h-6 rounded-md border px-2 text-[10px] outline-none focus:border-blue-400 ${priorityTone(priority)}`}>
					<option value="">未标注</option><option value="p0">Focus · P0</option><option value="p1">P1</option><option value="p2">P2</option>
				</select>)}
        </div>
		<div className={`flex flex-wrap items-start gap-2 px-1.5 ${compactPresentation ? 'mt-0.5' : cardHierarchy ? 'mt-1.5' : 'mt-1'}`}>
			  {cardHierarchy && showStructuralFields && (readOnly ? <span className="rounded-md border border-blue-200 bg-blue-50 px-2 py-1 text-[10px] text-blue-700">{businessCategoryOf(kr) || '未标注业务'}</span> : <BusinessCategoryField value={businessCategoryOf(kr)} categories={businessCategories} onChange={(value) => setKrBusinessCategory(kr.id, value)} allowEmpty ariaLabel="业务分类" className="h-6 max-w-36 rounded-md border border-blue-200 bg-blue-50 px-2 text-[10px] text-blue-700 outline-none focus:border-blue-400" />)}
			  {cardHierarchy && !compactPresentation && (readOnly ? <span className={`rounded-md border px-2 py-1 text-[10px] ${priorityTone(priority)}`}>{priority ? (priority === 'p0' ? 'Focus · P0' : priority.toUpperCase()) : '未标注'}</span> : <select value={priority} onChange={(event) => setKrPriority(kr.id, event.target.value as KrPriority | '')} aria-label="优先级标签" className={`h-6 rounded-md border px-2 text-[10px] outline-none focus:border-blue-400 ${priorityTone(priority)}`}><option value="">未标注</option><option value="p0">Focus · P0</option><option value="p1">P1</option><option value="p2">P2</option></select>)}
		  {showTags && <div className="min-w-0 flex-1"><KrTagEditor kr={kr} suggestions={tagSuggestions} readOnly={readOnly} /></div>}
		  {!cardHierarchy && <button
            type="button"
            aria-expanded={detailsOpen}
            onClick={onToggleDetails}
            className={`h-6 shrink-0 rounded-md border px-2 text-[10px] font-medium transition-colors ${showTags ? '' : 'ml-auto'} ${detailsOpen ? 'border-indigo-200 bg-indigo-50 text-indigo-700' : 'border-slate-200 bg-white text-slate-500 hover:border-indigo-200 hover:text-indigo-600'}`}
          >
            指标与拆解{definitionCount > 0 ? ` · ${definitionCount}` : ''}
          </button>}
        </div>
      </div>
      <div className={`flex min-w-12 justify-end gap-1 ${compactPresentation ? 'min-w-0 flex-wrap items-start self-start' : 'items-center'}`}>
		{reviewEnabled && !compactPresentation && <PreviewReviewButton target={reviewTarget} label="AI评审" />}
        {!compactPresentation && ownerControl}
		{!readOnly && <span className={`flex items-center gap-1 transition-opacity ${cardHierarchy && !confirmDelete ? 'sm:opacity-0 sm:group-hover/kr:opacity-100 sm:group-focus-within/kr:opacity-100' : ''}`}>
        <MoveButtons label="条 KR" onUp={onMoveUp} onDown={onMoveDown} />
        {confirmDelete ? (
          <>
            <span className="text-[9px] leading-tight text-red-500">{deleteWarning}</span>
            <button type="button" onClick={() => void remove()} disabled={deleting} className="h-6 rounded-md bg-red-600 px-2 text-[9px] font-medium !text-white hover:bg-red-700 disabled:opacity-50">{deleting ? '删除中' : '确认'}</button>
            <button type="button" onClick={() => setConfirmDelete(false)} className="h-6 px-1 text-[9px] text-slate-400 hover:text-slate-700">取消</button>
          </>
		) : <button type="button" onClick={() => setConfirmDelete(true)} title="删除 KR" className="h-6 rounded-md px-1.5 text-[10px] text-slate-300 transition-colors hover:bg-red-50 hover:text-red-600">删除</button>}
		</span>}
        {!compactPresentation && <CommentSurfaceHint target={commentTarget} />}
      </div>
		{reviewEnabled && <PreviewReviewPanel target={reviewTarget} className="col-span-2" />}
		{detailsOpen && <div className={`col-span-2 ${cardHierarchy ? 'px-0.5 pb-1' : 'px-1.5 pb-1'}`}><KrDefinitionDetails compactPresentation={compactPresentation} objectiveId={objectiveId} kr={kr} tagSuggestions={showTags ? tagSuggestions : undefined} deletePointWarning={deleteWarning} compactEmptyPointGroups={compactEmptyPointGroups} cardBody={cardHierarchy} readOnly={readOnly} reviewEnabled={reviewEnabled} /></div>}
    </article>
  )
}

function NewKrRow({ objective, businessCategories, peopleOptions, cardHierarchy, onClose, onCreated }: { objective: Objective; businessCategories: string[]; peopleOptions: KrOwner[]; cardHierarchy: boolean; onClose: () => void; onCreated: () => void }) {
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
      onCreated()
      onClose()
    } catch {
      setCreating(false)
    }
  }

  return (
			<div className={`grid gap-1.5 bg-blue-50/50 px-3.5 py-2 md:grid-cols-[minmax(18rem,1.3fr)_minmax(9rem,0.5fr)_minmax(9rem,0.5fr)_5.5rem_auto] md:items-center md:gap-2 ${cardHierarchy ? 'rounded-xl border border-blue-100 shadow-[0_2px_8px_rgba(31,35,40,0.025)]' : ''}`}>
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
	cardHierarchy,
	businessCategories,
	businessCategoryEditable,
	businessCategoryObjective,
	readOnly,
	reviewEnabled,
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
	cardHierarchy: boolean
	businessCategories: string[]
	businessCategoryEditable: boolean
	businessCategoryObjective?: Objective
	readOnly: boolean
	reviewEnabled: boolean
}) {
	const { updateObjective, deleteObjective, setKrBusinessCategory } = useBoard()
	const [editing, setEditing] = useState(false)
	const [title, setTitle] = useState(objective.title)
	const [busy, setBusy] = useState(false)
	const [confirmDelete, setConfirmDelete] = useState(false)
	const commentTarget = { type: 'objective' as const, id: objective.id, title: objective.title }
	const commentSurface = useCommentSurface(commentTarget)
	const reviewTarget = { kind: 'objective' as const, objectiveId: objective.id, title: objective.title }
	const completeObjective = businessCategoryObjective ?? objective
	const commonBusinessCategory = commonObjectiveBusinessCategory(completeObjective)
	const hasMixedBusinessCategories = completeObjective.krs.length > 0 && commonBusinessCategory === undefined

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
		<div id={commentTargetElementId(commentTarget)} onClick={commentSurface.onClick} className={`group/commentable flex flex-wrap items-center gap-2 ${cardHierarchy ? 'mb-3 px-1' : 'min-h-9 border-y border-slate-100 bg-slate-50/80 px-3.5 py-1.5 first:border-t-0'} ${commentSurface.enabled ? 'cursor-pointer hover:bg-indigo-50/70' : ''} ${commentSurface.selected || commentSurface.focused ? 'ring-2 ring-inset ring-indigo-500' : ''}`}>
			{!readOnly && editing ? <>
				<input autoFocus value={title} onChange={(event) => setTitle(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') void save(); if (event.key === 'Escape') cancel() }} aria-label="O 标题" className={`h-7 min-w-64 flex-1 rounded-md border border-slate-200 bg-white px-2 font-medium text-slate-700 outline-none focus:border-blue-400 ${cardHierarchy ? 'text-[15px]' : 'text-[11px]'}`} />
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
					<svg viewBox="0 0 12 12" aria-hidden className={`${cardHierarchy ? 'size-3' : 'size-2.5'} transition-transform ${open ? 'rotate-90' : ''}`}>
						<path d="M4 2.2 L8.8 6 L4 9.8 Z" fill="currentColor" />
					</svg>
				</button>
				<h3 className={`min-w-0 flex-1 ${cardHierarchy ? 'text-[17px] font-semibold leading-6 text-slate-800' : 'truncate text-[12px] font-bold text-slate-800'}`}>{objective.title}</h3>
				<CommentTargetButton target={{ type: 'objective', id: objective.id, title: objective.title }} />
				<span className={cardHierarchy ? 'rounded-full border border-slate-200 bg-white px-2.5 py-0.5 text-xs tabular-nums text-slate-400' : 'text-[9px] tabular-nums text-slate-400'}>{visibleKrCount}{visibleKrCount !== totalKrCount ? ` / ${totalKrCount}` : ''}{cardHierarchy ? ' 个 KR' : ' 条'}</span>
				{reviewEnabled && <PreviewReviewButton target={reviewTarget} label="AI评审" />}
				{!readOnly && businessCategoryEditable && completeObjective.krs.length > 0 && <BusinessCategoryField
					value={commonBusinessCategory ?? ''}
					categories={businessCategories}
					onChange={(value) => completeObjective.krs.forEach((kr) => setKrBusinessCategory(kr.id, value))}
					emptyLabel={hasMixedBusinessCategories ? '多种业务 · 批量修改' : '批量修改业务分类'}
					ariaLabel={`批量修改“O ${objective.title}”下全部 KR 的业务分类`}
					className="h-6 max-w-44 rounded-md border border-blue-200 bg-blue-50 px-2 text-[10px] text-blue-700 outline-none focus:border-blue-400"
				/>}
				{!readOnly && <MoveButtons label="个 O" onUp={onMoveUp} onDown={onMoveDown} />}
				{!readOnly && <button type="button" onClick={() => { setTitle(objective.title); setEditing(true) }} className="h-5 rounded-md px-1.5 text-[9px] text-slate-500 hover:bg-white hover:text-blue-600">重命名 O</button>}
				{!readOnly && totalKrCount === 0 && (confirmDelete ? <>
					<button type="button" disabled={busy} onClick={() => void remove()} className="h-5 rounded-md bg-red-600 px-1.5 text-[9px] font-medium text-white disabled:opacity-40">确认删除</button>
					<button type="button" disabled={busy} onClick={() => setConfirmDelete(false)} className="h-5 px-1 text-[9px] text-slate-400">取消</button>
				</> : <button type="button" onClick={() => setConfirmDelete(true)} className="h-5 rounded-md px-1.5 text-[9px] text-red-400 hover:bg-red-50 hover:text-red-600">删除空 O</button>)}
				{!readOnly && <button type="button" onClick={onToggleCreateKr} className={`h-5 rounded-md px-1.5 text-[9px] font-medium ${creatingKr ? 'bg-blue-50 text-blue-700' : 'text-blue-600 hover:bg-blue-50'}`}>{creatingKr ? '收起新建' : '+ 新建 KR'}</button>}
				<CommentSurfaceHint target={commentTarget} />
				{reviewEnabled && <PreviewReviewPanel target={reviewTarget} className="basis-full w-full" />}
			</>}
		</div>
	)
}

export function ManagementView({
	compactPresentation = false,
	title = 'OKR 管理',
	subtitle = '标签可标在整条 KR，也可下钻到策略/产品要点；长标签完整换行展示',
	showTags = true,
	deleteKrWarning = '连同各周进展一起删除',
	hierarchyNavigation = false,
	hierarchyScopeKey = '',
	cardHierarchy = false,
	defaultExpandDetails = false,
	hideStructuralFields = false,
	compactEmptyPointGroups = false,
	objectiveDragReorder = false,
	objectiveBusinessCategoryEditing = false,
	readOnly = false,
		reviewEnabled = false,
}: {
	compactPresentation?: boolean
	title?: string
	subtitle?: string
	showTags?: boolean
	deleteKrWarning?: string
	hierarchyNavigation?: boolean
	hierarchyScopeKey?: string
	cardHierarchy?: boolean
	defaultExpandDetails?: boolean
	hideStructuralFields?: boolean
	compactEmptyPointGroups?: boolean
	objectiveDragReorder?: boolean
	objectiveBusinessCategoryEditing?: boolean
	readOnly?: boolean
		reviewEnabled?: boolean
}) {
  const { objectives, quarter, syncState, createObjective, swapObjectives, reorderObjectives, swapKrs } = useBoard()
  const commentInteraction = useCommentInteraction()
  const [query, setQuery] = useState('')
  const [ownerFilters, setOwnerFilters] = useState<string[]>([])
  const [priority, setPriority] = useState('')
  const [tag, setTag] = useState('')
  // undefined means every category; '' is the untagged one, which is a real
  // choice here because management is where those KRs get their category.
  const [businessCategory, setBusinessCategory] = useState<string>()
	const [hierarchyPriority, setHierarchyPriority] = useState<KrPriority | '' | undefined>()
	const [hierarchyObjectiveId, setHierarchyObjectiveId] = useState('')
  const [creatingObjectiveId, setCreatingObjectiveId] = useState('')
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set())
  // 「指标与拆解」的展开态放在这里，工具栏的全部展开/折叠才管得到每一行。
  const [openDetails, setOpenDetails] = useState<Set<string>>(new Set())
  const [creatingObjective, setCreatingObjective] = useState(false)
  const [objectiveTitle, setObjectiveTitle] = useState('')
  const [objectiveQuarter, setObjectiveQuarter] = useState(quarter)
	const [placementNotice, setPlacementNotice] = useState('')
	const toolbarPriority = hierarchyNavigation ? '' : priority
	const hasFilters = Boolean(query.trim() || ownerFilters.length || toolbarPriority || (showTags && tag) || businessCategory !== undefined || hierarchyPriority !== undefined || hierarchyObjectiveId)
	const peopleOptions = useMemo(() => ownerOptions(objectives), [objectives])
	const ownerFilterOptions = useMemo(() => krOwnerOptions(objectives), [objectives])
	const ownersByKey = useMemo(() => new Map(ownerFilterOptions.map((owner) => [ownerIdentityKey(owner), owner])), [ownerFilterOptions])
	const selectedOwners = useMemo(() => ownerFilters.flatMap((key) => {
		const owner = ownersByKey.get(key)
		return owner ? [owner] : []
	}), [ownerFilters, ownersByKey])
	const ownerCounts = useMemo(() => krOwnerCounts(objectives, ownerFilterOptions), [objectives, ownerFilterOptions])
	const tags = useMemo(() => [...new Map(objectives.flatMap((objective) => objective.krs.flatMap((kr) => allTagsOf(kr).map((item) => [`${item.type}:${item.value}`, item] as const)))).entries()].map(([key, item]) => ({ key, ...item })).sort((left, right) => tagLabel(left.type, left.value).localeCompare(tagLabel(right.type, right.value))), [objectives])
	const businessCategories = useMemo(() => [...new Set(objectives.flatMap((objective) => objective.krs.map(businessCategoryOf)).filter(Boolean))].sort(), [objectives])
  const filtered = useMemo(() => objectives.map((objective) => ({
    ...objective,
		totalKrCount: objective.krs.length,
    krs: objective.krs.filter((kr) => {
      const matchesQuery = !query.trim() || `${objective.title} ${kr.title} ${kr.points.map((point) => point.title).join(' ')}`.toLowerCase().includes(query.trim().toLowerCase())
				return matchesQuery && krHasAnyOwner(kr, selectedOwners) && (!toolbarPriority || priorityOf(kr) === toolbarPriority) && (!showTags || !tag || allTagsOf(kr).some((item) => `${item.type}:${item.value}` === tag))
    }),
  })), [objectives, query, selectedOwners, showTags, tag, toolbarPriority])
	const navigation = useMemo(() => buildKRHierarchy(filtered.filter((objective) => objective.krs.length > 0)), [filtered])
  // Counts read every filter except the category itself, so each tab states how
  // many rows picking it would leave. Objectives the other filters emptied
  // contribute nothing rather than an untagged bucket of zero.
  const categoryOptions = useMemo(
		() => hierarchyNavigation
			? businessCategoryOptions(navigation)
			: withSelectedBusinessCategory(businessCategoryOptions(navigation), businessCategory),
		[businessCategory, hierarchyNavigation, navigation],
  )
	const categoryTotal = useMemo(() => filtered.reduce((total, objective) => total + objective.krs.length, 0), [filtered])
	const allBusiness = useMemo(() => buildAllBusinessNavigation(navigation), [navigation])
	const activeBusiness = useMemo(
		() => withCompletePriorityNavigation(businessCategory === undefined ? allBusiness : navigation.find((business) => business.value === businessCategory)),
		[allBusiness, businessCategory, navigation],
	)
	const activePriority = hierarchyPriority === undefined ? undefined : activeBusiness?.priorities.find((item) => item.value === hierarchyPriority)
	const directionObjectives = useMemo(() => {
		const derived = hierarchyPriority === undefined ? objectivesForBusiness(activeBusiness) : activePriority?.objectives ?? []
		const byID = new Map(derived.map((objective) => [objective.id, objective]))
		return objectives.map((objective) => byID.get(objective.id)).filter((objective): objective is Objective => Boolean(objective))
	}, [activeBusiness, activePriority, hierarchyPriority, objectives])
	const reorderVisibleObjectives = useCallback((visibleIds: string[]) => {
		const fullOrder = mergeVisibleObjectiveOrder(objectives.map((objective) => objective.id), visibleIds)
		void reorderObjectives(fullOrder).catch(() => undefined)
	}, [objectives, reorderObjectives])
	const groups = useMemo(() => {
		if (hierarchyNavigation) return filterObjectivesByHierarchy(filtered, businessCategory, hierarchyPriority, hierarchyObjectiveId)
		return filtered
			.map((objective) => ({ ...objective, krs: objective.krs.filter((kr) => businessCategory === undefined || businessCategoryOf(kr) === businessCategory) }))
			.filter((objective) => !hasFilters || objective.krs.length > 0)
	}, [businessCategory, filtered, hasFilters, hierarchyNavigation, hierarchyObjectiveId, hierarchyPriority])
  const resultCount = groups.reduce((total, objective) => total + objective.krs.length, 0)
  const tagCount = objectives.reduce((total, objective) => total + objective.krs.reduce((sum, kr) => sum + allTagsOf(kr).length, 0), 0)
	const defaultQuarter = quarter || `${new Date().getFullYear()}-Q${Math.floor(new Date().getMonth() / 3) + 1}`
	const definitionIdsKey = useMemo(() => objectives.flatMap((objective) => objective.krs.map((kr) => kr.id)).join('\n'), [objectives])

	useEffect(() => {
		setOwnerFilters((current) => {
			const valid = current.filter((key) => ownersByKey.has(key))
			return valid.length === current.length ? current : valid
		})
	}, [ownersByKey])

	useEffect(() => {
		if (!hierarchyNavigation) return
		setBusinessCategory(undefined)
		setHierarchyPriority(undefined)
		setHierarchyObjectiveId('')
	}, [hierarchyNavigation, hierarchyScopeKey])

	useEffect(() => {
		if (!defaultExpandDetails) return
		setOpenDetails(new Set(definitionIdsKey ? definitionIdsKey.split('\n') : []))
	}, [defaultExpandDetails, definitionIdsKey, hierarchyScopeKey])

	useEffect(() => {
		if (!hierarchyNavigation) return
		if (businessCategory !== undefined && !navigation.some((business) => business.value === businessCategory)) {
			setBusinessCategory(undefined)
			setHierarchyPriority(undefined)
			setHierarchyObjectiveId('')
			return
		}
		if (hierarchyPriority !== undefined && !activeBusiness?.priorities.some((item) => item.value === hierarchyPriority)) {
			setHierarchyPriority(undefined)
			setHierarchyObjectiveId('')
			return
		}
		if (hierarchyObjectiveId && !directionObjectives.some((objective) => objective.id === hierarchyObjectiveId)) setHierarchyObjectiveId('')
	}, [activeBusiness, businessCategory, directionObjectives, hierarchyNavigation, hierarchyObjectiveId, hierarchyPriority, navigation])

	useEffect(() => {
		const comment = commentInteraction.focused
		if (!comment) return
		const location = findCommentTargetLocation(objectives, comment)
		if (!location) return
		setQuery('')
		setOwnerFilters([])
		setPriority('')
		setTag('')
		if (hierarchyNavigation) {
			setBusinessCategory(location.kr ? businessCategoryOf(location.kr) : undefined)
			setHierarchyPriority(location.kr ? priorityOf(location.kr) : undefined)
			setHierarchyObjectiveId(location.objective.id)
		}
		setCollapsed((previous) => {
			if (!previous.has(location.objective.id)) return previous
			const next = new Set(previous)
			next.delete(location.objective.id)
			return next
		})
		if (location.kr) setOpenDetails((previous) => previous.has(location.kr!.id) ? previous : new Set(previous).add(location.kr!.id))
		const timeout = window.setTimeout(() => scrollToCommentSource(comment), 0)
		return () => window.clearTimeout(timeout)
	}, [commentInteraction.focused, hierarchyNavigation, objectives])

	const submitObjective = async () => {
		const title = objectiveTitle.trim()
		const targetQuarter = objectiveQuarter.trim()
		if (!title || !targetQuarter) return
		try {
			await createObjective({ quarter: targetQuarter, title })
			setObjectiveTitle('')
			setCreatingObjective(false)
			setPlacementNotice(`已新建 O“${title}”，位于 OKR 列表最底部。`)
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
    setOwnerFilters([])
    setPriority('')
    setTag('')
    setBusinessCategory(undefined)
		setHierarchyPriority(undefined)
		setHierarchyObjectiveId('')
  }

  return (
    <div className="flex flex-col gap-3">
      <section className={cardHierarchy ? '' : 'overflow-hidden rounded-xl border border-slate-200/80 bg-white shadow-[0_1px_2px_rgba(15,23,42,0.04)]'}>
        <div className={cardHierarchy ? 'rounded-xl border border-slate-200/80 bg-white px-3.5 py-3 shadow-[0_1px_2px_rgba(15,23,42,0.04)]' : 'border-b border-slate-100 px-3.5 py-3'}>
          <div className="flex flex-wrap items-center gap-2">
          {(title || subtitle) && <div className="mr-2">
            <div className="flex items-baseline gap-2">
              <h2 className="text-[13px] font-semibold text-slate-800">{title}</h2>
              <span className="text-[9px] tabular-nums text-slate-400">{resultCount} KR{showTags ? ` · ${tagCount} 标签` : ''}</span>
            </div>
            {subtitle && <p className="mt-0.5 text-[10px] text-slate-400">{subtitle}</p>}
          </div>}
          <div className="ml-auto rounded-lg bg-slate-50 px-2 py-1 text-[10px] text-slate-500">
            <span className="font-medium text-slate-700">{resultCount}</span> / {objectives.reduce((total, objective) => total + objective.krs.length, 0)} 条
          </div>
			{!readOnly && <button type="button" onClick={() => { setObjectiveQuarter(defaultQuarter); setCreatingObjective((value) => !value) }} className="h-8 rounded-lg bg-indigo-600 px-3 text-[10px] font-medium text-white hover:bg-indigo-700">+ 新建 O</button>}
          </div>
		  {!readOnly && creatingObjective && <div className="mt-3 flex flex-wrap items-center gap-2 rounded-xl border border-indigo-100 bg-indigo-50/60 p-2">
			<input value={objectiveQuarter} onChange={(event) => setObjectiveQuarter(event.target.value)} placeholder="2026-Q3" aria-label="季度" className="h-8 w-28 rounded-lg border border-slate-200 bg-white px-2.5 text-[11px] outline-none focus:border-indigo-400" />
			<input value={objectiveTitle} onChange={(event) => setObjectiveTitle(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') void submitObjective() }} placeholder="目标名称" aria-label="目标名称" autoFocus className="h-8 min-w-64 flex-1 rounded-lg border border-slate-200 bg-white px-2.5 text-[11px] outline-none focus:border-indigo-400" />
			<button type="button" onClick={() => void submitObjective()} disabled={!objectiveTitle.trim() || !objectiveQuarter.trim() || syncState.kind === 'saving'} className="h-8 rounded-lg bg-indigo-600 px-3 text-[10px] font-medium text-white disabled:opacity-40">创建目标</button>
			<button type="button" onClick={() => setCreatingObjective(false)} className="h-8 px-2 text-[10px] text-slate-400">取消</button>
		  </div>}
		  {placementNotice && <div role="status" className="mt-3 flex items-center gap-2 rounded-lg border border-emerald-100 bg-emerald-50 px-3 py-2 text-[10px] text-emerald-700"><span className="flex-1">{placementNotice}</span><button type="button" onClick={() => setPlacementNotice('')} className="text-emerald-500 hover:text-emerald-700">知道了</button></div>}
          <div className="mt-3 flex flex-wrap gap-1.5 rounded-xl border border-slate-200 bg-slate-50/70 p-2">
            <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索 O / KR 内容" className="h-8 min-w-48 flex-1 rounded-lg border border-slate-200 bg-white px-3 text-[11px] outline-none transition-colors focus:border-blue-400 focus:ring-2 focus:ring-blue-50 sm:max-w-72" />
            <OwnerFilterPicker options={ownerFilterOptions} ownerCounts={ownerCounts} selectedKeys={ownerFilters} onChange={setOwnerFilters} />
			{!hierarchyNavigation && <select value={priority} onChange={(event) => setPriority(event.target.value)} className="h-8 rounded-lg border border-slate-200 bg-white px-2.5 text-[10px] text-slate-600 outline-none focus:border-blue-400"><option value="">全部优先级</option><option value="p0">P0</option><option value="p1">P1</option><option value="p2">P2</option></select>}
            {showTags && <select value={tag} onChange={(event) => setTag(event.target.value)} title={tags.find((item) => item.key === tag)?.value ?? '全部标签'} className="h-8 max-w-80 rounded-lg border border-slate-200 bg-white px-2.5 text-[10px] text-slate-600 outline-none focus:border-blue-400"><option value="">全部标签</option>{tags.map((item) => <option key={item.key} value={item.key}>{tagLabel(item.type, item.value)}</option>)}</select>}
            {hasFilters && <button type="button" onClick={clearFilters} className="h-8 rounded-lg px-2.5 text-[10px] font-medium text-slate-500 hover:bg-white hover:text-slate-800">清空筛选</button>}
			<span className="ml-auto self-center text-[10px] text-slate-400">层级</span>
            <div className="inline-flex h-8 items-center overflow-hidden rounded-lg border border-slate-200 bg-white text-[10px]">
              <button type="button" onClick={expandAll} className="h-full px-2.5 text-slate-500 hover:bg-slate-50 hover:text-slate-700">全部展开</button>
              <button type="button" onClick={collapseToKr} className="h-full border-l border-slate-200 px-2.5 text-slate-500 hover:bg-slate-50 hover:text-slate-700">折叠到 KR</button>
            </div>
          </div>
			{hierarchyNavigation ? <div className="mt-2">
				<HierarchyNav
					navigation={navigation}
					activeBusiness={activeBusiness}
					activePriority={activePriority}
					activeObjectiveId={hierarchyObjectiveId}
					directionObjectives={directionObjectives}
					objectiveLabel="具体 O"
					showEmptyObjectives
					overview={businessCategory === undefined}
					showOverview
					onOverview={() => { setBusinessCategory(undefined); setHierarchyPriority(undefined); setHierarchyObjectiveId('') }}
					onBusiness={(value) => { setBusinessCategory(value); setHierarchyPriority(undefined); setHierarchyObjectiveId('') }}
						onPriority={(value) => { setHierarchyPriority(value as KrPriority | ''); setHierarchyObjectiveId('') }}
						onObjective={setHierarchyObjectiveId}
						onObjectiveOrderChange={!readOnly && objectiveDragReorder ? reorderVisibleObjectives : undefined}
						objectiveReorderDisabled={syncState.kind === 'loading' || syncState.kind === 'saving'}
					/>
			</div> : <div className="mt-2 rounded-xl border border-slate-200 bg-gradient-to-b from-slate-50/90 to-slate-100/65 p-2.5">
				<BusinessCategoryTabs options={categoryOptions} activeValue={businessCategory} total={categoryTotal} showOverview onSelect={setBusinessCategory} />
			</div>}
        </div>

        <div className={compactPresentation ? 'space-y-3 pt-1' : cardHierarchy ? 'space-y-6 pt-1' : ''}>
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
				cardHierarchy={cardHierarchy}
					businessCategories={businessCategories}
					businessCategoryEditable={objectiveBusinessCategoryEditing}
					businessCategoryObjective={objectives.find((item) => item.id === objective.id)}
					readOnly={readOnly}
						reviewEnabled={reviewEnabled}
              />
			  {!collapsed.has(objective.id) && <div className={compactPresentation ? 'space-y-1.5' : cardHierarchy ? 'space-y-3' : 'divide-y divide-slate-100'}>
						{!readOnly && creatingObjectiveId === objective.id && <NewKrRow objective={objective} businessCategories={businessCategories} peopleOptions={peopleOptions} cardHierarchy={cardHierarchy} onClose={() => setCreatingObjectiveId('')} onCreated={() => setPlacementNotice(`已新建 KR，位于“O ${objective.title}”下的 KR 列表最底部。`)} />}
						{objective.krs.map((kr, krIndex) => <KrEditorRow
                            compactPresentation={compactPresentation}
							key={kr.id}
							objectiveId={objective.id}
							kr={kr}
							tagSuggestions={tags}
							businessCategories={businessCategories}
							detailsOpen={openDetails.has(kr.id)}
							cardHierarchy={cardHierarchy}
							compactEmptyPointGroups={compactEmptyPointGroups}
							onToggleDetails={() => toggleDetails(kr.id)}
							onMoveUp={krIndex > 0 ? () => void swapKrs(objective.id, kr.id, objective.krs[krIndex - 1].id) : undefined}
							onMoveDown={krIndex < objective.krs.length - 1 ? () => void swapKrs(objective.id, kr.id, objective.krs[krIndex + 1].id) : undefined}
							showTags={showTags}
							showStructuralFields={!hideStructuralFields}
							deleteWarning={deleteKrWarning}
							readOnly={readOnly}
								reviewEnabled={reviewEnabled}
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
