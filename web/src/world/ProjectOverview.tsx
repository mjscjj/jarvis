import { useEffect, useMemo, useState } from 'react'
import { Alert, Card, Empty, Flex, Spin, Tag, Typography } from 'antd'
import dayjs from 'dayjs'
import { getPage, listGroups, listKeyMatters, listProjectResources } from '../api'
import type { Group, KeyMatter, PageLink, Project, Resource } from '../types'
import RelatedOKRCard from './RelatedOKRCard'

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
  const [links, setLinks] = useState<PageLink[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string>()

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError(undefined)
    Promise.all([
      listKeyMatters(1, 100, true, controller.signal),
      listProjectResources(project.id, controller.signal),
      listGroups({ page: 1, pageSize: 100, relatedOnly: true, keyword: project.name }, controller.signal),
      getPage('project', project.id, controller.signal),
    ]).then(([matterResult, resourceResult, groupResult, page]) => {
      if (controller.signal.aborted) return
      setMatters(matterResult.items.filter((item) => item.project_id === project.id))
      setResources(resourceResult.items)
      setGroups(groupResult.items.filter((item) => item.project_id === project.id))
      setLinks(uniqueLinks([...page.outgoing, ...page.backlinks]).filter((item) => item.type === 'person' || item.type === 'group'))
    }).catch((cause: unknown) => {
      if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError(errorText(cause))
    }).finally(() => {
      if (!controller.signal.aborted) setLoading(false)
    })
    return () => controller.abort()
  }, [project.id])

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
      {error && <Alert type="error" showIcon title="项目概览读取失败" description={error} />}
      {loading ? <div className="project-overview-loading"><Spin /></div> : (
        <div className="project-overview-grid">
          <section className="project-overview-block">
            <Text type="secondary" className="project-overview-label">季度目标</Text>
            <RelatedOKRCard type="project" id={project.id} compact />
          </section>
          <section className="project-overview-block">
            <Flex justify="space-between" align="center" gap={8}>
              <Text type="secondary" className="project-overview-label">里程碑</Text>
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
    </Card>
  )
}
