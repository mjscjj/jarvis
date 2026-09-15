import type { PersonSearchItem } from '../types'
import { PersonAvatar } from './PersonAvatar'

export function PersonSearchResults({
  people,
  loading,
  error,
  emptyMessage,
  showEmpty,
  hasMore = false,
  activeIndex,
  avatarUrls,
  keepInputFocus = false,
  onSelect,
}: {
  people: PersonSearchItem[]
  loading: boolean
  error: string
  emptyMessage: string
  showEmpty: boolean
  hasMore?: boolean
  activeIndex?: number
  avatarUrls?: Readonly<Record<string, string>>
  keepInputFocus?: boolean
  onSelect: (person: PersonSearchItem) => void
}) {
  if (loading) return <div className="px-2 py-3 text-center text-[10px] text-slate-400">正在搜索飞书联系人…</div>
  return (
    <>
      {people.map((person, index) => (
        <button
          key={person.email || person.name}
          type="button"
          disabled={person.isExternal}
          onMouseDown={keepInputFocus ? (event) => event.preventDefault() : undefined}
          onClick={() => onSelect(person)}
          className={`flex w-full items-center gap-2 rounded-md px-2 py-2 text-left disabled:cursor-not-allowed disabled:opacity-50 ${activeIndex === index ? 'bg-indigo-50' : 'hover:bg-slate-50'}`}
        >
          <PersonAvatar name={person.name} email={person.email} ownUrl={avatarUrls?.[person.email]} size="size-7 text-[10px]" tone="bg-slate-400" />
          <span className="min-w-0 flex-1">
            <span className="block text-[11px] font-medium text-slate-700">
              {person.name}
              {person.isExternal && <span className="ml-1 text-[9px] font-normal text-amber-600">外部，暂不可选择</span>}
            </span>
            <span className="block truncate text-[9px] text-slate-400">{[person.department, person.email].filter(Boolean).join(' · ') || '飞书用户'}</span>
          </span>
        </button>
      ))}
      {showEmpty && people.length === 0 && !error && <div className="px-2 py-3 text-center text-[10px] text-slate-400">{emptyMessage}</div>}
      {hasMore && !error && <div className="px-2 py-2 text-[10px] leading-4 text-amber-600">结果较多，请补全姓名或改用邮箱缩小范围</div>}
      {error && <div className="px-2 py-2 text-[10px] leading-4 text-red-600">{error}</div>}
    </>
  )
}
