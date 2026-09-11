import { useState, type CSSProperties } from 'react'
import { closestCenter, DndContext, KeyboardSensor, PointerSensor, useSensor, useSensors, type DragEndEvent } from '@dnd-kit/core'
import { arrayMove, horizontalListSortingStrategy, SortableContext, sortableKeyboardCoordinates, useSortable } from '@dnd-kit/sortable'
import { businessCategoryOptions, hierarchyKRCount, priorityKRCount, type BusinessNavigation, type PriorityNavigation } from '../hierarchy'
import type { Objective } from '../types'
import { BusinessCategoryTabs } from './BusinessCategoryTabs'

interface HierarchyNavProps {
  navigation: BusinessNavigation[]
  activeBusiness?: BusinessNavigation
	activePriority?: PriorityNavigation
	activeObjectiveId?: string
	directionObjectives?: Objective[]
	objectiveLabel?: string
	showEmptyObjectives?: boolean
  overview?: boolean
  showOverview?: boolean
  onOverview?: () => void
  onBusiness: (value: string) => void
  onPriority: (value: string) => void
	onObjective: (id: string) => void
	onObjectiveOrderChange?: (ids: string[]) => void
	objectiveReorderDisabled?: boolean
}

function priorityTone(value: string, selected: boolean) {
  if (!selected) return 'border-transparent text-slate-600 hover:bg-white'
  if (value === 'p0') return 'border-orange-200 bg-orange-50 text-orange-700 shadow-sm'
  if (value === 'p1') return 'border-amber-200 bg-amber-50 text-amber-700 shadow-sm'
  if (value === 'p2') return 'border-blue-200 bg-white text-blue-700 shadow-sm'
  return 'border-slate-200 bg-white text-slate-700 shadow-sm'
}

function SortableObjectiveTab({ objective, selected, showHandle, disabled, onSelect }: { objective: Objective; selected: boolean; showHandle: boolean; disabled: boolean; onSelect: () => void }) {
	const { attributes, listeners, setActivatorNodeRef, setNodeRef, transform, transition, isDragging } = useSortable({ id: objective.id, disabled })
	const style: CSSProperties = {
		transform: transform ? `translate3d(${transform.x}px, 0, 0)` : undefined,
		transition,
		zIndex: isDragging ? 20 : undefined,
	}
	return <div ref={setNodeRef} style={style} className={`inline-flex max-w-64 shrink-0 items-stretch overflow-hidden rounded-lg border transition-[border-color,background-color,box-shadow,opacity] ${isDragging ? 'opacity-55 shadow-lg' : ''} ${selected ? 'border-blue-300 bg-white text-blue-700 shadow-[inset_0_-3px_0_#2563eb,0_2px_6px_rgba(37,99,235,0.07)]' : 'border-slate-200 bg-white/60 text-slate-600 hover:border-slate-300 hover:bg-white'}`}>
		<button type="button" role="tab" aria-selected={selected} title={objective.title} onClick={onSelect} className="inline-flex min-w-0 items-center gap-1.5 px-2.5 py-1.5 text-[12px] font-semibold">
			<span className="truncate">{objective.title || '未命名方向'}</span><b className="rounded-full bg-slate-200/70 px-1.5 text-[10px]">{objective.krs.length}</b>
		</button>
		{showHandle && <button ref={setActivatorNodeRef} type="button" disabled={disabled} aria-label={`拖动“${objective.title || '未命名方向'}”调整顺序`} title="拖动调整 O 顺序；键盘按空格后用左右方向键移动" className="flex w-6 shrink-0 touch-none cursor-grab items-center justify-center border-l border-slate-200/80 text-[12px] text-slate-400 hover:bg-slate-50 hover:text-blue-600 active:cursor-grabbing disabled:cursor-wait disabled:opacity-30" {...attributes} {...listeners}>⠿</button>}
	</div>
}

export function HierarchyNav({
		navigation, activeBusiness, activePriority, activeObjectiveId, directionObjectives, objectiveLabel = '方向', showEmptyObjectives = false, overview = false, showOverview = false,
		onOverview, onBusiness, onPriority, onObjective, onObjectiveOrderChange, objectiveReorderDisabled = false,
	}: HierarchyNavProps) {
	const total = navigation.reduce((sum, item) => sum + hierarchyKRCount(item), 0)
	const objectives = directionObjectives ?? activePriority?.objectives ?? []
	const [activeDragId, setActiveDragId] = useState('')
	const sensors = useSensors(
		useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
		useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
	)
	const reorderAvailable = Boolean(onObjectiveOrderChange) && objectives.length > 1
	const reorderEnabled = reorderAvailable && !objectiveReorderDisabled
	const finishDrag = ({ active, over }: DragEndEvent) => {
		setActiveDragId('')
		if (!over || active.id === over.id || !reorderEnabled) return
		const ids = objectives.map((objective) => objective.id)
		const from = ids.indexOf(String(active.id))
		const to = ids.indexOf(String(over.id))
		if (from < 0 || to < 0) return
		onObjectiveOrderChange?.(arrayMove(ids, from, to))
	}
  return (
    <section aria-label="KR 分类导航" className="mb-3 rounded-xl border border-slate-200 bg-gradient-to-b from-slate-50/90 to-slate-100/65 p-3 shadow-[0_3px_12px_rgba(31,35,40,0.035)]">
      <BusinessCategoryTabs
        options={businessCategoryOptions(navigation)}
        activeValue={overview ? undefined : activeBusiness?.value}
        total={total}
        showOverview={showOverview}
        onSelect={(value) => value === undefined ? onOverview?.() : onBusiness(value)}
      />

      {activeBusiness && <div className="mt-2.5 grid gap-1.5 border-t border-slate-200/80 pt-2.5 lg:grid-cols-[5.25rem_minmax(0,1fr)] lg:gap-2.5">
        <span className="flex items-center text-[10px] font-bold tracking-[0.04em] text-slate-400">Focus / P1 / P2</span>
        <nav aria-label={`${activeBusiness.label}优先级`} role="tablist" className="inline-flex w-fit max-w-full items-center gap-1 overflow-x-auto rounded-lg bg-slate-200/60 p-1">
          {activeBusiness.priorities.map((priority) => {
            const selected = priority.value === activePriority?.value
            return <button key={priority.value || '__untagged__'} type="button" role="tab" aria-selected={selected} onClick={() => onPriority(priority.value)} className={`inline-flex shrink-0 items-center gap-1.5 rounded-md border px-2.5 py-1 text-[12px] font-semibold transition-colors ${priorityTone(priority.value, selected)}`}>{priority.label}<b className="rounded-full bg-slate-200/70 px-1.5 text-[10px]">{priorityKRCount(priority)}</b></button>
          })}
        </nav>
      </div>}

			{(objectives.length > 0 || showEmptyObjectives) && <div className="mt-2.5 grid gap-1.5 border-t border-slate-200/80 pt-2.5 lg:grid-cols-[5.25rem_minmax(0,1fr)] lg:gap-2.5">
				<span className="flex items-center text-[10px] font-bold tracking-[0.04em] text-slate-400">{objectiveLabel}</span>
					<DndContext sensors={sensors} collisionDetection={closestCenter} onDragStart={({ active }) => setActiveDragId(String(active.id))} onDragCancel={() => setActiveDragId('')} onDragEnd={finishDrag}>
					<nav aria-label={objectiveLabel} role="tablist" className={`flex gap-1.5 overflow-x-auto pb-0.5 ${activeDragId ? 'select-none' : ''}`}>
						<SortableContext items={objectives.map((objective) => objective.id)} strategy={horizontalListSortingStrategy}>
							{objectives.map((objective) => <SortableObjectiveTab key={objective.id} objective={objective} selected={objective.id === activeObjectiveId} showHandle={reorderAvailable} disabled={!reorderEnabled} onSelect={() => onObjective(objective.id)} />)}
						</SortableContext>
						{objectives.length === 0 && <span className="px-1 py-1.5 text-[11px] text-slate-400">暂无对应 O</span>}
					</nav>
					</DndContext>
      </div>}
    </section>
  )
}
