import { useEffect, useMemo, useRef, useState } from 'react'
import { Alert, Badge, Button, Card, DatePicker, Empty, Space, Spin, Tabs, Typography } from 'antd'
import {
  CalendarOutlined,
  CodeOutlined,
  FileTextOutlined,
  ReadOutlined,
  TeamOutlined,
  VideoCameraOutlined,
} from '@ant-design/icons'
import dayjs from 'dayjs'
import type { Dayjs } from 'dayjs'
import { generateDailyDigest, getDailyDigests, getMorningBriefs, getProfile, listGroups } from './api'
import DigestCard from './review/DigestCard'
import MeetingSummaryView from './review/MeetingSummaryView'
import MorningBriefCard from './review/MorningBriefCard'
import { CodeView, DocumentsView } from './review/WorklogViews'
import { usePageContext } from './pageContext'
import { isReviewDateStateFresh, reviewDateStateExpiresAt } from './reviewDateState'
import type { DailyDigest, DailyDigestScope, Group, MorningBrief, ProfileView } from './types'
import './styles/review-memory.css'

const DAILY_DIGEST_POLL_MS = 5000
const DAILY_DIGEST_RETRY_MS = 10000
const RECENT_DATE_TABS = 14
const { Text, Title } = Typography

type ReviewView = 'daily' | 'morning' | 'meetings' | 'groups' | 'docs' | 'code'

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

function reviewView(value: string | undefined): ReviewView {
  return value === 'daily' || value === 'meetings' || value === 'groups' || value === 'docs' || value === 'code' ? value : 'morning'
}

function dateTabLabel(date: string) {
  const value = dayjs(date)
  const weekday = ['周日', '周一', '周二', '周三', '周四', '周五', '周六'][value.day()]
  return (
    <span className="review-date-tab-label">
      <span>{value.isSame(dayjs(), 'day') ? '今天' : weekday}</span>
      <small>{value.format('M月D日')}</small>
    </span>
  )
}

function digestBadge(item?: DailyDigest) {
  if (item?.status === 'done') return 'success'
  if (item?.status === 'failed') return 'error'
  if (item?.status === 'generating') return 'processing'
  return 'default'
}

export default function Progress() {
  const { context, setViewState } = usePageContext()
  const [activeView, setActiveView] = useState<ReviewView>(() => reviewView(context.view_state.view))
  const [selectedDate, setSelectedDate] = useState<Dayjs>(() => {
    const routeDate = dayjs(context.view_state.date)
    return routeDate.isValid() && isReviewDateStateFresh(context.view_state.date_selected_at) ? routeDate : dayjs()
  })
  const [profile, setProfile] = useState<ProfileView>()
  const [profileError, setProfileError] = useState<string>()
  const [keyGroups, setKeyGroups] = useState<Group[]>([])
  const [groupsLoading, setGroupsLoading] = useState(false)
  const [groupsError, setGroupsError] = useState<string>()
  const [dailyItems, setDailyItems] = useState<DailyDigest[]>([])
  const [dailyLoading, setDailyLoading] = useState(false)
  const [dailyError, setDailyError] = useState<string>()
  const [morningItems, setMorningItems] = useState<MorningBrief[]>([])
  const [morningLoading, setMorningLoading] = useState(false)
  const [morningError, setMorningError] = useState<string>()
  const [generating, setGenerating] = useState<Set<string>>(new Set())
  const [dailyRefresh, setDailyRefresh] = useState(0)
  const generatingDatesRef = useRef<Set<string>>(new Set())

  const date = selectedDate.format('YYYY-MM-DD')
  const activeDateRef = useRef(date)
  activeDateRef.current = date

  useEffect(() => {
    setActiveView(reviewView(context.view_state.view))
  }, [context.view_state.view])

  useEffect(() => {
    const now = Date.now()
    const routeDate = dayjs(context.view_state.date)
    const selectedAt = context.view_state.date_selected_at
    if (!routeDate.isValid() || !isReviewDateStateFresh(selectedAt, now)) {
      const today = dayjs()
      setSelectedDate(today)
      if (context.view_state.date || selectedAt) {
        setViewState({ view: reviewView(context.view_state.view), date: today.format('YYYY-MM-DD') })
      }
      return
    }

    setSelectedDate(routeDate)
    const expiresAt = reviewDateStateExpiresAt(selectedAt)
    if (expiresAt === undefined) return
    const timer = window.setTimeout(() => {
      const today = dayjs()
      setSelectedDate(today)
      setViewState({ view: reviewView(context.view_state.view), date: today.format('YYYY-MM-DD') })
    }, expiresAt - now)
    return () => window.clearTimeout(timer)
  }, [context.view_state.date, context.view_state.date_selected_at, context.view_state.view, setViewState])

  useEffect(() => {
    getProfile()
      .then((result) => { setProfile(result); setProfileError(undefined) })
      .catch((cause: unknown) => setProfileError(errorText(cause)))
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    const load = async () => {
      setGroupsLoading(true)
      setGroupsError(undefined)
      try {
        const first = await listGroups({ page: 1, pageSize: 100, relatedOnly: false, keyOnly: true }, controller.signal)
        const items = [...first.items]
        for (let page = 2; items.length < first.total; page += 1) {
          const next = await listGroups({ page, pageSize: 100, relatedOnly: false, keyOnly: true }, controller.signal)
          items.push(...next.items)
        }
        if (!controller.signal.aborted) setKeyGroups(items)
      } catch (cause) {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setGroupsError(errorText(cause))
      } finally {
        if (!controller.signal.aborted) setGroupsLoading(false)
      }
    }
    void load()
    return () => controller.abort()
  }, [])

  useEffect(() => {
    if (activeView !== 'daily' && activeView !== 'groups') return
    const controller = new AbortController()
    let timer: number | undefined
    const scheduleLoad = (delay: number) => {
      timer = window.setTimeout(() => void load(false), delay)
    }
    const load = async (initial: boolean) => {
      if (initial) {
        setDailyLoading(true)
        setDailyItems([])
      }
      try {
        const result = await getDailyDigests(date, controller.signal)
        if (controller.signal.aborted) return
        setDailyItems(result.items)
        setDailyError(undefined)
        if (result.items.some((item) => item.status === 'generating')) {
          generatingDatesRef.current.add(date)
          scheduleLoad(DAILY_DIGEST_POLL_MS)
        } else {
          generatingDatesRef.current.delete(date)
        }
      } catch (cause) {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) {
          const retrying = generatingDatesRef.current.has(date)
          const detail = errorText(cause)
          setDailyError(retrying ? `${detail}；生成仍在进行，将在约 10 秒后重试` : detail)
          if (retrying) scheduleLoad(DAILY_DIGEST_RETRY_MS)
        }
      } finally {
        if (!controller.signal.aborted && initial) setDailyLoading(false)
      }
    }
    void load(true)
    return () => {
      controller.abort()
      if (timer !== undefined) window.clearTimeout(timer)
    }
  }, [activeView, date, dailyRefresh])

  useEffect(() => {
    if (activeView !== 'morning') return
    const controller = new AbortController()
    setMorningLoading(true)
    setMorningError(undefined)
    getMorningBriefs(RECENT_DATE_TABS, controller.signal)
      .then((result) => setMorningItems(result.items))
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setMorningError(errorText(cause))
      })
      .finally(() => {
        if (!controller.signal.aborted) setMorningLoading(false)
      })
    return () => controller.abort()
  }, [activeView])

  const selectView = (view: ReviewView) => {
    setActiveView(view)
    const freshDateState = isReviewDateStateFresh(context.view_state.date_selected_at)
    const next: Record<string, string> = {
      view,
      date: freshDateState ? date : dayjs().format('YYYY-MM-DD'),
      date_selected_at: freshDateState ? context.view_state.date_selected_at : String(Date.now()),
    }
    if (view === 'meetings' && context.view_state.meeting_id) next.meeting_id = context.view_state.meeting_id
    if (view === 'groups' && context.view_state.group_id) next.group_id = context.view_state.group_id
    setViewState(next)
  }

  const selectDate = (nextDate: Dayjs) => {
    setSelectedDate(nextDate)
    const next: Record<string, string> = {
      view: activeView,
      date: nextDate.format('YYYY-MM-DD'),
      date_selected_at: String(Date.now()),
    }
    if (activeView === 'groups' && context.view_state.group_id) next.group_id = context.view_state.group_id
    setViewState(next)
  }

  const selectMeeting = (meetingID: string) => {
    const freshDateState = isReviewDateStateFresh(context.view_state.date_selected_at)
    setViewState({
      view: 'meetings',
      date: freshDateState ? date : dayjs().format('YYYY-MM-DD'),
      date_selected_at: freshDateState ? context.view_state.date_selected_at : String(Date.now()),
      meeting_id: meetingID,
    })
  }

  const selectGroup = (groupID: string) => {
    const freshDateState = isReviewDateStateFresh(context.view_state.date_selected_at)
    setViewState({
      view: 'groups',
      date: freshDateState ? date : dayjs().format('YYYY-MM-DD'),
      date_selected_at: freshDateState ? context.view_state.date_selected_at : String(Date.now()),
      group_id: groupID,
    })
  }

  const digestFor = (scope: DailyDigestScope, scopeID: string) =>
    dailyItems.find((item) => item.scope === scope && item.scope_id === scopeID && item.digest_date === date)

  const generate = async (scope: DailyDigestScope, scopeID: string) => {
    const requestDate = date
    const key = `${requestDate}:${scope}:${scopeID}`
    setGenerating((current) => new Set(current).add(key))
    setDailyError(undefined)
    try {
      await generateDailyDigest(scope, scopeID, requestDate)
      generatingDatesRef.current.add(requestDate)
      if (activeDateRef.current === requestDate) {
        setDailyItems((current) => {
          const existing = current.find((item) => item.scope === scope && item.scope_id === scopeID)
          if (existing) {
            return current.map((item) => item === existing ? {
              ...item, status: 'generating', trigger_type: 'manual', started_at: new Date().toISOString(),
              cutoff_at: null, error_detail: null,
            } : item)
          }
          return [...current, {
            id: 0, scope, scope_id: scopeID, digest_date: requestDate, summary: '', status: 'generating',
            trigger_type: 'manual', source_count: 0, source_coverage: {}, engine: 'codex', error_detail: null,
            started_at: new Date().toISOString(), cutoff_at: null, generated_at: null, updated_at: new Date().toISOString(),
          }]
        })
        setDailyRefresh((value) => value + 1)
      }
    } catch (cause) {
      if (activeDateRef.current === requestDate) setDailyError(errorText(cause))
    } finally {
      setGenerating((current) => {
        const next = new Set(current)
        next.delete(key)
        return next
      })
    }
  }

  const recentDates = useMemo(() => {
    const values = Array.from({ length: RECENT_DATE_TABS }, (_, index) => dayjs().subtract(index, 'day').format('YYYY-MM-DD'))
    if (!values.includes(date)) values.push(date)
    return values.sort((left, right) => right.localeCompare(left))
  }, [date])

  const personalDigest = profile ? digestFor('person', profile.open_id) : undefined
  const personalGenerating = profile
    ? personalDigest?.status === 'generating' || generating.has(`${date}:person:${profile.open_id}`)
    : false
  const dailyContent = (
    <Space orientation="vertical" size={12} style={{ width: '100%' }}>
      {profileError && <Alert type="error" showIcon title="个人信息加载失败" description={profileError} />}
      {dailyError && <Alert type="error" showIcon title="每日总结加载失败" description={dailyError} />}
      <Spin spinning={dailyLoading}>
        {profile ? (
          <DigestCard
            title="我的工作回顾"
            item={personalDigest}
            generating={personalGenerating}
            primaryAction
            onGenerate={() => generate('person', profile.open_id)}
          />
        ) : <Card className="review-content-card" variant="borderless"><Spin size="small" /></Card>}
      </Spin>
    </Space>
  )
  const dailyTabs = recentDates.map((value) => ({
    key: value,
    label: dateTabLabel(value),
    children: value === date ? dailyContent : null,
  }))
  const morningBrief = morningItems.find((item) => item.date === date)
  const morningContent = (
    <Space orientation="vertical" size={12} style={{ width: '100%' }}>
      {morningError && <Alert type="error" showIcon title="晨报加载失败" description={morningError} />}
      <Spin spinning={morningLoading}>
        <MorningBriefCard item={morningBrief} />
      </Spin>
    </Space>
  )
  const morningTabs = recentDates.map((value) => ({
    key: value,
    label: dateTabLabel(value),
    children: value === date ? morningContent : null,
  }))

  const activeGroupID = keyGroups.some((group) => String(group.id) === context.view_state.group_id)
    ? context.view_state.group_id
    : keyGroups[0] ? String(keyGroups[0].id) : undefined
  const groupTabs = keyGroups.map((group) => {
    const scopeID = String(group.id)
    const item = digestFor('group', scopeID)
    const isGenerating = item?.status === 'generating' || generating.has(`${date}:group:${scopeID}`)
    return {
      key: scopeID,
      label: <Badge status={digestBadge(item)} text={group.name || '未命名会话'} />,
      children: scopeID === activeGroupID ? (
        <DigestCard
          title={group.name || '未命名会话'}
          item={item}
          generating={isGenerating}
          onGenerate={() => generate('group', scopeID)}
        />
      ) : null,
    }
  })
  const groupsContent = groupsError ? (
    <Alert type="error" showIcon title="关键群加载失败" description={groupsError} />
  ) : groupsLoading ? (
    <div className="review-loading"><Spin /></div>
  ) : keyGroups.length === 0 ? (
    <Card className="review-content-card" variant="borderless"><div className="review-state-panel"><Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无关键群" /></div></Card>
  ) : (
    <Space orientation="vertical" size={12} style={{ width: '100%' }}>
      {dailyError && <Alert type="error" showIcon title="群总结加载失败" description={dailyError} />}
      <Spin spinning={dailyLoading}>
        <Tabs
          className="review-secondary-tabs"
          activeKey={activeGroupID}
          onChange={selectGroup}
          items={groupTabs}
          tabBarGutter={8}
        />
      </Spin>
    </Space>
  )

  const topLevelTabs = [
    {
      key: 'morning', label: <span className="review-primary-tab-label"><ReadOutlined />晨报</span>,
      children: <Tabs className="review-secondary-tabs" activeKey={date} onChange={(value) => selectDate(dayjs(value))} items={morningTabs} tabBarGutter={8} />,
    },
    {
      key: 'daily', label: <span className="review-primary-tab-label"><ReadOutlined />每日总结</span>,
      children: <Tabs className="review-secondary-tabs" activeKey={date} onChange={(value) => selectDate(dayjs(value))} items={dailyTabs} tabBarGutter={8} />,
    },
    {
      key: 'meetings', label: <span className="review-primary-tab-label"><VideoCameraOutlined />会议总结</span>,
      children: <MeetingSummaryView date={date} selectedMeetingID={context.view_state.meeting_id} onSelectMeeting={selectMeeting} />,
    },
    { key: 'groups', label: <span className="review-primary-tab-label"><TeamOutlined />群总结</span>, children: groupsContent },
    { key: 'docs', label: <span className="review-primary-tab-label"><FileTextOutlined />文档</span>, children: <DocumentsView date={selectedDate} /> },
    { key: 'code', label: <span className="review-primary-tab-label"><CodeOutlined />代码</span>, children: <CodeView date={selectedDate} /> },
  ]

  return (
    <div className="progress review-page">
      <header className="review-hero">
        <div className="review-hero-copy">
          <Text className="review-eyebrow">WORK REVIEW</Text>
          <Title level={1}>回顾</Title>
        </div>
        <div className="review-date-control">
          <span className="review-date-icon"><CalendarOutlined /></span>
          <div className="review-date-picker-wrap">
            <Text>当前日期</Text>
            <DatePicker
              value={selectedDate}
              onChange={(value) => selectDate(value ?? dayjs())}
              allowClear={false}
              variant="borderless"
              format="YYYY年M月D日"
              disabledDate={(value) => value.isAfter(dayjs(), 'day')}
            />
          </div>
          <Button type="text" onClick={() => selectDate(dayjs())} disabled={selectedDate.isSame(dayjs(), 'day')}>回到今天</Button>
        </div>
      </header>

      <Tabs
        className="review-primary-tabs"
        activeKey={activeView}
        onChange={(value) => selectView(value as ReviewView)}
        items={topLevelTabs}
        tabBarGutter={12}
      />
    </div>
  )
}
