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
