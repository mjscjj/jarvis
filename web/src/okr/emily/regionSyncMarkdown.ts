import type { RegionSyncDraft } from './types'

function cell(value: string): string {
  return value.trim().replaceAll('\\', '\\\\').replaceAll('|', '\\|').replace(/\r?\n/g, '<br>') || '—'
}

function filenamePart(value: string): string {
  return value.trim().toLowerCase().replace(/[^a-z0-9_-]+/g, '-').replace(/^-+|-+$/g, '') || 'region'
}

export function buildRegionSyncMarkdown(draft: RegionSyncDraft): { filename: string; content: string } {
  const lines = [
    `# ${cell(draft.region)} 区域需求进度同步表`,
    '',
    `> ${cell(draft.quarter)} · ${cell(draft.week)} · 未发布草稿`,
    '',
    '| KR | 负责人 | 事项 | 状态 | 本周进展 | 来源 |',
    '| --- | --- | --- | --- | --- | --- |',
  ]
  if (draft.rows.length === 0) {
    lines.push('| — | — | — | — | 当前区域暂无内容 | — |')
  } else {
    for (const row of draft.rows) {
      lines.push(`| ${cell(row.krTitle)} | ${cell(row.ownerName)} | ${cell(row.pointTitle)} | ${cell(row.status)} | ${cell(row.detail)} | ${cell(`${row.objectiveTitle} / ${row.krId} / ${row.pointId} / ${row.source}`)} |`)
    }
  }
  return {
    filename: `emily-region-${filenamePart(draft.region)}-${filenamePart(draft.week)}.md`,
    content: `${lines.join('\n')}\n`,
  }
}
