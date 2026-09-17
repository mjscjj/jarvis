import assert from 'node:assert/strict'
import test from 'node:test'
import { buildFullMeetingMarkdown, buildPlanMarkdown } from '../src/okr/emily/meetingMarkdown.ts'
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

	assert.equal(output.title, '2026-W37 OKR Review')
  assert.match(output.content, /优先级：未标注 · 评分：0\.7/)
  assert.match(output.content, /##### KR1 策略 KR\n\n评分：0\.5/)
  assert.match(output.content, /\*\*本周进展\*\*/)
  assert.match(output.content, /仍在推进/)
  assert.match(output.content, /已经完成/)
  assert.doesNotMatch(output.content, /\n\*\*已完成\*\*\n/)
})

test('Preview 周里没打分的 KR 按 0 分导出', () => {
  const sample = objective('o-unscored', 'Preview 方向', '一级 KR')
  sample.krs[0].points = [{
    id: 'point-unscored',
    kind: 'strategy',
    title: '策略 KR',
    entries: [],
  }]

  const output = buildFullMeetingMarkdown([sample], '2026-Q3', '2026-W37', 'okr_weekly_preview_v1')

  assert.match(output.content, /优先级：未标注 · 评分：0\.0/)
  assert.match(output.content, /##### KR1 策略 KR\n\n评分：0\.0/)
  assert.doesNotMatch(output.content, /未评分/)
})

test('Plan 按业务方向导出五列表格并合并相同 O、KR 和优先级', () => {
  const output = buildPlanMarkdown({
    title: '2026 Q4 Biz OKR Plan',
    quarter: '2026-Q4',
    objectives: [{
      id: 'o-1',
      title: '提升 A & B',
      owners: [{ name: 'O 负责人', email: 'o@example.test' }],
      krs: [{
        id: 'kr-1',
        title: '增长 KR <一>',
        ownerName: 'KR 负责人',
        metricNote: '不应导出的口径',
        metrics: [{ id: 'metric-1', text: '不应导出的指标' }],
        tags: [{ type: 'business_category', value: '增长' }, { type: 'priority', value: 'p0' }],
        points: [
          { id: 'strategy-1', kind: 'strategy', title: '策略一', entries: [] },
          { id: 'strategy-2', kind: 'strategy', title: '策略二', entries: [] },
          { id: 'product-1', kind: 'product', title: '产品一', entries: [] },
        ],
      }, {
        id: 'kr-2',
        title: '增长 KR 二',
        metricNote: '',
        metrics: [],
        tags: [{ type: 'business_category', value: '增长' }, { type: 'priority', value: 'p2' }],
        points: [{ id: 'product-2', kind: 'product', title: '产品二', entries: [] }],
      }, {
        id: 'kr-3',
        title: 'AI KR',
        metricNote: '',
        metrics: [],
        tags: [{ type: 'business_category', value: 'AI提效' }],
        points: [],
      }],
    }],
  })

  assert.equal(output.title, '2026 Q4 Biz OKR Plan')
  assert.match(output.content, /## 增长/)
  assert.match(output.content, /## AI提效/)
  assert.equal((output.content.match(/<table>/g) ?? []).length, 2)
  assert.match(output.content, /<th background-color="light-gray"><p>O<\/p><\/th><th background-color="light-gray"><p>KR<\/p><\/th><th background-color="light-gray"><p>优先级<\/p><\/th><th background-color="light-gray"><p>策略具体KR<\/p><\/th><th background-color="light-gray"><p>产品具体KR<\/p><\/th>/)
  assert.match(output.content, /<td rowspan="3" vertical-align="top"><p>提升 A &amp; B<\/p><\/td>/)
  assert.match(output.content, /<td rowspan="2" vertical-align="top"><p>增长 KR &lt;一&gt;<\/p><\/td>/)
  assert.match(output.content, /<td rowspan="2" vertical-align="top"><p>Focus item<\/p><\/td>/)
  assert.match(output.content, /<td vertical-align="top"><p>P2<\/p><\/td>/)
  assert.doesNotMatch(output.content, /O 负责人|KR 负责人|不应导出的口径|不应导出的指标/)
})

test('Plan 没有业务方向的 KR 进入未标注业务表格', () => {
  const output = buildPlanMarkdown({
    title: '未分类 Plan',
    quarter: '2026-Q4',
    objectives: [objective('o-plain', '未分类 O', '未分类 KR')],
  })

  assert.match(output.content, /## 未标注业务/)
  assert.match(output.content, /<td vertical-align="top"><p>未分类 O<\/p><\/td>/)
  assert.match(output.content, /<td vertical-align="top"><p>未分类 KR<\/p><\/td>/)
})

test('Plan 中还没有 KR 的 O 仍保留在未标注业务表格', () => {
  const output = buildPlanMarkdown({
    title: '空 O Plan',
    quarter: '2026-Q4',
    objectives: [{ id: 'o-empty', title: '等待拆解的 O', krs: [] }],
  })

  assert.match(output.content, /## 未标注业务/)
  assert.match(output.content, /<tr><td vertical-align="top"><p>等待拆解的 O<\/p><\/td><td vertical-align="top"><p><\/p><\/td><td vertical-align="top"><p><\/p><\/td><td vertical-align="top"><p><\/p><\/td><td vertical-align="top"><p><\/p><\/td><\/tr>/)
})

test('Plan 未标注业务表不包含其他业务方向的空投影', () => {
  const categorized = objective('categorized', '已分类目标', '增长 KR')
  categorized.krs[0].tags = [{ type: 'business_category', value: '增长' }]
  const output = buildPlanMarkdown({
    title: '混合分类', quarter: '2026-Q4',
    objectives: [categorized, objective('plain', '未分类目标', '未分类 KR'), { id: 'empty', title: '待拆解目标', krs: [] }],
  })
  const untagged = output.content.split('## 未标注业务\n')[1]
  assert.ok(untagged)
  assert.match(untagged, /未分类目标/)
  assert.match(untagged, /待拆解目标/)
  assert.doesNotMatch(untagged, /已分类目标|增长 KR/)
})
