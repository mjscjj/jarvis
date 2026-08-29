import { useState } from 'react'
import { useBoard } from '../board'
import { MeegoBatchPreview } from './MeegoBatchPreview'
import { ReminderPreview } from './ReminderPreview'

type Tool = 'meego' | 'reminder'

const TOOLS: Array<{ value: Tool; mark: string; label: string; description: string }> = [
  { value: 'meego', mark: 'M', label: 'Meego 差异', description: '只读核对进度' },
  { value: 'reminder', mark: '催', label: '催办预览', description: '检查未填写项' },
]

export function WeeklyTools({ onOpenPoint }: { onOpenPoint: (pointId: string) => void }) {
  const { quarter, week } = useBoard()
  const [open, setOpen] = useState(false)
  const [tool, setTool] = useState<Tool>()
  const toggleTool = (next: Tool) => {
    setOpen(true)
    setTool((current) => current === next ? undefined : next)
  }

  return (
    <section className="mb-3 overflow-hidden rounded-xl border border-slate-200/80 bg-white shadow-[0_1px_2px_rgba(15,23,42,0.04)]">
      <button type="button" onClick={() => { setOpen((current) => !current); if (open) setTool(undefined) }} className={`flex w-full items-center gap-3 px-3.5 py-2 text-left hover:bg-slate-50 ${open ? 'border-b border-slate-100' : ''}`}>
        <div><h2 className="text-xs font-semibold text-slate-800">周报工具</h2><p className="mt-0.5 text-[10px] text-slate-400">Meego 原始差异与催填事实</p></div>
        <span className={`ml-auto text-xs text-slate-400 transition-transform ${open ? 'rotate-180' : ''}`}>⌄</span>
      </button>
      {open && <div className="grid grid-cols-2 gap-px bg-slate-100 sm:max-w-md">
        {TOOLS.map((item) => <button key={item.value} type="button" onClick={() => toggleTool(item.value)} className={`group flex min-w-0 items-center gap-2 bg-white px-3 py-2 text-left hover:bg-slate-50 ${tool === item.value ? 'relative z-10 bg-blue-50/70 ring-1 ring-inset ring-blue-200' : ''}`}><span className={`flex size-7 shrink-0 items-center justify-center rounded-lg text-[10px] font-semibold ${tool === item.value ? 'bg-blue-600 text-white' : 'bg-slate-100 text-slate-500'}`}>{item.mark}</span><span className="min-w-0"><span className="block truncate text-[11px] font-medium text-slate-700">{item.label}</span><span className="block truncate text-[9px] text-slate-400">{item.description}</span></span></button>)}
      </div>}
      {open && tool && <div className="border-t border-slate-100 p-3">
        {tool === 'meego' && <MeegoBatchPreview quarter={quarter} week={week} onClose={() => setTool(undefined)} onOpenPoint={onOpenPoint} />}
        {tool === 'reminder' && <ReminderPreview quarter={quarter} week={week} onClose={() => setTool(undefined)} />}
      </div>}
    </section>
  )
}
