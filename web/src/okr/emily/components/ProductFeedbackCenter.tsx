import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { usePageContext } from '../../../pageContext'
import { createProductFeedback, getProductFeedback, replyProductFeedback, setProductFeedbackPlusOne, setProductFeedbackResolved } from '../api'
import type { ImageRef, ProductFeedback, ProductFeedbackPerson } from '../types'
import { Images, usePastedImageUpload } from './ui'
import { PersonAvatar } from './PersonAvatar'

type FeedbackSort = 'latest' | 'popular'

interface ProductFeedbackContextValue {
  open: () => void
  openCount: number
}

const ProductFeedbackContext = createContext<ProductFeedbackContextValue | null>(null)

function displayTime(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return new Intl.DateTimeFormat('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false }).format(date)
}

function FeedbackAvatar({ person, small = false }: { person: ProductFeedbackPerson; small?: boolean }) {
  return <PersonAvatar name={person.name} email={person.email} ownUrl={person.avatarUrl} size={small ? 'size-5 text-[9px]' : 'size-7 text-[11px]'} tone="bg-indigo-400" />
}

function sourceLabel(source: Record<string, unknown>) {
  const state = source.view_state && typeof source.view_state === 'object' ? source.view_state as Record<string, unknown> : {}
  return [state.tab, state.quarter, state.week].filter((value): value is string => typeof value === 'string' && value.length > 0).join(' · ')
}

function PlusOnePeople({ people }: { people: ProductFeedbackPerson[] }) {
  if (people.length === 0) return null
  const names = people.map((person) => person.name).join('、')
  return <span className="flex -space-x-1" title={names} aria-label={`+1 用户：${names}`}>
    {people.slice(0, 5).map((person, index) => <span key={`${person.email ?? person.name}-${index}`} className="rounded-full ring-2 ring-white"><FeedbackAvatar person={person} small /></span>)}
    {people.length > 5 && <span className="flex size-5 items-center justify-center rounded-full bg-slate-200 text-[8px] font-semibold text-slate-600 ring-2 ring-white">+{people.length - 5}</span>}
  </span>
}

function FeedbackComposer({ sourceContext, onCreated }: { sourceContext: Record<string, unknown>; onCreated: (feedback: ProductFeedback) => void }) {
  const [expanded, setExpanded] = useState(false)
  const [title, setTitle] = useState('')
  const [content, setContent] = useState('')
  const [images, setImages] = useState<ImageRef[]>([])
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const fileRef = useRef<HTMLInputElement>(null)
  const upload = usePastedImageUpload((uploaded) => setImages((current) => [...current, ...uploaded].slice(0, 9)), saving || images.length >= 9, 9 - images.length)

  const submit = async () => {
    if (!title.trim() || !content.trim() || saving || upload.uploading) return
    setSaving(true)
    setError('')
    try {
      const created = await createProductFeedback({ title: title.trim(), content: content.trim(), images, sourceContext })
      setTitle('')
      setContent('')
      setImages([])
      setExpanded(false)
      onCreated(created)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '提交反馈失败')
    } finally {
      setSaving(false)
    }
  }

  if (!expanded) {
    return <button type="button" onClick={() => setExpanded(true)} className="w-full rounded-xl bg-indigo-600 px-4 py-2.5 text-sm font-semibold text-white shadow-sm hover:bg-indigo-700">＋ 提交新问题</button>
  }

  return <section className="rounded-xl border border-indigo-100 bg-indigo-50/40 p-3">
    <input value={title} maxLength={100} onChange={(event) => setTitle(event.target.value)} placeholder="一句话描述问题" aria-label="反馈标题" autoFocus className="h-9 w-full rounded-lg border border-slate-200 bg-white px-3 text-sm outline-none focus:border-indigo-400" />
    <textarea value={content} maxLength={4000} onPaste={upload.onPaste} onChange={(event) => setContent(event.target.value)} placeholder="请描述遇到的现象、预期结果；可直接粘贴截图" aria-label="反馈详情" rows={4} className="mt-2 w-full resize-y rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm leading-5 outline-none focus:border-indigo-400" />
    {images.length > 0 && <div className="mt-2"><Images value={images} onChange={setImages} pasteEnabled={false} maxDisplayWidth={240} /></div>}
    <div className="mt-2 flex flex-wrap items-center gap-2">
      <input ref={fileRef} type="file" accept="image/png,image/jpeg,image/gif,image/webp" multiple className="hidden" onChange={(event) => { upload.upload(Array.from(event.currentTarget.files ?? []).slice(0, 9 - images.length)); event.currentTarget.value = '' }} />
      <button type="button" disabled={upload.uploading || images.length >= 9} onClick={() => fileRef.current?.click()} className="rounded-lg border border-slate-200 bg-white px-2.5 py-1.5 text-xs font-medium text-slate-500 hover:border-indigo-200 hover:text-indigo-600 disabled:opacity-40">{upload.uploading ? '图片上传中…' : '添加截图'}</button>
      <span className="text-[10px] text-slate-400">最多 9 张，也可直接粘贴</span>
      <span className="ml-auto text-[10px] text-slate-400">{content.length}/4000</span>
    </div>
    {(error || upload.uploadError) && <p role="alert" className="mt-2 text-xs text-red-600">{error || upload.uploadError}</p>}
    <div className="mt-3 flex justify-end gap-2">
      <button type="button" disabled={saving} onClick={() => setExpanded(false)} className="rounded-lg px-3 py-1.5 text-xs font-medium text-slate-500 hover:bg-white">取消</button>
      <button type="button" disabled={!title.trim() || !content.trim() || saving || upload.uploading} onClick={() => void submit()} className="rounded-lg bg-indigo-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-indigo-700 disabled:opacity-40">{saving ? '提交中…' : '提交反馈'}</button>
    </div>
  </section>
}

function FeedbackCard({ feedback, onChange }: { feedback: ProductFeedback; onChange: (feedback: ProductFeedback) => void }) {
  const [replying, setReplying] = useState(false)
  const [reply, setReply] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const source = sourceLabel(feedback.sourceContext)

  const run = async (action: () => Promise<ProductFeedback>) => {
    if (busy) return
    setBusy(true)
    setError('')
    try {
      onChange(await action())
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '操作失败，请重试')
    } finally {
      setBusy(false)
    }
  }

  const submitReply = async () => {
    const value = reply.trim()
    if (!value) return
    await run(async () => {
      const updated = await replyProductFeedback(feedback.id, value)
      setReply('')
      setReplying(false)
      return updated
    })
  }

  return <article className="rounded-xl border border-slate-200 bg-white p-3 shadow-[0_1px_2px_rgba(15,23,42,0.04)]">
    <div className="flex items-start gap-2.5">
      <FeedbackAvatar person={feedback.author} />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
          <span className="text-xs font-semibold text-slate-700">{feedback.author.name}</span>
          <time className="text-[10px] text-slate-400">{displayTime(feedback.createdAt)}</time>
          {feedback.resolved && <span className="rounded-full bg-emerald-50 px-1.5 py-0.5 text-[9px] font-semibold text-emerald-600">已解决</span>}
          {source && <span className="ml-auto max-w-44 truncate text-[9px] text-slate-400" title={source}>{source}</span>}
        </div>
        <h3 className="mt-1.5 text-sm font-semibold leading-5 text-slate-900">{feedback.title}</h3>
        <p className="mt-1 whitespace-pre-wrap break-words text-xs leading-5 text-slate-600">{feedback.content}</p>
        {feedback.images.length > 0 && <div className="mt-2"><Images value={feedback.images} onChange={() => undefined} readOnly maxDisplayWidth={280} /></div>}
      </div>
    </div>

    {feedback.replies.length > 0 && <div className="mt-3 space-y-2 border-l-2 border-slate-100 pl-3">
      {feedback.replies.map((item) => <div key={item.id} className="flex items-start gap-2">
        <FeedbackAvatar person={item.author} small />
        <div className="min-w-0 flex-1 rounded-lg bg-slate-50 px-2.5 py-2">
          <div className="flex items-center gap-2"><span className="text-[11px] font-semibold text-slate-600">{item.author.name}</span><time className="text-[9px] text-slate-400">{displayTime(item.createdAt)}</time></div>
          <p className="mt-1 whitespace-pre-wrap break-words text-xs leading-5 text-slate-600">{item.content}</p>
        </div>
      </div>)}
    </div>}

    {replying && <div className="mt-3 rounded-lg border border-indigo-100 bg-indigo-50/40 p-2">
      <textarea value={reply} maxLength={2000} onChange={(event) => setReply(event.target.value)} placeholder={`回复 ${feedback.author.name}`} rows={2} autoFocus className="w-full resize-y rounded-md border border-slate-200 bg-white px-2.5 py-2 text-xs outline-none focus:border-indigo-400" />
      <div className="mt-1.5 flex justify-end gap-1.5"><button type="button" onClick={() => setReplying(false)} className="rounded px-2 py-1 text-[11px] text-slate-500">取消</button><button type="button" disabled={!reply.trim() || busy} onClick={() => void submitReply()} className="rounded-md bg-indigo-600 px-2.5 py-1 text-[11px] font-medium text-white disabled:opacity-40">回复</button></div>
    </div>}

    {error && <p role="alert" className="mt-2 text-[11px] text-red-600">{error}</p>}
    <footer className="mt-3 flex flex-wrap items-center gap-2 border-t border-slate-100 pt-2.5">
      <button type="button" disabled={busy} aria-pressed={feedback.myPlusOne} onClick={() => void run(() => setProductFeedbackPlusOne(feedback.id, !feedback.myPlusOne))} className={`rounded-lg border px-2.5 py-1 text-xs font-semibold disabled:opacity-40 ${feedback.myPlusOne ? 'border-indigo-200 bg-indigo-50 text-indigo-700' : 'border-slate-200 text-slate-500 hover:border-indigo-200 hover:text-indigo-600'}`}>+1{feedback.plusOnes.length > 0 ? ` · ${feedback.plusOnes.length}` : ''}</button>
      <PlusOnePeople people={feedback.plusOnes} />
      <button type="button" onClick={() => setReplying((value) => !value)} className="rounded-lg px-2 py-1 text-xs font-medium text-slate-500 hover:bg-slate-50 hover:text-indigo-600">回复{feedback.replies.length > 0 ? ` · ${feedback.replies.length}` : ''}</button>
      {feedback.canResolve && <button type="button" disabled={busy} onClick={() => void run(() => setProductFeedbackResolved(feedback, !feedback.resolved))} className="ml-auto rounded-lg px-2 py-1 text-xs font-medium text-slate-500 hover:bg-slate-50 hover:text-indigo-600 disabled:opacity-40">{feedback.resolved ? '重新打开' : '标记已解决'}</button>}
    </footer>
    {feedback.resolved && feedback.resolvedBy && <p className="mt-2 text-right text-[9px] text-slate-400">{feedback.resolvedBy.name} 于 {feedback.resolvedAt ? displayTime(feedback.resolvedAt) : ''} 标记解决</p>}
  </article>
}

function ProductFeedbackDrawer({ onClose, sourceContext, openCount, onOpenCountChange }: { onClose: () => void; sourceContext: Record<string, unknown>; openCount: number; onOpenCountChange: (count: number) => void }) {
  const [resolved, setResolved] = useState(false)
  const [sort, setSort] = useState<FeedbackSort>('latest')
  const [items, setItems] = useState<ProductFeedback[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const result = await getProductFeedback({ resolved, sort })
      setItems(result.items)
      setTotal(result.total)
      if (!resolved) onOpenCountChange(result.total)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '读取反馈失败')
    } finally {
      setLoading(false)
    }
  }, [onOpenCountChange, resolved, sort])

  useEffect(() => { void load() }, [load])
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => { if (event.key === 'Escape') onClose() }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  const changed = (feedback: ProductFeedback) => {
    if (feedback.resolved !== resolved) {
      setItems((current) => current.filter((item) => item.id !== feedback.id))
      setTotal((current) => Math.max(0, current - 1))
      onOpenCountChange(resolved ? openCount + 1 : Math.max(0, openCount - 1))
      return
    }
    setItems((current) => current.map((item) => item.id === feedback.id ? feedback : item))
  }

  return <div className="fixed inset-0 z-[220]" role="dialog" aria-modal="true" aria-label="建议反馈">
    <button type="button" aria-label="关闭建议反馈" onClick={onClose} className="absolute inset-0 bg-slate-950/20" />
    <aside className="absolute inset-y-0 right-0 flex w-full max-w-[520px] flex-col border-l border-slate-200 bg-slate-50 shadow-[-12px_0_36px_rgba(15,23,42,0.14)]">
      <header className="flex shrink-0 items-center border-b border-slate-200 bg-white px-4 py-3">
        <div><h2 className="text-base font-semibold text-slate-900">建议反馈</h2><p className="mt-0.5 text-[10px] text-slate-400">反馈问题、参与讨论，或为同样的问题 +1</p></div>
        <button type="button" onClick={onClose} className="ml-auto flex size-8 items-center justify-center rounded-lg text-xl text-slate-400 hover:bg-slate-100 hover:text-slate-600" aria-label="关闭">×</button>
      </header>
      <div className="shrink-0 space-y-3 border-b border-slate-200 bg-white px-4 py-3">
        <FeedbackComposer sourceContext={sourceContext} onCreated={(feedback) => { setResolved(false); setItems((current) => [feedback, ...current]); setTotal((current) => current + 1); onOpenCountChange(openCount + 1) }} />
        <div className="flex items-center gap-2">
          <div className="flex rounded-lg bg-slate-100 p-0.5 text-xs font-medium">
            <button type="button" onClick={() => setResolved(false)} className={`rounded-md px-3 py-1.5 ${!resolved ? 'bg-white text-indigo-700 shadow-sm' : 'text-slate-500'}`}>待解决 · {openCount}</button>
            <button type="button" onClick={() => setResolved(true)} className={`rounded-md px-3 py-1.5 ${resolved ? 'bg-white text-indigo-700 shadow-sm' : 'text-slate-500'}`}>已解决</button>
          </div>
          <select aria-label="反馈排序" value={sort} onChange={(event) => setSort(event.target.value as FeedbackSort)} className="ml-auto h-8 rounded-lg border border-slate-200 bg-white px-2 text-[11px] text-slate-500 outline-none"><option value="latest">最新活跃</option><option value="popular">最多 +1</option></select>
        </div>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto p-4">
        {loading ? <div className="py-16 text-center text-sm text-slate-400">正在加载反馈…</div> : error ? <div className="rounded-xl border border-red-200 bg-red-50 p-4 text-center text-xs text-red-600">{error}<button type="button" onClick={() => void load()} className="ml-2 underline">重试</button></div> : items.length === 0 ? <div className="py-16 text-center"><div className="text-2xl">💡</div><p className="mt-2 text-sm font-medium text-slate-600">{resolved ? '还没有已解决的反馈' : '还没有待解决的反馈'}</p><p className="mt-1 text-xs text-slate-400">{resolved ? '解决后的问题会保留在这里' : '发现问题时可以提交第一条反馈'}</p></div> : <div className="space-y-3">{items.map((feedback) => <FeedbackCard key={feedback.id} feedback={feedback} onChange={changed} />)}{total > items.length && <p className="py-2 text-center text-[10px] text-slate-400">当前显示前 {items.length} 条，共 {total} 条</p>}</div>}
      </div>
    </aside>
  </div>
}

export function ProductFeedbackProvider({ children }: { children: ReactNode }) {
  const { context } = usePageContext()
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [openCount, setOpenCount] = useState(0)
  const sourceContext = useMemo<Record<string, unknown>>(() => ({
    page_key: context.active_key,
    view_state: context.view_state,
    hash: window.location.hash,
    title: document.title,
  }), [context.active_key, context.view_state])

  useEffect(() => {
    void getProductFeedback({ resolved: false, sort: 'latest' }).then((result) => setOpenCount(result.total)).catch(() => undefined)
  }, [])

  const value = useMemo(() => ({ open: () => setDrawerOpen(true), openCount }), [openCount])
  return <ProductFeedbackContext.Provider value={value}>
    {children}
    {drawerOpen && <ProductFeedbackDrawer onClose={() => setDrawerOpen(false)} sourceContext={sourceContext} openCount={openCount} onOpenCountChange={setOpenCount} />}
  </ProductFeedbackContext.Provider>
}

export function ProductFeedbackTrigger({ className = '' }: { className?: string }) {
  const feedback = useContext(ProductFeedbackContext)
  if (!feedback) throw new Error('ProductFeedbackTrigger must be used within ProductFeedbackProvider')
  return <button type="button" onClick={feedback.open} aria-label="打开建议反馈" className={`relative flex h-8 items-center gap-1.5 whitespace-nowrap rounded-lg border border-slate-200 bg-white px-2.5 text-[10px] font-medium text-slate-600 hover:border-indigo-200 hover:text-indigo-600 ${className}`}>
    <span aria-hidden>💡</span><span className="hidden sm:inline">建议反馈</span>
    {feedback.openCount > 0 && <span className="min-w-4 rounded-full bg-indigo-600 px-1 text-center text-[9px] leading-4 text-white">{feedback.openCount > 99 ? '99+' : feedback.openCount}</span>}
  </button>
}
