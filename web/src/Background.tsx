import { lazy, Suspense } from 'react'
import { Space, Spin, Tabs } from 'antd'
import { useAgentIdentity } from './agentIdentity'
import AboutSettings from './AboutSettings'
import SharedMemory from './SharedMemory'
import AppModules from './AppModules'
import RuntimeSettings from './RuntimeSettings'
import SystemTasks from './SystemTasks'
import PageHeader from './components/PageHeader'
import FactsPanel from './world/FactsPanel'
import { usePageContext } from './pageContext'
import GroupsPanel from './background/GroupsPanel'
import KeyMattersPanel from './background/KeyMattersPanel'
import PersonsPanel from './background/PersonsPanel'
import ProfilePanel from './background/ProfilePanel'
import ProjectsPanel from './background/ProjectsPanel'
import ResourcePanel from './background/ResourcePanel'
import SkillsPanel from './background/SkillsPanel'
import './styles/review-memory.css'

const WorldMap = lazy(() => import('./world-map/WorldMap'))

type MemoryView = 'world-map' | 'projects' | 'persons' | 'groups' | 'resources' | 'key-matters' | 'facts' | 'profile'

function memoryView(value: string | undefined): MemoryView {
  return value === 'world-map' || value === 'projects' || value === 'persons' || value === 'groups' || value === 'resources' || value === 'key-matters' || value === 'facts' || value === 'profile'
    ? value
    : 'profile'
}

export default function Background() {
  const { name: agentName } = useAgentIdentity()
  const { context, setViewState } = usePageContext()

  return (
    <div className="memory-page">
      <PageHeader title="世界" subtitle={`浏览 ${agentName} 用来理解你、项目和协作关系的长期背景`} />
      <Tabs
        activeKey={memoryView(context.view_state.view)}
        onChange={(view) => setViewState({ view })}
        destroyOnHidden
        items={[
          {
            key: 'world-map',
            label: '世界地图',
            children: (
              <Suspense fallback={<div style={{ padding: 72, textAlign: 'center' }}><Spin size="large" /></div>}>
                <WorldMap />
              </Suspense>
            ),
          },
          { key: 'profile', label: '我的资料', children: <div className="memory-profile-view"><ProfilePanel /></div> },
          { key: 'projects', label: '项目', children: <ProjectsPanel /> },
          { key: 'persons', label: '人物', children: <PersonsPanel /> },
          { key: 'groups', label: '会话', children: <GroupsPanel /> },
          { key: 'resources', label: '资源', children: <ResourcePanel /> },
          { key: 'key-matters', label: '关键事项', children: <KeyMattersPanel /> },
          { key: 'facts', label: '全部事实', children: <FactsPanel /> },
        ]}
      />
    </div>
  )
}

type SettingsView = 'runtime' | 'scheduling' | 'memory' | 'extensions' | 'about'

function settingsView(value: string | undefined): SettingsView {
  return value === 'runtime' || value === 'scheduling' || value === 'memory' || value === 'extensions' || value === 'about'
    ? value
    : 'runtime'
}

export function Settings() {
  const { name: agentName } = useAgentIdentity()
  const { context, setViewState } = usePageContext()

  return (
    <div className="settings-page">
      <PageHeader title="系统设置" subtitle={`配置 ${agentName} 的运行、调度、共享记忆和扩展能力，查看应用版本`} />
      <Tabs
        activeKey={settingsView(context.view_state.view)}
        onChange={(view) => setViewState({ view })}
        items={[
          { key: 'runtime', label: '运行', children: <RuntimeSettings /> },
          { key: 'scheduling', label: '调度', children: <SystemTasks /> },
          { key: 'memory', label: '共享记忆', children: <SharedMemory /> },
          {
            key: 'extensions',
            label: '扩展',
            children: (
              <Space orientation="vertical" size={24} style={{ width: '100%' }}>
                <AppModules
                  moduleKeys={['biz-okr']}
                  title="业务应用"
                  description="OKR 提供业务打标、Plan、Review、周报与自动化。"
                />
                <SkillsPanel />
              </Space>
            ),
          },
          { key: 'about', label: '关于', children: <AboutSettings /> },
        ]}
      />
    </div>
  )
}
