import { Drawer } from 'antd'
import { useMemo, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import weeklyFill from '../help/weekly-fill.md?raw'
import reviewFill from '../help/review-fill.md?raw'
import plan from '../help/plan.md?raw'
import comments from '../help/comments.md?raw'
import meetings from '../help/meetings.md?raw'
import faq from '../help/faq.md?raw'
import pages from '../help/pages.md?raw'

export type UsageGuideScene = 'weekly-fill' | 'review-fill' | 'plan' | 'comments' | 'meetings'
type GuideSection = 'scenarios' | 'faq' | 'pages'

const SCENES: Array<{ key: UsageGuideScene; label: string; content: string }> = [
  { key: 'weekly-fill', label: '填写本周周报', content: weeklyFill },
  { key: 'review-fill', label: '填写 OKR Review', content: reviewFill },
  { key: 'plan', label: '制定下季度 OKR', content: plan },
  { key: 'comments', label: '查看与我相关的评论', content: comments },
  { key: 'meetings', label: '主持会议', content: meetings },
]

function Markdown({ children }: { children: string }) {
  return <ReactMarkdown
    remarkPlugins={[remarkGfm]}
    skipHtml
    components={{
      a: ({ node: _node, ...props }) => <a {...props} target="_blank" rel="noopener noreferrer" />,
    }}
  >{children}</ReactMarkdown>
}

function SupportLinks() {
  return <div className="okr-guide-support">
    <strong>仍然没有解决？</strong>
    <span>填写口径联系张若怡（Penny），页面报错或功能问题联系储节节（Jiejie）。</span>
  </div>
}

export function UsageGuideButton({ initialScene, className = 'h-8 text-[10px]' }: { initialScene: UsageGuideScene; className?: string }) {
  const [open, setOpen] = useState(false)
  const [section, setSection] = useState<GuideSection>('scenarios')
  const [scene, setScene] = useState<UsageGuideScene>(initialScene)
  const current = useMemo(() => SCENES.find((item) => item.key === scene) ?? SCENES[0], [scene])

  const show = () => {
    setSection('scenarios')
    setScene(initialScene)
    setOpen(true)
  }

  return <>
    <button type="button" onClick={show} className={`okr-usage-guide-trigger ${className}`}>使用说明</button>
    <Drawer
      open={open}
      onClose={() => setOpen(false)}
      placement="right"
      width="min(760px, 100vw)"
      title={<div><div className="text-[16px] font-semibold text-slate-900">Emily 使用说明</div><div className="mt-0.5 text-[11px] font-normal text-slate-400">按日常场景完成填写、评审和会议协作</div></div>}
      styles={{ body: { padding: 0 } }}
    >
      <div className="okr-guide-layout">
        <nav aria-label="使用说明目录" className="okr-guide-nav">
          <button type="button" className={section === 'scenarios' ? 'is-active' : ''} onClick={() => setSection('scenarios')}>使用场景</button>
          {section === 'scenarios' && <div className="okr-guide-scenes">{SCENES.map((item) => <button type="button" key={item.key} className={scene === item.key ? 'is-active' : ''} onClick={() => setScene(item.key)}>{item.label}</button>)}</div>}
          <button type="button" className={section === 'faq' ? 'is-active' : ''} onClick={() => setSection('faq')}>高频问题</button>
          <button type="button" className={section === 'pages' ? 'is-active' : ''} onClick={() => setSection('pages')}>页面介绍</button>
        </nav>
        <main className="okr-guide-content">
          <div className="okr-guide-markdown"><Markdown>{section === 'scenarios' ? current.content : section === 'faq' ? faq : pages}</Markdown></div>
          <SupportLinks />
        </main>
      </div>
    </Drawer>
  </>
}
