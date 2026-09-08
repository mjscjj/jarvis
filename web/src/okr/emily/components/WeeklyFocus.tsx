import { useCallback, useEffect, useMemo, useState } from 'react'
import MarkdownReport from '../../../components/MarkdownReport'
import { createFollowUp, deleteFollowUp, getFollowUps, updateFollowUp } from '../api'
import { useBoard, uid } from '../board'
import { useCommentInteraction } from '../commenting'
import { commentTargetKey } from '../comments'
import { canEditFollowUpStatus, FOLLOW_UP_STATUS_OPTIONS, isClosedFollowUp, sortFollowUpsByAssignDate } from '../followUps'
import { ownerOptions, splitOwnerNames } from '../people'
import type { CommentTarget, FollowUpItem, FollowUpStatus, KrOwner, PageComment, Point } from '../types'
import { FeishuPeoplePickerInput } from './FeishuPeoplePicker'

function krNeedsUpdate(points: Point[]) {
  return points.length === 0 || points.some((point) => !point.entries.some((entry) => entry.text.trim().length > 0))
}

function targetLabel(comment: PageComment) {
  if (comment.targetTitle?.trim()) return comment.targetTitle
  return ({ page: '整页评论', kr: 'KR 评论', metric: '核心数据评论', point: '具体 KR 评论', entry: '进展评论', follow_up: '待跟进事项评论' } as const)[comment.targetType]
}

function ownerKey(owner: KrOwner) {
  return owner.openId || `name:${owner.name}`
}

/** Review progress gaps, structured follow-ups, and legacy meeting-comment todo markers. */
export function WeeklyFocus({ comments, onOpenComment, readOnly = false, statusEditable = false }: {
  comments: PageComment[]
  onOpenComment: (comment: PageComment) => void
  readOnly?: boolean
  statusEditable?: boolean
}) {
  const { objectives, quarter, week } = useBoard()
  const commentInteraction = useCommentInteraction()
  const [items, setItems] = useState<FollowUpItem[]>([])
  const [loading, setLoading] = useState(false)
  const [savingID, setSavingID] = useState('')
  const [error, setError] = useState('')
  const [showDone, setShowDone] = useState(true)
  const [followUpsOpen, setFollowUpsOpen] = useState(true)

  const incomplete = useMemo(() => {
    const missing = new Map<string, number>()
    for (const objective of objectives) {
      for (const kr of objective.krs) {
        if (!krNeedsUpdate(kr.points)) continue
        const owners = splitOwnerNames(kr.ownerName)
        for (const owner of owners.length > 0 ? owners : ['未分配']) missing.set(owner, (missing.get(owner) ?? 0) + 1)
      }
    }
    return [...missing]
      .map(([name, count]) => ({ name, count }))
      .sort((left, right) => right.count - left.count || left.name.localeCompare(right.name, 'zh-Hans-CN'))
  }, [objectives])
  const legacyTodos = useMemo(() => comments.filter((comment) => comment.todo && !comment.resolved), [comments])
  const visibleItems = useMemo(() => sortFollowUpsByAssignDate(showDone ? items : items.filter((item) => !isClosedFollowUp(item.status))), [items, showDone])
  const peopleOptions = useMemo(() => {
    const result = new Map<string, KrOwner>()
    for (const owner of [...ownerOptions(objectives), ...items.flatMap((item) => item.owners)]) result.set(ownerKey(owner), owner)
    return [...result.values()].sort((left, right) => left.name.localeCompare(right.name, 'zh-Hans-CN'))
  }, [items, objectives])

  const load = useCallback(async () => {
    if (!quarter || !week) return
    setLoading(true)
    setError('')
    try {
      setItems((await getFollowUps(quarter, week)).items)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '读取待跟进事项失败')
    } finally {
      setLoading(false)
    }
  }, [quarter, week])

  useEffect(() => { void load() }, [load])
  useEffect(() => {
    const refresh = () => void load()
    window.addEventListener('jarvis:chat-completed', refresh)
    return () => window.removeEventListener('jarvis:chat-completed', refresh)
  }, [load])

  const patchLocal = (id: string, patch: Partial<FollowUpItem>) => {
    setItems((current) => current.map((item) => item.id === id ? { ...item, ...patch } : item))
  }

  const save = async (item: FollowUpItem, patch: Partial<FollowUpItem>) => {
    const next = { ...item, ...patch }
    patchLocal(item.id, patch)
    setSavingID(item.id)
    setError('')
    try {
      const saved = await updateFollowUp(next)
      setItems((current) => current.map((candidate) => candidate.id === saved.id ? saved : candidate))
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '保存待跟进事项失败')
      await load()
    } finally {
      setSavingID('')
    }
  }

  const add = async () => {
    const now = new Date().toISOString()
    const item: FollowUpItem = {
      id: uid('followup'), quarter, week, version: 0, topic: '新待跟进事项', owners: [], status: 'not_started',
      assignDate: '', update: '', sourcePayload: {}, sortOrder: items.length, createdBy: '', updatedBy: '', createdAt: now, updatedAt: now,
    }
    setSavingID(item.id)
    setError('')
    try {
      const saved = await createFollowUp(item)
      setItems((current) => [...current, saved])
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '新增待跟进事项失败')
    } finally {
      setSavingID('')
    }
  }

  const remove = async (item: FollowUpItem) => {
    if (!window.confirm(`确认删除“${item.topic}”？`)) return
    setSavingID(item.id)
    setError('')
    try {
      await deleteFollowUp(item)
      setItems((current) => current.filter((candidate) => candidate.id !== item.id))
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '删除待跟进事项失败')
      await load()
    } finally {
      setSavingID('')
    }
  }

  return (
    <section aria-label="本周重点关注" className="mb-4 rounded-xl border border-blue-100 bg-gradient-to-br from-white to-blue-50/35 p-3.5 shadow-[0_3px_12px_rgba(31,35,40,0.035)]">
      <header className="mb-2.5 flex flex-wrap items-end gap-2 border-b border-slate-100 pb-2">
        <div><span className="block text-[9px] font-bold tracking-[0.12em] text-blue-600">WEEKLY FOCUS</span><h2 className="text-[16px] font-semibold text-slate-900">本周重点关注</h2></div>
        <div className="ml-auto flex items-center gap-2 text-[10px] text-slate-500">
          <label className="inline-flex items-center gap-1"><input type="checkbox" checked={showDone} onChange={(event) => setShowDone(event.target.checked)} />显示已完成/废弃</label>
          {!readOnly && <button type="button" onClick={() => void add()} disabled={Boolean(savingID)} className="rounded-md border border-blue-200 bg-blue-50 px-2 py-1 font-medium text-blue-700 disabled:opacity-40">+ 新增事项</button>}
        </div>
      </header>
      <div className="space-y-2.5">
        <div className="min-w-0 rounded-lg border border-slate-200 bg-white/85 p-2.5">
          <h3 className="mb-2 flex items-center justify-between text-[12px] font-semibold text-slate-700"><span>未完成 OKR</span><b className="text-[10px] font-medium text-slate-400">{incomplete.length} 人</b></h3>
          {incomplete.length > 0 ? <div className="flex flex-wrap gap-1.5">{incomplete.map((item) => <span key={item.name} title={`${item.count} 条 KR 尚未完整填写本周进展`} className="inline-flex items-center gap-1 rounded-full border border-slate-200 bg-slate-50 py-0.5 pr-2 pl-1 text-[10px] text-slate-600"><span className="flex size-4 items-center justify-center rounded-full bg-slate-200 text-[8px] font-semibold">{item.name.slice(0, 1)}</span>{item.name}{item.count > 1 && <b className="text-blue-600">{item.count}</b>}</span>)}</div> : <div className="text-[11px] text-slate-400">本周具体 KR 均已填写</div>}
        </div>
        <div className="min-w-0 rounded-lg border border-slate-200 bg-white/85 p-2.5">
          <h3 className={`flex items-center justify-between gap-2 text-[12px] font-semibold text-slate-700 ${followUpsOpen ? 'mb-2' : ''}`}>
            <span className="flex items-center gap-2">
              <button
                type="button"
                aria-expanded={followUpsOpen}
                aria-controls="weekly-follow-ups-content"
                onClick={() => setFollowUpsOpen((open) => !open)}
                className="rounded-md border border-slate-200 bg-white px-2 py-0.5 text-[10px] font-medium text-slate-500 hover:border-blue-200 hover:text-blue-600"
              >
                {followUpsOpen ? '收起' : '展开'}
              </button>
              <span>待跟进事项</span>
            </span>
            <b className="text-[10px] font-medium text-slate-400">{items.filter((item) => !isClosedFollowUp(item.status)).length} 待跟进 / {items.length} 全部</b>
          </h3>
          {followUpsOpen && <div id="weekly-follow-ups-content">
            {error && <div className="mb-2 rounded-md bg-red-50 px-2 py-1.5 text-[10px] text-red-700">{error} <button type="button" onClick={() => void load()} className="underline">重试</button></div>}
            {loading ? <div className="py-4 text-center text-[11px] text-slate-400">正在读取待跟进事项…</div> : visibleItems.length > 0 ? (
            <div className="overflow-x-auto rounded-lg border border-slate-200">
              <table className="w-full min-w-[1040px] table-fixed border-collapse text-left text-[11px]">
                <colgroup><col className="w-[21%]"/><col className="w-[18%]"/><col className="w-[12%]"/><col className="w-[12%]"/><col className="w-[30%]"/><col className="w-[7%]"/></colgroup>
                <thead className="bg-slate-50 text-[10px] font-semibold text-slate-500"><tr><th className="border-b border-r border-slate-200 px-2 py-2">Topic</th><th className="border-b border-r border-slate-200 px-2 py-2">Owner</th><th className="border-b border-r border-slate-200 px-2 py-2">Status</th><th className="border-b border-r border-slate-200 px-2 py-2">Assign Date</th><th className="border-b border-r border-slate-200 px-2 py-2">Update</th><th className="border-b border-slate-200 px-1 py-2 text-center">评论</th></tr></thead>
                <tbody>{visibleItems.map((item) => {
                  const saving = savingID === item.id
                  const commentTarget: CommentTarget = { type: 'follow_up', id: item.id, title: item.topic }
                  const commentKey = commentTargetKey(commentTarget)
                  const commentCount = commentInteraction.counts[commentKey] ?? 0
                  const commentSelected = commentInteraction.selected ? commentTargetKey(commentInteraction.selected) === commentKey : false
                  return <tr key={item.id} className={`${isClosedFollowUp(item.status) ? 'bg-slate-50/70 text-slate-400' : 'bg-white text-slate-700'} align-top`}>
                    <td className="border-r border-b border-slate-100 p-1.5">{readOnly ? <span className="whitespace-pre-wrap font-medium">{item.topic}</span> : <textarea aria-label={`Topic ${item.topic}`} value={item.topic} onChange={(event) => patchLocal(item.id, { topic: event.target.value })} onBlur={(event) => void save(item, { topic: event.target.value.trim() })} className="min-h-16 w-full resize-y rounded border border-transparent bg-transparent p-1 font-medium outline-none hover:border-slate-200 focus:border-blue-300"/>}</td>
                    <td className="border-r border-b border-slate-100 p-2">{readOnly ? <span className="flex flex-wrap gap-1">{item.owners.map((owner) => <span key={ownerKey(owner)} className="rounded-full border border-slate-200 bg-white px-2 py-0.5">{owner.name}</span>)}</span> : <FeishuPeoplePickerInput owners={item.owners} options={peopleOptions} onChange={(owners) => void save(item, { owners })}/>}</td>
                    <td className="border-r border-b border-slate-100 p-1.5"><select aria-label={`Status ${item.topic}`} value={item.status} disabled={!canEditFollowUpStatus(readOnly, statusEditable) || saving} onChange={(event) => void save(item, { status: event.target.value as FollowUpStatus })} className="h-8 w-full rounded-md border border-slate-200 bg-white px-2 text-[10px] text-slate-600 outline-none disabled:bg-transparent">{FOLLOW_UP_STATUS_OPTIONS.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}</select></td>
                    <td className="border-r border-b border-slate-100 p-1.5">{readOnly ? item.assignDate : <input aria-label={`Assign Date ${item.topic}`} type="date" value={item.assignDate} disabled={saving} onChange={(event) => void save(item, { assignDate: event.target.value })} className="h-8 w-full rounded-md border border-slate-200 bg-white px-2 text-[10px] outline-none"/>}</td>
                    <td className="border-r border-b border-slate-100 p-1.5">{readOnly ? <MarkdownReport content={item.update || '—'} className="text-[11px]"/> : <textarea aria-label={`Update ${item.topic}`} value={item.update} onChange={(event) => patchLocal(item.id, { update: event.target.value })} onBlur={(event) => void save(item, { update: event.target.value.trim() })} className="min-h-16 w-full resize-y rounded border border-transparent bg-transparent p-1 outline-none hover:border-slate-200 focus:border-blue-300"/>}</td>
                    <td className="border-b border-slate-100 px-1 py-2 text-center">
                      <div className="flex items-center justify-center gap-1">
                        <button type="button" aria-label={`评论 ${item.topic}`} title="查看或添加评论" onClick={() => commentInteraction.select(commentTarget)} className={`inline-flex min-w-7 items-center justify-center gap-0.5 rounded-md border px-1 py-1 text-[10px] font-medium ${commentSelected ? 'border-indigo-300 bg-indigo-50 text-indigo-700' : 'border-slate-200 bg-white text-slate-500 hover:border-indigo-200 hover:text-indigo-600'}`}><span aria-hidden="true">💬</span>{commentCount > 0 && <span>{commentCount}</span>}</button>
                        {!readOnly && <button type="button" title="删除事项" disabled={saving} onClick={() => void remove(item)} className="px-1 text-slate-300 hover:text-red-500 disabled:opacity-40">×</button>}
                      </div>
                    </td>
                  </tr>
                })}</tbody>
              </table>
            </div>
            ) : <div className="text-[11px] text-slate-400">当前范围暂无待跟进事项</div>}
            {legacyTodos.length > 0 && <div className="mt-2 border-t border-slate-100 pt-2"><div className="mb-1 text-[10px] font-medium text-slate-500">会议评论待办</div><div className="grid gap-1">{legacyTodos.map((comment) => <button key={comment.id} type="button" title="打开对应评论" onClick={() => onOpenComment(comment)} className="grid w-full grid-cols-[minmax(0,1fr)_auto] items-baseline gap-2 rounded-md bg-amber-50 px-2 py-1.5 text-left hover:bg-amber-100"><span className="truncate text-[11px] font-medium text-slate-700">{comment.content}</span><span className="max-w-56 truncate text-[10px] text-slate-400">{targetLabel(comment)}</span></button>)}</div></div>}
          </div>}
        </div>
      </div>
    </section>
  )
}
