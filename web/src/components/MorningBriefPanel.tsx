import { useMemo, useState } from 'react'
import { ArrowRightOutlined, ReadOutlined } from '@ant-design/icons'
import { Button, Card, Modal, Select, Skeleton, Space, Tag, Typography } from 'antd'
import dayjs from 'dayjs'
import type { MorningBrief } from '../types'
import MarkdownReport from './MarkdownReport'

const { Text, Title } = Typography

interface MorningBriefPanelProps {
  briefs: MorningBrief[] | undefined
  loading: boolean
  today: string
}

function headlineOf(content: string): string | undefined {
  const lines = content.split('\n')
  const headingIndex = lines.findIndex((line) => /^##\s+今日一句话\s*$/.test(line.trim()))
  if (headingIndex < 0) return undefined
  for (const line of lines.slice(headingIndex + 1)) {
    const trimmed = line.trim()
    if (trimmed.startsWith('#')) break
    if (!trimmed) continue
    return trimmed.replace(/^>\s*/, '').replace(/\*\*|`/g, '')
  }
  return undefined
}

function briefDateLabel(date: string): string {
  const parsed = dayjs(date)
  return parsed.isValid() ? parsed.format('M 月 D 日') : date
}

export default function MorningBriefPanel({ briefs, loading, today }: MorningBriefPanelProps) {
  const [modalOpen, setModalOpen] = useState(false)
  const [selectedDate, setSelectedDate] = useState<string>()
  const todayBrief = briefs?.find((brief) => brief.date === today)
  const latestBrief = briefs?.[0]
  const selectedBrief = useMemo(
    () => briefs?.find((brief) => brief.date === selectedDate) ?? todayBrief ?? latestBrief,
    [briefs, latestBrief, selectedDate, todayBrief],
  )
  const previewBrief = todayBrief ?? latestBrief
  const headline = previewBrief ? headlineOf(previewBrief.content) : undefined

  function openBrief(date?: string) {
    setSelectedDate(date ?? todayBrief?.date ?? latestBrief?.date)
    setModalOpen(true)
  }

  return (
    <>
      <Card className="today-panel today-morning-panel" variant="borderless">
        <div className="morning-brief-card">
          <div className="morning-brief-icon" aria-hidden="true"><ReadOutlined /></div>
          <div className="morning-brief-copy">
            <div className="morning-brief-title-row">
              <div>
                <Text className="today-eyebrow">开工计划</Text>
                <Title level={3}>晨间作战简报</Title>
              </div>
              {!loading && briefs !== undefined && (
                todayBrief ? <Tag color="success">今日已生成</Tag> : <Tag color="gold">今日尚未生成</Tag>
              )}
            </div>

            {loading && briefs === undefined ? (
              <Skeleton active paragraph={{ rows: 1 }} title={false} />
            ) : briefs === undefined ? (
              <>
                <Text className="morning-brief-lead">晨报暂时无法读取</Text>
                <Text className="morning-brief-detail">请查看上方错误信息，恢复后刷新页面。</Text>
              </>
            ) : todayBrief ? (
              <>
                <Text className="morning-brief-lead">{headline ?? '今天的行动重点、日程容量与风险已经整理完成。'}</Text>
                <Text className="morning-brief-detail">生成于 {dayjs(todayBrief.generated_at).format('HH:mm')} · 完整原文保存在晨报归档中</Text>
              </>
            ) : (
              <>
                <Text className="morning-brief-lead">今天的晨报还没有生成</Text>
                <Text className="morning-brief-detail">
                  {latestBrief
                    ? `最近一份是 ${briefDateLabel(latestBrief.date)}；生成今日晨报后，刷新页面即可查看。`
                    : '当前还没有可展示的晨报归档。'}
                </Text>
                {headline && <Text className="morning-brief-latest">最近一份：{headline}</Text>}
              </>
            )}
          </div>

          {!loading && briefs !== undefined && briefs.length > 0 && (
            <Space className="morning-brief-actions" wrap>
              <Button type={todayBrief ? 'primary' : 'default'} onClick={() => openBrief()}>
                {todayBrief ? '查看今日晨报' : '查看最近一份'} <ArrowRightOutlined />
              </Button>
              {briefs.length > 1 && (
                <Button type="text" onClick={() => openBrief(latestBrief?.date)}>历史晨报 · {briefs.length}</Button>
              )}
            </Space>
          )}
        </div>
      </Card>

      <Modal
        title={selectedBrief ? `晨间作战简报 · ${briefDateLabel(selectedBrief.date)}` : '晨间作战简报'}
        open={modalOpen}
        footer={null}
        centered
        width={1200}
        onCancel={() => setModalOpen(false)}
        className="report-detail-modal morning-brief-modal"
        destroyOnHidden
      >
        {selectedBrief && briefs && (
          <div className="morning-brief-dialog">
            <div className="morning-brief-drawer-toolbar">
              <Select
                aria-label="选择晨报日期"
                value={selectedBrief.date}
                options={briefs.map((brief) => ({ value: brief.date, label: briefDateLabel(brief.date) }))}
                onChange={setSelectedDate}
              />
              <Text type="secondary">文件更新于 {dayjs(selectedBrief.generated_at).format('YYYY-MM-DD HH:mm')}</Text>
            </div>
            <MarkdownReport className="daily-digest-markdown morning-brief-markdown" content={selectedBrief.content} />
          </div>
        )}
      </Modal>
    </>
  )
}
