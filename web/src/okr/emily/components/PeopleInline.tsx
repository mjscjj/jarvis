import { PersonAvatar } from './PersonAvatar'

type DisplayPerson = { name: string; email?: string }

export function PersonInline({ person, compact = false }: { person: DisplayPerson; compact?: boolean }) {
  return (
    <span title={person.name} className={`inline-flex min-w-0 items-center gap-1 ${compact ? 'text-[9px]' : 'text-[10px]'}`}>
      <PersonAvatar name={person.name} email={person.email} size={compact ? 'size-3 text-[7px]' : 'size-4 text-[8px]'} />
      <span className="min-w-0 truncate">{person.name}</span>
    </span>
  )
}

export function PeopleInline({ people, empty = '未填写', compact = false, chips = false }: {
  people: DisplayPerson[]
  empty?: string
  compact?: boolean
  chips?: boolean
}) {
  if (people.length === 0) return <span>{empty}</span>
  return (
    <span className="inline-flex flex-wrap items-center gap-1">
      {people.map((person, index) => (
        <span key={`${person.email || person.name}:${index}`} className={chips ? 'inline-flex items-center rounded-full border border-slate-200 bg-white py-0.5 pr-2 pl-1' : 'inline-flex items-center'}>
          <PersonInline person={person} compact={compact} />
        </span>
      ))}
    </span>
  )
}
