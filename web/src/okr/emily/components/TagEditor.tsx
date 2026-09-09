import { useState } from 'react'
import { tagLabel } from '../labels'
import type { KrTag } from '../types'

const TAG_TYPES = [
  { value: 'custom', label: '自定义' },
  { value: 'management_focus', label: '管理重点' },
  { value: 'biweekly', label: '双周会' },
  { value: 'platform_report', label: '中台周报' },
  { value: 'region', label: '区域' },
]

function tagTone(type: string) {
  switch (type) {
    case 'management_focus': return 'bg-amber-50 text-amber-700 ring-amber-100'
    case 'biweekly': return 'bg-violet-50 text-violet-700 ring-violet-100'
    case 'platform_report': return 'bg-indigo-50 text-indigo-700 ring-indigo-100'
    case 'region': return 'bg-sky-50 text-sky-700 ring-sky-100'
    default: return 'bg-emerald-50 text-emerald-700 ring-emerald-100'
  }
}

export function TagChip({ tag, onRemove }: { tag: KrTag; onRemove?: () => void }) {
  const label = tagLabel(tag.type, tag.value)
  return (
    <span title={label} className={`group/tag inline-flex min-h-5 max-w-full items-start gap-1 rounded-md px-1.5 py-0.5 text-[10px] leading-4 ring-1 ring-inset ${tagTone(tag.type)}`}>
      <span className="min-w-0 whitespace-normal break-words [overflow-wrap:anywhere]">{label}</span>
      {onRemove && <button type="button" onClick={onRemove} title="移除标签" className="-mr-0.5 shrink-0 text-current opacity-40 transition-opacity hover:opacity-100 group-hover/tag:opacity-70">×</button>}
    </span>
  )
}

export function TagEditor({ idPrefix, tags, suggestions, onAdd, onRemove, emptyLabel = '+ 添加标签', readOnly = false }: {
  idPrefix: string
  tags: KrTag[]
  suggestions: KrTag[]
  onAdd: (value: string, type: string) => void
  onRemove: (type: string, value: string) => void
  emptyLabel?: string
	readOnly?: boolean
}) {
  const [editing, setEditing] = useState(false)
  const [expanded, setExpanded] = useState(false)
  const [type, setType] = useState('custom')
  const [value, setValue] = useState('')
  const values = [...new Set(suggestions.filter((item) => item.type === type).map((item) => item.value))].sort()
  const visibleTags = expanded ? tags : tags.slice(0, 4)
  const hiddenCount = tags.length - visibleTags.length

  const submit = () => {
    const clean = value.trim()
    if (!clean) return
    onAdd(clean, type)
    setValue('')
    setEditing(false)
  }

  return (
    <div className="min-w-0">
      <div className="flex min-h-6 min-w-0 flex-wrap items-start gap-1">
		{visibleTags.map((tag) => <TagChip key={`${tag.type}:${tag.value}`} tag={tag} onRemove={readOnly ? undefined : () => onRemove(tag.type, tag.value)} />)}
        {hiddenCount > 0 && <button type="button" onClick={() => setExpanded(true)} className="h-5 rounded-md bg-slate-50 px-1.5 text-[10px] text-slate-400 hover:bg-slate-100 hover:text-slate-600">+{hiddenCount}</button>}
        {expanded && tags.length > 4 && <button type="button" onClick={() => setExpanded(false)} className="h-5 px-1 text-[10px] text-slate-400 hover:text-slate-600">收起</button>}
		{!readOnly && !editing && <button type="button" onClick={() => setEditing(true)} className="h-6 rounded-md border border-dashed border-slate-300 px-2 text-[10px] font-medium text-slate-500 hover:border-blue-300 hover:bg-blue-50 hover:text-blue-600">{emptyLabel}</button>}
      </div>
      {editing && (
        <div className="mt-1.5 flex max-w-2xl flex-wrap items-center gap-1 rounded-md bg-slate-50 p-1">
          <select value={type} onChange={(event) => setType(event.target.value)} className="h-7 rounded border border-slate-200 bg-white px-1.5 text-[10px] text-slate-600 outline-none focus:border-blue-400">
            {TAG_TYPES.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
          </select>
          <input list={`${idPrefix}-${type}`} autoFocus value={value} onChange={(event) => setValue(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') submit(); if (event.key === 'Escape') setEditing(false) }} placeholder={type === 'region' ? '如 sea' : '标签值'} className="h-7 min-w-48 flex-1 rounded border border-slate-200 bg-white px-2 text-[10px] outline-none focus:border-blue-400" />
          <datalist id={`${idPrefix}-${type}`}>{values.map((item) => <option key={item} value={item} />)}</datalist>
          <button type="button" onClick={submit} disabled={!value.trim()} className="h-7 rounded bg-slate-800 px-2 text-[10px] font-medium text-white hover:bg-slate-700 disabled:opacity-30">添加</button>
          <button type="button" onClick={() => setEditing(false)} className="h-7 px-1.5 text-[10px] text-slate-400 hover:text-slate-600">取消</button>
        </div>
      )}
    </div>
  )
}
