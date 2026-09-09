import { useCallback, useEffect, useMemo, useState } from 'react'
import { Alert, Button, Card, Collapse, Empty, Flex, Select, Spin, Statistic, Table, Tabs, Tag, Typography } from 'antd'
import { NodeIndexOutlined, ReloadOutlined } from '@ant-design/icons'
import { listPages, listRelations, listWorldProgress } from '../api'
import type { WorldProgress, WorldProgressSignal } from '../types'
import { usePageContext } from '../pageContext'
import { getGenericOKRBoard, getGenericOKRProgressBoard, listWeeklyReportWeeks } from './emily/api'
import type { BoardData } from './emily/api'
import type { Entry, Kr, Objective, Point, Status } from './emily/types'
import { relationsForOKRBoard, type OKRRelationRow } from './relationView'

const { Text, Title, Paragraph } = Typography

type PluginTab = 'structure' | 'progress' | 'relations'

const progressSignal: Record<WorldProgressSignal, { color: string; label: string }> = {
  unknown: { color: 'default', label: '信息不足' },
  green: { color: 'success', label: '正常' },
  yellow: { color: 'warning', label: '有风险' },
  red: { color: 'error', label: '严重风险' },
}

const formalStatusLabel: Record<Status, string> = {
  not_started: '未开始',
  in_progress: '进行中',
  done: '已完成',
  at_risk: '有风险',
  delayed: '已延期',
  blocked: '受阻',
}

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

function subjectKey(subjectType: string, subjectId: string): string {
  return `${subjectType}:${subjectId}`
}

function allEntries(kr: Kr): Entry[] {
  return kr.points.flatMap((point) => point.entries)
}

function formalSummary(entries: Entry[], emptyText: string): string {
  if (entries.length === 0) return emptyText
  const counts = new Map<Status, number>()
  for (const entry of entries) counts.set(entry.status, (counts.get(entry.status) ?? 0) + 1)
  return [...counts.entries()].map(([status, count]) => `${formalStatusLabel[status]} ${count}`).join(' · ')
}

function WorldAssessment({ value }: { value?: WorldProgress | null }) {
  if (!value) return <Text type="secondary">尚未形成 Jarvis 世界进展</Text>
  const presentation = progressSignal[value.signal]
  const refs = Array.isArray(value.evidence.refs)
    ? value.evidence.refs.filter((item): item is string => typeof item === 'string')
    : []
  return (
    <Flex vertical gap={6}>
      <Flex align="center" gap={8} wrap>
        <Tag color={presentation.color}>{presentation.label}</Tag>
        <Text type="secondary">评估于 {new Date(value.assessed_at).toLocaleString('zh-CN', { hour12: false })}</Text>
      </Flex>
      <Paragraph className="okr-plugin-world-summary">{value.summary}</Paragraph>
      {refs.length > 0 && <Text type="secondary">证据：{refs.join('、')}</Text>}
    </Flex>
  )
}

function SubjectProgress({
  level,
  title,
  formal,
  world,
  children,
}: {
  level: 'O' | 'KR' | '子 KR'
  title: string
  formal: string
  world?: WorldProgress | null
  children?: React.ReactNode
}) {
  return (
    <div className={`okr-plugin-progress-node okr-plugin-progress-${level === 'O' ? 'objective' : level === 'KR' ? 'kr' : 'point'}`}>
      <Flex align="center" gap={8} wrap>
        <Tag>{level}</Tag>
        <Text strong>{title}</Text>
      </Flex>
      <div className="okr-plugin-progress-columns">
        <section>
          <Text className="okr-plugin-progress-label">正式 OKR 口径</Text>
          <Paragraph>{formal}</Paragraph>
        </section>
        <section>
          <Text className="okr-plugin-progress-label">Jarvis 世界进展</Text>
          <WorldAssessment value={world} />
        </section>
      </div>
      {children}
    </div>
  )
}

function PointProgress({ point, world }: { point: Point; world?: WorldProgress | null }) {
  const formal = point.entries.length === 0
    ? '本周尚未填写正式进展。'
    : point.entries.map((entry) => `${formalStatusLabel[entry.status]}：${entry.text || '未填写说明'}`).join('；')
  return <SubjectProgress level="子 KR" title={point.title} formal={formal} world={world} />
}

function KRProgress({ kr, worldBySubject }: { kr: Kr; worldBySubject: Map<string, WorldProgress | null> }) {
  const entries = allEntries(kr)
  const metrics = kr.metrics.filter((item) => item.text.trim()).map((item) => `${item.text}${item.light ? `（${progressSignal[item.light].label}）` : ''}`)
  const formal = [
    formalSummary(entries, '本周下属子 KR 尚未填写正式进展。'),
    metrics.length > 0 ? `周期指标：${metrics.join('；')}` : '',
  ].filter(Boolean).join('；')
  return (
    <SubjectProgress level="KR" title={kr.title} formal={formal} world={worldBySubject.get(subjectKey('okr_kr', kr.id))}>
      <div className="okr-plugin-progress-children">
        {kr.points.map((point) => (
          <PointProgress key={point.id} point={point} world={worldBySubject.get(subjectKey('okr_point', point.id))} />
        ))}
      </div>
    </SubjectProgress>
  )
}

function ObjectiveProgress({ objective, worldBySubject }: { objective: Objective; worldBySubject: Map<string, WorldProgress | null> }) {
  const entries = objective.krs.flatMap(allEntries)
  return (
    <SubjectProgress
      level="O"
      title={objective.title}
      formal={formalSummary(entries, '本周下属 KR 尚未填写正式进展。')}
      world={worldBySubject.get(subjectKey('okr_objective', objective.id))}
    >
      <div className="okr-plugin-progress-children">
        {objective.krs.map((kr) => <KRProgress key={kr.id} kr={kr} worldBySubject={worldBySubject} />)}
      </div>
    </SubjectProgress>
  )
}

function StructureView({ board }: { board: BoardData }) {
  if (board.objectives.length === 0) return <Empty description="当前季度还没有 OKR" />
  return (
    <Collapse
      items={board.objectives.map((objective) => ({
        key: objective.id,
        label: <Flex align="center" gap={8}><Tag>O</Tag><Text strong>{objective.title}</Text><Text type="secondary">{objective.krs.length} 个 KR</Text></Flex>,
        children: (
          <Flex vertical gap={12}>
            {objective.krs.map((kr) => (
              <Card key={kr.id} size="small" title={<Flex gap={8} align="center"><Tag>KR</Tag><span>{kr.title}</span></Flex>}>
                <Flex vertical gap={8}>
                  <Text type="secondary">负责人：{kr.owners?.map((owner) => owner.name).join('、') || '未设置'}</Text>
                  {kr.metrics.length > 0 && <Text>指标：{kr.metrics.map((metric) => metric.text).filter(Boolean).join('；') || '未填写'}</Text>}
                  <Flex vertical gap={4}>
                    {kr.points.map((point) => <Text key={point.id}><Tag>子 KR</Tag>{point.title}</Text>)}
                  </Flex>
                </Flex>
              </Card>
            ))}
          </Flex>
        ),
      }))}
    />
  )
}

export default function OKRPluginPage() {
  const { context, setViewState, navigate } = usePageContext()
  const requestedTab = context.view_state.plugin_tab
  const activeTab: PluginTab = requestedTab === 'progress' || requestedTab === 'relations' ? requestedTab : 'structure'
  const [board, setBoard] = useState<BoardData>()
  const [progressBoard, setProgressBoard] = useState<BoardData>()
  const [quarter, setQuarter] = useState(context.view_state.quarter ?? '')
  const [week, setWeek] = useState(context.view_state.week ?? '')
  const [worldBySubject, setWorldBySubject] = useState(new Map<string, WorldProgress | null>())
  const [relations, setRelations] = useState<OKRRelationRow[]>([])
  const [loading, setLoading] = useState(true)
  const [relationsLoading, setRelationsLoading] = useState(false)
  const [progressLoading, setProgressLoading] = useState(false)
  const [progressReloadRevision, setProgressReloadRevision] = useState(0)
  const [quarterError, setQuarterError] = useState<string>()
  const [progressError, setProgressError] = useState<string>()
  const [relationsError, setRelationsError] = useState<string>()

  const loadQuarter = useCallback(async (targetQuarter = '') => {
    setLoading(true)
    setQuarterError(undefined)
    try {
      const nextBoard = await getGenericOKRBoard(targetQuarter)
      const catalog = await listWeeklyReportWeeks(nextBoard.quarter)
      const requestedWeek = context.view_state.week ?? ''
      const nextWeek = catalog.weeks.some((item) => item.week === requestedWeek) ? requestedWeek : catalog.weeks[0]?.week ?? ''
      setBoard(nextBoard)
      setQuarter(nextBoard.quarter)
      setWeek(nextWeek)
    } catch (cause) {
      setQuarterError(errorText(cause))
    } finally {
      setLoading(false)
    }
  }, [context.view_state.week])

  useEffect(() => {
    void loadQuarter(context.view_state.quarter ?? '')
  }, []) // The plugin owns subsequent quarter changes explicitly.

  const loadRelations = useCallback(async (nextBoard: BoardData, signal?: AbortSignal) => {
    setRelationsLoading(true)
    setRelations([])
    setRelationsError(undefined)
    try {
      const [result, worldPages] = await Promise.all([
        listRelations({ nodeTypes: ['okr_objective', 'okr_kr', 'okr_point'] }, signal),
        listPages(true, signal),
      ])
      if (signal?.aborted) return
      setRelations(relationsForOKRBoard(result.items, nextBoard.objectives, worldPages))
    } catch (cause) {
      if (!(cause instanceof DOMException && cause.name === 'AbortError')) setRelationsError(errorText(cause))
    } finally {
      if (!signal?.aborted) setRelationsLoading(false)
    }
  }, [])

  useEffect(() => {
    if (!board) {
      setRelations([])
      return
    }
    const controller = new AbortController()
    void loadRelations(board, controller.signal)
    return () => controller.abort()
  }, [board, loadRelations])

  useEffect(() => {
    if (!quarter || !week) {
      setProgressBoard(undefined)
      setWorldBySubject(new Map())
      return
    }
    const controller = new AbortController()
    setProgressLoading(true)
    setProgressBoard(undefined)
    setWorldBySubject(new Map())
    setProgressError(undefined)
    getGenericOKRProgressBoard(quarter, week)
      .then(async (nextBoard) => {
        const result = await listWorldProgress(week, controller.signal)
        if (controller.signal.aborted) return
        setProgressBoard(nextBoard)
        setWorldBySubject(new Map(result.items.map((item) => [subjectKey(item.subject_type, item.subject_id), item])))
      })
      .catch((cause) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setProgressError(errorText(cause))
      })
      .finally(() => { if (!controller.signal.aborted) setProgressLoading(false) })
    return () => controller.abort()
  }, [quarter, week, progressReloadRevision])

  const counts = useMemo(() => {
    const objectives = board?.objectives.length ?? 0
    const allKRs = board?.objectives.flatMap((objective) => objective.krs) ?? []
    const krs = allKRs.length
    const points = allKRs.flatMap((kr) => kr.points).length
    const metrics = allKRs.flatMap((kr) => kr.metrics).length
    const owners = allKRs.reduce((total, kr) => total + (kr.owners?.length ?? 0) + kr.points.reduce((pointTotal, point) => pointTotal + (point.owners?.length ?? 0), 0), 0)
    return { objectives, krs, points, metrics, owners }
  }, [board])
  const error = [quarterError, progressError, relationsError].filter(Boolean).join('；')

  const changeRoute = (patch: Record<string, string>) => setViewState({
    ...context.view_state,
    plugin: 'okr',
    plugin_tab: activeTab,
    quarter,
    week,
    ...patch,
  })

  const relationColumns = [
    {
      title: 'OKR 内容',
      key: 'okr',
      render: (_: unknown, row: OKRRelationRow) => (
        <Flex vertical gap={2}>
          <Flex align="center" gap={8}>
            <Tag color={row.okr_level === 'O' ? 'purple' : row.okr_level === 'KR' ? 'blue' : 'cyan'}>{row.okr_level}</Tag>
            <Text strong>{row.okr_title}</Text>
          </Flex>
          {row.okr_context && <Text type="secondary">{row.okr_context}</Text>}
        </Flex>
      ),
    },
    { title: '关联方式', dataIndex: 'display_relation', width: 130 },
    {
      title: '现实对象',
      key: 'world',
      render: (_: unknown, row: OKRRelationRow) => (
        <Flex align="center" gap={8}>
          <Tag>{row.world_type_label}</Tag>
          <Text strong>{row.world_name}</Text>
        </Flex>
      ),
    },
    { title: '确认情况', dataIndex: 'confirmed_at', width: 100, render: (value: string | null) => <Tag color={value ? 'green' : 'default'}>{value ? '已确认' : '待确认'}</Tag> },
    {
      title: '判断把握',
      dataIndex: 'confidence',
      width: 100,
      render: (value: number | null) => {
        if (value == null) return <Text type="secondary">未评估</Text>
        if (value >= 0.9) return <Tag color="green">高</Tag>
        if (value >= 0.7) return <Tag color="gold">中</Tag>
        return <Tag>低</Tag>
      },
    },
  ]
  return (
    <div className="plugin-detail okr-plugin-page">
      <div className="plugin-detail-heading">
        <div>
          <Title level={3}>OKR 插件</Title>
          <Text type="secondary">通用 Objective、KR、子 KR、正式进展及其与 Jarvis 世界的连接。</Text>
        </div>
        <Button icon={<ReloadOutlined />} loading={loading || progressLoading || relationsLoading} onClick={() => { setProgressReloadRevision((value) => value + 1); void loadQuarter(quarter) }}>重新载入</Button>
      </div>
      {error && <Alert type="error" showIcon closable message={error} onClose={() => { setQuarterError(undefined); setProgressError(undefined); setRelationsError(undefined) }} />}
      <Flex gap={12} wrap>
        <Select
          value={quarter || undefined}
          placeholder="选择季度"
          options={(board?.availableQuarters ?? []).map((value) => ({ value, label: value.replace('-', ' ') }))}
          onChange={(value) => { changeRoute({ quarter: value, week: '' }); void loadQuarter(value) }}
          style={{ minWidth: 140 }}
        />
        {activeTab === 'progress' && (
          <Select
            value={week || undefined}
            placeholder="尚无周次"
            options={(progressBoard?.availableWeeks ?? []).map((value) => ({ value, label: value }))}
            onChange={(value) => { setWeek(value); changeRoute({ week: value }) }}
            style={{ minWidth: 150 }}
          />
        )}
      </Flex>
      <Flex gap={12} wrap>
        <Card size="small"><Statistic title="Objective" value={counts.objectives} /></Card>
        <Card size="small"><Statistic title="KR" value={counts.krs} /></Card>
        <Card size="small"><Statistic title="子 KR" value={counts.points} /></Card>
        <Card size="small"><Statistic title="指标" value={counts.metrics} /></Card>
        <Card size="small"><Statistic title="Owner 出现项" value={counts.owners} /></Card>
        <Card size="small"><Statistic title="现实关联" value={relations.length} /></Card>
      </Flex>
      <Tabs
        activeKey={activeTab}
        onChange={(key) => changeRoute({ plugin_tab: key })}
        items={[
          {
            key: 'structure',
            label: 'OKR 结构',
            children: loading || !board ? <Spin /> : <StructureView board={board} />,
          },
          {
            key: 'progress',
            label: '进展',
            children: progressLoading ? <Spin /> : !week ? <Empty description="当前季度还没有已开启的周次" /> : (
              <Flex vertical gap={16}>
                <Alert type="info" showIcon message="正式进展由人在 OKR 页面填写；Jarvis 世界进展独立保存，只在这里对照展示，不会覆盖人工内容。" />
                {progressBoard?.objectives.map((objective) => (
                  <ObjectiveProgress key={objective.id} objective={objective} worldBySubject={worldBySubject} />
                ))}
              </Flex>
            ),
          },
          {
            key: 'relations',
            label: '现实关联',
            children: relationsLoading ? <Spin /> : (
              <Flex vertical gap={16}>
                <Alert type="info" showIcon message="这里展示已找到的 OKR 与项目、关键事项或协作人的联系。没有关联也正常；待确认的联系会先保留，确认后才会出现在全景图中。" />
                <Flex align="center" justify="space-between" gap={12} wrap>
                  <Card size="small"><Statistic title="已确认关联" value={relations.filter((relation) => Boolean(relation.confirmed_at)).length} /></Card>
                  <Button type="primary" icon={<NodeIndexOutlined />} onClick={() => navigate('background', { view: 'world-map' })}>打开 OKR 全景</Button>
                </Flex>
                {relations.length === 0 ? <Empty description="尚未找到 OKR 与项目、关键事项或协作人的关联" /> : (
                  <Table<OKRRelationRow> rowKey="id" columns={relationColumns} dataSource={relations} pagination={false} />
                )}
              </Flex>
            ),
          },
        ]}
      />
    </div>
  )
}
