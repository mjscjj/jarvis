import { Modal } from 'antd'
import { useEffect, useState } from 'react'
import { ManagementView } from './components/ManagementView'
import { QuarterSelect } from './components/QuarterSelect'
import { useBoard } from './board'

export default function CoreApp() {
  const { quarter, syncState, reset } = useBoard()
  const tone = syncState.kind === 'saving' ? 'text-blue-600' : syncState.kind === 'saved' ? 'text-emerald-600' : syncState.kind === 'error' || syncState.kind === 'conflict' ? 'text-red-600' : 'text-slate-400'
  const errorKey = syncState.kind === 'error' ? `${syncState.title ?? ''}\n${syncState.message}` : ''
  const [dismissedError, setDismissedError] = useState('')

  useEffect(() => {
    if (syncState.kind === 'ready' || syncState.kind === 'saved') setDismissedError('')
  }, [syncState.kind])

  const dismissError = () => setDismissedError(errorKey)

  return (
    <div className="min-h-full">
      <header className="sticky top-0 z-40 border-b border-slate-200/80 bg-white/95 backdrop-blur">
        <div className="mx-auto flex min-h-14 max-w-[1580px] items-center gap-3 px-4 py-2 sm:px-6 lg:px-8">
          <span className="flex size-8 items-center justify-center rounded-lg bg-indigo-600 text-xs font-semibold text-white shadow-sm">O</span>
          <div className="leading-tight"><h1 className="text-[14px] font-semibold tracking-tight text-slate-900">Emily · OKR</h1><div className="mt-1 text-[10px] text-slate-400">{quarter ? quarter.replace('-', ' ') : 'OKR'} · 目标与拆解</div></div>
          <span className={`ml-3 text-[10px] ${tone}`} aria-live="polite">{syncState.message}</span>
          <div className="ml-auto flex items-center gap-2">
            <QuarterSelect />
            <button type="button" onClick={reset} className="h-8 rounded-lg border border-slate-200 bg-white px-3 text-[10px] text-slate-500 hover:bg-slate-50">重新载入</button>
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-[1580px] px-4 py-3 sm:px-6 sm:py-4 lg:px-8">
        {(syncState.kind === 'error' || syncState.kind === 'conflict') && <div className="mb-3 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700">{syncState.message}</div>}
        <ManagementView />
      </main>
      <Modal
        title={syncState.kind === 'error' ? syncState.title ?? '暂时无法保存' : '暂时无法保存'}
        open={Boolean(errorKey && errorKey !== dismissedError)}
        onCancel={dismissError}
        footer={null}
        centered
        width={460}
      >
        <p className="mt-2 text-sm leading-6 text-slate-600">{syncState.kind === 'error' ? syncState.message : ''}</p>
        <div className="mt-4 flex justify-end">
          <button type="button" onClick={dismissError} className="rounded-lg bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700">知道了</button>
        </div>
      </Modal>
    </div>
  )
}
