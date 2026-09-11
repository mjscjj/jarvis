import { useEffect, useState } from 'react'
import { Alert, Button, Checkbox, Input, Modal, Select, Space, Spin, Table, Tabs, Tag, Typography, message } from 'antd'
import type { TableColumnsType } from 'antd'
import { createTask, getDelegation, listDelegations, listDelegationTasks, updateDelegation } from './api'
import type { Delegation, DelegationCheck } from './types'
import MergedPageHeader from './components/MergedPageHeader'
import MarkdownReport from './components/MarkdownReport'
import { usePageContext } from './pageContext'
import { taskStatusMeta } from './status'
import { checkInput, progressText, updatedProgress } from './delegations/presentation'

const errorText = (e: unknown) => e instanceof Error ? e.message : String(e)

export default function Delegations() {
  const { context, navigate, setViewState } = usePageContext()
  const state = context.view_state.state === 'closed' ? 'closed' : 'open'
  const page = Math.max(1, Number(context.view_state.page) || 1)
  const selectedID = Number(context.view_state.delegation) || null
  const [query, setQuery] = useState('')
  const [items, setItems] = useState<Delegation[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string>()
  const [refresh, setRefresh] = useState(0)
  const [detail, setDetail] = useState<Delegation>()
  const [detailLoading, setDetailLoading] = useState(false)
  const [checks, setChecks] = useState<DelegationCheck[]>([])
  const [checkPage, setCheckPage] = useState(1)
  const [checksMore, setChecksMore] = useState(false)
  const [edit, setEdit] = useState(false)
  const [note, setNote] = useState('')
  const [closed, setClosed] = useState(false)
  const [busy, setBusy] = useState(false)
  const [savingStatusID, setSavingStatusID] = useState<number>()
  const [messageApi, messageContext] = message.useMessage()

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    listDelegations(state, query, page, controller.signal)
      .then(result => {
        const lastPage = Math.max(1, Math.ceil(result.total / 20))
        if (page > lastPage) {
          setViewState({ mode: 'delegated', state, page: lastPage, delegation: selectedID ?? undefined })
          return
        }
        setItems(result.items); setTotal(result.total); setError(undefined)
      })
      .catch(e => { if (!controller.signal.aborted) setError(errorText(e)) })
      .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [state, query, page, refresh])

  useEffect(() => {
    setDetail(undefined); setChecks([]); setCheckPage(1); setEdit(false)
    if (!selectedID) return
    const controller = new AbortController()
    setDetailLoading(true)
    Promise.all([getDelegation(selectedID, controller.signal), listDelegationTasks(selectedID, 1, controller.signal)])
      .then(([row, history]) => { setDetail(row); setChecks(history.items); setChecksMore(history.items.length === 20) })
      .catch(e => { if (!controller.signal.aborted) setError(errorText(e)) })
      .finally(() => { if (!controller.signal.aborted) setDetailLoading(false) })
    return () => controller.abort()
  }, [selectedID, refresh])

  const select = (id?: number) => setViewState({ mode: 'delegated', state, page, delegation: id })

  async function changeStatus(row: Delegation, next: 'open' | 'closed') {
    if (savingStatusID !== undefined || Boolean(row.closed_at) === (next === 'closed')) return
    setSavingStatusID(row.id)
    setError(undefined)
    try {
      // List rows only contain a preview; preserve the complete current progress.
      const latest = await getDelegation(row.id)
      await updateDelegation(latest.id, latest.version, latest.content ?? null, next === 'closed')
      setRefresh(v => v + 1)
      messageApi.success(next === 'closed' ? '交办已结束，可在“已结束”中查看' : '已恢复跟踪，可在“未结束”中查看')
    } catch (e) { setError(errorText(e)) } finally { setSavingStatusID(undefined) }
  }

  async function save() {
    if (!detail || !note.trim()) return
    setBusy(true)
    try {
      await updateDelegation(detail.id, detail.version, updatedProgress(detail.content, note.trim()), closed)
      setEdit(false); setRefresh(v => v + 1)
      messageApi.success('交办进展已更新')
    } catch (e) { setError(errorText(e)) } finally { setBusy(false) }
  }

  async function check() {
    if (!detail) return
    setBusy(true)
    try {
      // Inspect all linked checks, including older waiting/needs_human work.
      for (let p = 1; ; p++) {
        const history = await listDelegationTasks(detail.id, p)
        const active = history.items.find(t => ['pending', 'executing', 'waiting', 'needs_human'].includes(t.status))
        if (active) { messageApi.info(`已有检查任务 #${active.id}，请查看关联记录`); return }
        if (history.items.length < 20) break
      }
      const latest = await getDelegation(detail.id)
      if (latest.closed_at) { messageApi.info('这条交办已结束；需要继续时先恢复跟踪'); setRefresh(v => v + 1); return }
      const task = await createTask(checkInput(latest))
      messageApi.success(`已创建检查任务 #${task.id}`); setRefresh(v => v + 1)
    } catch (e) { setError(errorText(e)) } finally { setBusy(false) }
  }

  const columns: TableColumnsType<Delegation> = [
    { title: '原始交办', dataIndex: 'title', render: (title: string, row) => <Button type="link" onClick={() => select(row.id)}>{title}</Button> },
    { title: '当前进展', dataIndex: 'summary', render: (value: string) => value || '待核验' },
    { title: '跟踪状态', width: 130, render: (_, row) => <Select<'open' | 'closed'>
      aria-label={`修改交办“${row.title}”的跟踪状态`}
      size="small"
      style={{ width: 104 }}
      value={row.closed_at ? 'closed' : 'open'}
      options={[{ value: 'open', label: '未结束' }, { value: 'closed', label: '已结束' }]}
      loading={savingStatusID === row.id}
      disabled={savingStatusID !== undefined || loading}
      onChange={next => changeStatus(row, next)}
    /> },
    { title: '最近更新', dataIndex: 'updated_at', render: (value: string) => new Date(value).toLocaleString() },
  ]

  return <>
    {messageContext}
    <MergedPageHeader title="任务" subtitle="记录你交给他人的事项与真实交付进展" activeKey="delegated"
      tabs={[{ key: 'tasks', label: '任务' }, { key: 'automations', label: '自动化' }, { key: 'delegated', label: '我的交办' }]}
      onChange={key => { if (key !== 'delegated') navigate(key === 'automations' ? 'scheduled-tasks' : 'tasks') }}>
      <Button onClick={() => setRefresh(v => v + 1)} loading={loading}>刷新</Button>
    </MergedPageHeader>
    {error && <Alert type="error" showIcon title="操作未完成" description={error} closable onClose={() => setError(undefined)} />}
    <Tabs activeKey={state} items={[{ key: 'open', label: '未结束' }, { key: 'closed', label: '已结束' }]}
      onChange={next => setViewState({ mode: 'delegated', state: next, page: 1 })} />
    <Input.Search placeholder="搜索事项、负责人或背景" allowClear onSearch={value => { setQuery(value); setViewState({ mode: 'delegated', state, page: 1 }) }} style={{ maxWidth: 420, marginBottom: 16 }} />
    <Table rowKey="id" columns={columns} dataSource={items} loading={loading}
      pagination={{ current: page, total, pageSize: 20, showSizeChanger: false, onChange: p => setViewState({ mode: 'delegated', state, page: p }) }}
      locale={{ emptyText: '这个分组暂无交办' }} />
    <Modal open={selectedID !== null} title={detail?.title || '交办详情'} width={900} onCancel={() => select()} footer={null}>
      {detailLoading && <Spin />}
      {detail && <Space orientation="vertical" style={{ width: '100%' }} size="large">
        <Space><Tag>{detail.closed_at ? '已结束' : '未结束'}</Tag><Typography.Text type="secondary">交办 #{detail.id}</Typography.Text></Space>
        <div><Typography.Title level={5}>当前进展</Typography.Title><MarkdownReport content={progressText(detail.content) || '尚未核验。原始交办与背景见下方。'} /></div>
        <Space>
          <Button onClick={() => { setNote(progressText(detail.content)); setClosed(Boolean(detail.closed_at)); setEdit(true) }}>更新进展 / 结束跟踪</Button>
          {!detail.closed_at && <Button type="primary" loading={busy} onClick={check}>核验进展</Button>}
        </Space>
        {edit && <Space orientation="vertical" style={{ width: '100%' }}>
          <Input.TextArea value={note} onChange={e => setNote(e.target.value)} autoSize={{ minRows: 3 }} placeholder="写清当前情况；已交付或取消时说明依据" />
          <Checkbox checked={closed} onChange={e => setClosed(e.target.checked)}>结束跟踪（交付或取消的依据请写在进展中）</Checkbox>
          <Space><Button onClick={save} disabled={!note.trim()} loading={busy}>保存</Button><Button onClick={() => setEdit(false)}>取消</Button></Space>
        </Space>}
        <div><Typography.Title level={5}>原始交办</Typography.Title><MarkdownReport content={detail.source_quote} />
          <details><summary>创建时的完整来源与背景</summary><pre style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{JSON.stringify(detail.source_payload, null, 2)}</pre></details>
          {detail.content != null && <details><summary>完整核验记录</summary><pre style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{JSON.stringify(detail.content, null, 2)}</pre></details>}
        </div>
        <div style={{ width: '100%' }}><Typography.Title level={5}>关联检查</Typography.Title>
          <Typography.Paragraph type="secondary">这里的完成表示本次工作结束，对方是否交付以交办进展为准。</Typography.Paragraph>
          {checks.map(t => <div key={t.id} style={{ marginBottom: 8 }}><a href={`#/work/task/${t.id}`}>#{t.id} {t.title}</a> <Tag>{taskStatusMeta[t.status].label}</Tag><div>{t.summary}</div></div>)}
          {checks.length === 0 && <Typography.Text type="secondary">暂无检查任务</Typography.Text>}
          {checksMore && <Button onClick={async () => { try { const next = await listDelegationTasks(detail.id, checkPage + 1); setChecks(v => [...v, ...next.items]); setCheckPage(v => v + 1); setChecksMore(next.items.length === 20) } catch (e) { setError(errorText(e)) } }}>更多检查记录</Button>}
        </div>
      </Space>}
    </Modal>
  </>
}
