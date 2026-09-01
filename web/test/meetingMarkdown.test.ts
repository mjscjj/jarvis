import assert from 'node:assert/strict'
import test from 'node:test'
import { buildFullMeetingMarkdown } from '../src/okr/emily/meetingMarkdown.ts'
import type { Objective } from '../src/okr/emily/types.ts'

function objective(id: string, title: string, krTitle: string): Objective {
  return {
    id,
    title,
    krs: [{
      id: `${id}-kr`,
      title: krTitle,
      metricNote: '',
      metrics: [],
      points: [],
    }],
  }
}

test('周报会议导出包含当前季度的全部 OKR 方向', () => {
  const output = buildFullMeetingMarkdown([
    objective('o-1', '方向一', 'KR 一'),
    objective('o-2', '方向二', 'KR 二'),
  ], '2026-Q3', '2026-W36')

  assert.equal(output.title, '2026-W36 OKR 周报会议')
  assert.match(output.content, /## 方向一/)
  assert.match(output.content, /### KR 一/)
  assert.match(output.content, /## 方向二/)
  assert.match(output.content, /### KR 二/)
})

test('Preview 周导出评分并把全部状态合并为本周进展', () => {
  const sample = objective('o-preview', 'Preview 方向', '一级 KR')
  sample.krs[0].score = { value: 0.7, version: 1 }
  sample.krs[0].points = [{
    id: 'point-preview',
    kind: 'strategy',
    title: '策略 KR',
    score: { value: 0.5, version: 0 },
    entries: [
      { id: 'doing', status: 'in_progress', text: '仍在推进', docs: [], images: [] },
      { id: 'done', status: 'done', text: '已经完成', docs: [], images: [] },
    ],
  }]

  const output = buildFullMeetingMarkdown([sample], '2026-Q3', '2026-W37', 'okr_weekly_preview_v1')

  assert.match(output.content, /优先级：未标注 · 评分：0\.7/)
  assert.match(output.content, /##### KR1 策略 KR\n\n评分：0\.5/)
  assert.match(output.content, /\*\*本周进展\*\*/)
  assert.match(output.content, /仍在推进/)
  assert.match(output.content, /已经完成/)
  assert.doesNotMatch(output.content, /\n\*\*已完成\*\*\n/)
})
