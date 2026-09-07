import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from 'react'
import { createPortal } from 'react-dom'
import { ownerIdentityKey, rankKrOwnerSuggestions } from '../people'
import type { KrOwner } from '../types'
import { PersonAvatar } from './PersonAvatar'

const MAX_VISIBLE_CHIPS = 2
const MAX_SUGGESTIONS = 5
const RECENT_OWNER_STORAGE_KEY = 'jarvis.okr.owner-filter-recents.v1'

function readRecentKeys(): string[] {
  try {
    const value = JSON.parse(window.localStorage.getItem(RECENT_OWNER_STORAGE_KEY) ?? '[]')
    return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : []
  } catch {
    return []
  }
}

export function OwnerFilterPicker({ options, ownerCounts, selectedKeys, onChange }: {
  options: KrOwner[]
  ownerCounts: ReadonlyMap<string, number>
  selectedKeys: string[]
  onChange: (keys: string[]) => void
}) {
  const root = useRef<HTMLDivElement>(null)
  const panel = useRef<HTMLDivElement>(null)
  const input = useRef<HTMLInputElement>(null)
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [recentKeys, setRecentKeys] = useState<string[]>([])
  const [panelStyle, setPanelStyle] = useState<CSSProperties>({ top: 0, left: 0 })
  const selectedSet = useMemo(() => new Set(selectedKeys), [selectedKeys])
  const optionsByKey = useMemo(() => new Map(options.map((owner) => [ownerIdentityKey(owner), owner])), [options])
  const selectedOwners = useMemo(() => selectedKeys.flatMap((key) => {
    const owner = optionsByKey.get(key)
    return owner ? [owner] : []
  }), [optionsByKey, selectedKeys])
  const duplicateNames = useMemo(() => {
    const counts = new Map<string, number>()
    for (const owner of options) counts.set(owner.name, (counts.get(owner.name) ?? 0) + 1)
    return new Set([...counts].filter(([, count]) => count > 1).map(([name]) => name))
  }, [options])
  const visibleOptions = useMemo(() => {
    const clean = query.trim().toLocaleLowerCase()
    if (!clean) return rankKrOwnerSuggestions(options, recentKeys, ownerCounts, MAX_SUGGESTIONS)
    return options
      .filter((owner) => owner.name.toLocaleLowerCase().includes(clean) || owner.openId.toLocaleLowerCase().includes(clean))
      .slice(0, MAX_SUGGESTIONS)
  }, [options, ownerCounts, query, recentKeys])
  const hiddenSelectedCount = Math.max(0, selectedOwners.length - MAX_VISIBLE_CHIPS)

  useEffect(() => setRecentKeys(readRecentKeys()), [])

  const placePanel = useCallback(() => {
    const rect = root.current?.getBoundingClientRect()
    if (!rect) return
    const width = 280
    const margin = 8
    const below = window.innerHeight - rect.bottom - margin
    const above = rect.top - margin
    const openAbove = below < 240 && above > below
    const maxHeight = Math.max(180, Math.min(340, openAbove ? above : below))
    const left = Math.min(Math.max(margin, rect.right - width), Math.max(margin, window.innerWidth - width - margin))
    setPanelStyle({
      position: 'fixed',
      top: openAbove ? Math.max(margin, rect.top - maxHeight - 4) : rect.bottom + 4,
      left,
      width,
      maxHeight,
      zIndex: 1000,
    })
  }, [])

  const openPicker = () => {
    placePanel()
    setOpen(true)
  }

  const closePicker = () => {
    setOpen(false)
    setQuery('')
  }

  useEffect(() => {
    if (!open) return
    const onDown = (event: MouseEvent) => {
      const target = event.target as Node
      if (!root.current?.contains(target) && !panel.current?.contains(target)) closePicker()
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [open])

  useEffect(() => {
    if (!open) return
    const focusFrame = window.requestAnimationFrame(() => input.current?.focus({ preventScroll: true }))
    window.addEventListener('resize', placePanel)
    window.addEventListener('scroll', placePanel, true)
    return () => {
      window.cancelAnimationFrame(focusFrame)
      window.removeEventListener('resize', placePanel)
      window.removeEventListener('scroll', placePanel, true)
    }
  }, [open, placePanel, selectedOwners.length])

  const remember = (key: string) => {
    setRecentKeys((current) => {
      const next = [key, ...current.filter((item) => item !== key)].slice(0, MAX_SUGGESTIONS)
      try {
        window.localStorage.setItem(RECENT_OWNER_STORAGE_KEY, JSON.stringify(next))
      } catch {
        // 浏览器禁用本地存储时，只保留当前页面内的最近选择。
      }
      return next
    })
  }

  const toggle = (key: string) => {
    if (selectedSet.has(key)) {
      onChange(selectedKeys.filter((selected) => selected !== key))
      return
    }
    remember(key)
    onChange([...selectedKeys, key])
  }

  const remove = (key: string) => onChange(selectedKeys.filter((selected) => selected !== key))

  return (
    <div ref={root} className="inline-flex min-w-0 items-center gap-1">
      {selectedOwners.slice(0, MAX_VISIBLE_CHIPS).map((owner) => {
        const key = ownerIdentityKey(owner)
        return (
          <span key={key} className="group/owner-filter inline-flex h-7 shrink-0 items-center gap-1 rounded-full border border-slate-200 bg-slate-50 py-0.5 pr-1.5 pl-1 text-[11px] font-medium text-slate-600 shadow-sm">
            <PersonAvatar name={owner.name} openId={owner.openId} size="size-5 text-[9px]" tone="bg-slate-400" />
            <span className="max-w-20 truncate">{owner.name}</span>
            <button type="button" onClick={() => remove(key)} title={`移除${owner.name}筛选`} aria-label={`移除${owner.name}筛选`} className="ml-0.5 text-sm leading-none text-slate-300 hover:text-red-500">×</button>
          </span>
        )
      })}
      {hiddenSelectedCount > 0 && (
        <button type="button" onClick={openPicker} title={selectedOwners.slice(MAX_VISIBLE_CHIPS).map((owner) => owner.name).join('、')} className="inline-flex h-7 shrink-0 items-center rounded-full border border-slate-200 bg-slate-50 px-2 text-[10px] font-medium text-slate-500 shadow-sm hover:border-blue-200 hover:text-blue-600">
          +{hiddenSelectedCount}
        </button>
      )}
      <button
        type="button"
        onClick={() => { if (open) closePicker(); else openPicker() }}
        aria-haspopup="dialog"
        aria-expanded={open}
        className={`inline-flex h-7 shrink-0 items-center gap-1 rounded-full border border-dashed px-2.5 text-[11px] font-medium transition-colors ${open ? 'border-blue-400 bg-blue-50 text-blue-600' : 'border-slate-300 bg-white text-slate-400 hover:border-blue-300 hover:bg-blue-50 hover:text-blue-600'}`}
      >
        搜索负责人
      </button>
      {open && createPortal(
        <div ref={panel} role="dialog" aria-label="搜索负责人" style={panelStyle} className="flex overflow-hidden rounded-xl border border-slate-200 bg-white text-left shadow-lg">
          <div className="flex min-h-0 w-full flex-col">
            <div className="shrink-0 border-b border-slate-100 p-2.5">
              <div className="mb-1.5 flex items-center gap-1.5 text-[11px] font-medium text-slate-600"><span className="size-1.5 rounded-full bg-blue-500" />搜索负责人</div>
              <input
                ref={input}
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                onKeyDown={(event) => { if (event.key === 'Escape') closePicker() }}
                placeholder="输入姓名搜索"
                className="h-8 w-full rounded-md border border-slate-200 bg-white px-2.5 text-xs text-slate-700 outline-none placeholder:text-slate-400 focus:border-blue-400 focus:ring-2 focus:ring-blue-100"
              />
            </div>
            <div className="min-h-0 flex-1 overflow-auto p-1">
              {visibleOptions.map((owner) => {
                const key = ownerIdentityKey(owner)
                const selected = selectedSet.has(key)
                const identity = duplicateNames.has(owner.name) && owner.openId ? ` · ${owner.openId.slice(-6)}` : ''
                return (
                  <button key={key} type="button" onClick={() => toggle(key)} aria-pressed={selected} className={`flex w-full items-center gap-2.5 rounded-lg px-2 py-2 text-left transition-colors ${selected ? 'bg-slate-50' : 'hover:bg-slate-50'}`}>
                    <PersonAvatar name={owner.name} openId={owner.openId} size="size-8 text-xs" tone="bg-slate-400" />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-xs font-semibold text-slate-700">{owner.name}</span>
                      <span className="mt-0.5 block truncate text-[10px] text-slate-400">当前 OKR 负责人{identity}</span>
                    </span>
                    {selected && <span aria-hidden className="flex size-4 shrink-0 items-center justify-center rounded-full bg-blue-500 text-[9px] font-semibold text-white">✓</span>}
                  </button>
                )
              })}
              {visibleOptions.length === 0 && <div className="px-3 py-8 text-center text-xs text-slate-400">没有匹配的 OKR 负责人</div>}
            </div>
          </div>
        </div>,
        document.body,
      )}
    </div>
  )
}
