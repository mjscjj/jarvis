import { Tooltip } from 'antd'
import type { CSSProperties } from 'react'
import jarvisIcon from '../assets/jarvis-icon.png'
import type { ExecutingTaskState } from '../hooks/useExecutingTaskCount'
import '../styles/agent-activity-icon.css'

const activateHint = '点击查看版本与更新'
const particles = Array.from({ length: 18 }, (_, index) => {
  const angle = index * Math.PI / 9
  const radius = index % 3 === 0 ? 45 : 39
  return { x: 50 + Math.cos(angle) * radius, y: 50 + Math.sin(angle) * radius, size: index % 3 === 0 ? 1.8 : 1.1 }
})

export function AgentActivityIcon({ name, enabled, count, error, onActivate }: ExecutingTaskState & { name: string; enabled: boolean; onActivate: () => void }) {
  const running = enabled && !error && count !== undefined && count > 0
  const status = !enabled ? 'inactive' : error ? 'error' : count === undefined ? 'loading' : running ? 'running' : 'idle'
  const label = !enabled ? '' : error
    ? '任务状态读取失败'
    : count === undefined ? '正在读取任务状态'
      : running ? `正在执行 ${count} 个任务` : '暂无执行中的任务'

  return (
    <Tooltip title={enabled ? `${error ? `${label}：${error}` : label} · ${activateHint}` : activateHint}>
      <button type="button" className="agent-activity-icon" data-state={status} aria-label={enabled ? `${name}：${label}，${activateHint}` : `${name}：${activateHint}`} onClick={onActivate}>
        <img className="sider-brand-icon" src={jarvisIcon} alt="" />
        <svg className="agent-activity-particles" viewBox="0 0 100 100" fill="none" aria-hidden="true">
          <g className="agent-activity-swarm">
            <circle cx="50" cy="50" r="42" stroke="#60dcff" strokeWidth=".6" strokeDasharray="16 28 3 35" opacity=".7" />
            {particles.map((particle, index) => (
              <g className="agent-activity-spark" key={index} style={{ '--spark-delay': `${-index * .23}s` } as CSSProperties}>
                <circle cx={particle.x} cy={particle.y} r={particle.size * 2.8} fill={index % 3 === 0 ? '#a58bff' : '#26cfff'} opacity=".2" />
                <circle cx={particle.x} cy={particle.y} r={particle.size} fill={index % 3 === 0 ? '#c3b6ff' : '#c7f9ff'} />
              </g>
            ))}
          </g>
          <g className="agent-activity-swarm agent-activity-swarm-reverse">
            <path d="M 50 4 A 46 46 0 0 1 82.5 17.5" stroke="#aa8dff" strokeWidth="1.2" strokeLinecap="round" />
            <circle cx="50" cy="4" r="2" fill="#eee5ff" />
            <path d="M 50 96 A 46 46 0 0 1 17.5 82.5" stroke="#3cdcff" strokeWidth="1.2" strokeLinecap="round" />
            <circle cx="50" cy="96" r="2" fill="#e1fcff" />
          </g>
        </svg>
        {enabled && error && <span className="agent-activity-error" aria-hidden="true">!</span>}
      </button>
    </Tooltip>
  )
}
