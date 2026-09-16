import { useEffect, useMemo, useState } from 'react'
import { Alert, Button, Card, DatePicker, Empty, Flex, Form, Input, Modal, Popconfirm, Spin, Tag, Typography } from 'antd'
import dayjs from 'dayjs'
import { closeProjectChange, closeProjectRisk, createProjectChange, createProjectRisk, getPage, listGroups, listKeyMatters, listProjectChanges, listProjectResources, listProjectRisks, updateProjectRisk } from '../api'
import type { Group, KeyMatter, PageLink, Project, ProjectChange, ProjectChangeInput, ProjectRisk, ProjectRiskInput, Resource } from '../types'
import RelatedOKRCard from './RelatedOKRCard'
import SummaryPageEditor from './SummaryPageEditor'

const { Text } = Typography

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

function uniqueLinks(links: PageLink[]): PageLink[] {
  const seen = new Set<string>()
  return links.filter((link) => {
    const key = `${link.type}:${link.id}`
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
}

function EmptyOverview({ text }: { text: string }) {
  return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={text} />
}

export default function ProjectOverview({ project }: { project: Project }) {
  const [matters, setMatters] = useState<KeyMatter[]>([])
  const [resources, setResources] = useState<Resource[]>([])
  const [groups, setGroups] = useState<Group[]>([])
  const [risks, setRisks] = useState<ProjectRisk[]>([])
  const [changes, setChanges] = useState<ProjectChange[]>([])
  const [links, setLinks] = useState<PageLink[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string>()
  const [refreshRevision, setRefreshRevision] = useState(0)
  const [riskOpen, setRiskOpen] = useState(false)
  const [changeOpen, setChangeOpen] = useState(false)
  const [pageDetail, setPageDetail] = useState<{ type: 'project_risk' | 'project_change'; id: number; title: string }>()
  const [saving, setSaving] = useState(false)
  const [riskForm] = Form.useForm<ProjectRiskInput>()
  const [changeForm] = Form.useForm<{ title: string; changed_at: dayjs.Dayjs }>()

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError(undefined)
    Promise.all([
      listKeyMatters(1, 100, true, controller.signal),
      listProjectResources(project.id, controller.signal),
      listGroups({ page: 1, pageSize: 100, relatedOnly: true, keyword: project.name }, controller.signal),
      listProjectRisks(project.id, false, controller.signal),
      listProjectChanges(project.id, false, controller.signal),
      getPage('project', project.id, controller.signal),
    ]).then(([matterResult, resourceResult, groupResult, riskResult, changeResult, page]) => {
      if (controller.signal.aborted) return
      setMatters(matterResult.items.filter((item) => item.project_id === project.id))
      setResources(resourceResult.items)
      setGroups(groupResult.items.filter((item) => item.project_id === project.id))
      setRisks(riskResult.items)
      setChanges(changeResult.items)
      setLinks(uniqueLinks([...page.outgoing, ...page.backlinks]).filter((item) => item.type === 'person' || item.type === 'group'))
    }).catch((cause: unknown) => {
      if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError(errorText(cause))
    }).finally(() => {
      if (!controller.signal.aborted) setLoading(false)
    })
    return () => controller.abort()
  }, [project.id, refreshRevision])

  const refresh = () => setRefreshRevision((value) => value + 1)

  const submitRisk = async () => {
    const values = await riskForm.validateFields()
    setSaving(true)
    setError(undefined)
    try {
      await createProjectRisk({ ...values, project_id: project.id, triggered_at: null })
      setRiskOpen(false)
      riskForm.resetFields()
      refresh()
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSaving(false)
    }
  }

  const submitChange = async () => {
    const values = await changeForm.validateFields()
    setSaving(true)
    setError(undefined)
    try {
      const input: ProjectChangeInput = { project_id: project.id, title: values.title, changed_at: values.changed_at.toISOString() }
      await createProjectChange(input)
      setChangeOpen(false)
      changeForm.resetFields()
      refresh()
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSaving(false)
    }
  }

  const triggerRisk = async (risk: ProjectRisk) => {
    setError(undefined)
    try {
      await updateProjectRisk(risk.id, {
        project_id: risk.project_id, title: risk.title, probability: risk.probability, impact: risk.impact,
        triggered_at: new Date().toISOString(),
      })
      refresh()
    } catch (cause: unknown) { setError(errorText(cause)) }
  }

  const closeRisk = async (id: number) => {
    try { await closeProjectRisk(id); refresh() } catch (cause: unknown) { setError(errorText(cause)) }
  }

  const closeChange = async (id: number) => {
    try { await closeProjectChange(id); refresh() } catch (cause: unknown) { setError(errorText(cause)) }
  }

  const peopleAndTeams = useMemo(() => {
    const values = [`我 · ${project.role === 'owner' ? '负责人' : '参与者'}`]
    for (const link of links) values.push(link.name || `${link.type}:${link.id}`)
    for (const group of groups) values.push(group.name || group.chat_id)
    return [...new Set(values)]
  }, [groups, links, project.role])

  return (
    <Card
      size="small"
      className="project-overview-card"
      title="项目概览"
      extra={<Text type="secondary">稳定的季度层 · 从现有关系自动汇总</Text>}
    >
      {error && <Alert type="error" showIcon title="项目概览操作失败" description={error} />}
      {loading ? <div className="project-overview-loading"><Spin /></div> : (
        <div className="project-overview-grid">
          <section className="project-overview-block">
            <Text type="secondary" className="project-overview-label">季度目标</Text>
            <RelatedOKRCard type="project" id={project.id} compact />
          </section>
          <section className="project-overview-block">
            <Flex justify="space-between" align="center" gap={8}>
              <Text type="secondary" className="project-overview-label">关键事项</Text>
              <Tag>{matters.length} 项</Tag>
            </Flex>
            {matters.length === 0 ? <EmptyOverview text="暂无关联关键事项" /> : (
              <div className="project-overview-list">
                {matters.slice(0, 4).map((matter) => (
                  <div className="project-overview-item" key={matter.id}>
                    <span className={`project-overview-dot ${matter.closed_at ? 'is-done' : ''}`} />
                    <div><Text>{matter.title}</Text><Text type="secondary">{matter.due_at ? dayjs(matter.due_at).format('M 月 D 日') : '未设置日期'}</Text></div>
                    <Tag color={matter.closed_at ? 'green' : 'gold'}>{matter.closed_at ? '已完成' : (matter.status || '进行中')}</Tag>
                  </div>
                ))}
              </div>
            )}
          </section>
          <section className="project-overview-block">
            <Flex justify="space-between" align="center" gap={8}>
              <Text type="secondary" className="project-overview-label">项目风险</Text>
              <Flex gap={6}><Tag color={risks.some((risk) => risk.triggered_at) ? 'red' : 'gold'}>{risks.length} 项</Tag><Button size="small" type="text" onClick={() => setRiskOpen(true)}>新增</Button></Flex>
            </Flex>
            {risks.length === 0 ? <EmptyOverview text="暂无开放风险" /> : (
              <div className="project-overview-list">
                {risks.slice(0, 4).map((risk) => (
                  <div className="project-overview-item" key={risk.id}>
                    <span className={`project-overview-dot ${risk.triggered_at ? '' : 'is-done'}`} />
                    <div><Button type="link" className="project-overview-title-link" onClick={() => setPageDetail({ type: 'project_risk', id: risk.id, title: risk.title })}>{risk.title}</Button><Text type="secondary">{[risk.probability, risk.impact].filter(Boolean).join(' · ') || '待评估'}</Text></div>
                    <Flex gap={4}>
                      {!risk.triggered_at && <Button size="small" type="link" onClick={() => void triggerRisk(risk)}>标记触发</Button>}
                      <Popconfirm title="关闭这条风险？" onConfirm={() => void closeRisk(risk.id)}><Button size="small" type="link">关闭</Button></Popconfirm>
                    </Flex>
                  </div>
                ))}
              </div>
            )}
          </section>
          <section className="project-overview-block">
            <Flex justify="space-between" align="center" gap={8}>
              <Text type="secondary" className="project-overview-label">项目变更</Text>
              <Flex gap={6}><Tag color="purple">{changes.length} 项</Tag><Button size="small" type="text" onClick={() => setChangeOpen(true)}>新增</Button></Flex>
            </Flex>
            {changes.length === 0 ? <EmptyOverview text="暂无项目变更" /> : (
              <div className="project-overview-list">
                {changes.slice(0, 4).map((change) => (
                  <div className="project-overview-item" key={change.id}>
                    <span className="project-overview-dot" />
                    <div><Button type="link" className="project-overview-title-link" onClick={() => setPageDetail({ type: 'project_change', id: change.id, title: change.title })}>{change.title}</Button><Text type="secondary">{dayjs(change.changed_at).format('M 月 D 日')}</Text></div>
                    <Popconfirm title="关闭这条变更记录？" onConfirm={() => void closeChange(change.id)}><Button size="small" type="link">关闭</Button></Popconfirm>
                  </div>
                ))}
              </div>
            )}
          </section>
          <section className="project-overview-block">
            <Flex justify="space-between" align="center" gap={8}>
              <Text type="secondary" className="project-overview-label">交付物</Text>
              <Tag>{resources.length} 项</Tag>
            </Flex>
            {resources.length === 0 ? <EmptyOverview text="暂无关联资源" /> : (
              <div className="project-overview-list">
                {resources.slice(0, 4).map((resource) => (
                  <div className="project-overview-item" key={resource.id}>
                    <span className="project-overview-dot is-done" />
                    <div><Text>{resource.title}</Text><Text type="secondary">{resource.summary || resource.url || resource.local_path || '已关联到项目'}</Text></div>
                    <Tag color="blue">{resource.resource_type}</Tag>
                  </div>
                ))}
              </div>
            )}
          </section>
          <section className="project-overview-block">
            <Flex justify="space-between" align="center" gap={8}>
              <Text type="secondary" className="project-overview-label">人员 / 团队</Text>
              <Tag>{peopleAndTeams.length} 个</Tag>
            </Flex>
            <Flex gap={8} wrap className="project-overview-people">
              {peopleAndTeams.map((value) => <Tag key={value}>{value}</Tag>)}
            </Flex>
          </section>
        </div>
      )}
      <Modal title="新增项目风险" open={riskOpen} confirmLoading={saving} onOk={() => void submitRisk()} onCancel={() => setRiskOpen(false)}>
        <Form form={riskForm} layout="vertical" initialValues={{ probability: '', impact: '' }}>
          <Form.Item name="title" label="风险标题" rules={[{ required: true, whitespace: true }]}><Input /></Form.Item>
          <Form.Item name="probability" label="发生概率"><Input placeholder="自由文本，例如：高 / 约 60%" /></Form.Item>
          <Form.Item name="impact" label="影响"><Input placeholder="自由文本，例如：上线延期两周" /></Form.Item>
        </Form>
      </Modal>
      <Modal title="记录项目变更" open={changeOpen} confirmLoading={saving} onOk={() => void submitChange()} onCancel={() => setChangeOpen(false)}>
        <Form form={changeForm} layout="vertical" initialValues={{ changed_at: dayjs() }}>
          <Form.Item name="title" label="变更标题" rules={[{ required: true, whitespace: true }]}><Input /></Form.Item>
          <Form.Item name="changed_at" label="生效时间" rules={[{ required: true }]}><DatePicker showTime style={{ width: '100%' }} /></Form.Item>
        </Form>
      </Modal>
      <Modal title={pageDetail?.title} open={Boolean(pageDetail)} footer={null} width={760} destroyOnHidden onCancel={() => setPageDetail(undefined)}>
        {pageDetail && <SummaryPageEditor type={pageDetail.type} id={pageDetail.id} />}
      </Modal>
    </Card>
  )
}
