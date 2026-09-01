import { useState } from 'react'
import type { WeeklyScore } from '../types'

const SCORE_OPTIONS = Array.from({ length: 11 }, (_, index) => (index / 10).toFixed(1))

export function WeeklyScoreControl({ score, onChange, readOnly = false, label = 'OKR 评分' }: { score?: WeeklyScore; onChange?: (score?: number) => Promise<void>; readOnly?: boolean; label?: string }) {
  const [busy, setBusy] = useState(false)
  const display = score ? score.value.toFixed(1) : '未评分'

  if (readOnly) {
    return <span aria-label={label} className="inline-flex h-6 shrink-0 items-center rounded-md border border-violet-200 bg-violet-50 px-1.5 text-[10px] font-semibold leading-none text-violet-700">评分 {display}</span>
  }

  return (
    <label className="inline-flex h-6 shrink-0 items-center gap-1 rounded-md border border-violet-200 bg-violet-50 px-1.5 text-[10px] font-semibold leading-none text-violet-700">
      <span>评分</span>
      <select
        aria-label={label}
        disabled={busy}
        value={score ? score.value.toFixed(1) : ''}
        onClick={(event) => event.stopPropagation()}
        onChange={(event) => {
          const next = event.target.value === '' ? undefined : Number(event.target.value)
          setBusy(true)
          void (onChange?.(next) ?? Promise.resolve())
            .catch(() => undefined)
            .finally(() => setBusy(false))
        }}
        className="bg-transparent text-[10px] font-semibold leading-none text-violet-700 outline-none disabled:opacity-50"
      >
        <option value="">未评分</option>
        {SCORE_OPTIONS.map((value) => <option key={value} value={value}>{value}</option>)}
      </select>
    </label>
  )
}
