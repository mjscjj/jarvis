import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useFeishuPeopleSearch } from '../../../useFeishuPeopleSearch'
import { searchOKRPeople } from '../api'
import { useBoard } from '../board'
import { addOrResolveOwner, joinOwnerNames, ownerIdentityKey, ownerOptions, splitOwnerNames } from '../people'
import type { Kr, KrOwner, PersonSearchItem, Point } from '../types'
import { PersonAvatar } from './PersonAvatar'
import { PersonSearchResults } from './PersonSearchResults'

export function FeishuPeoplePickerInput({ owners, options, onChange, compact = false, small = false, preferredDepartmentKeywords = [] }: { owners: KrOwner[]; options: KrOwner[]; onChange: (owners: KrOwner[]) => void; compact?: boolean; small?: boolean; preferredDepartmentKeywords?: string[] }) {
  const root = useRef<HTMLSpanElement>(null)
  const panel = useRef<HTMLSpanElement>(null)
  const input = useRef<HTMLInputElement>(null)
  const [open, setOpen] = useState(false)
	const peopleSearch = useFeishuPeopleSearch({ searchFn: searchOKRPeople, active: open, debounceMs: 280 })
	const [resultAvatars, setResultAvatars] = useState<Record<string, string>>({})
	const [panelStyle, setPanelStyle] = useState<React.CSSProperties>({ position: 'fixed', top: 0, left: 0 })
	const selectedOpenIds = useMemo(() => new Set(owners.map((owner) => owner.email).filter(Boolean)), [owners])
	const remoteResults = useMemo<PersonSearchItem[]>(() => peopleSearch.candidates.map((person) => ({
		email: person.email,
		name: person.name,
		department: person.department,
        unionId: person.union_id,
		isExternal: person.is_external,
		hasChatted: person.has_chatted,
	})).sort((left, right) => {
		const rank = (person: PersonSearchItem) => {
			const department = person.department.toLowerCase()
			const index = preferredDepartmentKeywords.findIndex((keyword) => department.includes(keyword.toLowerCase()))
			return index < 0 ? preferredDepartmentKeywords.length : index
		}
		return rank(left) - rank(right)
	}), [peopleSearch.candidates, preferredDepartmentKeywords])

  const localResults = useMemo(() => options
		.filter((item) => item.email && !selectedOpenIds.has(item.email) && (!peopleSearch.query.trim() || item.name.toLowerCase().includes(peopleSearch.query.trim().toLowerCase())))
		.slice(0, 6)
		.map((owner) => ({ email: owner.email, name: owner.name, department: '当前 OKR 负责人', isExternal: false, hasChatted: false })), [options, peopleSearch.query, selectedOpenIds])

  useEffect(() => {
    if (!open) return
    const onDown = (event: MouseEvent) => {
      const target = event.target as Node
      if (!root.current?.contains(target) && !panel.current?.contains(target)) setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [open])

	const placePanel = useCallback(() => {
		const rect = root.current?.getBoundingClientRect()
		if (!rect) return
		const width = 320
		const margin = 8
		const below = window.innerHeight - rect.bottom - margin
		const above = rect.top - margin
		const openAbove = below < 220 && above > below
		const maxHeight = Math.max(180, Math.min(360, openAbove ? above : below))
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
		// The portal must be fixed before it enters document.body. Otherwise its
		// focused input briefly lives at the end of the page and moves the window.
		placePanel()
		setOpen(true)
	}

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
	}, [open, owners.length, placePanel])

  useEffect(() => {
    setResultAvatars(Object.fromEntries(peopleSearch.candidates.filter(p => p.avatar_url).map(p => [p.email, p.avatar_url!])))
  }, [peopleSearch.candidates])

  const add = (person: PersonSearchItem) => {
		onChange(addOrResolveOwner(owners, { email: person.email, name: person.name, unionId: person.unionId }))
		peopleSearch.reset()
    setOpen(false)
  }

  const remove = (index: number) => {
		onChange(owners.filter((_, ownerIndex) => ownerIndex !== index))
  }

	const searching = Boolean(peopleSearch.query.trim())
	const visibleResults = searching ? remoteResults.filter((item) => !selectedOpenIds.has(item.email)) : localResults

  return (
    <span ref={root} className={`group/people relative inline-flex max-w-full min-w-0 items-center ${compact && !small ? '-space-x-1' : small ? 'flex-wrap gap-0.5' : 'flex-wrap gap-1'}`}>
      {owners.map((owner, index) => (
        small ? <button type="button" key={`${ownerIdentityKey(owner)}:${index}`} onClick={openPicker} title={owner.name} aria-label={`管理关联人：${owner.name}`} className="inline-flex min-h-4 max-w-full items-center gap-1 rounded px-0.5 py-0 !text-[8px] leading-3 text-slate-500 hover:bg-slate-100"><PersonAvatar name={owner.name} email={owner.email} size="size-3 text-[7px]" /><span className="min-w-0 break-words text-left">{owner.name}</span></button> : compact ? <span key={`${ownerIdentityKey(owner)}:${index}`} title={owner.name} className={`relative inline-flex size-6 items-center justify-center rounded-full ring-2 ring-white ${owner.email ? 'bg-slate-50' : 'bg-amber-50'}`}>
          <PersonAvatar name={owner.name} email={owner.email} size="size-5 text-[8px]" tone={owner.email ? 'bg-slate-300' : 'bg-amber-400'} />
        </span> : <span key={`${ownerIdentityKey(owner)}:${index}`} title={owner.email ? undefined : '身份未解析，请搜索飞书联系人后重新选择'} className={`group/person inline-flex h-5 items-center gap-1 rounded-full pr-1.5 pl-1 text-[10px] ring-1 ${owner.email ? 'bg-slate-50 text-slate-600 ring-slate-200' : 'bg-amber-50 text-amber-700 ring-amber-200'}`}>
          <PersonAvatar name={owner.name} email={owner.email} tone={owner.email ? 'bg-slate-300' : 'bg-amber-400'} />
          {owner.name}
          <button type="button" onClick={() => remove(index)} title="移除人员" className="text-slate-300 hover:text-red-500">×</button>
        </span>
      ))}
      <button type="button" onClick={() => { if (open) setOpen(false); else openPicker() }} title={compact || small ? '管理关联人' : undefined} aria-label={compact || small ? '管理关联人' : undefined} className={`${small ? 'size-4 shrink-0 rounded px-0 text-[10px]' : compact ? 'ml-2 size-6 rounded-full px-0 text-sm' : 'h-6 rounded-md px-2 text-[10px]'} border border-dashed border-slate-300 font-medium text-slate-400 hover:border-blue-300 hover:bg-blue-50 hover:text-blue-600`}>
        {compact || small ? '+' : '+ 人员'}
      </button>
      {compact && !small && owners.length > 0 && <span className="pointer-events-none invisible absolute top-full right-0 z-20 mt-1 whitespace-nowrap rounded-md bg-slate-800 px-2 py-1 text-[10px] text-white opacity-0 shadow-lg transition-opacity group-hover/people:visible group-hover/people:opacity-100">{owners.map((owner) => owner.name).join('、')}</span>}
      {open && createPortal(
        <span ref={panel} style={panelStyle} className="overflow-hidden rounded-lg border border-slate-200 bg-white text-left shadow-xl">
          <span className="block border-b border-slate-100 p-2">
            <span className="mb-1.5 flex items-center gap-1.5 text-[10px] font-medium text-slate-500"><span className="size-1.5 rounded-full bg-blue-500" />飞书联系人</span>
			{small && owners.length > 0 && <span className="flex flex-wrap gap-1 border-b border-slate-100 p-2">{owners.map((owner, index) => <span key={`${ownerIdentityKey(owner)}:${index}`} className="inline-flex items-center gap-1 rounded bg-slate-50 px-2 py-1 text-[11px]"><PersonAvatar name={owner.name} email={owner.email} />{owner.name}<button type="button" onClick={() => remove(index)} aria-label={`移除${owner.name}`} className="text-slate-400 hover:text-red-500">×</button></span>)}</span>}
          <input ref={input} value={peopleSearch.query} onChange={(event) => peopleSearch.setQuery(event.target.value)} onKeyDown={(event) => { if (event.key === 'Escape') setOpen(false) }} placeholder="输入姓名或邮箱搜索" className="h-8 w-full rounded-md border border-slate-200 bg-slate-50 px-2.5 text-[11px] text-slate-700 outline-none focus:border-blue-400 focus:bg-white" />
          </span>
          {compact && !small && owners.length > 0 && <span className="block border-b border-slate-100 p-2">
            <span className="mb-1.5 block text-[9px] text-slate-400">已关联</span>
            <span className="flex flex-wrap gap-1">{owners.map((owner, index) => <span key={`${ownerIdentityKey(owner)}:${index}`} className="inline-flex h-6 items-center gap-1 rounded-full bg-slate-50 pr-1 pl-1.5 text-[10px] text-slate-600 ring-1 ring-slate-200"><PersonAvatar name={owner.name} email={owner.email} />{owner.name}<button type="button" onClick={() => remove(index)} title={`移除${owner.name}`} className="text-slate-300 hover:text-red-500">×</button></span>)}</span>
          </span>}
          <span className="block max-h-64 overflow-auto p-1">
            <PersonSearchResults
              people={visibleResults}
              loading={peopleSearch.loading}
              error={peopleSearch.error}
              emptyMessage="未找到飞书联系人，请补全姓名或改用邮箱"
              showEmpty={peopleSearch.hasSearched}
              hasMore={peopleSearch.hasMore}
              avatarUrls={searching ? resultAvatars : undefined}
              onSelect={add}
            />
          </span>
        </span>,
        document.body,
      )}
    </span>
  )
}

export function FeishuPeoplePicker({ kr, compact = false, small = false }: { kr: Kr; compact?: boolean; small?: boolean }) {
	const { objectives, setKrOwner } = useBoard()
	const options = useMemo(() => ownerOptions(objectives), [objectives])
	const people = useMemo(() => splitOwnerNames(kr.ownerName), [kr.ownerName])
	const owners = useMemo<KrOwner[]>(() => {
		if (kr.owners?.length) return kr.owners
		return people.map((name, index) => ({ name, email: index === 0 ? (kr.ownerEmail ?? '') : '' }))
	}, [kr.ownerEmail, kr.owners, people])
	return <FeishuPeoplePickerInput owners={owners} options={options} compact={compact} small={small} onChange={(nextOwners) => {
		const nextNames = joinOwnerNames(nextOwners.map((owner) => owner.name))
		setKrOwner(kr.id, nextNames, nextOwners[0]?.email ?? '', nextOwners)
	}} />
}

export function PointPeoplePicker({ krId, point, small = false }: { krId: string; point: Point; small?: boolean }) {
	const { objectives, setPointOwners } = useBoard()
	const options = useMemo(() => ownerOptions(objectives), [objectives])
	return <FeishuPeoplePickerInput small={small} owners={point.owners ?? []} options={options} onChange={(nextOwners) => setPointOwners(krId, point.id, nextOwners)} />
}
