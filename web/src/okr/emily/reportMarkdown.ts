import type { ReportDraft } from './types'

export interface ReportMarkdownExport {
  filename: string
  content: string
}

const inlineMarkdown = /([-\\`*_[\]{}()<>#+.!|])/g

function escapeInline(value: string): string {
  return value.trim().replace(inlineMarkdown, '\\$1')
}

function quoteDetail(value: string): string {
  const lines = value.trim().split(/\r?\n/)
  return (lines.length === 1 && lines[0] === '' ? ['暂无补充'] : lines)
    .map((line) => `> ${line.replaceAll('\\', '\\\\').replace(/^([#>*+-]|\d+\.)\s/, '\\$1 ')}`)
    .join('\n')
}

function safeFilenamePart(value: string): string {
  return value.trim().toLowerCase().replace(/[^a-z0-9_-]+/g, '-').replace(/^-+|-+$/g, '') || 'report'
}

/** Pure local projection: it performs no API, database, or publication write. */
export function buildReportDraftMarkdown(draft: ReportDraft): ReportMarkdownExport {
  const lines = [
    `# ${escapeInline(draft.title) || '未命名汇报草稿'}`,
    '',
    `> ${escapeInline(draft.quarter)} · ${escapeInline(draft.week)} · 未发布草稿`,
    '',
  ]
  const footnotes: string[] = []
  let footnoteIndex = 0

  for (const section of draft.sections) {
    lines.push(`## ${escapeInline(section.title)}`, '')
    if (section.items.length === 0) {
      lines.push('_暂无内容_', '')
      continue
    }
    for (const item of section.items) {
      footnoteIndex += 1
      const ref = `来源-${footnoteIndex}`
      lines.push(`### ${escapeInline(item.pointTitle)}`, '', quoteDetail(item.detail), '', `来源：[^${ref}]`, '')
      footnotes.push(
        `[^${ref}]: O：${escapeInline(item.objectiveTitle)}；KR：${escapeInline(item.krTitle)}；事项：${escapeInline(item.pointTitle)}；负责人：${escapeInline(item.ownerName) || '未填写'}；周次：${escapeInline(item.week)}；数据来源：${escapeInline(item.source) || '未知'}`,
      )
    }
  }

  if (footnotes.length > 0) lines.push('---', '', '## 来源脚注', '', ...footnotes, '')
  const version = draft.saved ? `v${draft.version}` : 'preview'
  return {
    filename: `emily-${safeFilenamePart(draft.reportType)}-${safeFilenamePart(draft.quarter)}-${safeFilenamePart(draft.week)}-${version}.md`,
    content: `${lines.join('\n').trimEnd()}\n`,
  }
}
