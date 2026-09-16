import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { ReactNode, RefObject } from 'react'
import { createComment, createPlanComment, createRegionalAlignmentComment, deleteComment, getComments, getPlanComments, getRegionalAlignmentComments, updateComment } from '../api'
import { scrollToCommentSource, scrollToCommentTarget } from '../commenting'
import { buildCommentDocumentOrder, buildCommentOKRContextIndex, commentCountsByTarget, commentMatchesTarget, commentMessageCount, commentOKRContext, commentTargetKey, groupCommentsByTarget, sortCommentsByDocumentOrder, todayCommentReviewItems } from '../comments'
import type { CommentOKRContext } from '../comments'
import type { CommentMention, CommentTarget, ImageRef, Objective, PageComment, RegionalCode } from '../types'
import { CommentContent, CommentMentionInput } from './CommentMentionInput'
import type { CommentDraft } from './CommentMentionInput'
import { CommentDeliveryStatus } from './CommentDeliveryStatus'
import { PersonAvatar } from './PersonAvatar'
import { Images, usePastedImageUpload } from './ui'

const EMPTY_FOLLOW_UP_ORDER: readonly string[] = []

export type CommentReviewMode = 'all' | 'today'

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
  return ({ page: '整页', objective: 'O', kr: 'KR', metric: '核心数据', point: '具体 KR', entry: '进展条目', follow_up: '待跟进事项', alignment_item: '对齐事项' } as const)[type]
}

function Avatar({ name, email, small = false }: { name: string; email?: string; small?: boolean }) {
  return <PersonAvatar name={name} email={email} size={small ? 'size-6 text-[10px]' : 'size-7 text-[11px]'} tone="bg-indigo-400" />
}

function wasEdited(comment: PageComment) {
  return new Date(comment.updatedAt).getTime() - new Date(comment.createdAt).getTime() > 1000
}

function notificationErrorText(comment: PageComment) {
  return comment.notificationErrors?.join('；') ?? ''
}

function CommentEditorFields({ value, images, objectives, placeholder, rows, autoFocus, inputRef, disabled = false, onChange, onImagesChange, onUploadingChange, onSubmitShortcut }: {
  value: CommentDraft
  images: ImageRef[]
  objectives: Objective[]
  placeholder: string
  rows: number
  autoFocus?: boolean
  inputRef?: RefObject<HTMLTextAreaElement | null>
  disabled?: boolean
  onChange: (value: CommentDraft) => void
  onImagesChange: (images: ImageRef[]) => void
  onUploadingChange?: (uploading: boolean) => void
  onSubmitShortcut: () => void
}) {
  const fileInputRef = useRef<HTMLInputElement>(null)
  const upload = usePastedImageUpload((uploaded) => onImagesChange([...images, ...uploaded]), disabled || images.length >= 9, 9 - images.length)
  useEffect(() => onUploadingChange?.(upload.uploading), [onUploadingChange, upload.uploading])
  const chooseFiles = (files: FileList | null) => {
    if (!files) return
    upload.upload(Array.from(files).slice(0, Math.max(0, 9 - images.length)))
  }
  return (
    <>
      <CommentMentionInput value={value} objectives={objectives} placeholder={placeholder} rows={rows} autoFocus={autoFocus} inputRef={inputRef} onPaste={upload.onPaste} onChange={onChange} onSubmitShortcut={onSubmitShortcut} />
      {(images.length > 0 || !disabled) && <div className="mt-2 flex flex-wrap items-end gap-2">
        {images.length > 0 && <Images value={images} onChange={onImagesChange} pasteEnabled={false} maxDisplayWidth={300} />}
        {!disabled && <>
          <input ref={fileInputRef} type="file" accept="image/png,image/jpeg,image/gif,image/webp" multiple className="hidden" onChange={(event) => { chooseFiles(event.currentTarget.files); event.currentTarget.value = '' }} />
          <button type="button" disabled={upload.uploading || images.length >= 9} onClick={() => fileInputRef.current?.click()} className="rounded-md border border-slate-200 bg-white px-2 py-1 text-[10px] font-medium text-slate-500 hover:border-indigo-200 hover:text-indigo-600 disabled:cursor-not-allowed disabled:opacity-40">{upload.uploading ? '上传中…' : images.length >= 9 ? '最多 9 张' : '添加图片'}</button>
          <span className="text-[10px] text-slate-300">也可直接粘贴截图</span>
        </>}
        {upload.uploadError && <span className="text-[10px] text-red-500">{upload.uploadError}{upload.canRetry && <button type="button" onClick={upload.retry} className="ml-1 underline">重试</button>}</span>}
      </div>}
    </>
  )
}

function EditableCommentBody({ comment, objectives, compact = false, footer, onEdit, onDelete }: {
  comment: PageComment
  objectives: Objective[]
  compact?: boolean
  footer?: ReactNode
  onEdit: (id: string, content: string, mentions: CommentMention[], images: ImageRef[]) => Promise<void>
  onDelete: (id: string) => Promise<void>
}) {
  const [editing, setEditing] = useState(false)
  const [confirmingDelete, setConfirmingDelete] = useState(false)
  const [value, setValue] = useState<CommentDraft>({ content: comment.content, mentions: comment.mentions })
  const [images, setImages] = useState<ImageRef[]>(comment.images ?? [])
  const [imageUploading, setImageUploading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (editing) return
    setValue({ content: comment.content, mentions: comment.mentions })
    setImages(comment.images ?? [])
  }, [comment.content, comment.images, comment.mentions, editing])

  const save = async () => {
    const content = value.content.trim()
    const mentionsUnchanged = value.mentions.length === comment.mentions.length
      && value.mentions.every((mention, index) => mention.email === comment.mentions[index]?.email && mention.name === comment.mentions[index]?.name)
    const imagesUnchanged = JSON.stringify(images) === JSON.stringify(comment.images ?? [])
    if ((!content && images.length === 0) || saving || imageUploading || (content === comment.content && mentionsUnchanged && imagesUnchanged)) {
      if (content === comment.content && mentionsUnchanged && imagesUnchanged) setEditing(false)
      return
    }
    setSaving(true)
    setError('')
    try {
      await onEdit(comment.id, content, value.mentions, images)
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
        <CommentEditorFields autoFocus value={value} images={images} objectives={objectives} onChange={setValue} onImagesChange={setImages} onUploadingChange={setImageUploading} onSubmitShortcut={() => void save()} placeholder="修改评论…" rows={compact ? 2 : 3} />
        {error && <div className="mb-1 text-[11px] text-red-600">{error}</div>}
        <div className="flex justify-end gap-1.5">
          <button type="button" onClick={() => { setEditing(false); setValue({ content: comment.content, mentions: comment.mentions }); setImages(comment.images ?? []); setError('') }} className="rounded px-2 py-1 text-[11px] text-slate-500 hover:bg-slate-100">取消</button>
          <button type="button" onClick={() => void save()} disabled={(!value.content.trim() && images.length === 0) || saving || imageUploading} className="rounded-md bg-indigo-600 px-2.5 py-1 text-[11px] font-medium text-white disabled:bg-slate-300">{saving ? '保存中…' : imageUploading ? '图片上传中…' : '保存'}</button>
        </div>
      </div>
    )
  }

  return (
    <>
      {comment.content && <p className={`whitespace-pre-wrap break-words text-slate-700 ${compact ? 'mt-0.5 text-[12px] leading-[18px]' : 'mt-1 text-[13px] leading-5'}`}><CommentContent content={comment.content} mentions={comment.mentions} /></p>}
      {(comment.images?.length ?? 0) > 0 && <div className="mt-2"><Images value={comment.images ?? []} onChange={() => undefined} readOnly maxDisplayWidth={300} /></div>}
      <CommentDeliveryStatus comment={comment} />
      {!comment.notifications?.length && notificationErrorText(comment) && <div className="mt-1 text-[10px] text-amber-700">评论已保存，但{notificationErrorText(comment)}</div>}
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

function CommentSourceCard({ source, context, onNavigate }: { source: PageComment | CommentTarget; context?: CommentOKRContext; onNavigate: () => void }) {
  const isTarget = 'type' in source
  const type = isTarget ? source.type : source.targetType
  const title = isTarget ? source.title : source.targetTitle
  const selectedText = isTarget ? source.selection?.text : source.selectedText
  const showExactTarget = Boolean(title && type !== 'objective' && type !== 'kr')
  return (
    <button type="button" title="跳转到评论原文" onClick={onNavigate} className="block w-full rounded-lg border border-indigo-100 bg-indigo-50/60 px-2.5 py-2 text-left transition-colors hover:border-indigo-200 hover:bg-indigo-100/70 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-500">
      <div className="mb-1.5 flex items-center gap-1 text-[10px] font-semibold text-indigo-500"><span>评论原文</span><span aria-hidden>↗</span></div>
      <div className="space-y-1" aria-label="对应的 O、KR 和原文">
        {context?.objective && <div className="flex min-w-0 items-start gap-1.5 text-[10px] leading-4 text-slate-600" title={context.objective.title}><span className="shrink-0 rounded border border-blue-200 bg-blue-50 px-1.5 font-semibold text-blue-700">O</span><span className="line-clamp-2 min-w-0">{context.objective.title}</span></div>}
        {context?.kr && <div className="flex min-w-0 items-start gap-1.5 text-[10px] leading-4 text-slate-600" title={context.kr.title}><span className="shrink-0 rounded border border-violet-200 bg-violet-50 px-1.5 font-semibold text-violet-700">KR</span><span className="line-clamp-2 min-w-0">{context.kr.title}</span></div>}
        {showExactTarget && <div className="flex min-w-0 items-start gap-1.5 text-[10px] leading-4 text-slate-600" title={title}><span className="shrink-0 rounded border border-slate-200 bg-white px-1.5 font-semibold text-slate-500">{targetLabel(type)}</span><span className="line-clamp-2 min-w-0">{title}</span></div>}
        {selectedText && <blockquote className="mt-1.5 border-l-2 border-indigo-300 pl-2 text-[12px] font-medium leading-[18px] text-slate-700">“{selectedText}”</blockquote>}
        {!context?.objective && !context?.kr && !showExactTarget && !selectedText && <div className="line-clamp-2 text-[11px] text-slate-600">{title || '整页评论'}</div>}
      </div>
    </button>
  )
}

function ReplyComposer({ comment, objectives, createReply, onCreated, onCancel }: { comment: PageComment; objectives: Objective[]; createReply: (parentId: string, content: string, mentions: CommentMention[], images: ImageRef[]) => Promise<PageComment>; onCreated: (comment: PageComment) => void; onCancel: () => void }) {
  const [value, setValue] = useState<CommentDraft>({ content: '', mentions: [] })
  const [images, setImages] = useState<ImageRef[]>([])
  const [imageUploading, setImageUploading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const inputRef = useRef<HTMLTextAreaElement>(null)
  useEffect(() => inputRef.current?.focus(), [])

  const submit = async () => {
    const content = value.content.trim()
    if ((!content && images.length === 0) || saving || imageUploading) return
    setSaving(true)
    setError('')
    try {
      const created = await createReply(comment.id, content, value.mentions, images)
      onCreated(created)
      if (notificationErrorText(created)) setError(`回复已保存，但${notificationErrorText(created)}`)
      onCancel()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '回复失败')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="mt-2 rounded-lg border border-indigo-100 bg-indigo-50/40 p-2">
      <CommentEditorFields inputRef={inputRef} value={value} images={images} objectives={objectives} onChange={setValue} onImagesChange={setImages} onUploadingChange={setImageUploading} onSubmitShortcut={() => void submit()} placeholder={`回复 ${comment.authorName}，输入 @ 选择提醒人`} rows={2} />
      {error && <div className="mb-1 text-[11px] text-red-600">{error}</div>}
      <div className="flex items-center justify-end gap-1.5">
        <button type="button" onClick={onCancel} className="rounded px-2 py-1 text-[11px] text-slate-500 hover:bg-white">取消</button>
        <button type="button" onClick={() => void submit()} disabled={(!value.content.trim() && images.length === 0) || saving || imageUploading} className="rounded-md bg-indigo-600 px-2.5 py-1 text-[11px] font-medium text-white hover:bg-indigo-700 disabled:cursor-not-allowed disabled:bg-slate-300">{saving ? '回复中…' : imageUploading ? '图片上传中…' : '回复'}</button>
      </div>
    </div>
  )
}

function CommentThread({ comment, objectives, createReply, showSource, okrContext, todoEnabled, focusCommentId, markFocusAsToday = false, onNavigateToSource, onReply, onEdit, onPatch, onDelete }: {
  comment: PageComment
  objectives: Objective[]
  createReply: (parentId: string, content: string, mentions: CommentMention[], images: ImageRef[]) => Promise<PageComment>
  showSource: boolean
  okrContext?: CommentOKRContext
  todoEnabled: boolean
  focusCommentId?: string
  markFocusAsToday?: boolean
  onNavigateToSource: (comment: PageComment) => void
  onReply: (rootId: string, reply: PageComment) => void
  onEdit: (id: string, content: string, mentions: CommentMention[], images: ImageRef[]) => Promise<void>
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
  const rootFocused = focusCommentId === comment.id
  return (
    <article id={`comment-${comment.id}`} aria-current={rootFocused ? 'true' : undefined} className={`border-b border-slate-100 px-4 py-3.5 last:border-b-0 ${comment.resolved ? 'bg-slate-50/60' : ''} ${rootFocused ? 'bg-indigo-50/70 ring-2 ring-inset ring-indigo-300' : ''}`}>
      {showSource && <div className="mb-2.5"><CommentSourceCard source={comment} context={okrContext} onNavigate={() => onNavigateToSource(comment)} /></div>}
      <div className="flex items-start gap-2.5">
        <Avatar name={comment.authorName} />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="text-[12px] font-semibold text-slate-700">{comment.authorName}</span>
            <time className="text-[10px] text-slate-400">{displayTime(comment.createdAt)}</time>
            {wasEdited(comment) && <span className="text-[9px] text-slate-300">已编辑</span>}
            {rootFocused && markFocusAsToday && <span className="rounded-full bg-indigo-100 px-1.5 py-0.5 text-[9px] font-semibold text-indigo-700">今日新增</span>}
            {comment.todo && <span className="rounded-full bg-amber-100 px-1.5 py-0.5 text-[9px] font-semibold text-amber-700">To do</span>}
            {comment.resolved && <span className="rounded-full bg-emerald-50 px-1.5 py-0.5 text-[9px] font-semibold text-emerald-600">已解决</span>}
            <div className="ml-auto flex shrink-0 items-center gap-1 whitespace-nowrap text-[10px] font-medium leading-[14px]">
              {todoEnabled && <button type="button" disabled={actionSaving} aria-pressed={comment.todo} onClick={() => void patch({ todo: !comment.todo })} className={`rounded-md border px-1.5 py-0.5 disabled:opacity-50 ${comment.todo ? 'border-amber-300 bg-amber-50 text-amber-700' : 'border-slate-200 text-slate-400 hover:border-amber-200 hover:text-amber-700'}`}>{comment.todo ? '取消 To do' : '标记 To do'}</button>}
              <button type="button" disabled={actionSaving} onClick={() => void patch({ resolved: !comment.resolved })} className={`rounded-md border px-1.5 py-0.5 disabled:opacity-50 ${comment.resolved ? 'border-slate-200 text-slate-500' : 'border-indigo-200 bg-indigo-50 text-indigo-600'}`}>{comment.resolved ? '重新打开' : '标记解决'}</button>
            </div>
          </div>
          <EditableCommentBody comment={comment} objectives={objectives} onEdit={onEdit} onDelete={onDelete} footer={<button type="button" onClick={() => setReplying((value) => !value)} className="font-medium text-slate-400 hover:text-indigo-600">回复{comment.replies.length > 0 ? ` · ${comment.replies.length}` : ''}</button>} />
          {actionError && <div className="mt-1 text-[10px] text-red-600">{actionError}</div>}

          {comment.replies.length > 0 && (
            <div className="mt-2 space-y-2.5 border-l-2 border-slate-100 pl-3">
              {comment.replies.map((reply) => (
                <div id={`comment-${reply.id}`} key={reply.id} aria-current={focusCommentId === reply.id ? 'true' : undefined} className={`flex items-start gap-2 rounded-lg ${focusCommentId === reply.id ? 'bg-indigo-50 px-2 py-1.5 ring-2 ring-indigo-300' : ''}`}>
                  <Avatar name={reply.authorName} small />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2"><span className="text-[11px] font-semibold text-slate-600">{reply.authorName}</span><time className="text-[10px] text-slate-400">{displayTime(reply.createdAt)}</time>{wasEdited(reply) && <span className="text-[9px] text-slate-300">已编辑</span>}{focusCommentId === reply.id && markFocusAsToday && <span className="rounded-full bg-indigo-100 px-1.5 py-0.5 text-[9px] font-semibold text-indigo-700">今日新增</span>}</div>
                    <EditableCommentBody comment={reply} objectives={objectives} compact onEdit={onEdit} onDelete={onDelete} />
                  </div>
                </div>
              ))}
            </div>
          )}

          {replying && <ReplyComposer comment={comment} objectives={objectives} createReply={createReply} onCreated={(reply) => onReply(comment.id, reply)} onCancel={() => setReplying(false)} />}
        </div>
      </div>
    </article>
  )
}

interface CommentDrawerProps {
  open: boolean
  reviewEnabled?: boolean
  reviewMode?: CommentReviewMode
  quarter: string
  week?: string
  planId?: string
  alignmentId?: string
  alignmentRegion?: RegionalCode
  sourceTab: string
  scopeLabel?: string
  objectives: Objective[]
  followUpOrder?: readonly string[]
  target?: CommentTarget
  focusCommentId?: string
  todoEnabled?: boolean
  onStartReview: (mode: CommentReviewMode) => void
  onShowAll: () => void
  onClose: () => void
  onFocusCommentChange: (comment?: PageComment) => void
  onCountChange: (count: number) => void
  onCountsChange: (counts: Record<string, number>) => void
  onCommentsChange: (comments: PageComment[]) => void
}

export function CommentDrawer({ open, reviewEnabled = false, reviewMode, quarter, week = '', planId, alignmentId, alignmentRegion, sourceTab, scopeLabel, objectives, followUpOrder = EMPTY_FOLLOW_UP_ORDER, target, focusCommentId, todoEnabled = false, onStartReview, onShowAll, onClose, onFocusCommentChange, onCountChange, onCountsChange, onCommentsChange }: CommentDrawerProps) {
  const [comments, setComments] = useState<PageComment[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [draft, setDraft] = useState<CommentDraft>({ content: '', mentions: [] })
  const [draftImages, setDraftImages] = useState<ImageRef[]>([])
  const [draftImageUploading, setDraftImageUploading] = useState(false)
  const [error, setError] = useState('')
  const [navigationNotice, setNavigationNotice] = useState('')
  const [reviewItemId, setReviewItemId] = useState('')
  const [showHistory, setShowHistory] = useState(false)
  const [reviewDate, setReviewDate] = useState(() => new Date())
  const revealedFocusId = useRef('')
  const loadVersion = useRef(0)
  const inFlightLoadVersion = useRef<number | null>(null)
  const mergeStaleLoadVersion = useRef<number | null>(null)
  const [submittedId, setSubmittedId] = useState('')
  const scopeKey = `${alignmentRegion ?? ''}:${alignmentId ?? ''}:${planId ?? ''}:${quarter}:${week}`
  const scopeRef = useRef(scopeKey)
  scopeRef.current = scopeKey


  const publishSummary = useCallback((next: PageComment[]) => {
    const active = next.filter((comment) => !comment.resolved)
    onCountChange(commentMessageCount(active))
    onCountsChange(commentCountsByTarget(active))
    onCommentsChange(active)
  }, [onCommentsChange, onCountChange, onCountsChange])

  const summaryRef = useRef(publishSummary)
  summaryRef.current = publishSummary

  const load = useCallback(async () => {
    const version = ++loadVersion.current
    inFlightLoadVersion.current = version
    mergeStaleLoadVersion.current = null
    setLoading(true)
    setError('')
    try {
      const value = alignmentRegion ? await getRegionalAlignmentComments(quarter, alignmentRegion) : planId ? await getPlanComments(planId) : await getComments(quarter, week)
      if (version !== loadVersion.current) {
        // A comment can be saved while the first list is loading. Keep the
        // freshly saved comment, then add the older threads from that list.
        if (mergeStaleLoadVersion.current === version && scopeRef.current === scopeKey) {
          mergeStaleLoadVersion.current = null
          setComments(current => {
            const saved = new Set(current.map(comment => comment.id))
            return [...value.comments.filter(comment => !saved.has(comment.id)), ...current]
          })
        }
        return
      }
      setComments(value.comments)
      summaryRef.current(value.comments)
    } catch (reason) {
      if (version !== loadVersion.current) return
      setError(reason instanceof Error ? reason.message : '评论加载失败')
    } finally {
      if (inFlightLoadVersion.current === version) inFlightLoadVersion.current = null
      if (version === loadVersion.current) setLoading(false)
    }
  }, [alignmentRegion, planId, quarter, scopeKey, week])

  useEffect(() => {
    setComments([])
    summaryRef.current([])
    setSubmittedId('')
    void load()
    return () => { loadVersion.current++ }
  }, [load])
  useEffect(() => {
    setDraft({ content: '', mentions: [] })
    setDraftImages([])
  }, [target?.id, target?.selection?.end, target?.selection?.start, target?.selection?.text, target?.type])
  useEffect(() => {
    setShowHistory(false)
    revealedFocusId.current = ''
  }, [scopeKey])

  useEffect(() => {
    if (!open) return
    let timer: number
    const updateDay = () => {
      window.clearTimeout(timer)
      const now = new Date()
      setReviewDate(now)
      const midnight = new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1)
      timer = window.setTimeout(updateDay, midnight.getTime() - now.getTime() + 10)
    }
    updateDay()
    window.addEventListener('focus', updateDay)
    return () => {
      window.clearTimeout(timer)
      window.removeEventListener('focus', updateDay)
    }
  }, [open])

  useEffect(() => {
    if (!loading) publishSummary(comments)
  }, [comments, loading, publishSummary])

  useEffect(() => {
    if (!open) {
      setShowHistory(false)
      revealedFocusId.current = ''
      return
    }
    const requestedId = target?.commentId || focusCommentId
    if (!requestedId || revealedFocusId.current === requestedId) return
    const thread = comments.find((comment) => comment.id === requestedId || comment.replies.some((reply) => reply.id === requestedId))
    if (!thread) return
    revealedFocusId.current = requestedId
    if (thread.resolved) setShowHistory(true)
  }, [comments, focusCommentId, open, target?.commentId])

  useEffect(() => {
    if (!open || loading || !focusCommentId) return
    const frame = window.requestAnimationFrame(() => {
      const element = document.getElementById(`comment-${focusCommentId}`)
      element?.scrollIntoView({ block: 'center' })
      element?.classList.add('ring-2', 'ring-inset', 'ring-indigo-300')
    })
    return () => window.cancelAnimationFrame(frame)
  }, [focusCommentId, loading, open, showHistory])

  useEffect(() => {
    if (!open || !submittedId || loading) return
    const frame = requestAnimationFrame(() => document.getElementById(`comment-${submittedId}`)?.scrollIntoView({ block: 'nearest', behavior: 'smooth' }))
    return () => cancelAnimationFrame(frame)
  }, [submittedId, loading, open])

  useEffect(() => {
    const pending = comments.some(c => [c, ...c.replies].some(x => x.notifications?.some(n => n.status === 'pending' || n.status === 'sending')))
    if (!open || loading || saving || !pending) return
    const version = loadVersion.current
    const timer = window.setTimeout(async () => {
      try {
        const next = alignmentRegion ? await getRegionalAlignmentComments(quarter, alignmentRegion) : planId ? await getPlanComments(planId) : await getComments(quarter, week)
        if (loadVersion.current !== version) return
        setComments(current => current.map(c => {
          const fresh = next.comments.find(x => x.id === c.id)
          return fresh ? { ...c, notifications: fresh.notifications, replies: c.replies.map(r => ({ ...r, notifications: fresh.replies.find(x => x.id === r.id)?.notifications ?? r.notifications })) } : c
        }))
      } catch { /* A failed status refresh must not erase a saved comment. */ }
    }, 2000)
    return () => window.clearTimeout(timer)
  }, [alignmentRegion, comments, loading, open, planId, quarter, saving, week])

  const documentOrder = useMemo(() => buildCommentDocumentOrder(objectives, followUpOrder), [followUpOrder, objectives])
  const scopedComments = useMemo(() => target ? comments.filter((comment) => commentMatchesTarget(comment, target)) : comments, [comments, target])
  const resolvedCount = scopedComments.filter((comment) => comment.resolved).length
  const visibleComments = useMemo(() => sortCommentsByDocumentOrder(
    showHistory ? scopedComments : scopedComments.filter((comment) => !comment.resolved),
    documentOrder,
  ), [documentOrder, scopedComments, showHistory])
  const visibleCount = commentMessageCount(visibleComments)
  const okrContextIndex = useMemo(() => buildCommentOKRContextIndex(objectives), [objectives])
  const visibleGroups = useMemo(() => groupCommentsByTarget(visibleComments), [visibleComments])
  const reviewComments = useMemo(() => sortCommentsByDocumentOrder(
    showHistory ? comments : comments.filter((comment) => !comment.resolved),
    documentOrder,
  ), [comments, documentOrder, showHistory])
  const allReviewItems = useMemo(() => reviewComments.map((comment) => ({ thread: comment, comment })), [reviewComments])
  const todayReviewItems = useMemo(() => todayCommentReviewItems(comments, documentOrder, reviewDate), [comments, documentOrder, reviewDate])
  const reviewItems = reviewMode === 'today' ? todayReviewItems : allReviewItems
  const reviewIndex = reviewItems.findIndex((item) => item.comment.id === reviewItemId)
  const reviewItem = reviewIndex >= 0 ? reviewItems[reviewIndex] : undefined
  const targetContext = target ? okrContextIndex[commentTargetKey(target)] : undefined

  useEffect(() => {
    if (!reviewMode || !open) {
      setReviewItemId('')
      return
    }
    if (loading || reviewItems.length === 0) return
    setReviewItemId((current) => reviewItems.some((item) => item.comment.id === current) ? current : reviewItems[0].comment.id)
  }, [loading, open, reviewItems, reviewMode])

  useEffect(() => {
    onFocusCommentChange(open && reviewMode ? reviewItem?.comment : undefined)
  }, [onFocusCommentChange, open, reviewItem, reviewMode])

  useEffect(() => {
    if (!open || !reviewMode || !reviewItem) return
    const frame = window.requestAnimationFrame(() => document.getElementById(`comment-${reviewItem.comment.id}`)?.scrollIntoView({ block: 'center' }))
    return () => window.cancelAnimationFrame(frame)
  }, [open, reviewItem, reviewMode])

  const addRoot = async () => {
    const content = draft.content.trim()
    if ((!content && draftImages.length === 0) || saving || draftImageUploading) return
    setSaving(true)
    setError('')
    const submittedScope = scopeKey
    const activeTarget = target ?? (alignmentId
      ? { type: 'page' as const, id: alignmentId, title: scopeLabel || '区域 OKR 对齐' }
      : planId
      ? { type: 'page' as const, id: planId, title: scopeLabel || 'Biz OKR Plan' }
      : { type: 'page' as const, id: `${quarter}:${week}`, title: `${week} OKR 页面` })
    try {
      const input = {
        content,
        mentions: draft.mentions,
        images: draftImages,
        targetType: activeTarget.type,
        targetId: activeTarget.id,
        targetTitle: activeTarget.title,
        selectedText: activeTarget.selection?.text,
        selectionStart: activeTarget.selection?.start,
        selectionEnd: activeTarget.selection?.end,
        selectionPrefix: activeTarget.selection?.prefix,
        selectionSuffix: activeTarget.selection?.suffix,
      }
      const created = alignmentRegion ? await createRegionalAlignmentComment(quarter, alignmentRegion, input) : planId ? await createPlanComment(planId, input) : await createComment({ quarter, week, sourceTab, ...input })
      if (scopeRef.current !== submittedScope) return
      if (inFlightLoadVersion.current === loadVersion.current) mergeStaleLoadVersion.current = loadVersion.current
      loadVersion.current++
      setLoading(false)
      setComments((current) => [...current.filter(item => item.id !== created.id), created])
      setSubmittedId(created.id)
      if (reviewMode) setReviewItemId(created.id)
      setDraft({ content: '', mentions: [] })
      setDraftImages([])
      if (notificationErrorText(created)) setError(`评论已保存，但${notificationErrorText(created)}`)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '评论发布失败')
    } finally {
      setSaving(false)
    }
  }

  const createReply = useCallback((parentId: string, content: string, mentions: CommentMention[], images: ImageRef[]) => (
    alignmentRegion ? createRegionalAlignmentComment(quarter, alignmentRegion, { parentId, content, mentions, images }) : planId ? createPlanComment(planId, { parentId, content, mentions, images }) : createComment({ quarter, week, sourceTab, parentId, content, mentions, images })
  ), [alignmentRegion, planId, quarter, sourceTab, week])

  const addReply = (rootId: string, reply: PageComment) => {
    loadVersion.current++
    setLoading(false)
    setSubmittedId(reply.id)
    setComments((current) => current.map((comment) => comment.id === rootId ? { ...comment, replies: [...comment.replies, reply] } : comment))
  }

  const currentComment = (id: string) => {
    for (const comment of comments) {
      if (comment.id === id) return comment
      const reply = comment.replies.find((candidate) => candidate.id === id)
      if (reply) return reply
    }
    throw new Error('评论已变化，请刷新后重试')
  }

  const editExistingComment = async (id: string, content: string, mentions: CommentMention[], images: ImageRef[]) => {
    const updated = await updateComment(currentComment(id), { content, mentions, images })
    setComments((current) => current.map((comment) => {
      if (comment.id === id) return { ...updated, replies: comment.replies }
      return { ...comment, replies: comment.replies.map((reply) => reply.id === id ? { ...updated, replies: [] } : reply) }
    }))
  }

  const patchExistingComment = async (id: string, patch: { todo?: boolean; resolved?: boolean }) => {
    const updated = await updateComment(currentComment(id), patch)
    setComments((current) => current.map((comment) => {
      if (comment.id === id) return { ...updated, replies: comment.replies }
      return { ...comment, replies: comment.replies.map((reply) => reply.id === id ? { ...updated, replies: [] } : reply) }
    }))
  }

  const removeExistingComment = async (id: string) => {
    await deleteComment(currentComment(id))
    setComments((current) => current
      .filter((comment) => comment.id !== id)
      .map((comment) => ({ ...comment, replies: comment.replies.filter((reply) => reply.id !== id) })))
  }

  const navigateToCommentSource = (comment: PageComment) => {
    setNavigationNotice('')
    onFocusCommentChange(comment)
    window.setTimeout(() => {
      const result = scrollToCommentSource(comment)
      if (result === 'target' && comment.selectedText) setNavigationNotice('选中的原文已变化，已定位到所属内容')
      if (result === 'missing') setNavigationNotice('原文已删除，或不在当前页面中')
    }, 180)
  }

  const navigateToTargetSource = () => {
    if (!target) return
    const preferred = visibleComments.find((comment) => comment.id === target.commentId) ?? visibleComments[0]
    if (preferred) {
      navigateToCommentSource(preferred)
      return
    }
    setNavigationNotice('')
    const result = scrollToCommentTarget(target)
    if (result === 'missing') setNavigationNotice('原文已删除，或不在当前页面中')
  }

  const startReview = (mode: CommentReviewMode) => {
    // Preserve the comment that opened the drawer as the initial review item.
    // If it is not eligible for the selected mode (for example, an older
    // comment in today's review), the review-items effect selects the first
    // eligible item instead.
    const requestedId = target?.commentId || focusCommentId || ''
    const requestedThread = comments.find((comment) => comment.id === requestedId || comment.replies.some((reply) => reply.id === requestedId))
    setReviewItemId(mode === 'all' ? requestedThread?.id ?? requestedId : requestedId)
    onStartReview(mode)
  }

  const renderThread = (comment: PageComment, showSource: boolean, activeCommentId?: string, markActiveAsToday = false) => (
    <CommentThread
      key={comment.id}
      comment={comment}
      objectives={objectives}
      createReply={createReply}
      showSource={showSource}
      okrContext={commentOKRContext(comment, okrContextIndex)}
      todoEnabled={todoEnabled}
      focusCommentId={activeCommentId}
      markFocusAsToday={markActiveAsToday}
      onNavigateToSource={navigateToCommentSource}
      onReply={addReply}
      onEdit={editExistingComment}
      onPatch={patchExistingComment}
      onDelete={removeExistingComment}
    />
  )

  return (
    <>
      {open && <button type="button" aria-label="关闭评论" onClick={onClose} className="fixed inset-0 z-40 bg-slate-900/20 sm:hidden" />}
      <aside aria-hidden={!open} className={`fixed inset-y-0 right-0 z-50 flex w-full max-w-[400px] flex-col border-l border-slate-200 bg-white shadow-[-12px_0_32px_rgba(15,23,42,0.10)] transition-transform duration-200 ${open ? 'translate-x-0' : 'translate-x-full'}`}>
        <header className="shrink-0 border-b border-slate-200 px-4 py-2.5">
          <div className="flex min-h-8 items-center gap-2">
            <div className="min-w-0 flex-1">
              <h2 className="truncate text-[14px] font-semibold text-slate-800">{reviewMode === 'today' ? '今日评论' : reviewMode === 'all' ? '逐条浏览' : target ? `${targetLabel(target.type)}评论` : '全部评论'}</h2>
              <p className="text-[10px] text-slate-400">{reviewMode && reviewItem
                ? reviewMode === 'today'
                  ? `第 ${reviewIndex + 1}/${reviewItems.length} 条今日新增评论`
                  : `第 ${reviewIndex + 1}/${reviewItems.length} 个讨论串`
                : reviewMode === 'today'
                  ? `${reviewItems.length} 条今日新增评论`
                  : target
                    ? `${visibleComments.length} 个讨论串 · ${visibleCount} 条评论`
                    : `${scopeLabel || week} · ${visibleGroups.length} 个原文 · ${visibleCount} 条评论`}</p>
            </div>
            {(reviewMode || target) && <button type="button" onClick={onShowAll} className="shrink-0 rounded-md px-2 py-1 text-[11px] text-indigo-600 hover:bg-indigo-50">查看全部</button>}
            <button type="button" onClick={onClose} aria-label="关闭评论" className="flex size-8 shrink-0 items-center justify-center rounded-lg text-xl text-slate-400 hover:bg-slate-100 hover:text-slate-700">×</button>
          </div>
          {(!reviewMode || (reviewMode !== 'today' && (resolvedCount > 0 || showHistory))) && <div className="mt-1.5 flex flex-wrap items-center justify-end gap-1">
            {!reviewMode && reviewEnabled && reviewComments.length > 0 && <button type="button" onClick={() => startReview('all')} className="shrink-0 rounded-md px-2 py-1 text-[11px] text-indigo-600 hover:bg-indigo-50">逐条浏览</button>}
            {!reviewMode && reviewEnabled && <button type="button" onClick={() => startReview('today')} className="flex shrink-0 items-center gap-1 rounded-md px-2 py-1 text-[11px] text-indigo-600 hover:bg-indigo-50">只浏览今日评论 <span className="rounded-full bg-indigo-100 px-1.5 text-[9px] font-semibold text-indigo-700">{todayReviewItems.length}</span></button>}
            {!reviewMode && !target && <button type="button" onClick={() => void load()} className="shrink-0 rounded-md px-2 py-1 text-[11px] text-slate-400 hover:bg-slate-100 hover:text-slate-600">刷新</button>}
            {(resolvedCount > 0 || showHistory) && <button type="button" aria-pressed={showHistory} onClick={() => setShowHistory((value) => !value)} className={`shrink-0 rounded-md px-2 py-1 text-[11px] ${showHistory ? 'bg-slate-100 text-slate-700' : 'text-indigo-600 hover:bg-indigo-50'}`}>{showHistory ? '隐藏历史评论' : `显示历史评论${resolvedCount > 0 ? ` ${resolvedCount}` : ''}`}</button>}
          </div>}
        </header>

        {!reviewMode && <div className="shrink-0 border-b border-slate-100 bg-slate-50/60 p-3">
          {target && <div className="mb-2"><CommentSourceCard source={target} context={targetContext} onNavigate={navigateToTargetSource} /></div>}
          <div className="rounded-xl border border-slate-200 bg-white p-2.5 shadow-[0_1px_2px_rgba(15,23,42,0.03)]">
            <CommentEditorFields value={draft} images={draftImages} objectives={objectives} onChange={setDraft} onImagesChange={setDraftImages} onUploadingChange={setDraftImageUploading} onSubmitShortcut={() => void addRoot()} placeholder={target ? '针对这段内容发表评论，输入 @ 选择提醒人…' : alignmentId ? '对当前区域对齐页发表评论，输入 @ 选择提醒人…' : planId ? '对当前 Plan 发表评论，输入 @ 选择提醒人…' : '对本周页面发表评论，输入 @ 选择提醒人…'} rows={3} />
            <div className="mt-1 flex items-center gap-2">
              <span className="text-[10px] text-slate-300">Enter 发布 · Shift+Enter 换行</span>
              <button type="button" onClick={() => void addRoot()} disabled={(!draft.content.trim() && draftImages.length === 0) || saving || draftImageUploading} className="ml-auto rounded-md bg-indigo-600 px-3 py-1.5 text-[11px] font-medium text-white hover:bg-indigo-700 disabled:cursor-not-allowed disabled:bg-slate-300">{saving ? '发布中…' : draftImageUploading ? '图片上传中…' : '发布评论'}</button>
            </div>
          </div>
          {error && <div className="mt-2 text-[11px] text-red-600">{error}</div>}
        </div>}

        {submittedId && <div role="status" className="shrink-0 border-b border-emerald-100 bg-emerald-50 px-4 py-2 text-xs text-emerald-700">评论已发布</div>}
        {navigationNotice && <div role="status" className="shrink-0 border-b border-amber-100 bg-amber-50 px-4 py-2 text-[11px] text-amber-700">{navigationNotice}</div>}

        <div className="min-h-0 flex-1 overflow-y-auto">
          {loading ? <div className="px-4 py-10 text-center text-xs text-slate-400">正在读取评论…</div> : (reviewMode ? reviewItems : visibleComments).length === 0 ? (
            <div className="px-8 py-16 text-center"><div className="mx-auto mb-3 flex size-10 items-center justify-center rounded-full bg-slate-100 text-lg text-slate-400">💬</div><p className="text-[13px] font-medium text-slate-600">{reviewMode === 'today' ? '今天暂无新增评论' : resolvedCount > 0 && !showHistory ? '当前没有未解决评论' : target ? '这段内容还没有评论' : '还没有评论'}</p><p className="mt-1 text-[11px] text-slate-400">{reviewMode === 'today' ? '按当前设备所在时区统计今天新发布的评论与回复' : resolvedCount > 0 && !showHistory ? '点击右上角「显示历史评论」查看已解决讨论' : '提出问题、补充背景或回复讨论'}</p></div>
          ) : reviewMode ? reviewItem ? (
            renderThread(reviewItem.thread, true, reviewItem.comment.id, reviewMode === 'today')
          ) : <div className="px-4 py-10 text-center text-xs text-slate-400">正在定位第一条评论…</div>
          : target ? visibleComments.map((comment) => renderThread(comment, false))
          : visibleGroups.map((group) => (
            <section key={group.key} className="border-b-4 border-slate-100 last:border-b-0">
              <div className="px-3 pt-3">
                <CommentSourceCard source={group.comments[0]} context={commentOKRContext(group.comments[0], okrContextIndex)} onNavigate={() => navigateToCommentSource(group.comments[0])} />
                <div className="px-1 pt-1.5 text-[10px] text-slate-400">{group.comments.length} 个讨论串 · {group.messageCount} 条评论</div>
              </div>
              {group.comments.map((comment) => renderThread(comment, false))}
            </section>
          ))}
        </div>
        {reviewMode && reviewItem && <footer className="shrink-0 border-t border-slate-200 bg-white p-3">
          {reviewIndex === reviewItems.length - 1 && <div className="mb-2 rounded-lg bg-emerald-50 px-3 py-2 text-center text-[12px] font-semibold text-emerald-700">{reviewMode === 'today' ? '已浏览完今日新增评论' : '已浏览完所有讨论串'}</div>}
          <div className="grid grid-cols-2 gap-2">
            <button type="button" disabled={reviewIndex === 0} onClick={() => setReviewItemId(reviewItems[reviewIndex - 1].comment.id)} className="h-10 rounded-lg border border-slate-200 bg-white text-[13px] font-semibold text-slate-600 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-40">上一条</button>
            <button type="button" disabled={reviewIndex === reviewItems.length - 1} onClick={() => setReviewItemId(reviewItems[reviewIndex + 1].comment.id)} className="h-10 rounded-lg bg-indigo-600 text-[13px] font-semibold text-white hover:bg-indigo-700 disabled:cursor-not-allowed disabled:bg-slate-300">下一条</button>
          </div>
        </footer>}
      </aside>
    </>
  )
}
