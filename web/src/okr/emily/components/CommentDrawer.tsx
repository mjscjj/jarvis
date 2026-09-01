import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { createComment, deleteComment, getComments, updateComment } from '../api'
import { commentCountsByTarget, commentMatchesTarget, commentMessageCount } from '../comments'
import type { CommentTarget, PageComment } from '../types'

function displayTime(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  const now = new Date()
  const sameDay = date.toDateString() === now.toDateString()
  return new Intl.DateTimeFormat('zh-CN', sameDay
    ? { hour: '2-digit', minute: '2-digit', hour12: false }
    : { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false }).format(date)
}

function targetLabel(type: PageComment['targetType']) {
  return ({ page: '整页', kr: 'KR', metric: '核心数据', point: '具体 KR', entry: '进展条目' } as const)[type]
}

function Avatar({ name, small = false }: { name: string; small?: boolean }) {
  return <span className={`flex shrink-0 items-center justify-center rounded-full bg-indigo-50 font-semibold text-indigo-600 ${small ? 'size-6 text-[10px]' : 'size-7 text-[11px]'}`}>{name.trim().slice(0, 1) || '我'}</span>
}

function wasEdited(comment: PageComment) {
  return new Date(comment.updatedAt).getTime() - new Date(comment.createdAt).getTime() > 1000
}

function EditableCommentBody({ comment, compact = false, footer, onEdit, onDelete }: {
  comment: PageComment
  compact?: boolean
  footer?: ReactNode
  onEdit: (id: string, content: string) => Promise<void>
  onDelete: (id: string) => Promise<void>
}) {
  const [editing, setEditing] = useState(false)
  const [confirmingDelete, setConfirmingDelete] = useState(false)
  const [value, setValue] = useState(comment.content)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => { if (!editing) setValue(comment.content) }, [comment.content, editing])

  const save = async () => {
    const content = value.trim()
    if (!content || saving || content === comment.content) {
      if (content === comment.content) setEditing(false)
      return
    }
    setSaving(true)
    setError('')
    try {
      await onEdit(comment.id, content)
      setEditing(false)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '评论修改失败')
    } finally {
      setSaving(false)
    }
  }

  const remove = async () => {
    if (saving) return
    setSaving(true)
    setError('')
    try {
      await onDelete(comment.id)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '评论删除失败')
      setSaving(false)
    }
  }

  if (editing) {
    return (
      <div className="mt-1.5 rounded-lg border border-indigo-200 bg-white p-2">
        <textarea autoFocus value={value} onChange={(event) => setValue(event.target.value)} onKeyDown={(event) => { if ((event.metaKey || event.ctrlKey) && event.key === 'Enter') void save() }} rows={compact ? 2 : 3} maxLength={2000} className="w-full resize-none bg-transparent text-[13px] leading-5 text-slate-700 outline-none" />
        {error && <div className="mb-1 text-[11px] text-red-600">{error}</div>}
        <div className="flex justify-end gap-1.5">
          <button type="button" onClick={() => { setEditing(false); setValue(comment.content); setError('') }} className="rounded px-2 py-1 text-[11px] text-slate-500 hover:bg-slate-100">取消</button>
          <button type="button" onClick={() => void save()} disabled={!value.trim() || saving} className="rounded-md bg-indigo-600 px-2.5 py-1 text-[11px] font-medium text-white disabled:bg-slate-300">{saving ? '保存中…' : '保存'}</button>
        </div>
      </div>
    )
  }

  return (
    <>
      <p className={`whitespace-pre-wrap break-words text-slate-700 ${compact ? 'mt-0.5 text-[12px] leading-[18px]' : 'mt-1 text-[13px] leading-5'}`}>{comment.content}</p>
      <div className="mt-1.5 flex min-h-5 items-center gap-2 text-[11px]">
        {footer}
        <button type="button" onClick={() => { setEditing(true); setConfirmingDelete(false) }} className="font-medium text-slate-400 hover:text-indigo-600">编辑</button>
        {confirmingDelete ? (
          <>
            <button type="button" onClick={() => void remove()} disabled={saving} className="font-medium text-red-600 hover:text-red-700">{saving ? '删除中…' : '确认删除'}</button>
            <button type="button" onClick={() => setConfirmingDelete(false)} className="text-slate-400 hover:text-slate-600">取消</button>
          </>
        ) : <button type="button" onClick={() => setConfirmingDelete(true)} className="font-medium text-slate-400 hover:text-red-600">删除</button>}
      </div>
      {error && <div className="mt-1 text-[11px] text-red-600">{error}</div>}
    </>
  )
}

function ReplyComposer({ comment, quarter, week, onCreated, onCancel }: { comment: PageComment; quarter: string; week: string; onCreated: (comment: PageComment) => void; onCancel: () => void }) {
  const [value, setValue] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const inputRef = useRef<HTMLTextAreaElement>(null)
  useEffect(() => inputRef.current?.focus(), [])

  const submit = async () => {
    const content = value.trim()
    if (!content || saving) return
    setSaving(true)
    setError('')
    try {
      onCreated(await createComment({ quarter, week, parentId: comment.id, content }))
      onCancel()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '回复失败')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="mt-2 rounded-lg border border-indigo-100 bg-indigo-50/40 p-2">
      <textarea ref={inputRef} value={value} onChange={(event) => setValue(event.target.value)} onKeyDown={(event) => { if ((event.metaKey || event.ctrlKey) && event.key === 'Enter') void submit() }} placeholder={`回复 ${comment.authorName}`} rows={2} maxLength={2000} className="w-full resize-none bg-transparent text-[13px] leading-5 text-slate-700 outline-none placeholder:text-slate-400" />
      {error && <div className="mb-1 text-[11px] text-red-600">{error}</div>}
      <div className="flex items-center justify-end gap-1.5">
        <button type="button" onClick={onCancel} className="rounded px-2 py-1 text-[11px] text-slate-500 hover:bg-white">取消</button>
        <button type="button" onClick={() => void submit()} disabled={!value.trim() || saving} className="rounded-md bg-indigo-600 px-2.5 py-1 text-[11px] font-medium text-white hover:bg-indigo-700 disabled:cursor-not-allowed disabled:bg-slate-300">{saving ? '回复中…' : '回复'}</button>
      </div>
    </div>
  )
}

function CommentThread({ comment, quarter, week, showTarget, meetingMode, canComment, onSignIn, onReply, onEdit, onPatch, onDelete }: {
  comment: PageComment
  quarter: string
  week: string
  showTarget: boolean
  meetingMode: boolean
  canComment: boolean
  onSignIn: () => void
  onReply: (rootId: string, reply: PageComment) => void
  onEdit: (id: string, content: string) => Promise<void>
  onPatch: (id: string, patch: { todo?: boolean; resolved?: boolean }) => Promise<void>
  onDelete: (id: string) => Promise<void>
}) {
  const [replying, setReplying] = useState(false)
  const [actionError, setActionError] = useState('')
  const [actionSaving, setActionSaving] = useState(false)
  const patch = async (value: { todo?: boolean; resolved?: boolean }) => {
    if (actionSaving) return
    setActionSaving(true)
    setActionError('')
    try {
      await onPatch(comment.id, value)
    } catch (reason) {
      setActionError(reason instanceof Error ? reason.message : '评论状态更新失败')
    } finally {
      setActionSaving(false)
    }
  }
  return (
    <article className={`border-b border-slate-100 px-4 py-3.5 last:border-b-0 ${comment.resolved ? 'bg-slate-50/60' : ''}`}>
      {showTarget && comment.targetTitle && (
        <div className="mb-2 flex items-start gap-1.5 rounded-md bg-slate-50 px-2 py-1.5 text-[11px] text-slate-500">
          <span className="shrink-0 font-medium text-slate-400">{targetLabel(comment.targetType)}</span>
          <span className="line-clamp-2 min-w-0">{comment.targetTitle}</span>
        </div>
      )}
      {comment.selectedText && (
        <blockquote className="mb-2 rounded-r-md border-l-2 border-indigo-300 bg-indigo-50/60 px-2 py-1.5 text-[11px] leading-[17px] text-slate-600">
          “{comment.selectedText}”
        </blockquote>
      )}
      <div className="flex items-start gap-2.5">
        <Avatar name={comment.authorName} />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="text-[12px] font-semibold text-slate-700">{comment.authorName}</span>
            <time className="text-[10px] text-slate-400">{displayTime(comment.createdAt)}</time>
            {wasEdited(comment) && <span className="text-[9px] text-slate-300">已编辑</span>}
            {comment.todo && <span className="rounded-full bg-amber-100 px-1.5 py-0.5 text-[9px] font-semibold text-amber-700">To do</span>}
            {comment.resolved && <span className="rounded-full bg-emerald-50 px-1.5 py-0.5 text-[9px] font-semibold text-emerald-600">已解决</span>}
            <span className="min-w-1 flex-1" />
            {meetingMode && <button type="button" disabled={actionSaving} aria-pressed={comment.todo} onClick={() => void patch({ todo: !comment.todo })} className={`rounded-md border px-1.5 py-0.5 text-[9px] font-medium disabled:opacity-50 ${comment.todo ? 'border-amber-300 bg-amber-50 text-amber-700' : 'border-slate-200 text-slate-400 hover:border-amber-200 hover:text-amber-700'}`}>{comment.todo ? '取消 To do' : '标记 To do'}</button>}
            <button type="button" disabled={actionSaving} onClick={() => void patch({ resolved: !comment.resolved })} className={`rounded-md border px-1.5 py-0.5 text-[9px] font-medium disabled:opacity-50 ${comment.resolved ? 'border-slate-200 text-slate-500' : 'border-indigo-200 bg-indigo-50 text-indigo-600'}`}>{comment.resolved ? '重新打开' : '标记解决'}</button>
          </div>
          <EditableCommentBody comment={comment} onEdit={onEdit} onDelete={onDelete} footer={<button type="button" onClick={() => canComment ? setReplying((value) => !value) : onSignIn()} className="font-medium text-slate-400 hover:text-indigo-600">回复{comment.replies.length > 0 ? ` · ${comment.replies.length}` : ''}</button>} />
          {actionError && <div className="mt-1 text-[10px] text-red-600">{actionError}</div>}

          {comment.replies.length > 0 && (
            <div className="mt-2 space-y-2.5 border-l-2 border-slate-100 pl-3">
              {comment.replies.map((reply) => (
                <div key={reply.id} className="flex items-start gap-2">
                  <Avatar name={reply.authorName} small />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2"><span className="text-[11px] font-semibold text-slate-600">{reply.authorName}</span><time className="text-[10px] text-slate-400">{displayTime(reply.createdAt)}</time>{wasEdited(reply) && <span className="text-[9px] text-slate-300">已编辑</span>}</div>
                    <EditableCommentBody comment={reply} compact onEdit={onEdit} onDelete={onDelete} />
                  </div>
                </div>
              ))}
            </div>
          )}

          {replying && canComment && <ReplyComposer comment={comment} quarter={quarter} week={week} onCreated={(reply) => onReply(comment.id, reply)} onCancel={() => setReplying(false)} />}
        </div>
      </div>
    </article>
  )
}

interface CommentDrawerProps {
  open: boolean
  quarter: string
  week: string
  target?: CommentTarget
  meetingMode: boolean
  canComment: boolean
  onSignIn: () => void
  onShowAll: () => void
  onClose: () => void
  onCountChange: (count: number) => void
  onCountsChange: (counts: Record<string, number>) => void
  onCommentsChange: (comments: PageComment[]) => void
}

export function CommentDrawer({ open, quarter, week, target, meetingMode, canComment, onSignIn, onShowAll, onClose, onCountChange, onCountsChange, onCommentsChange }: CommentDrawerProps) {
  const [comments, setComments] = useState<PageComment[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [draft, setDraft] = useState('')
  const [error, setError] = useState('')

  const publishSummary = useCallback((next: PageComment[]) => {
    onCountChange(commentMessageCount(next))
    onCountsChange(commentCountsByTarget(next))
    onCommentsChange(next)
  }, [onCommentsChange, onCountChange, onCountsChange])

  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const value = await getComments(quarter, week)
      setComments(value.comments)
      publishSummary(value.comments)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '评论加载失败')
    } finally {
      setLoading(false)
    }
  }, [publishSummary, quarter, week])

  useEffect(() => { void load() }, [load])
  useEffect(() => { setDraft('') }, [target?.id, target?.selection?.end, target?.selection?.start, target?.selection?.text, target?.type])

  useEffect(() => {
	if (!loading) publishSummary(comments)
  }, [comments, loading, publishSummary])

  const visibleComments = useMemo(() => {
    if (!target) return comments
    return comments.filter((comment) => commentMatchesTarget(comment, target))
  }, [comments, target])
  const visibleCount = commentMessageCount(visibleComments)

  const addRoot = async () => {
    const content = draft.trim()
    if (!content || saving) return
    setSaving(true)
    setError('')
    const activeTarget = target ?? { type: 'page' as const, id: `${quarter}:${week}`, title: `${week} OKR 页面` }
    try {
      const created = await createComment({
        quarter,
        week,
        content,
        targetType: activeTarget.type,
        targetId: activeTarget.id,
        targetTitle: activeTarget.title,
        selectedText: activeTarget.selection?.text,
        selectionStart: activeTarget.selection?.start,
        selectionEnd: activeTarget.selection?.end,
        selectionPrefix: activeTarget.selection?.prefix,
        selectionSuffix: activeTarget.selection?.suffix,
      })
      setComments((current) => [...current, created])
      setDraft('')
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '评论发布失败')
    } finally {
      setSaving(false)
    }
  }

  const addReply = (rootId: string, reply: PageComment) => {
    setComments((current) => current.map((comment) => comment.id === rootId ? { ...comment, replies: [...comment.replies, reply] } : comment))
  }

  const editExistingComment = async (id: string, content: string) => {
    const updated = await updateComment(id, { content })
    setComments((current) => current.map((comment) => {
      if (comment.id === id) return { ...updated, replies: comment.replies }
      return { ...comment, replies: comment.replies.map((reply) => reply.id === id ? { ...updated, replies: [] } : reply) }
    }))
  }

  const patchExistingComment = async (id: string, patch: { todo?: boolean; resolved?: boolean }) => {
    const updated = await updateComment(id, patch)
    setComments((current) => current.map((comment) => {
      if (comment.id === id) return { ...updated, replies: comment.replies }
      return { ...comment, replies: comment.replies.map((reply) => reply.id === id ? { ...updated, replies: [] } : reply) }
    }))
  }

  const removeExistingComment = async (id: string) => {
    await deleteComment(id)
    setComments((current) => current
      .filter((comment) => comment.id !== id)
      .map((comment) => ({ ...comment, replies: comment.replies.filter((reply) => reply.id !== id) })))
  }

  return (
    <>
      {open && <button type="button" aria-label="关闭评论" onClick={onClose} className="fixed inset-0 z-40 bg-slate-900/20 sm:hidden" />}
      <aside aria-hidden={!open} className={`fixed inset-y-0 right-0 z-50 flex w-full max-w-[400px] flex-col border-l border-slate-200 bg-white shadow-[-12px_0_32px_rgba(15,23,42,0.10)] transition-transform duration-200 ${open ? 'translate-x-0' : 'translate-x-full'}`}>
        <header className="flex h-14 shrink-0 items-center gap-2 border-b border-slate-200 px-4">
          <div className="min-w-0">
            <h2 className="truncate text-[14px] font-semibold text-slate-800">{target ? `${targetLabel(target.type)}评论` : '全部评论'}</h2>
            <p className="text-[10px] text-slate-400">{week} · {visibleCount} 条讨论</p>
          </div>
          {target && <button type="button" onClick={onShowAll} className="ml-auto rounded-md px-2 py-1 text-[11px] text-indigo-600 hover:bg-indigo-50">查看全部</button>}
          {!target && <button type="button" onClick={() => void load()} className="ml-auto rounded-md px-2 py-1 text-[11px] text-slate-400 hover:bg-slate-100 hover:text-slate-600">刷新</button>}
          <button type="button" onClick={onClose} aria-label="关闭评论" className="flex size-8 items-center justify-center rounded-lg text-xl text-slate-400 hover:bg-slate-100 hover:text-slate-700">×</button>
        </header>

        <div className="shrink-0 border-b border-slate-100 bg-slate-50/60 p-3">
          {target && (
            <div className="mb-2 rounded-lg border border-indigo-100 bg-indigo-50/60 px-2.5 py-2">
              <div className="text-[10px] font-semibold text-indigo-500">正在评论 · {target.selection ? '选中文字' : targetLabel(target.type)}</div>
              {target.selection ? (
                <blockquote className="mt-1 border-l-2 border-indigo-300 pl-2 text-[12px] font-medium leading-[18px] text-slate-700">“{target.selection.text}”</blockquote>
              ) : <div className="mt-0.5 line-clamp-3 text-[12px] leading-[18px] text-slate-700">{target.title}</div>}
            </div>
          )}
          {canComment ? <div className="rounded-xl border border-slate-200 bg-white p-2.5 shadow-[0_1px_2px_rgba(15,23,42,0.03)]">
              <textarea value={draft} onChange={(event) => setDraft(event.target.value)} onKeyDown={(event) => { if ((event.metaKey || event.ctrlKey) && event.key === 'Enter') void addRoot() }} placeholder={target ? '针对这段内容发表评论…' : '对本周页面发表评论…'} rows={3} maxLength={2000} className="w-full resize-none bg-transparent text-[13px] leading-5 text-slate-700 outline-none placeholder:text-slate-400" />
              <div className="mt-1 flex items-center gap-2">
                <span className="text-[10px] text-slate-300">⌘ + Enter 发布</span>
                <button type="button" onClick={() => void addRoot()} disabled={!draft.trim() || saving} className="ml-auto rounded-md bg-indigo-600 px-3 py-1.5 text-[11px] font-medium text-white hover:bg-indigo-700 disabled:cursor-not-allowed disabled:bg-slate-300">{saving ? '发布中…' : '发布评论'}</button>
              </div>
            </div> : <div className="rounded-xl border border-blue-100 bg-white p-3 text-center shadow-[0_1px_2px_rgba(15,23,42,0.03)]">
              <p className="text-[11px] text-slate-500">登录后使用真实身份发表评论</p>
              <button type="button" onClick={onSignIn} className="mt-2 rounded-md bg-blue-600 px-3 py-1.5 text-[11px] font-medium text-white hover:bg-blue-700">飞书登录</button>
            </div>}
          {error && <div className="mt-2 text-[11px] text-red-600">{error}</div>}
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto">
          {loading ? <div className="px-4 py-10 text-center text-xs text-slate-400">正在读取评论…</div> : visibleComments.length === 0 ? (
            <div className="px-8 py-16 text-center"><div className="mx-auto mb-3 flex size-10 items-center justify-center rounded-full bg-slate-100 text-lg text-slate-400">💬</div><p className="text-[13px] font-medium text-slate-600">{target ? '这段内容还没有评论' : '还没有评论'}</p><p className="mt-1 text-[11px] text-slate-400">提出问题、补充背景或回复讨论</p></div>
          ) : visibleComments.map((comment) => <CommentThread key={comment.id} comment={comment} quarter={quarter} week={week} showTarget={!target} meetingMode={meetingMode} canComment={canComment} onSignIn={onSignIn} onReply={addReply} onEdit={editExistingComment} onPatch={patchExistingComment} onDelete={removeExistingComment} />)}
        </div>
      </aside>
    </>
  )
}
