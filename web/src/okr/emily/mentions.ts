import type { CommentMention } from './types'

export interface MentionQuery {
  start: number
  end: number
  query: string
}

export function mentionQueryAtCaret(content: string, caret: number): MentionQuery | undefined {
  const prefix = content.slice(0, Math.max(0, caret))
  const start = prefix.lastIndexOf('@')
  if (start < 0) return undefined
  const query = prefix.slice(start + 1)
  if (/\s|@/.test(query)) return undefined
  return { start, end: caret, query }
}

export function mentionsPresentInContent(content: string, mentions: CommentMention[]) {
  return mentions.filter((mention) => content.includes(`@${mention.name}`))
}

export function insertCommentMention(
  content: string,
  trigger: MentionQuery,
  mention: CommentMention,
  current: CommentMention[],
) {
  const token = `@${mention.name}`
  const needsSpace = trigger.end >= content.length || !/^\s/.test(content.slice(trigger.end))
  const nextContent = `${content.slice(0, trigger.start)}${token}${needsSpace ? ' ' : ''}${content.slice(trigger.end)}`
  const sameName = current.find((item) => item.name === mention.name && item.email !== mention.email)
  if (sameName) throw new Error(`已有同名 @${mention.name}，请先移除后再选择`)
  const nextMentions = current.some((item) => item.email === mention.email) ? current : [...current, mention]
  return {
    content: nextContent,
    mentions: nextMentions,
    caret: trigger.start + token.length + (needsSpace ? 1 : 0),
  }
}
