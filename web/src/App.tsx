import { useState } from 'react'
import { Button, Layout, Menu, Typography } from 'antd'
import type { MenuProps } from 'antd'
import Confirmations from './Confirmations'
import Tasks from './Tasks'
import Background from './Background'
import Overview from './Overview'
import Progress from './Progress'
import Debug from './Debug'
import Todos from './Todos'
import Chat from './Chat'
import { PageContextProvider, usePageContext } from './pageContext'

const { Sider, Content } = Layout
const { Text, Title } = Typography

const DEFAULT_KEY = 'overview'

const menuItems: MenuProps['items'] = [
  { key: 'overview', label: '总览看板' },
  { key: 'todos', label: 'Todo 线索' },
  { key: 'confirmations', label: '待确认' },
  { key: 'tasks', label: 'Task 执行' },
  { key: 'background', label: '背景设置' },
  { key: 'progress', label: '进度' },
  { key: 'debug', label: '调试' },
]

function AppShell() {
  const { context, navigate } = usePageContext()
  const [refreshKey, setRefreshKey] = useState(0)

  const pages: Record<string, React.ReactNode> = {
    overview: <Overview />,
    todos: <Todos refreshKey={refreshKey} />,
    confirmations: <Confirmations />,
    tasks: <Tasks />,
    background: <Background />,
    progress: <Progress />,
    debug: <Debug />,
  }

  return (
    <Layout className="app-shell">
      <Sider className="app-sider" width={220} theme="light">
        <div className="sider-brand">
          <Text className="eyebrow">LOCAL WORK INTELLIGENCE</Text>
          <Title level={3}>Jarvis</Title>
        </div>
        <Menu
          mode="inline"
          selectedKeys={[context.active_key]}
          items={menuItems}
          onClick={({ key }) => navigate(key)}
          className="app-menu"
        />
      </Sider>
      <Layout>
        <div className="app-main">
          <Content className="app-content">
            <div className="app-toolbar">
              <Button onClick={() => setRefreshKey((value) => value + 1)}>刷新</Button>
            </div>
            {pages[context.active_key]}
          </Content>
          <aside className="chat-dock">
            <Chat />
          </aside>
        </div>
      </Layout>
    </Layout>
  )
}

export default function App() {
  return (
    <PageContextProvider initialKey={DEFAULT_KEY}>
      <AppShell />
    </PageContextProvider>
  )
}
