import { useBoard } from '../board'
import { quarterOptions } from '../quarterCatalog'

export function QuarterSelect() {
  const { quarter, availableQuarters, setQuarter, syncState } = useBoard()
  const quarters = quarterOptions(quarter, availableQuarters)
  const disabled = syncState.kind === 'loading' || syncState.kind === 'saving' || syncState.kind === 'conflict'

  return (
    <div className="flex h-8 items-center rounded-full border border-slate-200 bg-white px-2.5 text-[11px] shadow-[0_1px_2px_rgba(15,23,42,0.03)]">
      <select
        aria-label="OKR 季度"
        value={quarter}
        onChange={(event) => setQuarter(event.target.value)}
        disabled={disabled || quarters.length === 0}
        className="bg-transparent font-medium text-slate-600 outline-none disabled:cursor-wait disabled:text-slate-400"
      >
        {quarters.map((item) => <option key={item} value={item}>{item.replace('-', ' ')}</option>)}
      </select>
    </div>
  )
}
