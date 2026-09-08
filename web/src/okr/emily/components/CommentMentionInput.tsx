import { useEffect, useMemo, useRef, useState } from 'react'
import type { ClipboardEventHandler, KeyboardEvent, ReactNode, RefObject } from 'react'
import { useFeishuPeopleSearch } from '../../../useFeishuPeopleSearch'
import { insertCommentMention, mentionQueryAtCaret, mentionsPresentInContent } from '../mentions'
import { ownerOptions } from '../people'
import type { CommentMention, Objective, PersonSearchItem } from '../types'

export interface CommentDraft {
  content: string
  mentions: CommentMention[]
}

function personLabel(person: PersonSearchItem) {
  return [person.department, person.email].filter(Boolean).join(' · ') || '飞书用户'
}

export function CommentMentionInput({ value, objectives, placeholder, rows, autoFocus, inputRef, onPaste, onChange, onSubmitShortcut }: {
  value: CommentDraft
  objectives: Objective[]
  placeholder: string
  rows: number
  autoFocus?: boolean
  inputRef?: RefObject<HTMLTextAreaElement | null>
  onPaste?: ClipboardEventHandler<HTMLTextAreaElement>
  onChange: (value: CommentDraft) => void
  onSubmitShortcut: () => void
}) {
  const ownRef = useRef<HTMLTextAreaElement>(null)
  const textareaRef = inputRef ?? ownRef
  const [trigger, setTrigger] = useState(() => undefined as ReturnType<typeof mentionQueryAtCaret>)
  const [activeIndex, setActiveIndex] = useState(0)
  const [selectionError, setSelectionError] = useState('')
  const search = useFeishuPeopleSearch({ active: Boolean(trigger), debounceMs: 250 })
  const selected = useMemo(() => new Set(value.mentions.map((mention) => mention.openId)), [value.mentions])

  const localResults = useMemo<PersonSearchItem[]>(() => ownerOptions(objectives)
    .filter((owner) => owner.openId && !selected.has(owner.openId) && (!trigger?.query || owner.name.toLowerCase().includes(trigger.query.toLowerCase())))
    .slice(0, 6)
    .map((owner) => ({ openId: owner.openId, name: owner.name, department: '当前 OKR 负责人', email: '', isExternal: false, hasChatted: false })), [objectives, selected, trigger?.query])
  const remoteResults = useMemo<PersonSearchItem[]>(() => search.candidates
    .map((person) => ({ openId: person.open_id, name: person.name, department: person.department, email: person.email, isExternal: person.is_external, hasChatted: person.has_chatted }))
    .filter((person) => person.openId && !selected.has(person.openId)), [search.candidates, selected])
  const results = trigger?.query.trim() ? remoteResults : localResults

  useEffect(() => {
    if (!trigger) {
      search.reset()
      return
    }
    search.setQuery(trigger.query)
    setActiveIndex(0)
  }, [trigger?.query, trigger?.start]) // eslint-disable-line react-hooks/exhaustive-deps

  const refreshTrigger = (content: string, caret: number) => setTrigger(mentionQueryAtCaret(content, caret))
  const select = (person: PersonSearchItem) => {
    if (!trigger || person.isExternal) return
    try {
      const next = insertCommentMention(value.content, trigger, { openId: person.openId, name: person.name }, value.mentions)
      onChange({ content: next.content, mentions: next.mentions })
      setTrigger(undefined)
      setSelectionError('')
      window.requestAnimationFrame(() => {
        textareaRef.current?.focus()
        textareaRef.current?.setSelectionRange(next.caret, next.caret)
      })
    } catch (cause) {
      setSelectionError(cause instanceof Error ? cause.message : '无法选择该人员')
    }
  }
  const handleKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.nativeEvent.isComposing) return
    if (trigger && results.length > 0 && (event.key === 'ArrowDown' || event.key === 'ArrowUp')) {
      event.preventDefault()
      setActiveIndex((current) => (current + (event.key === 'ArrowDown' ? 1 : results.length - 1)) % results.length)
      return
    }
    if (trigger && results.length > 0 && event.key === 'Enter' && !event.metaKey && !event.ctrlKey && !event.shiftKey) {
      event.preventDefault()
      select(results[activeIndex] ?? results[0])
      return
    }
    if (trigger && event.key === 'Escape') {
      event.preventDefault()
      setTrigger(undefined)
      return
    }
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault()
      onSubmitShortcut()
    }
  }

  return (
    <div className="relative">
      <textarea
        ref={textareaRef}
        autoFocus={autoFocus}
        value={value.content}
        onChange={(event) => {
          const content = event.target.value
          onChange({ content, mentions: mentionsPresentInContent(content, value.mentions) })
          refreshTrigger(content, event.target.selectionStart)
        }}
        onClick={(event) => refreshTrigger(value.content, event.currentTarget.selectionStart)}
        onKeyUp={(event) => {
          if (!['ArrowDown', 'ArrowUp', 'Enter', 'Escape'].includes(event.key)) refreshTrigger(event.currentTarget.value, event.currentTarget.selectionStart)
        }}
        onKeyDown={handleKeyDown}
        onPaste={onPaste}
        placeholder={placeholder}
        rows={rows}
        maxLength={2000}
        className="w-full resize-none bg-transparent text-[13px] leading-5 text-slate-700 outline-none placeholder:text-slate-400"
      />
      {trigger && (
        <div className="absolute right-0 left-0 top-full z-20 mt-1 max-h-52 overflow-auto rounded-lg border border-slate-200 bg-white p-1 shadow-xl">
          {search.loading && <div className="px-2 py-2 text-[10px] text-slate-400">正在搜索飞书联系人…</div>}
          {!search.loading && results.map((person, index) => (
            <button key={person.openId} type="button" disabled={person.isExternal} onMouseDown={(event) => event.preventDefault()} onClick={() => select(person)} className={`flex w-full items-center justify-between rounded-md px-2 py-2 text-left ${index === activeIndex ? 'bg-indigo-50' : 'hover:bg-slate-50'} disabled:cursor-not-allowed disabled:opacity-50`}>
              <span className="text-[11px] font-medium text-slate-700">{person.name}{person.isExternal && <span className="ml-1 text-[9px] text-amber-600">外部，暂不可提醒</span>}</span>
              <span className="ml-3 truncate text-[9px] text-slate-400">{personLabel(person)}</span>
            </button>
          ))}
          {!search.loading && trigger.query.trim() && search.hasSearched && results.length === 0 && !search.error && <div className="px-2 py-2 text-[10px] text-slate-400">未找到可提醒的联系人</div>}
          {!search.loading && !trigger.query.trim() && results.length === 0 && <div className="px-2 py-2 text-[10px] text-slate-400">输入姓名或邮箱搜索联系人</div>}
          {search.error && <div className="px-2 py-2 text-[10px] text-red-600">{search.error}</div>}
        </div>
      )}
      {value.mentions.length > 0 && (
        <div className="mt-1 flex flex-wrap gap-1">
          {value.mentions.map((mention) => <span key={mention.openId} className="rounded-full bg-indigo-50 px-1.5 py-0.5 text-[9px] font-medium text-indigo-600">@{mention.name}</span>)}
        </div>
      )}
      {selectionError && <div className="mt-1 text-[10px] text-red-600">{selectionError}</div>}
    </div>
  )
}

export function CommentContent({ content, mentions }: { content: string; mentions: CommentMention[] }): ReactNode {
  const names = [...new Set(mentions.map((mention) => mention.name.trim()).filter(Boolean))]
  if (names.length === 0) return content
  const tokens = names.map((name) => `@${name}`).sort((left, right) => right.length - left.length)
  const parts: ReactNode[] = []
  let cursor = 0
  while (cursor < content.length) {
    let nextIndex = -1
    let nextToken = ''
    for (const token of tokens) {
      const index = content.indexOf(token, cursor)
      if (index >= 0 && (nextIndex < 0 || index < nextIndex || (index === nextIndex && token.length > nextToken.length))) {
        nextIndex = index
        nextToken = token
      }
    }
    if (nextIndex < 0) {
      parts.push(content.slice(cursor))
      break
    }
    if (nextIndex > cursor) parts.push(content.slice(cursor, nextIndex))
    parts.push(<span key={`${nextIndex}:${nextToken}`} className="font-medium text-indigo-600">{nextToken}</span>)
    cursor = nextIndex + nextToken.length
  }
  return parts
}
