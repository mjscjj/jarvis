import { useEffect, useState } from 'react'
import { Alert, Button, Card, Descriptions, Drawer, Empty, Flex, Input, Modal, Select, Space, Spin, Table, Tag, Timeline, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { approveTask, executeTask, finishTask, listTaskRuns, listTasks, rejectTask, rerunTask, supplementTask } from './api'
import type { ExecutionRun, ProposalResult, RunEnrichment, Task, TaskStatus } from './types'
import PageHeader from './components/PageHeader'
import StatusBadge from './components/StatusBadge'
import { taskStatusMeta as statusMeta } from './status'

const { Link, Paragraph, Text } = Typography

// runStatusColor 把 ExecutionRun 状态映射到 Timeline 圆点/标签颜色。
function runStatusColor(status: string): string {
  if (status === 'succeeded') return 'green'
  if (status === 'failed') return 'red'
  if (status === 'running') return 'blue'
  return 'gray'
}

function formatDuration(ms: number | null): string {
  if (ms == null) return '—'
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

// enrichmentKindLabel 给未带 label 的 enrichment 一个可读中文标题兜底。
function enrichmentKindLabel(kind: string): string {
  switch (kind) {
    case 'context': return '正文'
    case 'doc_link': return '相关文档'
    case 'code_link': return '相关代码'
    case 'commit_digest': return 'Commit 摘要'
    default: return kind || '补充'
  }
}

// EnrichmentBlock 把 codex 的一条 enrichment 渲染成人能读的块，而不是 JSON：
//   - doc_link/code_link：detail 里多个路径以 "；" 或换行分隔，逐条可复制
//   - 其它（context/commit_digest/未知）：多行正文，保留换行
function EnrichmentBlock({ item }: { item: RunEnrichment }) {
  const label = item.label?.trim() || enrichmentKindLabel(item.kind)
  const isLink = item.kind === 'doc_link' || item.kind === 'code_link'
  const paths = isLink
    ? item.detail.split(/[；;\n]+/).map((p) => p.trim()).filter(Boolean)
    : []
  return (
    <div style={{ background: 'var(--color-bg-soft)', borderRadius: 8, padding: '10px 12px' }}>
      <Text strong style={{ fontSize: 13 }}>{label}</Text>
      {isLink ? (
        <Space direction="vertical" size={2} style={{ width: '100%', marginTop: 6 }}>
          {paths.map((path, index) => (
            <Text key={index} className="mono" style={{ fontSize: 12 }} copyable>{path}</Text>
          ))}
        </Space>
      ) : (
        <Paragraph style={{ whiteSpace: 'pre-wrap', margin: '6px 0 0', fontSize: 13, lineHeight: 1.6 }}>{item.detail}</Paragraph>
      )}
    </div>
  )
}

// RunCard 展示单次执行的结构化产物：状态/耗时/时间 + code_change 的 MR/分支/commit/
// diff，codex 自述 summary、结构化 enrichments（正文/文档/commit）、待你拍板的
// needs_followup，以及错误详情。原始 JSON 收进最底部折叠，日常不占视线。
function RunCard({ run }: { run: ExecutionRun }) {
  const enrichments = run.output?.enrichments ?? []
  const followup = run.output?.needs_followup?.trim()
  return (
    <Space direction="vertical" size={10} style={{ width: '100%' }}>
      <Space size={12} wrap>
        <StatusBadge label={run.status} color={statusMeta[run.status === 'succeeded' ? 'done' : run.status === 'failed' ? 'failed' : 'executing']?.color ?? '#888'} />
        <Text type="secondary">#{run.id}</Text>
        <Tag>{run.action_type}</Tag>
        <Text type="secondary">沙箱 {run.sandbox}</Text>
        <Text type="secondary">耗时 {formatDuration(run.duration_ms)}</Text>
      </Space>
      <Text type="secondary" style={{ fontSize: 12 }}>
        {new Date(run.started_at).toLocaleString()}
        {run.finished_at ? ` → ${new Date(run.finished_at).toLocaleTimeString()}` : '（未结束）'}
        {run.codex_session_id ? ` · session ${run.codex_session_id.slice(0, 12)}…` : ''}
      </Text>
      {(run.merge_request_url || run.branch || run.commit || run.diff_path) && (
        <Descriptions size="small" column={1} styles={{ label: { width: 90 } }}>
          {run.merge_request_url && (
            <Descriptions.Item label="Merge Request">
              <Link href={run.merge_request_url} target="_blank">{run.merge_request_url}</Link>
            </Descriptions.Item>
          )}
          {run.branch && <Descriptions.Item label="分支"><Text className="mono">{run.branch}</Text></Descriptions.Item>}
          {run.commit && <Descriptions.Item label="Commit"><Text className="mono">{run.commit.slice(0, 12)}</Text></Descriptions.Item>}
          {run.diff_path && <Descriptions.Item label="Diff"><Text className="mono" copyable>{run.diff_path}</Text></Descriptions.Item>}
        </Descriptions>
      )}
      {run.summary && <Paragraph style={{ marginBottom: 0, whiteSpace: 'pre-wrap' }}>{run.summary}</Paragraph>}
      {enrichments.length > 0 && (
        <Space direction="vertical" size={8} style={{ width: '100%' }}>
          {enrichments.map((item, index) => <EnrichmentBlock key={index} item={item} />)}
        </Space>
      )}
      {followup && (
        <Alert type="info" showIcon message="待你拍板 / 后续" description={<Text style={{ whiteSpace: 'pre-wrap' }}>{followup}</Text>} />
      )}
      {run.error_detail && <Alert type="error" showIcon message="执行错误" description={<Text className="mono">{run.error_detail}</Text>} />}
      {run.output && Object.keys(run.output).length > 0 && (
        <details><summary style={{ cursor: 'pointer', color: '#888' }}>codex 原始输出（JSON）</summary><pre className="inline-json">{JSON.stringify(run.output, null, 2)}</pre></details>
      )}
    </Space>
  )
}

// proposalOf reads the pending proposal off a Task whose execution_result was
// written by the propose stage (stage="proposal"). Returns null otherwise.
function proposalOf(task: Task): ProposalResult | null {
  const result = task.execution_result as ProposalResult | null
  if (result && result.stage === 'proposal' && result.proposal) return result
  return null
}

// ProposalCard renders the high-risk external write awaiting approval: the action
// description, the target object, and the COMPLETE artifact the user is approving
// (the exact document/message that will be written out), plus any enrichments.
function ProposalCard({ result }: { result: ProposalResult }) {
  const { proposal } = result
  return (
    <Space direction="vertical" size={12} style={{ width: '100%' }}>
      {result.summary && <Paragraph style={{ marginBottom: 0, whiteSpace: 'pre-wrap' }}>{result.summary}</Paragraph>}
      <Descriptions size="small" column={1} styles={{ label: { width: 72 } }}>
        <Descriptions.Item label="动作">{proposal.action}</Descriptions.Item>
        <Descriptions.Item label="目标">{proposal.target}</Descriptions.Item>
      </Descriptions>
      <div>
        <Text strong style={{ fontSize: 13 }}>完整产出物（批准后将真正写出/发送）</Text>
        <Paragraph style={{ whiteSpace: 'pre-wrap', margin: '6px 0 0', fontSize: 13, lineHeight: 1.6, background: 'var(--color-bg-soft)', borderRadius: 8, padding: '10px 12px' }}>{proposal.artifact}</Paragraph>
      </div>
      {result.enrichments && result.enrichments.length > 0 && (
        <Space direction="vertical" size={8} style={{ width: '100%' }}>
          {result.enrichments.map((item, index) => <EnrichmentBlock key={index} item={item} />)}
        </Space>
      )}
      {result.needs_followup?.trim() && (
        <Alert type="info" showIcon message="待你拍板 / 后续" description={<Text style={{ whiteSpace: 'pre-wrap' }}>{result.needs_followup}</Text>} />
      )}
    </Space>
  )
}

// External actions reach outside this machine and cannot be auto-run; the
// backend still requires the click, but we warn before triggering.
const externalActions = new Set(['summary_post', 'reply_message', 'schedule_meeting', 'doc_write', 'manual_followup'])

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

export default function Tasks() {
  const allStatuses: TaskStatus[] = ['pending', 'executing', 'awaiting_approval', 'done', 'failed']
  const [statuses, setStatuses] = useState<TaskStatus[]>(allStatuses)
  const [items, setItems] = useState<Task[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [refreshKey, setRefreshKey] = useState(0)
  const [detail, setDetail] = useState<Task>()
  const [selected, setSelected] = useState<Task>()
  const [finishStatus, setFinishStatus] = useState<'done' | 'failed'>('done')
  const [summary, setSummary] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [executingId, setExecutingId] = useState<number>()
  const [rerunTarget, setRerunTarget] = useState<Task>()
  const [rerunNote, setRerunNote] = useState('')
  const [rejectTarget, setRejectTarget] = useState<Task>()
  const [rejectReason, setRejectReason] = useState('')
  const [rerunSubmitting, setRerunSubmitting] = useState(false)
  const [runs, setRuns] = useState<ExecutionRun[]>([])
  const [runsLoading, setRunsLoading] = useState(false)
  const [runsError, setRunsError] = useState<string>()

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    listTasks(statuses, 1, 100, controller.signal)
      .then((result) => { setItems(result.items); setError(undefined) })
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError(errorText(cause))
      })
      .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [statuses, refreshKey])

  // 打开详情抽屉时拉该 Task 的执行历史。detail 关闭（undefined）时清空。
  useEffect(() => {
    if (!detail) { setRuns([]); setRunsError(undefined); return }
    const controller = new AbortController()
    setRunsLoading(true)
    setRunsError(undefined)
    listTaskRuns(detail.id, controller.signal)
      .then((result) => setRuns(result.items))
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setRunsError(errorText(cause))
      })
      .finally(() => { if (!controller.signal.aborted) setRunsLoading(false) })
    return () => controller.abort()
  }, [detail, refreshKey])

  const openFinish = (task: Task, status: 'done' | 'failed') => {
    setSelected(task)
    setFinishStatus(status)
    setSummary('')
  }

  const submit = async () => {
    if (!selected || !summary.trim()) { setError('执行结果不能为空'); return }
    setSubmitting(true)
    try {
      const result = finishStatus === 'done' ? { summary: summary.trim() } : { error: summary.trim() }
      await finishTask(selected.id, selected.version, finishStatus, result)
      setSelected(undefined)
      setRefreshKey((value) => value + 1)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSubmitting(false)
    }
  }

  const runExecute = async (task: Task) => {
    if (externalActions.has(task.action_type)) {
      const ok = window.confirm(`「${task.title}」是对外动作（${task.action_type}），执行会真实触达外部。确认由 codex 执行？`)
      if (!ok) return
    }
    setExecutingId(task.id)
    setError(undefined)
    try {
      await executeTask(task.id)
      setRefreshKey((value) => value + 1)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setExecutingId(undefined)
    }
  }

  const runApprove = async (task: Task) => {
    const ok = window.confirm(`批准后 codex 会真正落地这条对外写入（${task.action_type}）。确认批准并执行？`)
    if (!ok) return
    setExecutingId(task.id)
    setError(undefined)
    try {
      await approveTask(task.id, task.version)
      setDetail(undefined)
      setRefreshKey((value) => value + 1)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setExecutingId(undefined)
    }
  }

  const openReject = (task: Task) => {
    setRejectTarget(task)
    setRejectReason('')
  }

  const submitReject = async () => {
    if (!rejectTarget) return
    const task = rejectTarget
    setExecutingId(task.id)
    setError(undefined)
    try {
      await rejectTask(task.id, task.version, rejectReason.trim())
      setRejectTarget(undefined)
      setDetail(undefined)
      setRefreshKey((value) => value + 1)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setExecutingId(undefined)
    }
  }

  const openRerun = (task: Task) => {
    setRerunTarget(task)
    setRerunNote('')
  }

  const submitRerun = async () => {
    if (!rerunTarget) return
    const task = rerunTarget
    setRerunSubmitting(true)
    setError(undefined)
    try {
      const note = rerunNote.trim()
      if (note) await supplementTask(task.id, task.version, note)
      await rerunTask(task.id)
      setRerunTarget(undefined)
      setRefreshKey((value) => value + 1)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setRerunSubmitting(false)
    }
  }

  const columns: TableColumnsType<Task> = [
    { title: '任务', dataIndex: 'title', render: (_, task) => <Space direction="vertical" size={2}><Text strong>{task.title}</Text><Text type="secondary">Todo #{task.todo_id} · {task.action_type}</Text></Space> },
    { title: '状态', dataIndex: 'status', width: 110, render: (status: TaskStatus) => <StatusBadge label={statusMeta[status].label} color={statusMeta[status].color} /> },
    { title: '方案', width: 320, render: (_, task) => <pre className="inline-json">{JSON.stringify(task.plan, null, 2)}</pre> },
    { title: '结果', width: 260, render: (_, task) => task.execution_result ? <pre className="inline-json">{JSON.stringify(task.execution_result, null, 2)}</pre> : '—' },
    {
      title: '操作', width: 220, render: (_, task) => {
        if (task.status === 'pending') {
          return <Space onClick={(e) => e.stopPropagation()}>
            <Button type="primary" size="small" loading={executingId === task.id} onClick={(e) => { e.stopPropagation(); runExecute(task) }}>执行</Button>
            <Button size="small" onClick={(e) => { e.stopPropagation(); openFinish(task, 'done') }}>手动完成</Button>
            <Button danger size="small" onClick={(e) => { e.stopPropagation(); openFinish(task, 'failed') }}>失败</Button>
          </Space>
        }
        if (task.status === 'executing') return <StatusBadge label="codex 执行中…" color={statusMeta.executing.color} />
        if (task.status === 'awaiting_approval') {
          return <Space onClick={(e) => e.stopPropagation()}>
            <Button type="primary" size="small" loading={executingId === task.id} onClick={(e) => { e.stopPropagation(); runApprove(task) }}>批准落地</Button>
            <Button danger size="small" onClick={(e) => { e.stopPropagation(); openReject(task) }}>驳回</Button>
          </Space>
        }
        if (task.status === 'done' || task.status === 'failed') {
          return <Space onClick={(e) => e.stopPropagation()}>
            <Button size="small" onClick={(e) => { e.stopPropagation(); openRerun(task) }}>重跑</Button>
          </Space>
        }
        return '—'
      },
    },
  ]

  return <>
    <PageHeader title="任务执行" subtitle="已确认的可执行任务，点行查看方案与结果">
      <Button onClick={() => setRefreshKey((value) => value + 1)} loading={loading}>刷新</Button>
    </PageHeader>
    <Card className="filter-card" variant="borderless">
      <Flex gap={16} align="end" wrap>
        <label className="filter-field filter-status"><Text type="secondary">Task 状态</Text><Select mode="multiple" value={statuses} options={Object.entries(statusMeta).map(([value, meta]) => ({ value, label: meta.label }))} onChange={(values) => setStatuses(values.length ? values : allStatuses)} /></label>
      </Flex>
    </Card>
    {error && <Alert type="error" showIcon message="Task 操作失败" description={error} closable onClose={() => setError(undefined)} />}
    <Card className="table-card" variant="borderless"><Table<Task> rowKey="id" columns={columns} dataSource={items} loading={loading} pagination={false} scroll={{ x: 1050 }} onRow={(task) => ({ onClick: () => setDetail(task), className: 'clickable-row' })} /></Card>
    <Drawer title={detail?.title || 'Task 详情'} open={Boolean(detail)} width={680} onClose={() => setDetail(undefined)}>
      {detail && <Space direction="vertical" size={20} className="drawer-content">
        <Space><StatusBadge label={statusMeta[detail.status].label} color={statusMeta[detail.status].color} /><Tag>{detail.action_type}</Tag></Space>
        <Descriptions column={2} size="small">
          <Descriptions.Item label="Todo">#{detail.todo_id}</Descriptions.Item>
          <Descriptions.Item label="版本">v{detail.version}</Descriptions.Item>
          <Descriptions.Item label="确认人">{detail.confirmed_by || '—'}</Descriptions.Item>
          <Descriptions.Item label="确认时间">{detail.confirmed_at ? new Date(detail.confirmed_at).toLocaleString() : '—'}</Descriptions.Item>
          <Descriptions.Item label="自主模式">{detail.autonomy_mode || '—'}</Descriptions.Item>
          <Descriptions.Item label="项目">{detail.project_id != null ? `#${detail.project_id}` : '未关联'}</Descriptions.Item>
        </Descriptions>
        <section>
          <Text type="secondary">执行历史（共 {runs.length} 次）</Text>
          {runsError && <Alert type="error" showIcon style={{ marginTop: 8 }} message="执行历史加载失败" description={runsError} />}
          {runsLoading ? (
            <div style={{ padding: '16px 0', textAlign: 'center' }}><Spin size="small" /></div>
          ) : runs.length === 0 ? (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="尚无执行记录" style={{ marginTop: 8 }} />
          ) : (
            <Timeline
              style={{ marginTop: 12 }}
              items={runs.map((run) => ({ color: runStatusColor(run.status), children: <RunCard run={run} /> }))}
            />
          )}
        </section>
        {proposalOf(detail) && (
          <section>
            <Alert
              type="warning"
              showIcon
              style={{ marginBottom: 12 }}
              message="待你审批的对外写入方案"
              description="codex 判断这是高风险对外写入，只产出了方案与完整产出物，尚未真正写入/发送。请审阅下方产出物，批准后才会真正落地。"
            />
            <ProposalCard result={proposalOf(detail)!} />
            <Space style={{ marginTop: 12 }}>
              <Button type="primary" loading={executingId === detail.id} onClick={() => runApprove(detail)}>批准落地</Button>
              <Button danger onClick={() => openReject(detail)}>驳回</Button>
            </Space>
          </section>
        )}
        <section><Text type="secondary">执行方案</Text><pre>{JSON.stringify(detail.plan, null, 2)}</pre></section>
        {detail.execution_supplements && detail.execution_supplements.length > 0 && (
          <section>
            <Text type="secondary">执行阶段补充（M5）</Text>
            <Space direction="vertical" size={8} style={{ width: '100%', marginTop: 8 }}>
              {detail.execution_supplements.map((item, index) => (
                <blockquote key={index} style={{ margin: 0 }}>
                  {item.note}
                  <br />
                  <Text type="secondary" style={{ fontSize: 12 }}>{new Date(item.at).toLocaleString()}</Text>
                </blockquote>
              ))}
            </Space>
          </section>
        )}
        {!proposalOf(detail) && (
          <section><Text type="secondary">结果汇总（Task 最新快照）</Text>{detail.execution_result ? <pre>{JSON.stringify(detail.execution_result, null, 2)}</pre> : <Paragraph type="secondary" style={{ marginTop: 8 }}>尚未执行</Paragraph>}</section>
        )}
        <section><Text type="secondary">背景（M4 产出）</Text><pre>{JSON.stringify(detail.background, null, 2)}</pre></section>
      </Space>}
    </Drawer>
    <Modal title={finishStatus === 'done' ? '记录完成结果' : '记录失败原因'} open={Boolean(selected)} confirmLoading={submitting} onOk={submit} onCancel={() => setSelected(undefined)} okText="提交">
      <Input.TextArea rows={5} value={summary} onChange={(event) => setSummary(event.target.value)} placeholder={finishStatus === 'done' ? '完成了什么、产物在哪里' : '失败原因和需要的后续处理'} />
    </Modal>
    <Modal
      title={rerunTarget ? `重跑「${rerunTarget.title}」` : '重跑任务'}
      open={Boolean(rerunTarget)}
      confirmLoading={rerunSubmitting}
      onOk={submitRerun}
      onCancel={() => setRerunTarget(undefined)}
      okText="确认重跑"
    >
      <Space direction="vertical" size={8} style={{ width: '100%' }}>
        {rerunTarget && externalActions.has(rerunTarget.action_type) && (
          <Alert type="warning" showIcon message={`对外动作（${rerunTarget.action_type}）`} description="重跑会再次真实触达外部，请确认后再提交。" />
        )}
        <Text type="secondary">可选填写补充信息/指示；留空则直接重跑。填写后会持久保存，之后每次重跑都会带上。</Text>
        <Input.TextArea rows={4} value={rerunNote} onChange={(event) => setRerunNote(event.target.value)} placeholder="例如：这次改用 xxx 文档模板；标题要包含季度；只发给 A 不要发给 B 等（可不填）" />
      </Space>
    </Modal>
    <Modal
      title={rejectTarget ? `驳回「${rejectTarget.title}」的方案` : '驳回方案'}
      open={Boolean(rejectTarget)}
      confirmLoading={Boolean(rejectTarget) && executingId === rejectTarget?.id}
      onOk={submitReject}
      onCancel={() => setRejectTarget(undefined)}
      okText="确认驳回"
      okButtonProps={{ danger: true }}
    >
      <Space direction="vertical" size={8} style={{ width: '100%' }}>
        <Text type="secondary">驳回后任务将标记为失败，不会真正写出任何内容。可填写驳回原因（可不填）；之后可重跑重新产出方案。</Text>
        <Input.TextArea rows={4} value={rejectReason} onChange={(event) => setRejectReason(event.target.value)} placeholder="例如：措辞不合适 / 目标群选错了 / 内容还需补充数据（可不填）" />
      </Space>
    </Modal>
  </>
}
