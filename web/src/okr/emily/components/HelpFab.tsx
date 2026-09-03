import { useEffect, useRef, useState } from 'react'

// 填写页遇到问题时找谁。open_id 取自 Emily pro 小组的真实成员，换人要改这里。
const FEISHU_CHAT = 'https://applink.larkoffice.com/client/chat/open?openId='

const CONTACTS = [
  {
    openId: 'ou_4f9796d856ffa67c1b32206f6e81e5bf',
    title: 'OKR 填写规范',
    detail: '填写口径、字段含义',
    person: '张若怡 (Penny)',
  },
  {
    openId: 'ou_cfd9e106436c46adf20aaf9fe076c65d',
    title: '技术支持',
    detail: '页面报错、功能问题',
    person: '储节节 (Jiejie Chu)',
  },
]

const DESIGNER = { openId: 'ou_0a60892810f8dc26f1a5156b352198c9', name: '李潇琳 (Claire Li)' }

/**
 * 右下角的求助入口，叠在 Jarvis 对话悬浮球上方。
 *
 * 面板向上展开：按钮本身贴着视口底部，向下没有空间。
 */
export function HelpFab() {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    function onDown(event: MouseEvent) {
      if (!ref.current?.contains(event.target as Node)) setOpen(false)
    }
    function onKey(event: KeyboardEvent) {
      if (event.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  return (
    <div ref={ref} className="okr-help-fab">
      {open && (
        <div role="dialog" aria-label="帮助与反馈" className="absolute right-0 bottom-[calc(100%+10px)] w-64 overflow-hidden rounded-xl border border-slate-200 bg-white shadow-[0_12px_32px_rgba(15,23,42,0.16)]">
          {CONTACTS.map((contact) => (
            <a
              key={contact.openId}
              href={`${FEISHU_CHAT}${contact.openId}`}
              target="_blank"
              rel="noreferrer"
              className="block border-b border-slate-100 px-3 py-2.5 transition-colors hover:bg-slate-50"
            >
              <div className="text-[12px] font-semibold text-slate-800">{contact.title}</div>
              <div className="mt-0.5 text-[10px] leading-4 text-slate-400">{contact.detail}</div>
              <div className="mt-1 text-[10px] font-medium text-blue-600">→ {contact.person}</div>
            </a>
          ))}
          <a
            href={`${FEISHU_CHAT}${DESIGNER.openId}`}
            target="_blank"
            rel="noreferrer"
            className="block bg-slate-50/70 px-3 py-2 text-[10px] text-slate-400 transition-colors hover:text-slate-600"
          >
            Design by {DESIGNER.name}
          </a>
        </div>
      )}
      <button
        type="button"
        onClick={() => setOpen((value) => !value)}
        aria-expanded={open}
        aria-label={open ? '收起帮助' : '帮助与反馈'}
        title="帮助与反馈"
        className={`flex size-9 items-center justify-center rounded-full text-[16px] font-bold text-white shadow-[0_4px_14px_rgba(37,99,235,0.45)] transition-colors ${open ? 'bg-blue-700' : 'bg-blue-600 hover:bg-blue-700'}`}
      >
        ?
      </button>
    </div>
  )
}
