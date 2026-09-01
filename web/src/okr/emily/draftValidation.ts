import type { Kr } from './types'

export interface KrDraftIssue {
  title: string
  message: string
}

export function findKrDraftIssue(kr: Kr): KrDraftIssue | undefined {
  const emptyMetrics = kr.metrics.filter((metric) => !metric.text.trim()).length
  const emptyStrategyPoints = kr.points.filter((point) => point.kind === 'strategy' && !point.title.trim()).length
  const emptyProductPoints = kr.points.filter((point) => point.kind === 'product' && !point.title.trim()).length
  if (emptyMetrics === 0 && emptyStrategyPoints === 0 && emptyProductPoints === 0) return undefined

  const missing: string[] = []
  if (emptyMetrics > 0) missing.push(`${emptyMetrics} 条核心数据`)
  if (emptyStrategyPoints > 0) missing.push(`${emptyStrategyPoints} 条策略具体 KR`)
  if (emptyProductPoints > 0) missing.push(`${emptyProductPoints} 条产品具体 KR`)

  return {
    title: emptyMetrics > 0 && emptyStrategyPoints === 0 && emptyProductPoints === 0
      ? '核心数据还没填写完整'
      : '这条 KR 还没填写完整',
    message: `“${kr.title}”中有${missing.join('、')}还是空白。请填写内容，或删除不需要的空白项。当前页面里的其他修改仍然保留，修正后会自动继续保存。`,
  }
}
