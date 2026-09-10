import { useEffect, useState } from 'react'
import { Alert, Button, Space, Typography } from 'antd'
import { bootstrapSetupWorldModel, getTask, rerunTask } from './api'
import type { Task } from './types'
import { worldModelProgress } from './onboardingState'
import MarkdownReport from './components/MarkdownReport'

const taskKey = 'jarvis.onboardingTaskId'
const errorText = (cause: unknown) => cause instanceof Error ? cause.message : String(cause)

// This observer lives beside the application, not in its installation gate.
// Task/run records remain the only truth; localStorage just remembers the ID.
export function WorldModelSetup({ worldModelReady }: { worldModelReady: boolean }) {
  const [taskId, setTaskId] = useState<number | null>(() => {
    const value = Number(localStorage.getItem(taskKey))
    return Number.isSafeInteger(value) && value > 0 ? value : null
  })
  const [task, setTask] = useState<Task | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [refresh, setRefresh] = useState(0)
  const [collapsed, setCollapsed] = useState(false)
  const [dismissed, setDismissed] = useState(false)

  useEffect(() => {
    if (taskId || worldModelReady) return
    let cancelled = false
    setError('')
    void bootstrapSetupWorldModel().then(result => {
      if (cancelled) return
      localStorage.setItem(taskKey, String(result.task_id))
      setTaskId(result.task_id)
    }).catch(cause => {
      if (!cancelled) setError(`初始化任务未能启动：${errorText(cause)}。你可以继续使用 Jarvis。`)
    })
    return () => { cancelled = true }
  }, [taskId, worldModelReady, refresh])

  useEffect(() => {
    if (!taskId) return
    const controller = new AbortController()
    let failures = 0
    let timer: number
    const poll = async () => {
      try {
        const next = await getTask(taskId, controller.signal)
        if (controller.signal.aborted) return
        failures = 0
        setTask(next); setError('')
        if (next.status === 'done') return
      } catch (cause) {
        if (controller.signal.aborted) return
        if (++failures >= 3) {
          setError(`进度读取失败：${errorText(cause)}。这不代表后台任务已经停止。`)
          return
        }
      }
      timer = window.setTimeout(() => void poll(), 2500)
    }
    void poll()
    return () => { controller.abort(); window.clearTimeout(timer) }
  }, [taskId, refresh])

  useEffect(() => {
    if (task?.status === 'done' || task?.status === 'needs_human' || task?.status === 'failed') setCollapsed(false)
  }, [task?.status])

  const continueTask = async () => {
    if (!task || busy) return
    setBusy(true); setError('')
    try {
      // A fresh attempt of the SAME task preserves source, prior runs and all
      // written entities. The Skill resumes from its persisted worknote.
      await rerunTask(task.id)
      setRefresh(value => value + 1)
    } catch (cause) { setError(`未能继续初始化：${errorText(cause)}`) }
    finally { setBusy(false) }
  }

  if (dismissed || (worldModelReady && !taskId)) return null
  const progress = task ? worldModelProgress(task) : { title: '工作背景正在准备', detail: '可以先使用 Jarvis，初始化任务会在后台继续。' }
  const failed = task?.status === 'failed' || task?.status === 'observing'
  const done = task?.status === 'done'
  const needsHuman = task?.status === 'needs_human'
  return <aside className="world-model-setup" aria-label="工作背景初始化">
    <div className="world-model-setup-heading">
      <Typography.Text strong>{progress.title}</Typography.Text>
      <Button type="text" size="small" onClick={() => setCollapsed(value => !value)}>{collapsed ? '展开' : '收起'}</Button>
    </div>
    {!collapsed && <>
      <div className="world-model-setup-detail"><MarkdownReport content={progress.detail} /></div>
      {!done && <Typography.Text type="secondary">后台处理不影响使用；这里只展示任务实际记录的进展。</Typography.Text>}
      {task?.last_progress_at && <Typography.Text type="secondary">最近进展：{new Date(task.last_progress_at).toLocaleString()}</Typography.Text>}
      {error && <Alert type="warning" showIcon message={error} />}
      <Space wrap>
        {taskId && <a href={`#/work/task/${taskId}`}>{needsHuman ? '回答并继续' : '查看任务与运行记录'}</a>}
        {failed && <Button size="small" loading={busy} onClick={() => void continueTask()}>从已有结果继续</Button>}
        {error && <Button size="small" onClick={() => setRefresh(value => value + 1)}>重新检查</Button>}
        {done && <Button size="small" onClick={() => { localStorage.removeItem(taskKey); setDismissed(true) }}>知道了</Button>}
      </Space>
    </>}
  </aside>
}
