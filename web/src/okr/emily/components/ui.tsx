/**
 * 表格里用的控件。展示态没有边框，hover / focus 才显形，
 * 这样整页看上去还是一张文档表格，而不是一堆表单。
 */

import { useCallback, useEffect, useLayoutEffect, useRef, useState, type ClipboardEvent, type ReactNode } from 'react'
import { uploadImage } from '../api'
import { uid } from '../board'
import { useBoard } from '../board'
import { commentTargetFromThread, commentTargetKey } from '../comments'
import { useCommentInteraction } from '../commenting'
import { imageFilesFromClipboard } from '../imagePaste'
import { DOT_CLASS, TONE_CLASS, TONE_TEXT_CLASS, statusOf } from '../template'
import type { CommentTarget, DocLink, ImageRef, Status } from '../types'

function Popover({
  open,
  onClose,
  children,
  width,
}: {
  open: boolean
  onClose: () => void
  children: ReactNode
  width?: number
}) {
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    function onDown(e: MouseEvent) {
      if (!ref.current?.parentElement?.contains(e.target as Node)) onClose()
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open, onClose])

  if (!open) return null
  return (
    <div
      ref={ref}
      style={width ? { width } : undefined}
      className="absolute top-[calc(100%+3px)] left-0 z-30 max-h-72 min-w-32 overflow-auto rounded-md border border-slate-200 bg-white p-1 shadow-lg"
    >
      {children}
    </div>
  )
}

/** 状态下拉框。选「已完成」这条就归到「已完成」列。 */
export function StatusSelect({
  value,
  onChange,
  readOnly = false,
}: {
  value: Status
  onChange: (v: Status) => void
  readOnly?: boolean
}) {
  const [open, setOpen] = useState(false)
  const current = statusOf(value)!
  const { enums } = useBoard()
  const options = enums.statuses.map(statusOf).filter((item) => item !== undefined)

  if (readOnly) {
    return (
      <span title={current.label} className={`inline-flex shrink-0 items-center gap-1.5 whitespace-nowrap text-[10px] font-medium leading-5 ${TONE_TEXT_CLASS[current.tone]}`}>
        <span className={`size-1.5 shrink-0 rounded-full ${DOT_CLASS[current.tone]}`} />
        <span>{current.label}</span>
      </span>
    )
  }

  return (
    <span className="relative inline-block align-top">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className={`status-select-button inline-flex shrink-0 cursor-pointer items-center gap-1.5 rounded-full px-2 py-1 font-medium transition-[background-color,box-shadow] hover:shadow-sm ${TONE_CLASS[current.tone]}`}
      >
        <span className={`size-1.5 shrink-0 rounded-full ${DOT_CLASS[current.tone]}`} />
        <span>{current.label}</span>
        <svg viewBox="0 0 12 12" aria-hidden className={`size-2.5 opacity-45 transition-transform ${open ? 'rotate-180' : ''}`} fill="none" stroke="currentColor" strokeWidth="1.5">
          <path d="m3 4.5 3 3 3-3" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </button>
      <Popover open={open} onClose={() => setOpen(false)}>
        {options.map((s) => (
          <button
            key={s.value}
            type="button"
            onClick={() => {
              onChange(s.value)
              setOpen(false)
            }}
            className={`flex w-full items-center gap-2 rounded px-2 py-1 text-left text-xs hover:bg-slate-50 ${
              s.value === value ? 'bg-slate-50' : ''
            }`}
          >
            <span className={`size-1.5 shrink-0 rounded-full ${DOT_CLASS[s.tone]}`} />
            {s.label}
          </button>
        ))}
      </Popover>
    </span>
  )
}

/** 输入停 500ms 自动提交，不需要点保存 */
function useDebounced(value: string, commit: (v: string) => void, delay = 500) {
  const [local, setLocal] = useState(value)
  const dirty = useRef(false)

  useEffect(() => {
    if (!dirty.current) setLocal(value)
  }, [value])

  useEffect(() => {
    if (!dirty.current || local === value) return
    const t = window.setTimeout(() => {
      commit(local)
      dirty.current = false
    }, delay)
    return () => window.clearTimeout(t)
  }, [local, value, commit, delay])

  return [
    local,
    (v: string) => {
      dirty.current = true
      setLocal(v)
    },
  ] as const
}

export function Text({
  value,
  onChange,
  placeholder,
  className = '',
  fit,
  readOnly = false,
  commentTarget,
}: {
  value: string
  onChange: (v: string) => void
  placeholder?: string
  className?: string
  /** 宽度跟着内容走，而不是撑满父容器；用于让后面的控件紧贴文本 */
  fit?: boolean
  readOnly?: boolean
  commentTarget?: CommentTarget
}) {
  const [local, setLocal] = useDebounced(value, onChange)
  const ref = useRef<HTMLTextAreaElement>(null)
  const commentInteraction = useCommentInteraction()
  const selectionTargetKey = commentTarget ? commentTargetKey(commentTarget) : ''
  const pendingSelection = commentInteraction.pendingSelection?.targetKey === selectionTargetKey ? commentInteraction.pendingSelection.selection : undefined
  const selectionThreads = commentTarget ? commentInteraction.comments.filter((comment) => comment.selectedText && commentTargetKey(comment) === commentTargetKey(commentTarget)) : []

  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${el.scrollHeight}px`
  }, [local])

  const captureSelection = () => {
    const element = ref.current
    if (!element || !commentTarget) return
    if (element.selectionStart === element.selectionEnd) {
      if (commentInteraction.pendingSelection?.targetKey === selectionTargetKey) commentInteraction.setPendingSelection(undefined)
      return
    }
    let start = element.selectionStart
    let end = element.selectionEnd
    const raw = local.slice(start, end)
    const leading = raw.match(/^\s*/)?.[0].length ?? 0
    const trailing = raw.match(/\s*$/)?.[0].length ?? 0
    start += leading
    end -= trailing
    if (end <= start) return
    commentInteraction.setPendingSelection({
      targetKey: selectionTargetKey,
      selection: {
        text: local.slice(start, end),
        start,
        end,
        prefix: local.slice(Math.max(0, start - 48), start),
        suffix: local.slice(end, Math.min(local.length, end + 48)),
      },
    })
  }

  return <>
    <textarea
      ref={ref}
      rows={1}
      value={local}
      readOnly={readOnly}
      placeholder={placeholder}
      onChange={(e) => setLocal(e.target.value)}
      onSelect={captureSelection}
      onKeyDown={(event) => {
        if (event.key !== 'Escape' || event.currentTarget.selectionStart === event.currentTarget.selectionEnd) return
        const end = event.currentTarget.selectionEnd
        event.currentTarget.setSelectionRange(end, end)
      }}
      className={`resize-none rounded border border-transparent bg-transparent px-1 py-0.5 leading-relaxed outline-none transition-colors ${readOnly ? 'cursor-default' : 'hover:border-slate-200 focus:border-blue-400 focus:bg-white'} ${
        fit ? 'w-auto max-w-full min-w-24' : 'w-full'
      } ${className}`}
    />
    {pendingSelection && commentTarget && (
      <button
        type="button"
        data-comment-selection-trigger
        onMouseDown={(event) => event.preventDefault()}
        onClick={() => {
          commentInteraction.select({ ...commentTarget, title: local, selection: pendingSelection })
          commentInteraction.setPendingSelection(undefined)
          ref.current?.setSelectionRange(pendingSelection.end, pendingSelection.end)
        }}
        className="shrink-0 self-start whitespace-nowrap rounded-md bg-slate-900 px-2 py-1 text-[10px] font-medium text-white shadow-sm hover:bg-indigo-700"
      >评论选中文字</button>
    )}
    {!pendingSelection && selectionThreads.length > 0 && (
      <button
        type="button"
        title="查看划词评论"
        onClick={() => commentInteraction.select(commentTargetFromThread(selectionThreads[0]))}
        className="shrink-0 self-start whitespace-nowrap rounded-md bg-amber-50 px-1.5 py-1 text-[9px] font-medium text-amber-700 ring-1 ring-amber-200 hover:bg-amber-100"
      >{selectionThreads.length} 处划词</button>
    )}
  </>
}

function guessTitle(url: string) {
  if (url.includes('/wiki/')) return '飞书知识库文档'
  if (url.includes('/docx/')) return '飞书文档'
  if (url.includes('/sheets/')) return '飞书表格'
  if (url.includes('meego')) return 'Meego 需求'
  try {
    return new URL(url).hostname.replace('www.', '')
  } catch {
    return '链接'
  }
}

export function Links({
  value,
  onChange,
  readOnly = false,
}: {
  value: DocLink[]
  onChange: (v: DocLink[]) => void
  readOnly?: boolean
}) {
  const [open, setOpen] = useState(false)
  const [url, setUrl] = useState('')
  const [title, setTitle] = useState('')

  function add() {
    if (!url.trim()) return
    onChange([...value, { id: uid('d'), title: title.trim() || guessTitle(url), url: url.trim() }])
    setUrl('')
    setTitle('')
    setOpen(false)
  }

  return (
    <>
      {value.map((d) => (
        <span
          key={d.id}
          className="group/link inline-flex max-w-64 items-center gap-1 rounded bg-blue-50 px-1 py-0.5 align-middle text-[11px] leading-4 text-blue-700 ring-1 ring-inset ring-blue-200"
        >
          <a href={d.url} target="_blank" rel="noreferrer" className="truncate hover:underline">
            {d.title}
          </a>
          {!readOnly && (
            <button
              type="button"
              onClick={() => onChange(value.filter((v) => v.id !== d.id))}
              className="text-blue-300 opacity-0 group-hover/link:opacity-100 hover:text-red-500"
            >
              ×
            </button>
          )}
        </span>
      ))}
      {!readOnly && <span className="relative inline-block align-middle">
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          className={`rounded px-1 text-[11px] text-slate-300 transition-opacity hover:bg-slate-100 hover:text-slate-600 ${
            open ? '' : 'opacity-0 group-hover/entry:opacity-100'
          }`}
        >
          + 链接
        </button>
        <Popover open={open} onClose={() => setOpen(false)} width={248}>
          <div className="space-y-1 p-1">
            <input
              autoFocus
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && add()}
              placeholder="粘贴飞书文档 / Meego 链接"
              className="w-full rounded border border-slate-200 px-1.5 py-1 text-xs outline-none focus:border-blue-400"
            />
            <input
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && add()}
              placeholder="标题（留空自动识别）"
              className="w-full rounded border border-slate-200 px-1.5 py-1 text-xs outline-none focus:border-blue-400"
            />
            <button
              type="button"
              onClick={add}
              className="w-full rounded bg-blue-600 py-1 text-xs font-medium text-white hover:bg-blue-700"
            >
              添加
            </button>
          </div>
        </Popover>
      </span>}
    </>
  )
}

const DEFAULT_IMAGE_WIDTH = 180

export function usePastedImageUpload(onUploaded: (images: ImageRef[]) => void, disabled = false) {
  const [uploading, setUploading] = useState(false)
  const [uploadError, setUploadError] = useState('')
  const [failedFiles, setFailedFiles] = useState<File[]>([])

  const upload = useCallback(async (files: File[]) => {
    if (files.length === 0 || disabled || uploading) return
    setUploading(true)
    setUploadError('')
    setFailedFiles([])
    try {
      const results = await Promise.allSettled(files.map(uploadImage))
      const uploaded = results.flatMap((result) => result.status === 'fulfilled' ? [result.value] : [])
      const failed = files.filter((_, index) => results[index]?.status === 'rejected')
      if (uploaded.length > 0) onUploaded(uploaded)
      if (failed.length > 0) {
        const firstFailure = results.find((result) => result.status === 'rejected')
        const reason = firstFailure?.status === 'rejected'
          ? firstFailure.reason instanceof Error ? firstFailure.reason.message : String(firstFailure.reason)
          : ''
        setFailedFiles(failed)
        setUploadError(`${failed.length} 张图片上传失败${reason ? `：${reason}` : ''}`)
      }
    } catch (error) {
      setFailedFiles(files)
      setUploadError(error instanceof Error ? error.message : '图片上传失败')
    } finally {
      setUploading(false)
    }
  }, [disabled, onUploaded, uploading])

  const onPaste = useCallback((event: ClipboardEvent<HTMLElement>) => {
    const files = imageFilesFromClipboard(event.clipboardData.files)
    if (files.length === 0 || disabled) return
    event.preventDefault()
    if (uploading) {
      setUploadError('已有图片正在上传，请稍候')
      return
    }
    void upload(files)
  }, [disabled, upload, uploading])

  return {
    onPaste,
    uploading,
    uploadError,
    canRetry: failedFiles.length > 0 && !uploading,
    retry: () => void upload(failedFiles),
  }
}

/**
 * 一张图。外层用原生 CSS resize，拖右下角就能改大小；
 * 尺寸经 ResizeObserver 回写，所以刷新之后还是你调好的大小。
 */
function ResizableImage({
  image,
  onResize,
  onRemove,
  onMoveLeft,
  onMoveRight,
  onZoom,
  readOnly,
  maxDisplayWidth,
}: {
  image: ImageRef
  onResize: (width: number) => void
  onRemove: () => void
  onMoveLeft?: () => void
  onMoveRight?: () => void
  onZoom: () => void
  readOnly: boolean
  maxDisplayWidth?: number
}) {
  const boxRef = useRef<HTMLSpanElement>(null)
  const committed = useRef(image.width ?? DEFAULT_IMAGE_WIDTH)

  useEffect(() => {
    const el = boxRef.current
    if (!el || readOnly) return
    let timer = 0
    const ro = new ResizeObserver(() => {
      const w = Math.round(el.getBoundingClientRect().width)
      if (w === 0 || Math.abs(w - committed.current) < 2) return
      // 拖拽过程中连续触发，停手 300ms 才落一次
      window.clearTimeout(timer)
      timer = window.setTimeout(() => {
        committed.current = w
        onResize(w)
      }, 300)
    })
    ro.observe(el)
    return () => {
      window.clearTimeout(timer)
      ro.disconnect()
    }
  }, [onResize, readOnly])

  return (
    <span
      ref={boxRef}
      style={{ width: maxDisplayWidth ? Math.min(image.width ?? DEFAULT_IMAGE_WIDTH, maxDisplayWidth) : image.width ?? DEFAULT_IMAGE_WIDTH }}
      title={readOnly ? image.name : '拖右下角改大小'}
      className={`group/img relative inline-block max-w-full min-w-12 overflow-hidden rounded border border-slate-200 align-top ${readOnly ? '' : 'resize-x'}`}
    >
      <img src={image.url} alt={image.name} className="block w-full" />
      <button
        type="button"
        onClick={onZoom}
        title="查看原图"
        className="absolute top-0.5 left-0.5 hidden size-4 items-center justify-center rounded bg-slate-900/55 text-[9px] text-white group-hover/img:flex"
      >
        ⤢
      </button>
      {!readOnly && (
        <>
          <button
            type="button"
            onClick={onRemove}
            title="删除图片"
            className="absolute top-0.5 right-0.5 hidden size-4 items-center justify-center rounded bg-slate-900/55 text-[10px] text-white group-hover/img:flex"
          >
            ×
          </button>
          {(onMoveLeft || onMoveRight) && (
            <span className="absolute bottom-0.5 left-0.5 hidden items-center gap-0.5 group-hover/img:flex">
              {onMoveLeft && <button type="button" onClick={onMoveLeft} title="向前移动" className="flex size-4 items-center justify-center rounded bg-slate-900/55 text-[10px] text-white">‹</button>}
              {onMoveRight && <button type="button" onClick={onMoveRight} title="向后移动" className="flex size-4 items-center justify-center rounded bg-slate-900/55 text-[10px] text-white">›</button>}
            </span>
          )}
        </>
      )}
    </span>
  )
}

/** 支持直接 ⌘V 粘贴截图，粘完可以拖着改大小 */
export function Images({
  value,
  onChange,
  readOnly = false,
  maxDisplayWidth,
  pasteEnabled = true,
}: {
  value: ImageRef[]
  onChange: (v: ImageRef[]) => void
  readOnly?: boolean
  maxDisplayWidth?: number
  pasteEnabled?: boolean
}) {
  const [zoom, setZoom] = useState<ImageRef | null>(null)
  const [focused, setFocused] = useState(false)
  const paste = usePastedImageUpload((uploaded) => onChange([...value, ...uploaded]), readOnly || !pasteEnabled)

  function move(from: number, to: number) {
    const next = [...value]
    const [image] = next.splice(from, 1)
    next.splice(to, 0, image)
    onChange(next)
  }

  return (
    <>
      <span
        onPaste={readOnly || !pasteEnabled ? undefined : paste.onPaste}
        onFocus={() => setFocused(true)}
        onBlur={() => setFocused(false)}
        tabIndex={readOnly || !pasteEnabled ? -1 : 0}
        className="inline-flex flex-wrap items-start gap-1 rounded align-top outline-none"
      >
        {value.map((img, index) => (
          <ResizableImage
            key={img.id}
            image={img}
            onResize={(width) =>
              onChange(value.map((v) => (v.id === img.id ? { ...v, width } : v)))
            }
            onRemove={() => onChange(value.filter((v) => v.id !== img.id))}
            onMoveLeft={index > 0 ? () => move(index, index - 1) : undefined}
            onMoveRight={index < value.length - 1 ? () => move(index, index + 1) : undefined}
            onZoom={() => setZoom(img)}
            readOnly={readOnly}
            maxDisplayWidth={maxDisplayWidth}
          />
        ))}
        {!readOnly && pasteEnabled && <span
          className={`rounded px-1 text-[11px] transition-opacity ${
            focused
              ? 'bg-blue-50 text-blue-600'
              : 'text-slate-300 opacity-0 group-hover/entry:opacity-100 hover:bg-slate-100 hover:text-slate-600'
          }`}
        >
          {paste.uploading ? '上传中…' : focused ? '⌘V 粘贴截图' : '+ 图'}
        </span>}
        {!readOnly && pasteEnabled && paste.uploadError && <span className="text-[11px] text-red-500" title={paste.uploadError}>
          上传失败{paste.canRetry && <button type="button" onClick={paste.retry} className="ml-1 underline">重试</button>}
        </span>}
      </span>

      {zoom && (
        <div
          onClick={() => setZoom(null)}
          className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/70 p-8"
        >
          <img src={zoom.url} alt={zoom.name} className="max-h-full max-w-full rounded shadow-2xl" />
        </div>
      )}
    </>
  )
}
