import { useEffect, useState } from 'react'
import { useBoard } from './board'
import { PlanBoardProvider, usePlanBoard } from './planStore'
import { QuarterSelect } from './components/QuarterSelect'
import { ManagementView } from './components/ManagementView'
import { WeeklyShareNav } from './components/WeeklyShareNav'
import { ActivityLogButton } from './components/ActivityLogButton'
import type { WeeklyShareTab } from './share'

function SyncNotice() {
  const { syncState, retry } = useBoard()
  if (syncState.kind !== 'error') return null
  return (
    <div className="mb-3 flex items-center gap-2 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-800">
      <span className="flex-1">{syncState.message}</span>
      <button type="button" onClick={retry} className="rounded-md border border-red-200 bg-white px-2.5 py-1 hover:bg-red-100">重试</button>
    </div>
  )
}

function NewPlanPanel({ onClose }: { onClose: () => void }) {
  const { quarter, createPlan, syncState } = usePlanBoard()
  const [targetQuarter, setTargetQuarter] = useState(quarter)
  const [title, setTitle] = useState('')

  useEffect(() => setTargetQuarter(quarter), [quarter])

  const submit = async () => {
    const cleanTitle = title.trim()
    const cleanQuarter = targetQuarter.trim()
    if (!cleanTitle || !cleanQuarter) return
    await createPlan({ quarter: cleanQuarter, title: cleanTitle })
    setTitle('')
    onClose()
  }

  return (
    <section className="mb-3 flex flex-wrap items-center gap-2 rounded-xl border border-indigo-100 bg-indigo-50/60 p-3">
      <div className="mr-2">
        <div className="text-xs font-semibold text-slate-700">新建 OKR Plan</div>
        <div className="mt-0.5 text-[10px] text-slate-400">创建独立草稿，不会改动正式 OKR。</div>
      </div>
      <input value={targetQuarter} onChange={(event) => setTargetQuarter(event.target.value)} placeholder="2026-Q3" aria-label="季度" className="h-9 w-28 rounded-lg border border-slate-200 bg-white px-3 text-xs outline-none focus:border-indigo-400" />
      <input autoFocus value={title} onChange={(event) => setTitle(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') void submit(); if (event.key === 'Escape') onClose() }} placeholder="Plan 名称" aria-label="Plan 名称" className="h-9 min-w-64 flex-1 rounded-lg border border-slate-200 bg-white px-3 text-xs outline-none focus:border-indigo-400" />
      <button type="button" onClick={() => void submit()} disabled={!targetQuarter.trim() || !title.trim() || syncState.kind === 'saving'} className="h-9 rounded-lg bg-indigo-600 px-4 text-xs font-medium text-white disabled:opacity-40">确认新建</button>
      <button type="button" onClick={onClose} className="h-9 px-2 text-xs text-slate-400">取消</button>
    </section>
  )
}

function PlanCanvas({ shared = false, onShareTabChange }: { shared?: boolean; onShareTabChange?: (tab: WeeklyShareTab) => void }) {
  const { plan, plans, quarter, syncState, selectPlan, deleteCurrentPlan } = usePlanBoard()
  const [creatingPlan, setCreatingPlan] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const saving = syncState.kind === 'saving' || syncState.kind === 'loading'

  return (
    <>
      <header className="sticky top-0 z-40 border-b border-slate-200/80 bg-white/95 backdrop-blur">
        <div className="mx-auto flex min-h-14 max-w-[1580px] flex-wrap items-center gap-3 px-4 py-2 sm:px-6 lg:px-8">
          <span className="flex size-8 items-center justify-center rounded-lg bg-emerald-600 text-xs font-semibold text-white shadow-sm">P</span>
          <div className="leading-tight">
            <h1 className="text-[14px] font-semibold tracking-tight text-slate-900">Emily · OKR Plan</h1>
            <div className="mt-1 text-[10px] text-slate-400">{quarter.replace('-', ' ')} · 计划草稿</div>
          </div>
          {shared && <WeeklyShareNav currentTab="okr-plan" onChange={(tab) => onShareTabChange?.(tab)} />}
          <span className={`text-[10px] ${syncState.kind === 'saving' ? 'text-blue-600' : syncState.kind === 'saved' ? 'text-emerald-600' : syncState.kind === 'error' ? 'text-red-600' : 'text-slate-400'}`} aria-live="polite">{syncState.message}</span>
          <div className="ml-auto flex flex-wrap items-center gap-2">
            <QuarterSelect />
            <select aria-label="选择 Plan" value={plan?.id ?? ''} disabled={plans.length === 0 || saving} onChange={(event) => { setConfirmDelete(false); selectPlan(event.target.value) }} className="h-8 min-w-40 rounded-lg border border-slate-200 bg-white px-2.5 text-[10px] text-slate-600 outline-none disabled:text-slate-400">
              {plans.length === 0 && <option value="">暂无 Plan</option>}
              {plans.map((item) => <option key={item.id} value={item.id}>{item.title}</option>)}
            </select>
            <ActivityLogButton surface="plan" quarter={quarter} planId={plan?.id} disabled={!plan} />
            <button type="button" onClick={() => { setConfirmDelete(false); setCreatingPlan((value) => !value) }} className="h-8 rounded-lg bg-emerald-600 px-3 text-[10px] font-medium text-white hover:bg-emerald-700">新建 Plan</button>
            <button type="button" disabled={!plan || saving} onClick={() => { setCreatingPlan(false); setConfirmDelete(true) }} className="h-8 rounded-lg border border-red-200 bg-red-50 px-3 text-[10px] font-medium text-red-700 hover:bg-red-100 disabled:opacity-40">删除 Plan</button>
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-[1580px] px-4 py-3 sm:px-6 sm:py-4 lg:px-8">
        {creatingPlan && <NewPlanPanel onClose={() => setCreatingPlan(false)} />}
        {confirmDelete && plan && (
          <section className="mb-3 flex flex-wrap items-center gap-2 rounded-xl border border-red-200 bg-red-50 p-3">
            <div className="mr-2 flex-1">
              <div className="text-xs font-semibold text-red-800">确认删除 {plan.title}？</div>
              <div className="mt-0.5 text-[10px] text-red-600">只删除这个 Plan 草稿，不会删除正式 O、KR 或周报。</div>
            </div>
            <button type="button" onClick={() => { void deleteCurrentPlan().then(() => setConfirmDelete(false)) }} disabled={saving} className="h-9 rounded-lg bg-red-600 px-4 text-xs font-medium text-white disabled:opacity-40">确认删除</button>
            <button type="button" onClick={() => setConfirmDelete(false)} disabled={saving} className="h-9 px-2 text-xs text-slate-500 disabled:opacity-40">取消</button>
          </section>
        )}
        <SyncNotice />
        {plan ? (
          <div className={`transition-opacity ${saving ? 'pointer-events-none opacity-55' : ''}`}>
            <ManagementView
              title={plan.title}
              subtitle={`OKR Plan 草稿独立保存，当前版本 v${plan.version}；结构编辑与“管理与打标”一致，不影响正式 OKR。`}
              showTags
              deleteKrWarning="只删除这个 Plan 草稿里的 KR"
            />
          </div>
        ) : (
          <section className="rounded-2xl border border-dashed border-slate-300 bg-white px-6 py-12 text-center">
            <div className="text-sm font-semibold text-slate-700">当前季度暂无 OKR Plan</div>
            <div className="mt-1 text-xs text-slate-400">点击顶部“新建 Plan”开始规划。</div>
          </section>
        )}
      </main>
    </>
  )
}

export default function PlanApp({
  initialQuarter = '',
  onQuarterChange,
  shared = false,
  onShareTabChange,
}: {
  initialQuarter?: string
  onQuarterChange?: (quarter: string) => void
  shared?: boolean
  onShareTabChange?: (tab: WeeklyShareTab) => void
}) {
  return (
    <PlanBoardProvider initialQuarter={initialQuarter} onQuarterChange={onQuarterChange}>
      <PlanCanvas shared={shared} onShareTabChange={onShareTabChange} />
    </PlanBoardProvider>
  )
}
