import { useState } from 'react'
import type { ReactNode } from 'react'
import {
  LayoutDashboard,
  CheckSquare,
  PlayCircle,
  Settings,
  BarChart3,
  Bug,
  MessageSquare,
  Search,
  RefreshCw,
} from 'lucide-react'
import { useLocalStorage } from '../../hooks/useLocalStorage'

interface NavItem {
  key: string
  label: string
  icon: ReactNode
}

const navItems: NavItem[] = [
  { key: 'overview', label: 'Overview', icon: <LayoutDashboard size={18} /> },
  { key: 'todos', label: '待办', icon: <CheckSquare size={18} /> },
  { key: 'tasks', label: '任务', icon: <PlayCircle size={18} /> },
  { key: 'background', label: '背景', icon: <Settings size={18} /> },
]

const systemItems: NavItem[] = [
  { key: 'progress', label: '进度', icon: <BarChart3 size={18} /> },
  { key: 'debug', label: '调试', icon: <Bug size={18} /> },
]

interface AppShellProps {
  activeKey: string
  onNavigate: (key: string) => void
  onRefresh: () => void
  children: ReactNode
  chat: ReactNode
}

export default function AppShell({ activeKey, onNavigate, onRefresh, children, chat }: AppShellProps) {
  const [chatOpen, setChatOpen] = useLocalStorage('jarvis.chatOpen', false)
  const [systemOpen, setSystemOpen] = useState(false)

  const isSystemActive = systemItems.some((item) => item.key === activeKey)

  return (
    <div className="app-shell-v2">
      {/* Sidebar */}
      <aside className="sidebar">
        <div className="sidebar-brand">
          <div className="brand-title">Jarvis</div>
          <div className="brand-subtitle">Local Work Intelligence</div>
        </div>

        <nav className="sidebar-nav">
          {navItems.map((item) => (
            <button
              key={item.key}
              className={`nav-item ${activeKey === item.key ? 'active' : ''}`}
              onClick={() => onNavigate(item.key)}
            >
              <span className="nav-icon">{item.icon}</span>
              <span className="nav-label">{item.label}</span>
            </button>
          ))}

          <div className="nav-divider" />

          <button
            className={`nav-item nav-system ${isSystemActive || systemOpen ? 'active' : ''}`}
            onClick={() => setSystemOpen(!systemOpen)}
          >
            <span className="nav-icon"><Bug size={18} /></span>
            <span className="nav-label">系统</span>
            <span className="nav-chevron">{systemOpen ? '▾' : '▸'}</span>
          </button>

          {systemOpen && (
            <div className="nav-sub">
              {systemItems.map((item) => (
                <button
                  key={item.key}
                  className={`nav-item nav-item-sub ${activeKey === item.key ? 'active' : ''}`}
                  onClick={() => onNavigate(item.key)}
                >
                  <span className="nav-icon">{item.icon}</span>
                  <span className="nav-label">{item.label}</span>
                </button>
              ))}
            </div>
          )}
        </nav>

        <div className="sidebar-footer">
          <button className="nav-item" onClick={onRefresh}>
            <span className="nav-icon"><RefreshCw size={18} /></span>
            <span className="nav-label">刷新</span>
          </button>
        </div>
      </aside>

      {/* Main */}
      <main className="main-content">
        {children}
      </main>

      {/* Chat */}
      <button
        className="chat-fab"
        onClick={() => setChatOpen(!chatOpen)}
        aria-label={chatOpen ? '收起对话' : '打开对话'}
      >
        <MessageSquare size={22} />
      </button>
      <div className={`chat-panel-v2 ${chatOpen ? 'open' : ''}`}>
        {chat}
      </div>
    </div>
  )
}
