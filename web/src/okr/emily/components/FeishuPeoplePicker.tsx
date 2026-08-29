import { useEffect, useMemo, useRef, useState } from 'react'
import { searchPeople } from '../api'
import { useBoard } from '../board'
import { joinOwnerNames, splitOwnerNames } from '../people'
import type { Kr, KrOwner, PersonSearchItem } from '../types'

export function FeishuPeoplePicker({ kr, options }: { kr: Kr; options: string[] }) {
  const { setKrOwner } = useBoard()
  const root = useRef<HTMLSpanElement>(null)
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<PersonSearchItem[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const people = useMemo(() => splitOwnerNames(kr.ownerName), [kr.ownerName])
  const structuredOwners = useMemo<KrOwner[]>(() => {
    if (kr.owners?.length) return kr.owners
    return people.map((name, index) => ({ name, openId: index === 0 ? (kr.ownerOpenId ?? '') : '' }))
  }, [kr.ownerOpenId, kr.owners, people])

  const localResults = useMemo(() => options
    .filter((item) => !people.includes(item) && (!query.trim() || item.toLowerCase().includes(query.trim().toLowerCase())))
    .slice(0, 6)
    .map((name) => ({ openId: '', name, department: '当前 OKR 负责人' })), [options, people, query])

  useEffect(() => {
    if (!open) return
    const onDown = (event: MouseEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [open])

  useEffect(() => {
    const clean = query.trim()
    if (!open || !clean) {
      setResults([])
      setLoading(false)
      setError('')
      return
    }
    const controller = new AbortController()
    const timer = window.setTimeout(() => {
      setLoading(true)
      setError('')
      searchPeople(clean, controller.signal)
        .then((value) => setResults(value.users.filter((item) => !people.includes(item.name))))
        .catch((reason) => {
          if (!controller.signal.aborted) setError(reason instanceof Error ? reason.message : '飞书人员搜索失败')
        })
        .finally(() => {
          if (!controller.signal.aborted) setLoading(false)
        })
    }, 280)
    return () => {
      window.clearTimeout(timer)
      controller.abort()
    }
  }, [open, people, query])

  const add = (person: PersonSearchItem) => {
    const nextOwners = [...structuredOwners, { openId: person.openId, name: person.name }]
    const nextNames = joinOwnerNames(nextOwners.map((owner) => owner.name))
    setKrOwner(kr.id, nextNames, nextOwners[0]?.openId ?? '', nextOwners)
    setQuery('')
    setOpen(false)
  }

  const remove = (person: string) => {
    const nextOwners = structuredOwners.filter((owner) => owner.name !== person)
    setKrOwner(kr.id, joinOwnerNames(nextOwners.map((owner) => owner.name)), nextOwners[0]?.openId ?? '', nextOwners)
  }

  const addManual = () => {
    const clean = query.trim()
    if (!clean) return
    add({ openId: '', name: clean, department: '' })
  }

  const visibleResults = query.trim() ? results : localResults

  return (
    <span ref={root} className="relative inline-flex shrink-0 flex-wrap items-center gap-1">
      {people.map((person) => (
        <span key={person} className="group/person inline-flex h-5 items-center gap-1 rounded-full bg-slate-50 pr-1.5 pl-1 text-[10px] text-slate-600 ring-1 ring-slate-200">
          <span className="flex size-3.5 items-center justify-center rounded-full bg-slate-300 text-[8px] font-semibold text-white">{person.slice(0, 1)}</span>
          {person}
          <button type="button" onClick={() => remove(person)} title="移除人员" className="text-slate-300 hover:text-red-500">×</button>
        </span>
      ))}
      <button type="button" onClick={() => setOpen((value) => !value)} className="h-6 rounded-md border border-dashed border-slate-300 px-2 text-[10px] font-medium text-slate-400 hover:border-blue-300 hover:bg-blue-50 hover:text-blue-600">
        + 人员
      </button>
      {open && (
        <span className="absolute top-[calc(100%+4px)] right-0 z-50 w-80 overflow-hidden rounded-lg border border-slate-200 bg-white text-left shadow-xl">
          <span className="block border-b border-slate-100 p-2">
            <span className="mb-1.5 flex items-center gap-1.5 text-[10px] font-medium text-slate-500"><span className="size-1.5 rounded-full bg-blue-500" />飞书联系人</span>
            <input autoFocus value={query} onChange={(event) => setQuery(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter' && results.length === 0) addManual(); if (event.key === 'Escape') setOpen(false) }} placeholder="输入姓名或邮箱搜索" className="h-8 w-full rounded-md border border-slate-200 bg-slate-50 px-2.5 text-[11px] text-slate-700 outline-none focus:border-blue-400 focus:bg-white" />
          </span>
          <span className="block max-h-64 overflow-auto p-1">
            {loading && <span className="block px-2 py-3 text-center text-[10px] text-slate-400">正在搜索飞书联系人…</span>}
            {!loading && visibleResults.map((person) => (
              <button key={person.openId || person.name} type="button" onClick={() => add(person)} className="flex w-full items-center gap-2 rounded-md px-2 py-2 text-left hover:bg-slate-50">
                <span className="flex size-7 shrink-0 items-center justify-center rounded-full bg-slate-200 text-[10px] font-semibold text-slate-600">{person.name.slice(0, 1)}</span>
                <span className="min-w-0">
                  <span className="block text-[11px] font-medium text-slate-700">{person.name}</span>
                  <span className="block truncate text-[9px] text-slate-400">{person.department || '手动添加'}</span>
                </span>
              </button>
            ))}
            {!loading && query.trim() && visibleResults.length === 0 && !error && <span className="block px-2 py-3 text-center text-[10px] text-slate-400">未找到联系人，按回车可按姓名添加</span>}
            {!loading && error && <span className="block px-2 py-2 text-[10px] leading-4 text-amber-600">飞书搜索暂不可用；仍可输入姓名后按回车添加。</span>}
          </span>
        </span>
      )}
    </span>
  )
}
