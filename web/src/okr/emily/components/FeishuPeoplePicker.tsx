import { useEffect, useMemo, useRef, useState } from 'react'
import { searchPeople } from '../api'
import { useBoard } from '../board'
import { addOrResolveOwner, joinOwnerNames, ownerIdentityKey, ownerOptions, splitOwnerNames } from '../people'
import type { Kr, KrOwner, PersonSearchItem } from '../types'

export function FeishuPeoplePickerInput({ owners, options, onChange }: { owners: KrOwner[]; options: KrOwner[]; onChange: (owners: KrOwner[]) => void }) {
  const root = useRef<HTMLSpanElement>(null)
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<PersonSearchItem[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
	const [hasMore, setHasMore] = useState(false)
	const selectedOpenIds = useMemo(() => new Set(owners.map((owner) => owner.openId).filter(Boolean)), [owners])

  const localResults = useMemo(() => options
		.filter((item) => item.openId && !selectedOpenIds.has(item.openId) && (!query.trim() || item.name.toLowerCase().includes(query.trim().toLowerCase())))
		.slice(0, 6)
		.map((owner) => ({ openId: owner.openId, name: owner.name, department: '当前 OKR 负责人', email: '', isExternal: false, hasChatted: false })), [options, query, selectedOpenIds])

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
			setHasMore(false)
      return
    }
    const controller = new AbortController()
		setResults([])
		setHasMore(false)
    const timer = window.setTimeout(() => {
      setLoading(true)
      setError('')
      searchPeople(clean, controller.signal)
			.then((value) => {
				setResults(value.users.filter((item) => !selectedOpenIds.has(item.openId)))
				setHasMore(value.hasMore)
			})
        .catch((reason) => {
					if (!controller.signal.aborted) {
						setResults([])
						setHasMore(false)
						setError(reason instanceof Error ? reason.message : '飞书人员搜索失败')
					}
        })
        .finally(() => {
          if (!controller.signal.aborted) setLoading(false)
        })
    }, 280)
    return () => {
      window.clearTimeout(timer)
      controller.abort()
    }
  }, [open, query, selectedOpenIds])

  const add = (person: PersonSearchItem) => {
		onChange(addOrResolveOwner(owners, { openId: person.openId, name: person.name }))
    setQuery('')
    setOpen(false)
  }

  const remove = (index: number) => {
		onChange(owners.filter((_, ownerIndex) => ownerIndex !== index))
  }

  const visibleResults = query.trim() ? results : localResults

  return (
    <span ref={root} className="relative inline-flex shrink-0 flex-wrap items-center gap-1">
      {owners.map((owner, index) => (
			<span key={`${ownerIdentityKey(owner)}:${index}`} title={owner.openId ? undefined : '身份未解析，请搜索飞书联系人后重新选择'} className={`group/person inline-flex h-5 items-center gap-1 rounded-full pr-1.5 pl-1 text-[10px] ring-1 ${owner.openId ? 'bg-slate-50 text-slate-600 ring-slate-200' : 'bg-amber-50 text-amber-700 ring-amber-200'}`}>
				<span className={`flex size-3.5 items-center justify-center rounded-full text-[8px] font-semibold text-white ${owner.openId ? 'bg-slate-300' : 'bg-amber-400'}`}>{owner.name.slice(0, 1)}</span>
				{owner.name}
				<button type="button" onClick={() => remove(index)} title="移除人员" className="text-slate-300 hover:text-red-500">×</button>
			</span>
      ))}
      <button type="button" onClick={() => setOpen((value) => !value)} className="h-6 rounded-md border border-dashed border-slate-300 px-2 text-[10px] font-medium text-slate-400 hover:border-blue-300 hover:bg-blue-50 hover:text-blue-600">
        + 人员
      </button>
      {open && (
        <span className="absolute top-[calc(100%+4px)] right-0 z-50 w-80 overflow-hidden rounded-lg border border-slate-200 bg-white text-left shadow-xl">
          <span className="block border-b border-slate-100 p-2">
            <span className="mb-1.5 flex items-center gap-1.5 text-[10px] font-medium text-slate-500"><span className="size-1.5 rounded-full bg-blue-500" />飞书联系人</span>
			<input autoFocus value={query} onChange={(event) => setQuery(event.target.value)} onKeyDown={(event) => { if (event.key === 'Escape') setOpen(false) }} placeholder="输入姓名或邮箱搜索" className="h-8 w-full rounded-md border border-slate-200 bg-slate-50 px-2.5 text-[11px] text-slate-700 outline-none focus:border-blue-400 focus:bg-white" />
          </span>
          <span className="block max-h-64 overflow-auto p-1">
            {loading && <span className="block px-2 py-3 text-center text-[10px] text-slate-400">正在搜索飞书联系人…</span>}
            {!loading && visibleResults.map((person) => (
              <button key={person.openId || person.name} type="button" onClick={() => add(person)} className="flex w-full items-center gap-2 rounded-md px-2 py-2 text-left hover:bg-slate-50">
                <span className="flex size-7 shrink-0 items-center justify-center rounded-full bg-slate-200 text-[10px] font-semibold text-slate-600">{person.name.slice(0, 1)}</span>
                <span className="min-w-0">
				  <span className="block text-[11px] font-medium text-slate-700">{person.name}{person.isExternal && <span className="ml-1 text-[9px] font-normal text-amber-600">外部</span>}</span>
				  <span className="block truncate text-[9px] text-slate-400">{[person.department, person.email].filter(Boolean).join(' · ') || '飞书用户'}</span>
                </span>
              </button>
            ))}
			{!loading && query.trim() && visibleResults.length === 0 && !error && <span className="block px-2 py-3 text-center text-[10px] text-slate-400">未找到飞书联系人，请补全姓名或改用邮箱</span>}
			{!loading && hasMore && !error && <span className="block px-2 py-2 text-[10px] leading-4 text-amber-600">结果较多，请补全姓名或改用邮箱缩小范围</span>}
			{!loading && error && <span className="block px-2 py-2 text-[10px] leading-4 text-red-600">{error}</span>}
          </span>
        </span>
      )}
    </span>
  )
}

export function FeishuPeoplePicker({ kr }: { kr: Kr }) {
	const { objectives, setKrOwner } = useBoard()
	const options = useMemo(() => ownerOptions(objectives), [objectives])
	const people = useMemo(() => splitOwnerNames(kr.ownerName), [kr.ownerName])
	const owners = useMemo<KrOwner[]>(() => {
		if (kr.owners?.length) return kr.owners
		return people.map((name, index) => ({ name, openId: index === 0 ? (kr.ownerOpenId ?? '') : '' }))
	}, [kr.ownerOpenId, kr.owners, people])
	return <FeishuPeoplePickerInput owners={owners} options={options} onChange={(nextOwners) => {
		const nextNames = joinOwnerNames(nextOwners.map((owner) => owner.name))
		setKrOwner(kr.id, nextNames, nextOwners[0]?.openId ?? '', nextOwners)
	}} />
}
