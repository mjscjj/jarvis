import { createContext, useContext, useEffect, useMemo, useRef, type CSSProperties, type InputHTMLAttributes, type MouseEvent, type ReactNode, type TextareaHTMLAttributes } from 'react'
import { createPortal } from 'react-dom'
import { commentTargetFromThread, commentTargetKey, resolveCommentSelection } from './comments'
import type { CommentTarget, PageComment, TextSelection } from './types'

export interface PendingCommentSelection {
  targetKey: string
  instanceKey?: string
  selection: TextSelection
  rect?: { top: number; left: number; bottom: number; width: number }
}

export interface CommentInteraction {
  enabled?: boolean
  triggerMode?: 'button' | 'surface'
  selected?: CommentTarget
  focused?: PageComment
  comments: PageComment[]
  counts: Record<string, number>
  pendingSelection?: PendingCommentSelection
  setPendingSelection: (value?: PendingCommentSelection) => void
  select: (target: CommentTarget) => void
}

const CommentInteractionContext = createContext<CommentInteraction>({ comments: [], counts: {}, setPendingSelection: () => undefined, select: () => undefined })

export function CommentInteractionProvider({ value, children }: { value: CommentInteraction; children: ReactNode }) {
  return <CommentInteractionContext.Provider value={value}>{children}</CommentInteractionContext.Provider>
}

export function useCommentInteraction() {
  return useContext(CommentInteractionContext)
}

export function useCommentSurface(target: CommentTarget) {
  const interaction = useCommentInteraction()
  const key = commentTargetKey(target)
  const enabled = Boolean(interaction.enabled && interaction.triggerMode === 'surface')
  const selected = !interaction.focused && interaction.selected ? commentTargetKey(interaction.selected) === key : false
  const focused = interaction.focused ? commentTargetKey(interaction.focused) === key : false
  return {
    enabled,
    selected,
    focused,
    count: interaction.counts[key] ?? 0,
    onClick: (event: MouseEvent<HTMLElement>) => {
      if (!enabled) return
      const source = event.target
      if (source instanceof Element && source.closest('button,input,textarea,select,a,summary,[contenteditable="true"]')) return
      if (window.getSelection()?.toString().trim()) return
      event.stopPropagation()
      interaction.select(target)
    },
  }
}

export function CommentSurfaceHint({ target }: { target: CommentTarget }) {
  const surface = useCommentSurface(target)
  if (!surface.enabled) return null
  return surface.count > 0
    ? <span className="ml-auto shrink-0 rounded-full bg-indigo-50 px-1.5 py-0.5 text-[9px] font-medium text-indigo-600">{surface.count} 条评论</span>
    : <span className="ml-auto shrink-0 text-[9px] font-medium text-indigo-400 opacity-0 transition-opacity group-hover/commentable:opacity-100">点击评论</span>
}

export function commentTargetElementId(target: Pick<CommentTarget, 'type' | 'id'> | Pick<PageComment, 'targetType' | 'targetId'>) {
  return `comment-target-${encodeURIComponent(commentTargetKey(target))}`
}

export function commentSelectionElementId(commentId: string) {
  return `comment-selection-${encodeURIComponent(commentId)}`
}

function visibleElement(elements: Element[]) {
  return elements.find((element) => {
    const rect = element.getBoundingClientRect()
    const style = window.getComputedStyle(element)
    return rect.width > 0 && rect.height > 0 && style.display !== 'none' && style.visibility !== 'hidden'
  }) as HTMLElement | undefined
}

function targetElements(target: Pick<CommentTarget, 'type' | 'id'> | Pick<PageComment, 'targetType' | 'targetId'>) {
  const key = commentTargetKey(target)
  return [...document.querySelectorAll(`[data-comment-target-key="${CSS.escape(key)}"]`)]
}

export function scrollToCommentSource(comment: PageComment) {
  const selection = visibleElement([...document.querySelectorAll(`[data-comment-selection-id="${CSS.escape(comment.id)}"]`)]) ?? document.getElementById(commentSelectionElementId(comment.id))
  const target = visibleElement(targetElements(comment)) ?? document.getElementById(commentTargetElementId(comment))
  const element = selection ?? target
  element?.scrollIntoView({ behavior: 'smooth', block: 'center', inline: 'nearest' })
  return selection ? 'selection' : target ? 'target' : 'missing'
}

export function scrollToCommentTarget(target: CommentTarget) {
  const selection = target.commentId
    ? visibleElement([...document.querySelectorAll(`[data-comment-selection-id="${CSS.escape(target.commentId)}"]`)]) ?? document.getElementById(commentSelectionElementId(target.commentId))
    : null
  const element = selection ?? visibleElement(targetElements(target)) ?? document.getElementById(commentTargetElementId(target))
  element?.scrollIntoView({ behavior: 'smooth', block: 'center', inline: 'nearest' })
  return selection ? 'selection' : element ? 'target' : 'missing'
}

function selectionWithin(root: HTMLElement, sourceText: string): { selection: TextSelection; rect: PendingCommentSelection['rect'] } | undefined {
  const browserSelection = window.getSelection()
  if (!browserSelection || browserSelection.rangeCount === 0 || browserSelection.isCollapsed) return undefined
  const range = browserSelection.getRangeAt(0)
  if (!root.contains(range.commonAncestorContainer)) return undefined
  const raw = range.toString()
  const leading = raw.match(/^\s*/)?.[0].length ?? 0
  const trailing = raw.match(/\s*$/)?.[0].length ?? 0
  if (raw.length <= leading + trailing) return undefined

  const prefixRange = document.createRange()
  prefixRange.selectNodeContents(root)
  prefixRange.setEnd(range.startContainer, range.startOffset)
  const start = prefixRange.toString().length + leading
  const end = start + raw.length - leading - trailing
  if (sourceText.slice(start, end) !== raw.slice(leading, raw.length - trailing)) return undefined
  const bounds = range.getBoundingClientRect()
  return {
    selection: {
      text: sourceText.slice(start, end),
      start,
      end,
      prefix: sourceText.slice(Math.max(0, start - 48), start),
      suffix: sourceText.slice(end, Math.min(sourceText.length, end + 48)),
    },
    rect: { top: bounds.top, left: bounds.left, bottom: bounds.bottom, width: bounds.width },
  }
}

function SelectionCommentBubble({ target, sourceText, instanceKey }: { target: CommentTarget; sourceText: string; instanceKey: string }) {
  const interaction = useCommentInteraction()
  const pending = interaction.pendingSelection
  if (!pending?.rect || pending.targetKey !== commentTargetKey(target) || pending.instanceKey !== instanceKey) return null
  const style: CSSProperties = {
    left: Math.max(12, pending.rect.left + pending.rect.width / 2),
    top: Math.max(12, pending.rect.bottom + 8),
    transform: 'translateX(-50%)',
  }
  return createPortal(<button
    type="button"
    data-comment-selection-trigger
    onMouseDown={(event) => event.preventDefault()}
    onClick={() => {
      interaction.select({ ...target, title: target.title || sourceText, selection: pending.selection })
      interaction.setPendingSelection(undefined)
      window.getSelection()?.removeAllRanges()
    }}
    style={style}
    className="fixed z-[70] whitespace-nowrap rounded-full bg-slate-900 px-3 py-1.5 text-[11px] font-semibold text-white shadow-lg hover:bg-indigo-700"
  >💬 评论</button>, document.body)
}

function renderHighlightedText(text: string, target: CommentTarget, interaction: CommentInteraction) {
  const threads = interaction.comments.flatMap((comment) => {
    if (!comment.selectedText || commentTargetKey(comment) !== commentTargetKey(target)) return []
    const range = resolveCommentSelection(text, comment)
    return range ? [{ comment, ...range }] : []
  })
  if (!threads.length) return text
  const boundaries = [...new Set([0, text.length, ...threads.flatMap(({ start, end }) => [start, end])])].sort((left, right) => left - right)
  return boundaries.slice(0, -1).map((start, index) => {
    const end = boundaries[index + 1]
    const covering = threads.filter((thread) => thread.start <= start && thread.end >= end)
    if (!covering.length) return <span key={`${start}:${end}`}>{text.slice(start, end)}</span>
    const focused = covering.some(({ comment }) => interaction.focused?.id === comment.id)
    return <mark
      key={`${start}:${end}`}
      data-comment-selection-id={covering.length === 1 ? covering[0].comment.id : undefined}
      onClick={(event) => { event.stopPropagation(); interaction.select(commentTargetFromThread(covering[0].comment)) }}
      className={`cursor-pointer rounded-sm px-0 text-inherit underline decoration-2 underline-offset-2 ${focused ? 'bg-indigo-100 decoration-indigo-500' : 'bg-amber-100/80 decoration-amber-400'}`}
    >{text.slice(start, end)}</mark>
  })
}

export function CommentableText({ target, text, instanceKey, children, className = '' }: { target: CommentTarget; text: string; instanceKey: string; children?: ReactNode; className?: string }) {
  const interaction = useCommentInteraction()
  const ref = useRef<HTMLSpanElement>(null)
  const key = commentTargetKey(target)
  const selected = !interaction.focused && interaction.selected ? commentTargetKey(interaction.selected) === key : false
  const focused = interaction.focused ? commentTargetKey(interaction.focused) === key : false
  const count = interaction.counts[key] ?? 0
  const rendered = useMemo(() => renderHighlightedText(text, target, interaction), [focused, interaction.comments, key, text])
  const capture = () => {
    if (!interaction.enabled || !ref.current) return
    const result = selectionWithin(ref.current, text)
    if (!result) return
    interaction.setPendingSelection({ targetKey: key, instanceKey, ...result })
  }
  useEffect(() => {
    if (interaction.pendingSelection?.instanceKey !== instanceKey) return
    const clear = () => interaction.setPendingSelection(undefined)
    window.addEventListener('resize', clear)
    window.addEventListener('scroll', clear, true)
    return () => { window.removeEventListener('resize', clear); window.removeEventListener('scroll', clear, true) }
  }, [instanceKey, interaction.pendingSelection?.instanceKey])
  return <span
    ref={ref}
    data-comment-target-key={key}
    data-comment-instance={instanceKey}
    onMouseUp={capture}
    onKeyUp={capture}
    className={`rounded-sm ${selected || focused ? 'bg-indigo-50 ring-2 ring-indigo-300' : ''} ${className}`}
  >{typeof rendered === 'string' && children ? children : rendered}{count > 0 && <span role="button" tabIndex={0} title="查看评论" onClick={(event) => { event.stopPropagation(); interaction.select(target) }} onKeyDown={(event) => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); event.stopPropagation(); interaction.select(target) } }} className="ml-1 inline-flex min-w-5 items-center justify-center rounded-full bg-amber-50 px-1 text-[9px] font-semibold text-amber-700 ring-1 ring-amber-200">💬 {count}</span>}<SelectionCommentBubble target={target} sourceText={text} instanceKey={instanceKey} /></span>
}

export function CommentableField({ target, instanceKey, children, className = '' }: { target: CommentTarget; instanceKey: string; children: ReactNode; className?: string }) {
  const interaction = useCommentInteraction()
  const key = commentTargetKey(target)
  const count = interaction.counts[key] ?? 0
  const selected = !interaction.focused && interaction.selected ? commentTargetKey(interaction.selected) === key : false
  const focused = interaction.focused ? commentTargetKey(interaction.focused) === key : false
  return <div data-comment-target-key={key} data-comment-instance={instanceKey} className={`group/comment-field relative rounded-lg ${selected || focused ? 'ring-2 ring-indigo-400' : ''} ${className}`}>
    {children}
    {interaction.enabled && <button
      type="button"
      title={`评论 ${target.title}`}
      aria-label={`评论 ${target.title}`}
      onMouseDown={(event) => event.preventDefault()}
      onClick={(event) => { event.stopPropagation(); interaction.select(target) }}
      className={`absolute right-1 top-1 z-10 flex h-6 items-center rounded-full border border-indigo-200 bg-white/95 px-1.5 text-[9px] font-semibold text-indigo-600 shadow-sm transition-opacity ${count > 0 ? 'opacity-100' : 'opacity-0 group-hover/comment-field:opacity-100 group-focus-within/comment-field:opacity-100'}`}
    >💬{count > 0 ? ` ${count}` : ''}</button>}
  </div>
}

function useInputCommentSelection(target: CommentTarget, text: string, instanceKey: string, ref: React.RefObject<HTMLInputElement | HTMLTextAreaElement | null>) {
  const interaction = useCommentInteraction()
  const key = commentTargetKey(target)
  const capture = () => {
    if (!interaction.enabled || !ref.current) return
    let start = ref.current.selectionStart ?? 0
    let end = ref.current.selectionEnd ?? 0
    if (end <= start) return
    const raw = text.slice(start, end)
    const leading = raw.match(/^\s*/)?.[0].length ?? 0
    const trailing = raw.match(/\s*$/)?.[0].length ?? 0
    start += leading
    end -= trailing
    if (end <= start) return
    const bounds = ref.current.getBoundingClientRect()
    interaction.setPendingSelection({
      targetKey: key,
      instanceKey,
      selection: {
        text: text.slice(start, end),
        start,
        end,
        prefix: text.slice(Math.max(0, start - 48), start),
        suffix: text.slice(end, Math.min(text.length, end + 48)),
      },
      rect: { top: bounds.top, left: bounds.right - 80, bottom: bounds.top + 28, width: 64 },
    })
  }
  return { interaction, key, capture }
}

export function CommentableTextarea({ target, instanceKey, className = '', ...props }: { target: CommentTarget; instanceKey: string } & TextareaHTMLAttributes<HTMLTextAreaElement>) {
  const ref = useRef<HTMLTextAreaElement>(null)
  const text = String(props.value ?? '')
  const { interaction, key, capture } = useInputCommentSelection(target, text, instanceKey, ref)
  const active = interaction.selected && commentTargetKey(interaction.selected) === key
  const count = interaction.counts[key] ?? 0
  return <span data-comment-target-key={key} data-comment-instance={instanceKey} className="relative block">
    <textarea {...props} ref={ref} onSelect={capture} className={`${active ? 'ring-2 ring-indigo-300' : ''} ${className}`} />
    {count > 0 && <button type="button" title="查看评论" onMouseDown={(event) => event.preventDefault()} onClick={() => interaction.select(target)} className="absolute right-1 top-1 z-10 rounded-full border border-amber-200 bg-amber-50 px-1.5 py-0.5 text-[9px] font-semibold text-amber-700 shadow-sm">💬 {count}</button>}
    <SelectionCommentBubble target={target} sourceText={text} instanceKey={instanceKey} />
  </span>
}

export function CommentableInput({ target, instanceKey, className = '', ...props }: { target: CommentTarget; instanceKey: string } & InputHTMLAttributes<HTMLInputElement>) {
  const ref = useRef<HTMLInputElement>(null)
  const text = String(props.value ?? '')
  const { interaction, key, capture } = useInputCommentSelection(target, text, instanceKey, ref)
  const active = interaction.selected && commentTargetKey(interaction.selected) === key
  const count = interaction.counts[key] ?? 0
  return <span data-comment-target-key={key} data-comment-instance={instanceKey} className="relative block">
    <input {...props} ref={ref} onSelect={capture} className={`${active ? 'ring-2 ring-indigo-300' : ''} ${className}`} />
    {count > 0 && <button type="button" title="查看评论" onMouseDown={(event) => event.preventDefault()} onClick={() => interaction.select(target)} className="absolute right-1 top-1/2 z-10 -translate-y-1/2 rounded-full border border-amber-200 bg-amber-50 px-1.5 py-0.5 text-[9px] font-semibold text-amber-700 shadow-sm">💬 {count}</button>}
    <SelectionCommentBubble target={target} sourceText={text} instanceKey={instanceKey} />
  </span>
}

export function CommentTargetButton({ target, className = '' }: { target: CommentTarget; className?: string }) {
  const interaction = useCommentInteraction()
  if (!interaction.enabled || interaction.triggerMode === 'surface') return null
  const key = commentTargetKey(target)
  const count = interaction.counts[key] ?? 0
  const selected = !interaction.focused && interaction.selected ? commentTargetKey(interaction.selected) === key : false
  const focused = interaction.focused ? commentTargetKey(interaction.focused) === key : false
  return (
    <button
      type="button"
      title="查看或添加评论"
      aria-label={`评论 ${target.title}`}
      onClick={(event) => { event.stopPropagation(); interaction.select(target) }}
      className={`inline-flex h-6 shrink-0 items-center justify-center gap-1 rounded-md border px-1.5 text-[9px] font-medium transition-colors ${selected || focused ? 'border-indigo-300 bg-indigo-50 text-indigo-700' : 'border-slate-200 bg-white text-slate-400 hover:border-indigo-200 hover:text-indigo-600'} ${className}`}
    >
      <span aria-hidden>💬</span>{count > 0 && <span>{count}</span>}
    </button>
  )
}
