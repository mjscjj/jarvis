import { createRoot } from 'react-dom/client'
import { UsageGuideButton } from '../../src/okr/emily/components/UsageGuide'
import '../../src/styles.css'
import '../../src/okr/emily/index.css'

createRoot(document.getElementById('root')!).render(
  <div id="okr-workspace-root" className="min-h-screen bg-slate-50 p-6">
    <div className="flex w-fit items-center gap-2">
      <UsageGuideButton initialScene="plan" />
      <button type="button" className="h-8 rounded-lg border border-slate-200 bg-white px-2.5 text-[10px] text-slate-600">建议反馈</button>
    </div>
  </div>,
)
